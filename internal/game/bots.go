package game

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"babibingo/internal/models"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// BotManager manages bot players
type BotManager struct {
	engine        *Engine
	bots          []*Bot
	mu            sync.RWMutex
	isRunning     bool
	stopChan      chan bool
	desiredCount  int // ✅ Desired number of bots per game
}

// Bot represents a simulated player
type Bot struct {
	User       *models.User
	CardNumber int
}

// NewBotManager creates a new bot manager
func NewBotManager(engine *Engine) *BotManager {
	return &BotManager{
		engine:       engine,
		bots:         make([]*Bot, 0),
		stopChan:     make(chan bool),
		desiredCount: 20, // ✅ Default: 20 bots per game
	}
}

// ✅ SetDesiredCount sets the desired number of bots per game
func (bm *BotManager) SetDesiredCount(count int) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if count < 0 {
		count = 0
	}
	if count > 100 {
		count = 100
	}
	bm.desiredCount = count
	log.Printf("🤖 Desired bot count set to: %d", count)
}

// ✅ GetDesiredCount returns the desired number of bots per game
func (bm *BotManager) GetDesiredCount() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.desiredCount
}

// ✅ Get or create bot users
func (bm *BotManager) getOrCreateBotUsers(count int) ([]*models.User, error) {
	var botUsers []*models.User

	// ✅ First, try to find existing bots that are not currently in a game
	// We'll use a simple approach: find bots that haven't been used recently
	var existingBots []models.User
	err := bm.engine.db.
		Where("is_bot = ?", true).
		Where("last_active < ? OR last_active IS NULL", time.Now().Add(-1*time.Hour)).
		Limit(count).
		Find(&existingBots).Error

	if err != nil {
		return nil, err
	}

	// ✅ If we have enough existing bots, use them
	if len(existingBots) >= count {
		for i := 0; i < count; i++ {
			botUsers = append(botUsers, &existingBots[i])
		}
		log.Printf("🤖 Using %d existing bot users", count)
		return botUsers, nil
	}

	// ✅ If we don't have enough, create new ones
	needed := count - len(existingBots)
	log.Printf("🤖 Need %d more bots, creating new ones", needed)

	// Add existing bots first
	for i := range existingBots {
		botUsers = append(botUsers, &existingBots[i])
	}

	// Get existing IDs and referral codes to avoid conflicts
	existingIDs, existingCodes := bm.getExistingUserData()

	// Create new bot users
	for i := 0; i < needed; i++ {
		telegramID := generateUniqueTelegramID(existingIDs)
		name := generateRandomName()
		phone := generateRandomPhone()
		referralCode := generateReferralCode(existingCodes)

		user := &models.User{
			TelegramID:   telegramID,
			FirstName:    name,
			LastName:     "",
			PhoneNumber:  phone,
			Balance:      1000.0,
			ReferralCode: referralCode,
			IsBot:        true, // ✅ Mark as bot
			CreatedAt:    time.Now(),
			LastActive:   time.Now(),
		}

		if err := bm.engine.db.Create(user).Error; err != nil {
			log.Printf("⚠️ Failed to create bot user: %v", err)
			continue
		}

		botUsers = append(botUsers, user)
		log.Printf("🤖 Created new bot user: %s (%s)", user.FirstName, user.PhoneNumber)
	}

	return botUsers, nil
}

