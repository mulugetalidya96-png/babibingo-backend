package game

import (
	"babibingo/internal/models"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	GameStatusWaiting   = "waiting"
	GameStatusCalling   = "calling"
	GameStatusFinished  = "finished"
	GameStatusCancelled = "cancelled"

	LobbyDuration      = 60 * time.Second
	CallInterval       = 3 * time.Second
	MaxCalls           = 75
	StakeAmount        = 20.0
	MaxCardsPerPlayer  = 2
	MaxPlayers         = 400
	HouseCutPercent    = 0.10 // 10% house cut
)

// GameEvent represents a WebSocket event
type GameEvent struct {
	Type        string      `json:"type"`
	GameID      string      `json:"game_id,omitempty"`
	Status      string      `json:"status,omitempty"`
	CallNumber  int         `json:"call_number,omitempty"`
	CallDisplay string      `json:"call_display,omitempty"`
	Called      []string    `json:"called,omitempty"`
	Players     int         `json:"players,omitempty"`
	BoardCount  int         `json:"board_count,omitempty"`
	Timer       int         `json:"timer,omitempty"`
	Winner      *WinnerInfo `json:"winner,omitempty"`
	Pool        float64     `json:"pool,omitempty"`
	Stake       float64     `json:"stake,omitempty"`
	Message     string      `json:"message,omitempty"`
	CardNumber  int         `json:"card_number,omitempty"`
	UserID      int64       `json:"user_id,omitempty"`
	Card       *models.CardJSON `json:"card,omitempty"`
}

type WinnerInfo struct {
	UserID      int64   `json:"user_id"`
	Name        string  `json:"name"`
	Phone       string  `json:"phone"`
	Prize       float64 `json:"prize"`
	CardNumber  int     `json:"card_number"`
	Pattern     string  `json:"pattern"`
}

type Engine struct {
	db       *gorm.DB
	rdb      *redis.Client
	clients  map[string]*Client
	mu       sync.RWMutex
	currentGame *GameState
}

type Client struct {
	ID       string
	UserID   int64
	Conn     interface{} // WebSocket connection
	Send     chan []byte
}

type GameState struct {
	Game       *models.Game
	Timer      time.Duration
	CallIndex  int
	CalledNums []int
	// card number -> user id
	ReservedCards map[int]int64

	// user id -> reserved card numbers
	UserCards map[int64][]int
	mu         sync.RWMutex
}

func NewEngine(db *gorm.DB, rdb *redis.Client) *Engine {
	return &Engine{
		db:      db,
		rdb:     rdb,
		clients: make(map[string]*Client),
	}
}

func (e *Engine) Run() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		e.tick()
	}
}

func (e *Engine) tick() {
	if e.currentGame == nil {
		e.startNewGame()
		return
	}

	state := e.currentGame
	state.mu.Lock()
	defer state.mu.Unlock()

	switch state.Game.Status {
	case GameStatusWaiting:
		state.Timer -= 1 * time.Second
		if state.Timer <= 0 {
			e.startCalling(state)
		} else {
			e.broadcast(GameEvent{
				Type:       "timer.tick",
				GameID:     state.Game.ID.String(),
				Status:     GameStatusWaiting,
				Timer:      int(state.Timer.Seconds()),
				Players:    e.getPlayerCount(state.Game.ID),
				BoardCount: e.getBoardCount(state.Game.ID),
				Pool:       state.Game.TotalPool,
				Stake:      StakeAmount,
			})
		}

	case GameStatusCalling:
	state.Timer -= time.Second

	if state.Timer <= 0 {

		if state.CallIndex >= MaxCalls {
			e.endGame(state, nil)
			return
		}

		e.callNextNumber(state)
	}
	}
}

