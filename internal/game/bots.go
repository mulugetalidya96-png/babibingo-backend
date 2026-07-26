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

// Bot represents a simulated player
type Bot struct {
	ID         int64
	Name       string
	Phone      string
	TelegramID int64
	CardNumber int
}

// BotManager manages bot players
type BotManager struct {
	engine    *Engine
	bots      []*Bot
	mu        sync.RWMutex
	isRunning bool
	stopChan  chan bool
}

// NewBotManager creates a new bot manager
func NewBotManager(engine *Engine) *BotManager {
	return &BotManager{
		engine:   engine,
		bots:     make([]*Bot, 0),
		stopChan: make(chan bool),
	}
}

// Generate random names and phones
var (
	firstNames = []string{
		"Abebe", "Almaz", "Biruk", "Chala", "Dawit",
		"Eden", "Fiker", "Gizaw", "Hana", "Israel",
		"Kidist", "Lemma", "Meron", "Nebiyu", "Oli",
		"Rediet", "Sami", "Tigist", "Uriel", "Yonas",
		"Zewdu", "Amanuel", "Bereket", "Chaltu", "Daniel",
		"Elias", "Fasika", "Genet", "Henok", "Ibsa",
		"Kaleb", "Lidia", "Mulugeta", "Naomi", "Obsa",
		"Ruth", "Solomon", "Tesfaye", "Wubitu", "Yared",
		"Abdi", "Bontu", "Cherinet", "Diribe", "Eyob",
		"Feven", "Getachew", "Mekdes", "Nardos", "Tamrat",
	}
	
	lastNames = []string{
		"Alemayehu", "Bekele", "Chala", "Demeke", "Eshetu",
		"Girma", "Haile", "Kebede", "Lemma", "Mekonnen",
		"Negash", "Tadesse", "Wolde", "Yilma", "Zelalem",
		"Abera", "Belay", "Desta", "Endale", "Fikre",
		"Gizaw", "Hailu", "Kassa", "Lema", "Mulugeta",
		"Nega", "Tilahun", "Wondimu", "Yohannes", "Zewdie",
	}
	
	phonePrefixes = []string{
		"091", "092", "093", "094", "095", 
		"096", "097", "098", "099", "090",
	}
)

// generateRandomName generates a random Ethiopian name
func generateRandomName() string {
	firstName := firstNames[rand.Intn(len(firstNames))]
	lastName := lastNames[rand.Intn(len(lastNames))]
	return firstName + " " + lastName
}

// generateRandomPhone generates a random Ethiopian phone number
func generateRandomPhone() string {
	prefix := phonePrefixes[rand.Intn(len(phonePrefixes))]
	number := fmt.Sprintf("%07d", rand.Intn(10000000))
	return prefix + number
}

// generateUniqueTelegramID generates a unique Telegram ID for bots
func generateUniqueTelegramID(existingIDs map[int64]bool) int64 {
	var id int64
	for {
		// Use a range that won't conflict with real users (e.g., 1000000000 - 1999999999)
		id = 1000000000 + rand.Int63n(1000000000)
		if !existingIDs[id] {
			existingIDs[id] = true
			return id
		}
	}
}

// CreateBot creates a new bot
func (bm *BotManager) CreateBot(existingIDs map[int64]bool) *Bot {
	telegramID := generateUniqueTelegramID(existingIDs)
	name := generateRandomName()
	phone := generateRandomPhone()

	return &Bot{
		ID:         telegramID,
		Name:       name,
		Phone:      phone,
		TelegramID: telegramID,
	}
}

// ReserveCardsForBots reserves cards for bots
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

	// Limit bots to available cards
	if count > len(availableCards) {
		count = len(availableCards)
	}

	// Get existing Telegram IDs from database to avoid conflicts
	existingIDs := bm.getExistingUserIDs()

	log.Printf("🤖 Creating %d bots...", count)

	// Create and reserve cards for bots
	botsReserved := 0
	for i := 0; i < count && i < len(availableCards); i++ {
		// Create bot
		bot := bm.CreateBot(existingIDs)
		
		// Get a random available card
		cardIndex := rand.Intn(len(availableCards))
		cardNumber := availableCards[cardIndex]
		availableCards = append(availableCards[:cardIndex], availableCards[cardIndex+1:]...)

		// Create user in database
		user := &models.User{
			TelegramID: bot.TelegramID,
			FirstName:  bot.Name,
			LastName:   "",
			PhoneNumber: bot.Phone,
			Balance:    1000.0, // Give bots some balance
		}
		if err := engine.db.Create(user).Error; err != nil {
			log.Printf("⚠️ Failed to create bot user: %v", err)
			continue
		}

		// Reserve the card
		if err := engine.reserveCardForBot(state, bot.TelegramID, cardNumber); err != nil {
			log.Printf("⚠️ Failed to reserve card %d for bot %s: %v", cardNumber, bot.Name, err)
			continue
		}

		bot.CardNumber = cardNumber
		bm.bots = append(bm.bots, bot)
		botsReserved++

		log.Printf("🤖 Bot '%s' (%s) reserved card #%d", bot.Name, bot.Phone, cardNumber)

		// Small delay between bot actions to simulate human behavior
		time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)
	}

	log.Printf("✅ %d bots successfully reserved cards", botsReserved)
}

