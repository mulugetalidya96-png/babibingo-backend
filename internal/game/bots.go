package game

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"babibingo/internal/models"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// BotManager manages bot players
type BotManager struct {
	engine       *Engine
	bots         []*Bot
	mu           sync.RWMutex
	isRunning    bool
	stopChan     chan bool
	desiredCount int32
}

// Bot represents a simulated player
type Bot struct {
	User       *models.User
	CardNumber int
	GameID     string
}

// NewBotManager creates a new bot manager
func NewBotManager(engine *Engine) *BotManager {
	bm := &BotManager{
		engine:       engine,
		bots:         make([]*Bot, 0),
		stopChan:     make(chan bool),
		desiredCount: 20,
	}

	// Load saved desired count from database
	bm.loadDesiredCount()

	return bm
}

// loadDesiredCount - Load from database
func (bm *BotManager) loadDesiredCount() {
	var settings models.RobotBotSettings
	err := bm.engine.db.First(&settings).Error
	if err == nil {
		atomic.StoreInt32(&bm.desiredCount, int32(settings.DesiredCount))
		log.Printf("🤖 Loaded desired bot count from DB: %d", settings.DesiredCount)
	} else {
		if err == gorm.ErrRecordNotFound {
			// Create default settings
			settings = models.RobotBotSettings{
				DesiredCount: 20,
				UpdatedAt:    time.Now(),
			}
			if createErr := bm.engine.db.Create(&settings).Error; createErr != nil {
				log.Printf("⚠️ Failed to create default bot settings: %v", createErr)
			} else {
				log.Println("🤖 Created default bot settings in DB")
			}
		} else {
			log.Printf("⚠️ Failed to load bot settings: %v", err)
		}
	}
}

// saveDesiredCount - Save to databas
func (bm *BotManager) saveDesiredCount() {
	count := int(atomic.LoadInt32(&bm.desiredCount))

	var settings models.RobotBotSettings
	if err := bm.engine.db.First(&settings).Error; err != nil {
		log.Printf("⚠️ Failed to load bot settings: %v", err)
		return
	}

	settings.DesiredCount = count
	settings.UpdatedAt = time.Now()

	if err := bm.engine.db.Save(&settings).Error; err != nil {
		log.Printf("⚠️ Failed to save desired bot count: %v", err)
	}
}

// SetDesiredCount sets the desired number of bots per game (ATOMIC + PERSISTENT)
func (bm *BotManager) SetDesiredCount(count int) {
	if count < 0 {
		count = 0
	}
	if count > 200 {
		count = 200
	}
	atomic.StoreInt32(&bm.desiredCount, int32(count))

	// Save to database
	bm.saveDesiredCount()

	log.Printf("🤖 Desired bot count set to: %d", count)
}

// GetDesiredCount returns the desired number of bots per game (ATOMIC - NO MUTEX)
func (bm *BotManager) GetDesiredCount() int {
	if bm == nil {
		return 20
	}
	return int(atomic.LoadInt32(&bm.desiredCount))
}
// ActiveBotReservations returns the number of bot cards
// reserved in the current game.
func (bm *BotManager) ActiveBotReservations() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if bm.engine == nil || bm.engine.currentGame == nil {
		return 0
	}

	currentGameID := bm.engine.currentGame.Game.ID.String()

	count := 0
	for _, bot := range bm.bots {
		if bot.GameID == currentGameID {
			count++
		}
	}

	return count
}