func (e *Engine) startNewGame() {
	game := &models.Game{
		Status:            GameStatusWaiting,
		StakeAmount:       StakeAmount,
		MaxCardsPerPlayer: MaxCardsPerPlayer,
		MaxPlayers:        MaxPlayers,
		CalledNumbers: pq.Int64Array{},
		TotalPool:         0,
	}

	if err := e.db.Create(game).Error; err != nil {
		return
	}

	e.currentGame = &GameState{
		Game:       game,
		Timer:      LobbyDuration,
		CallIndex:  0,
		CalledNums: []int{},
		ReservedCards: make(map[int]int64),
	UserCards:     make(map[int64][]int),
	}

	e.broadcast(GameEvent{
		Type:       "game.new",
		GameID:     game.ID.String(),
		Status:     GameStatusWaiting,
		Timer:      int(LobbyDuration.Seconds()),
		Players:    0,
		BoardCount: 0,
		Pool:       0,
		Stake:      StakeAmount,
	})
}
func (e *Engine) ReserveCard(userID int64, cardNumber int) error {
	if e.currentGame == nil {
		return fmt.Errorf("no active game")
	}

	state := e.currentGame

	state.mu.Lock()
	defer state.mu.Unlock()

	// Only allow reservations before the game starts
	if state.Game.Status != GameStatusWaiting {
		return fmt.Errorf("game already started")
	}

	// Check user exists
	var user models.User
	if err := e.db.First(&user, userID).Error; err != nil {
		return fmt.Errorf("user not found")
	}

	// Check balance (don't deduct yet)
	if user.Balance < StakeAmount {
		return fmt.Errorf("insufficient balance")
	}

	// Card already reserved?
	if reservedBy, ok := state.ReservedCards[cardNumber]; ok {
		if reservedBy == userID {
			return fmt.Errorf("card already reserved by you")
		}
		return fmt.Errorf("card already reserved")
	}

	// Max cards per player
	if len(state.UserCards[userID]) >= MaxCardsPerPlayer {
		return fmt.Errorf("maximum %d cards allowed", MaxCardsPerPlayer)
	}

	// Reserve card
	state.ReservedCards[cardNumber] = userID
	state.UserCards[userID] = append(
		state.UserCards[userID],
		cardNumber,
	)
	cardData, found := GetCardByID(cardNumber)
	if !found {
	return fmt.Errorf("card not found")
}
	e.broadcast(GameEvent{
		Type:       "card.reserved",
		GameID:     state.Game.ID.String(),
		CardNumber: cardNumber,
		UserID:     userID,
		Card:       &cardData,
		Players:    len(state.UserCards),
	})

	return nil
}

func (e *Engine) startCalling(state *GameState) {
	state.Game.Status = GameStatusCalling
	state.Game.StartedAt = func() *time.Time { t := time.Now(); return &t }()
	state.Timer = CallInterval

	e.db.Save(state.Game)

	e.broadcast(GameEvent{
		Type:       "game.started",
		GameID:     state.Game.ID.String(),
		Status:     GameStatusCalling,
		Players:    e.getPlayerCount(state.Game.ID),
		BoardCount: e.getBoardCount(state.Game.ID),
		Pool:       state.Game.TotalPool,
	})
}

func (e *Engine) callNextNumber(state *GameState) {
	// Generate random number 1-75 that hasn't been called
	available := make([]int, 0, 75-len(state.CalledNums))
	calledSet := make(map[int]bool)
	for _, n := range state.CalledNums {
		calledSet[n] = true
	}
	for i := 1; i <= 75; i++ {
		if !calledSet[i] {
			available = append(available, i)
		}
	}

	if len(available) == 0 {
		e.endGame(state, nil)
		return
	}

	num := available[rand.Intn(len(available))]
	state.CalledNums = append(state.CalledNums, num)
	state.CallIndex++
	called := make([]int64, len(state.CalledNums))

for i, n := range state.CalledNums {
	called[i] = int64(n)
}

state.Game.CalledNumbers = pq.Int64Array(called)
	state.Timer = CallInterval

	e.db.Save(state.Game)

	letter := getBingoLetter(num)
	display := fmt.Sprintf("%s%d", letter, num)

	e.broadcast(GameEvent{
		Type:        "number.called",
		GameID:      state.Game.ID.String(),
		CallNumber:  num,
		CallDisplay: display,
		Called:      e.getCalledDisplays(state.CalledNums),
		Players:     e.getPlayerCount(state.Game.ID),
	})

	// Auto-mark cards for all players
	e.autoMarkCards(state.Game.ID, num)
}

func (e *Engine) autoMarkCards(gameID uuid.UUID, number int) {
	var cards []models.Card
	e.db.Where("game_id = ?", gameID).Find(&cards)

	for _, card := range cards {
		if containsNumber(card.CardData, number) {
			card.MarkedNumbers = append(card.MarkedNumbers, number)
			e.db.Save(&card)
		}
	}
}