// reserveCardForBot reserves a card for a bot (bypasses WebSocket)
func (e *Engine) reserveCardForBot(state *GameState, telegramID int64, cardNumber int) error {
	// Check if card is already reserved
	if _, ok := state.ReservedCards[cardNumber]; ok {
		return fmt.Errorf("card already reserved")
	}

	// Get user
	var user models.User
	if err := e.db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		return err
	}

	// Reserve in memory
	state.ReservedCards[cardNumber] = telegramID
	state.UserCards[telegramID] = append(state.UserCards[telegramID], cardNumber)
	e.UpdatePool(state)

	// Get card data
	cardData, found := GetCardByID(cardNumber)
	if !found {
		// Rollback
		delete(state.ReservedCards, cardNumber)
		userCards := state.UserCards[telegramID]
		for i, num := range userCards {
			if num == cardNumber {
				state.UserCards[telegramID] = append(userCards[:i], userCards[i+1:]...)
				break
			}
		}
		e.UpdatePool(state)
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

	if err := e.db.Create(&card).Error; err != nil {
		// Rollback
		delete(state.ReservedCards, cardNumber)
		userCards := state.UserCards[telegramID]
		for i, num := range userCards {
			if num == cardNumber {
				state.UserCards[telegramID] = append(userCards[:i], userCards[i+1:]...)
				break
			}
		}
		e.UpdatePool(state)
		return err
	}

	return nil
}

// getAvailableCards returns available card numbers
func (bm *BotManager) getAvailableCards(state *GameState) []int {
	available := make([]int, 0, 400)
	reserved := state.ReservedCards

	for i := 1; i <= 400; i++ {
		if _, ok := reserved[i]; !ok {
			available = append(available, i)
		}
	}
	return available
}

// getExistingUserIDs gets existing Telegram IDs from database
func (bm *BotManager) getExistingUserIDs() map[int64]bool {
	var users []models.User
	bm.engine.db.Find(&users)
	
	existingIDs := make(map[int64]bool)
	for _, user := range users {
		existingIDs[user.TelegramID] = true
	}
	return existingIDs
}

// StartBotRoutine starts the bot reservation routine
func (bm *BotManager) StartBotRoutine() {
	bm.mu.Lock()
	if bm.isRunning {
		bm.mu.Unlock()
		return
	}
	bm.isRunning = true
	bm.mu.Unlock()

	log.Println("🤖 Bot manager started")

	go func() {
		ticker := time.NewTicker(5 * time.Second)
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
		close(bm.stopChan)
	}
}

// checkAndReserveBots checks if bots should be reserved
func (bm *BotManager) checkAndReserveBots() {
	engine := bm.engine
	if engine.currentGame == nil {
		return
	}

	state := engine.currentGame
	state.mu.RLock()
	defer state.mu.RUnlock()

	if state.Game.Status != GameStatusWaiting {
		return
	}

	// Calculate how many bots to add (random 1-5 bots per tick)
	botCount := rand.Intn(4) + 1 // 1-4 bots
	
	// Don't add bots if we already have many
	currentPlayers := len(state.UserCards)
	if currentPlayers > 50 {
		return
	}

	// Don't add bots if there aren't enough available cards
	availableCount := 400 - len(state.ReservedCards)
	if availableCount < botCount {
		return
	}

	// Add bots with some randomness (50% chance per tick)
	if rand.Float32() < 0.4 {
		bm.ReserveCardsForBots(botCount)
	}
}

// GetBotStats returns bot statistics
func (bm *BotManager) GetBotStats() map[string]interface{} {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return map[string]interface{}{
		"total_bots": len(bm.bots),
		"is_running": bm.isRunning,
	}
}