// getOrCreateBotUsers gets or creates bot users
func (bm *BotManager) getOrCreateBotUsers(count int) ([]*models.User, error) {
	var botUsers []*models.User

	// First, try to find existing bots that are not currently in a game
	var existingBots []models.User
	err := bm.engine.db.
    Where("is_bot = ?", true).
    Limit(count).
    Find(&existingBots).Error

	if err != nil {
		return nil, err
	}

	// If we have enough existing bots, use them
	if len(existingBots) >= count {
		for i := 0; i < count; i++ {
			botUsers = append(botUsers, &existingBots[i])
		}
		log.Printf("🤖 Using %d existing bot users", count)
		return botUsers, nil
	}

	// If we don't have enough, create new ones
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
			AgentBalance: 0,
			ReferralCode: referralCode,
			IsBot:        true,
			IsAgent:      false,
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

// ReserveCardsForBots - Updated to respect desired count
// ReserveCardsForBots - Step 3
func (bm *BotManager) ReserveCardsForBots(count int) {

	engine := bm.engine

	if engine.currentGame == nil {
		log.Println("⚠️ No active game for bots to join")
		return
	}

	state := engine.currentGame


	// Read initial state
	state.mu.RLock()

	if state.Game.Status != GameStatusWaiting {
		state.mu.RUnlock()
		log.Println("⚠️ Game already started, bots cannot join")
		return
	}

	availableCards := bm.getAvailableCards(state)

	currentPlayers := len(state.UserCards)

	state.mu.RUnlock()



	if len(availableCards) == 0 {
		log.Println("⚠️ No available cards for bots")
		return
	}


	if count > len(availableCards) {
		count = len(availableCards)
	}


	if currentPlayers+count > MaxPlayers {
		count = MaxPlayers - currentPlayers
	}


	if count <= 0 {
		return
	}



	botUsers, err := bm.getOrCreateBotUsers(count)

	if err != nil {
		log.Printf(
			"⚠️ Failed creating bot users: %v",
			err,
		)
		return
	}



	log.Printf(
		"🤖 Reserving cards for %d bots...",
		len(botUsers),
	)



	botsReserved := 0



	for i, user := range botUsers {


		if i >= len(availableCards) {
			break
		}



		cardIndex := rand.Intn(len(availableCards))

		cardNumber := availableCards[cardIndex]


		availableCards = append(
			availableCards[:cardIndex],
			availableCards[cardIndex+1:]...,
		)



		var card *models.Card



		// ONLY LOCK MEMORY
		state.mu.Lock()


		if state.Game.Status != GameStatusWaiting {

			state.mu.Unlock()
			break
		}



		card, err = bm.reserveCardState(
			state,
			user,
			cardNumber,
		)


		state.mu.Unlock()



		if err != nil {

			log.Printf(
				"⚠️ Failed reserving card %d: %v",
				cardNumber,
				err,
			)

			continue
		}



		// ==============================
		// NO STATE LOCK BELOW THIS POINT
		// ==============================



		// Save card
		if err := bm.engine.db.Create(card).Error; err != nil {

			log.Printf(
				"⚠️ Failed saving card %d: %v",
				cardNumber,
				err,
			)


			// rollback memory reservation

			state.mu.Lock()


			delete(
				state.ReservedCards,
				cardNumber,
			)


			userCards := state.UserCards[user.TelegramID]


			for index, num := range userCards {

				if num == cardNumber {

					state.UserCards[user.TelegramID] =
						append(
							userCards[:index],
							userCards[index+1:]...,
						)

					break
				}
			}


			state.mu.Unlock()


			continue
		}




		// Update pool AFTER unlock

		state.mu.Lock()

		bm.engine.UpdatePool(state)

		grossPool := state.Game.TotalPool

		players := len(state.UserCards)

		state.mu.Unlock()



		netPool, houseCut := GetPoolBreakdown(grossPool)



		// Broadcast AFTER unlock

		bm.engine.broadcast(
			GameEvent{
				Type:       "card.reserved",
				GameID:     state.Game.ID.String(),
				CardNumber: cardNumber,
				UserID:     user.TelegramID,
				Card:       card,
				Players:    players,
				Pool:       netPool,
				GrossPool:  grossPool,
				HouseCut:   houseCut,
				Stake:      StakeAmount,
				Message:    fmt.Sprintf(
					"Card #%d reserved",
					cardNumber,
				),
			},
		)




		user.LastActive = time.Now()

		bm.engine.db.Save(user)




		// Protect only slice append

		bm.mu.Lock()

		bm.bots = append(
			bm.bots,
			&Bot{
				User:       user,
				CardNumber: cardNumber,
				GameID:     state.Game.ID.String(),
			},
		)

		bm.mu.Unlock()



		botsReserved++



		log.Printf(
			"🤖 Bot '%s' reserved card #%d",
			user.FirstName,
			cardNumber,
		)



		time.Sleep(
			time.Duration(
				50+rand.Intn(100),
			) * time.Millisecond,
		)
	}



	log.Printf(
		"✅ %d bots successfully reserved cards",
		botsReserved,
	)
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
func (bm *BotManager) reserveCardState(
	state *GameState,
	user *models.User,
	cardNumber int,
) (*models.Card, error) {

	// Check if card is already reserved
	if _, ok := state.ReservedCards[cardNumber]; ok {
		return nil, fmt.Errorf("card already reserved")
	}

	// Get card data
	cardData, found := GetCardByID(cardNumber)
	if !found {
		return nil, fmt.Errorf("card not found")
	}


	// ONLY MEMORY CHANGES

	state.ReservedCards[cardNumber] = user.TelegramID

	state.UserCards[user.TelegramID] =
		append(
			state.UserCards[user.TelegramID],
			cardNumber,
		)


	card := &models.Card{
		ID:            uuid.New(),
		GameID:        state.Game.ID,
		UserID:        user.ID,
		CardNumber:    cardNumber,
		CardData:      cardData,
		MarkedNumbers: pq.Int64Array{},
		IsWinner:      false,
		Status:        "reserved",
	}


	return card, nil
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

// checkAndReserveBots - Updated to use desired count
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

	availableCards := 400 - len(state.ReservedCards)
	state.mu.RUnlock()

	if availableCards == 0 {
		return
	}

	desiredBots := bm.GetDesiredCount()
	currentBots := bm.ActiveBotReservations()

	if currentBots >= desiredBots {
		return
	}

	needed := desiredBots - currentBots

	if needed > availableCards {
		needed = availableCards
	}

	// Reserve up to 10 bots per tick.
	const batchSize = 10
	if needed > batchSize {
		needed = batchSize
	}

	bm.ReserveCardsForBots(needed)
}
// GetBotStats - Updated to include desired count
func (bm *BotManager) GetBotStats() map[string]interface{} {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	// Count active bots in games
	activeBots := 0
	for _, bot := range bm.bots {
		if bot.GameID != "" {
			activeBots++
		}
	}

	return map[string]interface{}{
		"total_bots":     len(bm.bots),
		"active_bots":    activeBots,
		"is_running":     bm.isRunning,
		"desired_count":  int(atomic.LoadInt32(&bm.desiredCount)),
		"available_bots": len(bm.bots) - activeBots,
	}
}

// DeleteAllBotUsers deletes all bot users from the database
func (bm *BotManager) DeleteAllBotUsers() error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Get count before deletion
	var count int64
	bm.engine.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&count)

	// Delete all bot users
	result := bm.engine.db.Where("is_bot = ?", true).Delete(&models.User{})
	if result.Error != nil {
		return result.Error
	}

	// Clear bots from memory
	bm.bots = make([]*Bot, 0)
	log.Printf("🤖 Deleted %d bot users from database", result.RowsAffected)
	return nil
}