func (e *Engine) ClaimBingo(userID int64, cardID uuid.UUID) (*GameEvent, error) {
	if e.currentGame == nil || e.currentGame.Game.Status != GameStatusCalling {
		return nil, fmt.Errorf("no active game")
	}

	state := e.currentGame
	var card models.Card
	if err := e.db.Where("id = ? AND user_id = ? AND game_id = ?", cardID, userID, state.Game.ID).First(&card).Error; err != nil {
		return nil, fmt.Errorf("card not found")
	}

	// Check if card has a winning pattern
	pattern := checkWinPattern(card.CardData, card.MarkedNumbers)
	if pattern == "" {
		return nil, fmt.Errorf("no winning pattern")
	}

	// Winner found!
	var user models.User
	e.db.First(&user, userID)

	prize := state.Game.TotalPool * (1 - HouseCutPercent)

	winner := &WinnerInfo{
		UserID:     userID,
		Name:       user.FirstName,
		Phone:      maskPhone(user.PhoneNumber),
		Prize:      prize,
		CardNumber: card.CardNumber,
		Pattern:    pattern,
	}

	e.endGame(state, winner)

	return &GameEvent{
		Type:   "game.winner",
		Winner: winner,
		Pool:   state.Game.TotalPool,
	}, nil
}

func (e *Engine) endGame(state *GameState, winner *WinnerInfo) {
	state.Game.Status = GameStatusFinished
	state.Game.EndedAt = func() *time.Time { t := time.Now(); return &t }()

	if winner != nil {
		state.Game.WinnerUserID = &winner.UserID
		state.Game.WinnerPrize = winner.Prize

		// Update winner balance
		e.db.Model(&models.User{}).Where("id = ?", winner.UserID).
			UpdateColumn("balance", gorm.Expr("balance + ?", winner.Prize))

		// Create win transaction
		e.db.Create(&models.Transaction{
			UserID: winner.UserID,
			Type:   "win",
			Amount: winner.Prize,
			Status: "completed",
			Method: "system",
		})
	}

	e.db.Save(state.Game)

	e.broadcast(GameEvent{
		Type:   "game.ended",
		GameID: state.Game.ID.String(),
		Status: GameStatusFinished,
		Winner: winner,
		Pool:   state.Game.TotalPool,
	})

	// Reset after delay
	go func() {
		time.Sleep(10 * time.Second)
		e.currentGame = nil
	}()
}

func (e *Engine) JoinGame(userID int64, cardNumbers []int) (*models.Game, []models.Card, error) {
	if e.currentGame == nil {
		return nil, nil, fmt.Errorf("no active game")
	}

	state := e.currentGame
	state.mu.RLock()
	defer state.mu.RUnlock()

	if state.Game.Status != GameStatusWaiting {
		return nil, nil, fmt.Errorf("game already started")
	}

	// Check user balance
	var user models.User
	if err := e.db.First(&user, userID).Error; err != nil {
		return nil, nil, fmt.Errorf("user not found")
	}

	totalStake := float64(len(cardNumbers)) * StakeAmount
	if user.Balance < totalStake {
		return nil, nil, fmt.Errorf("insufficient balance")
	}

	// Check if cards are available
	var existingCards []models.Card
	e.db.Where("game_id = ? AND card_number IN ?", state.Game.ID, cardNumbers).Find(&existingCards)
	if len(existingCards) > 0 {
		return nil, nil, fmt.Errorf("some cards already taken")
	}

	// Check max cards per player
	var playerCards int64
	e.db.Model(&models.Card{}).Where("game_id = ? AND user_id = ?", state.Game.ID, userID).Count(&playerCards)
	if int(playerCards)+len(cardNumbers) > MaxCardsPerPlayer {
		return nil, nil, fmt.Errorf("max %d cards per player", MaxCardsPerPlayer)
	}

	// Deduct balance
	user.Balance -= totalStake
	e.db.Save(&user)

	// Create stake transaction
	e.db.Create(&models.Transaction{
		UserID: userID,
		Type:   "stake",
		Amount: totalStake,
		Status: "completed",
		Method: "system",
	})

	// Create or update game player
	var gamePlayer models.GamePlayer
	result := e.db.Where("game_id = ? AND user_id = ?", state.Game.ID, userID).First(&gamePlayer)
	if result.Error != nil {
		gamePlayer = models.GamePlayer{
			GameID:     state.Game.ID,
			UserID:     userID,
			CardsCount: len(cardNumbers),
			TotalStake: totalStake,
		}
		e.db.Create(&gamePlayer)
	} else {
		gamePlayer.CardsCount += len(cardNumbers)
		gamePlayer.TotalStake += totalStake
		e.db.Save(&gamePlayer)
	}

	// Create cards with predefined data
	var createdCards []models.Card
for _, cardNum := range cardNumbers {
	cardData, found := GetCardByID(cardNum)
	if !found {
		cardData = generateRandomCard(cardNum)
	}

	card := models.Card{
		GameID:     state.Game.ID,
		UserID:     userID,
		CardNumber: cardNum,
		CardData:   cardData,
	}
	e.db.Create(&card)
	createdCards = append(createdCards, card)
}

	// Update pool
	state.Game.TotalPool += totalStake
	e.db.Save(state.Game)

	return state.Game, createdCards, nil
}