// ✅ ReserveCardsForBots - Updated to respect desired count
func (bm *BotManager) ReserveCardsForBots(count int) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	engine := bm.engine
	if engine.currentGame == nil {
		log.Println("⚠️ No active game for bots to join")
		return
	}

	state := engine.currentGame
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.Game.Status != GameStatusWaiting {
		log.Println("⚠️ Game already started, bots cannot join")
		return
	}

	// Get available cards
	availableCards := bm.getAvailableCards(state)
	if len(availableCards) == 0 {
		log.Println("⚠️ No available cards for bots")
		return
	}

	// Limit bots to available cards and max players
	if count > len(availableCards) {
		count = len(availableCards)
	}

	// Don't exceed max players
	currentPlayers := len(state.UserCards)
	if currentPlayers+count > MaxPlayers {
		count = MaxPlayers - currentPlayers
	}

	// ✅ Don't exceed desired count
	desiredCount := bm.GetDesiredCount()
	if currentPlayers+count > desiredCount {
		count = desiredCount - currentPlayers
	}

	if count <= 0 {
		return
	}

	// ✅ Get or create bot users
	botUsers, err := bm.getOrCreateBotUsers(count)
	if err != nil {
		log.Printf("⚠️ Failed to get bot users: %v", err)
		return
	}

	log.Printf("🤖 Reserving cards for %d bots...", len(botUsers))

	botsReserved := 0
	for i, user := range botUsers {
		if i >= len(availableCards) {
			break
		}

		// Get a random available card
		cardIndex := rand.Intn(len(availableCards))
		cardNumber := availableCards[cardIndex]
		availableCards = append(availableCards[:cardIndex], availableCards[cardIndex+1:]...)

		// Reserve the card for the bot
		if err := bm.reserveCardForBot(state, user, cardNumber); err != nil {
			log.Printf("⚠️ Failed to reserve card %d for bot %s: %v", cardNumber, user.FirstName, err)
			continue
		}

		// Update last active time
		user.LastActive = time.Now()
		bm.engine.db.Save(user)

		bot := &Bot{
			User:       user,
			CardNumber: cardNumber,
		}
		bm.bots = append(bm.bots, bot)
		botsReserved++

		log.Printf("🤖 Bot '%s' (%s) reserved card #%d", user.FirstName, user.PhoneNumber, cardNumber)

		// Small delay between bot actions
		time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
	}

	log.Printf("✅ %d bots successfully reserved cards", botsReserved)
}

// reserveCardForBot reserves a card for a bot
func (bm *BotManager) reserveCardForBot(state *GameState, user *models.User, cardNumber int) error {
	// Check if card is already reserved
	if _, ok := state.ReservedCards[cardNumber]; ok {
		return fmt.Errorf("card already reserved")
	}

	// Reserve in memory
	state.ReservedCards[cardNumber] = user.TelegramID
	state.UserCards[user.TelegramID] = append(state.UserCards[user.TelegramID], cardNumber)
	bm.engine.UpdatePool(state)

	// Get card data
	cardData, found := GetCardByID(cardNumber)
	if !found {
		// Rollback
		delete(state.ReservedCards, cardNumber)
		userCards := state.UserCards[user.TelegramID]
		for i, num := range userCards {
			if num == cardNumber {
				state.UserCards[user.TelegramID] = append(userCards[:i], userCards[i+1:]...)
				break
			}
		}
		bm.engine.UpdatePool(state)
		return fmt.Errorf("card not found")
	}

	// Create card record
	card := models.Card{
		ID:            uuid.New(),
		GameID:        state.Game.ID,
		UserID:        user.ID,
		CardNumber:    cardNumber,
		CardData:      cardData,
		MarkedNumbers: pq.Int64Array{},
		IsWinner:      false,
		Status:        "reserved",
	}

	if err := bm.engine.db.Create(&card).Error; err != nil {
		// Rollback
		delete(state.ReservedCards, cardNumber)
		userCards := state.UserCards[user.TelegramID]
		for i, num := range userCards {
			if num == cardNumber {
				state.UserCards[user.TelegramID] = append(userCards[:i], userCards[i+1:]...)
				break
			}
		}
		bm.engine.UpdatePool(state)
		return err
	}

	// Broadcast the reservation event
	grossPool := state.Game.TotalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	bm.engine.broadcast(GameEvent{
		Type:       "card.reserved",
		GameID:     state.Game.ID.String(),
		CardNumber: cardNumber,
		UserID:     user.TelegramID,
		Card:       &card,
		Players:    len(state.UserCards),
		Pool:       netPool,
		GrossPool:  grossPool,
		HouseCut:   houseCut,
		Stake:      StakeAmount,
		Message:    fmt.Sprintf("Card #%d reserved", cardNumber),
	})

	return nil
}

// getAvailableCards returns available card numbers
func (bm *BotManager) getAvailableCards(state *GameState) []int {
	available := make([]int, 0, 400)
	for i := 1; i <= 400; i++ {
		if _, ok := state.ReservedCards[i]; !ok {
			available = append(available, i)
		}
	}
	return available
}

// getExistingUserData gets existing user IDs and referral codes
func (bm *BotManager) getExistingUserData() (map[int64]bool, map[string]bool) {
	var users []models.User
	bm.engine.db.Find(&users)

	existingIDs := make(map[int64]bool)
	existingCodes := make(map[string]bool)

	for _, user := range users {
		existingIDs[user.TelegramID] = true
		if user.ReferralCode != "" {
			existingCodes[user.ReferralCode] = true
		}
	}

	return existingIDs, existingCodes
}