// generateRandomName generates a random Ethiopian name (200+ unique combinations)
func generateRandomName() string {
	firstNames := []string{
		"Abebe", "Almaz", "Biruk", "Chala", "Dawit",
		"Eden", "Fiker", "Gizaw", "Hana", "Israel",
		"Kidist", "Lemma", "Meron", "Nebiyu", "Oli",
		"Rediet", "Sami", "Tigist", "Uriel", "Yonas",
		"Zewdu", "Amanuel", "Bereket", "Chaltu", "Daniel",
		"Elsabet", "Fikru", "Genet", "Henok", "Ibrahim",
		"Jerusalem", "Kalkidan", "Lidya", "Mekdes", "Natnael",
		"Omar", "Ruth", "Selam", "Tewodros", "Yared",
		"Zebib", "Abel", "Bethel", "Chernet", "Dagim",
		"Ermias", "Frehiwot", "Gebre", "Hirut", "Isayas",
		"Jember", "Kifle", "Lulseged", "Mamo", "Negash",
		"Rahel", "Semere", "Tigabu", "Wondimu", "Yohannes",
		"Abdi", "Binyam", "Demissie", "Endale", "Fisseha",
		"Getachew", "Habtamu", "Jemal", "Kassahun", "Leta",
		"Mulugeta", "Netsanet", "Samuel", "Tesfaye", "Wubishet",
		"Ayelech", "Berhan", "Chimdessa", "Debebe", "Esubalew",
		"Fetlework", "Gudeta", "Hailu", "Ibsa", "Jemila",
		"Kassu", "Lensa", "Mesfin", "Nigist", "Obsa",
		"Pascal", "Qeshi", "Reta", "Sileshi", "Tinsae",
		"Ubong", "Vivian", "Wesen", "Xavier", "Yemisrach",
		"Zerihun", "Aragaw", "Bulti", "Chane", "Diriba",
	}
	lastNames := []string{
		"Alemayehu", "Bekele", "Chala", "Demeke", "Eshetu",
		"Girma", "Haile", "Kebede", "Lemma", "Mekonnen",
		"Negash", "Tadesse", "Wolde", "Yilma", "Zelalem",
		"Assefa", "Berhanu", "Desta", "Endale", "Fesseha",
		"Gebre", "Hagos", "Kiros", "Melaku", "Nega",
		"Admassu", "Ayele", "Benti", "Dinka", "Ejigu",
		"Gemeda", "Hailu", "Jemaneh", "Kassaye", "Lema",
		"Mamo", "Nuru", "Regassa", "Sisay", "Tekle",
		"Umeta", "Wakjira", "Yigezu", "Zenebe", "Alem",
		"Bogale", "Dibaba", "Gemechu", "Hundessa", "Jilo",
		"Ketema", "Lelisa", "Merga", "Nuro", "Olana",
		"Qalicha", "Roba", "Shiferaw", "Tulu", "Wakgari",
		"Abdi", "Birhanu", "Degu", "Elias", "Guta",
		"Habte", "Imana", "Jebena", "Kaba", "Leta",
		"Mesa", "Nafisa", "Oda", "Pasha", "Qana",
		"Raya", "Saba", "Tomi", "Ula", "Vera",
		"Waga", "Xaba", "Yeka", "Zala", "Amsalu",
		"Beyene", "Dagne", "Emiru", "Fanta", "Gashaw",
	}
	// Add unique combinations by shuffling
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
func (bm *BotManager) ResetGameBots() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	bm.bots = bm.bots[:0]
}