func (e *Engine) GetCurrentGame() (*models.Game, int, int, float64, error) {
	if e.currentGame == nil {
		return nil, 0, 0, 0, fmt.Errorf("no active game")
	}

	state := e.currentGame
	state.mu.RLock()
	defer state.mu.RUnlock()

	players := e.getPlayerCount(state.Game.ID)
	boards := e.getBoardCount(state.Game.ID)

	return state.Game, players, boards, state.Game.TotalPool, nil
}

func (e *Engine) GetGameState(userID int64) (*GameStateResponse, error) {
	if e.currentGame == nil {
		return nil, fmt.Errorf("no active game")
	}

	state := e.currentGame
	state.mu.RLock()
	defer state.mu.RUnlock()

	var myCards []models.Card
	e.db.Where("game_id = ? AND user_id = ?", state.Game.ID, userID).Find(&myCards)

	calledDisplays := make([]string, 0, len(state.CalledNums))
	for _, n := range state.CalledNums {
		calledDisplays = append(calledDisplays, fmt.Sprintf("%s%d", getBingoLetter(n), n))
	}
    reservedCards := make([]int, 0, len(state.ReservedCards))
for card := range state.ReservedCards {
	reservedCards = append(reservedCards, card)
}
	return &GameStateResponse{
		GameID:      state.Game.ID.String(),
		Status:      state.Game.Status,
		Stake:       StakeAmount,
		Timer:       int(state.Timer.Seconds()),
		Players:     e.getPlayerCount(state.Game.ID),
		BoardCount:  e.getBoardCount(state.Game.ID),
		Pool:        state.Game.TotalPool,
		Called:      calledDisplays,
		MyCards:     myCards,
		MaxCards:    MaxCardsPerPlayer,
		ReservedCards: reservedCards,
	}, nil
}

type GameStateResponse struct {
	GameID     string         `json:"game_id"`
	Status     string         `json:"status"`
	Stake      float64        `json:"stake"`
	Timer      int            `json:"timer"`
	Players    int            `json:"players"`
	BoardCount int            `json:"board_count"`
	Pool       float64        `json:"pool"`
	Called     []string       `json:"called"`
	MyCards    []models.Card  `json:"my_cards"`
	MaxCards   int            `json:"max_cards"`
	ReservedCards []int `json:"reserved_cards"`
}

func (e *Engine) getPlayerCount(gameID uuid.UUID) int {
	var count int64
	e.db.Model(&models.GamePlayer{}).Where("game_id = ?", gameID).Count(&count)
	return int(count)
}

func (e *Engine) getBoardCount(gameID uuid.UUID) int {
	var count int64
	e.db.Model(&models.Card{}).Where("game_id = ?", gameID).Count(&count)
	return int(count)
}

func (e *Engine) getCalledDisplays(nums []int) []string {
	var displays []string
	for _, n := range nums {
		displays = append(displays, fmt.Sprintf("%s%d", getBingoLetter(n), n))
	}
	return displays
}