// StartBotRoutine starts the bot reservation routine
func (bm *BotManager) StartBotRoutine() {
	bm.mu.Lock()
	if bm.isRunning {
		bm.mu.Unlock()
		return
	}
	bm.isRunning = true
	bm.stopChan = make(chan bool)
	bm.mu.Unlock()

	log.Println("🤖 Bot manager started")

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-bm.stopChan:
				log.Println("🤖 Bot manager stopped")
				return
			case <-ticker.C:
				bm.checkAndReserveBots()
			}
		}
	}()
}

// StopBotRoutine stops the bot reservation routine
func (bm *BotManager) StopBotRoutine() {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	if bm.isRunning {
		bm.isRunning = false
		select {
		case bm.stopChan <- true:
		default:
		}
	}
}

// ✅ checkAndReserveBots - Updated to use desired count
func (bm *BotManager) checkAndReserveBots() {
	engine := bm.engine
	if engine.currentGame == nil {
		return
	}

	state := engine.currentGame
	state.mu.RLock()
	if state.Game.Status != GameStatusWaiting {
		state.mu.RUnlock()
		return
	}

	currentPlayers := len(state.UserCards)
	availableCount := 400 - len(state.ReservedCards)
	desiredCount := bm.GetDesiredCount()
	state.mu.RUnlock()

	// ✅ Don't add bots if we already have enough
	if currentPlayers >= desiredCount {
		return
	}

	// Don't add bots if there aren't enough available cards
	if availableCount < 2 {
		return
	}

	// ✅ Calculate how many bots to add to reach desired count
	needed := desiredCount - currentPlayers
	if needed > availableCount {
		needed = availableCount
	}
	
	// ✅ Add 1-3 bots at a time (slowly reach target)
	if needed > 3 {
		needed = rand.Intn(3) + 1 // 1-3 bots
	}

	// Add bots with some randomness (30% chance per tick)
	if rand.Float32() < 0.3 {
		bm.ReserveCardsForBots(needed)
	}
}

// ✅ GetBotStats - Updated to include desired count
func (bm *BotManager) GetBotStats() map[string]interface{} {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return map[string]interface{}{
		"total_bots":    len(bm.bots),
		"is_running":    bm.isRunning,
		"desired_count": bm.desiredCount,
	}
}

// generateRandomName generates a random Ethiopian name
func generateRandomName() string {
	firstNames := []string{
		"Abebe", "Almaz", "Biruk", "Chala", "Dawit",
		"Eden", "Fiker", "Gizaw", "Hana", "Israel",
		"Kidist", "Lemma", "Meron", "Nebiyu", "Oli",
		"Rediet", "Sami", "Tigist", "Uriel", "Yonas",
		"Zewdu", "Amanuel", "Bereket", "Chaltu", "Daniel",
	}
	lastNames := []string{
		"Alemayehu", "Bekele", "Chala", "Demeke", "Eshetu",
		"Girma", "Haile", "Kebede", "Lemma", "Mekonnen",
		"Negash", "Tadesse", "Wolde", "Yilma", "Zelalem",
	}

	firstName := firstNames[rand.Intn(len(firstNames))]
	lastName := lastNames[rand.Intn(len(lastNames))]
	return firstName + " " + lastName
}

// generateRandomPhone generates a random Ethiopian phone number
func generateRandomPhone() string {
	prefixes := []string{"091", "092", "093", "094", "095", "096", "097", "098", "099"}
	prefix := prefixes[rand.Intn(len(prefixes))]
	number := fmt.Sprintf("%07d", rand.Intn(10000000))
	return prefix + number
}

// generateUniqueTelegramID generates a unique Telegram ID for bots
func generateUniqueTelegramID(existingIDs map[int64]bool) int64 {
	var id int64
	for {
		id = 1000000000 + rand.Int63n(1000000000)
		if !existingIDs[id] {
			existingIDs[id] = true
			return id
		}
	}
}

// generateReferralCode generates a unique referral code
func generateReferralCode(existingCodes map[string]bool) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for {
		b := make([]byte, 8)
		for i := range b {
			b[i] = letters[rand.Intn(len(letters))]
		}
		code := string(b)
		if !existingCodes[code] {
			existingCodes[code] = true
			return code
		}
	}
}