func (e *Engine) broadcast(event GameEvent) {
	data, _ := json.Marshal(event)

	ctx := context.Background()
	e.rdb.Publish(ctx, "game:events", data)

	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, client := range e.clients {
		select {
		case client.Send <- data:
		default:
		}
	}
}

func (e *Engine) SubscribeEvents() *redis.PubSub {
	ctx := context.Background()
	return e.rdb.Subscribe(ctx, "game:events")
}

// Helper functions
func getBingoLetter(num int) string {
	switch {
	case num >= 1 && num <= 15:
		return "B"
	case num >= 16 && num <= 30:
		return "I"
	case num >= 31 && num <= 45:
		return "N"
	case num >= 46 && num <= 60:
		return "G"
	case num >= 61 && num <= 75:
		return "O"
	default:
		return ""
	}
}

func containsNumber(card models.CardJSON, num int) bool {
	for _, n := range card.B {
		if n == num { return true }
	}
	for _, n := range card.I {
		if n == num { return true }
	}
	for _, n := range card.N {
		if n != nil && *n == num { return true }
	}
	for _, n := range card.G {
		if n == num { return true }
	}
	for _, n := range card.O {
		if n == num { return true }
	}
	return false
}

func checkWinPattern(card models.CardJSON, marked []int) string {
	markedSet := make(map[int]bool)
	for _, n := range marked {
		markedSet[n] = true
	}

	// Build 5x5 grid
	grid := make([][]*int, 5)
	for i := range grid {
		grid[i] = make([]*int, 5)
	}

	// Fill grid
	for i, n := range card.B { grid[i][0] = &n }
	for i, n := range card.I { grid[i][1] = &n }
	grid[0][2] = card.N[0]
	grid[1][2] = card.N[1]
	grid[2][2] = nil // Free space
	grid[3][2] = card.N[3]
	grid[4][2] = card.N[4]
	for i, n := range card.G { grid[i][3] = &n }
	for i, n := range card.O { grid[i][4] = &n }

	// Check horizontal
	for row := 0; row < 5; row++ {
		win := true
		for col := 0; col < 5; col++ {
			if grid[row][col] == nil { continue } // Free space counts
			if !markedSet[*grid[row][col]] {
				win = false
				break
			}
		}
		if win { return "horizontal" }
	}

	// Check vertical
	for col := 0; col < 5; col++ {
		win := true
		for row := 0; row < 5; row++ {
			if grid[row][col] == nil { continue }
			if !markedSet[*grid[row][col]] {
				win = false
				break
			}
		}
		if win { return "vertical" }
	}

	// Check diagonal (top-left to bottom-right)
	win := true
	for i := 0; i < 5; i++ {
		if grid[i][i] == nil { continue }
		if !markedSet[*grid[i][i]] {
			win = false
			break
		}
	}
	if win { return "diagonal" }

	// Check diagonal (top-right to bottom-left)
	win = true
	for i := 0; i < 5; i++ {
		if grid[i][4-i] == nil { continue }
		if !markedSet[*grid[i][4-i]] {
			win = false
			break
		}
	}
	if win { return "diagonal" }

	return ""
}

func generateRandomCard(cardID int) models.CardJSON {
	return models.CardJSON{
		B:      pickRandom(1, 15, 5),
		I:      pickRandom(16, 30, 5),
		N:      appendNFreeSpace(pickRandom(31, 45, 4)),
		G:      pickRandom(46, 60, 5),
		O:      pickRandom(61, 75, 5),
		CardID: cardID,
	}
}

func pickRandom(min, max, count int) []int {
	nums := make([]int, max-min+1)
	for i := range nums {
		nums[i] = min + i
	}
	rand.Shuffle(len(nums), func(i, j int) { nums[i], nums[j] = nums[j], nums[i] })
	result := nums[:count]
	sort.Ints(result)
	return result
}

func appendNFreeSpace(nums []int) []*int {
	result := make([]*int, 5)
	result[0] = &nums[0]
	result[1] = &nums[1]
	result[2] = nil // Free space
	result[3] = &nums[2]
	result[4] = &nums[3]
	return result
}

func maskPhone(phone string) string {
	if len(phone) < 8 {
		return phone
	}
	return phone[:4] + "****" + phone[len(phone)-2:]
}
