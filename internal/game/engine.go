package game

import (
	"babibingo/internal/models"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

const (
	GameStatusWaiting   = "waiting"
	GameStatusCalling   = "calling"
	GameStatusFinished  = "finished"
	GameStatusCancelled = "cancelled"

	LobbyDuration      = 1000 * time.Second
	CallInterval       = 5 * time.Second
	MaxCalls           = 75
	StakeAmount        = 20.0
	MaxCardsPerPlayer  = 2
	MaxPlayers         = 400
	HouseCutPercent    = 0.10 // 10% house cut
)

// Engine is the main game engine
type Engine struct {
	db          *gorm.DB
	rdb         *redis.Client
	clients     map[string]*Client
	mu          sync.RWMutex
	currentGame *GameState
	botManager  *BotManager // ✅ NEW
}

// NewEngine creates a new game engine
func NewEngine(db *gorm.DB, rdb *redis.Client) *Engine {
	InitCardCache()
	
	engine := &Engine{
		db:      db,
		rdb:     rdb,
		clients: make(map[string]*Client),
	}
	
	// ✅ Initialize bot manager
	engine.botManager = NewBotManager(engine)
	
	return engine
}
// GetBotManager returns the bot manager
func (e *Engine) GetBotManager() *BotManager {
	return e.botManager
}

// StartBots starts the bot system
func (e *Engine) StartBots() {
	e.botManager.StartBotRoutine()
}

// StopBots stops the bot system
func (e *Engine) StopBots() {
	e.botManager.StopBotRoutine()
}
// GetCurrentGame returns the current game state
func (e *Engine) GetCurrentGame() (*models.Game, int, int, float64, float64, float64, error) {
	if e.currentGame == nil {
		return nil, 0, 0, 0, 0, 0, fmt.Errorf("no active game")
	}

	state := e.currentGame
	state.mu.RLock()
	defer state.mu.RUnlock()

	players := e.getPlayerCount(state.Game.ID)
	boards := e.getBoardCount(state.Game.ID)

	grossPool := state.Game.TotalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	return state.Game, players, boards, grossPool, netPool, houseCut, nil
}

// GetGameStatus returns the current game status
func (e *Engine) GetGameStatus() (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.currentGame == nil {
		return "", fmt.Errorf("no active game")
	}
	return e.currentGame.Game.Status, nil
}

// GetGameState returns the game state for a user
func (e *Engine) GetGameState(userID int64) (*GameStateResponse, error) {
	if e.currentGame == nil {
		return nil, fmt.Errorf("no active game")
	}

	state := e.currentGame
	state.mu.RLock()
	defer state.mu.RUnlock()

	// Get user by Telegram ID
	user, err := e.getUserByTelegramID(userID)
	if err != nil {
		return nil, err
	}

	var myCards []models.Card
	e.db.Where("game_id = ? AND user_id = ?", state.Game.ID, user.ID).Find(&myCards)

	calledDisplays := make([]string, 0, len(state.CalledNums))
	for _, n := range state.CalledNums {
		calledDisplays = append(calledDisplays, fmt.Sprintf("%s%d", getBingoLetter(n), n))
	}

	reservedCards := make([]int, 0, len(state.ReservedCards))
	for card := range state.ReservedCards {
		reservedCards = append(reservedCards, card)
	}

	grossPool := state.Game.TotalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	return &GameStateResponse{
		GameID:        state.Game.ID.String(),
		Status:        state.Game.Status,
		Stake:         StakeAmount,
		Timer:         int(state.Timer.Seconds()),
		Players:       e.getPlayerCount(state.Game.ID),
		BoardCount:    e.getBoardCount(state.Game.ID),
		Pool:          netPool,
		GrossPool:     grossPool,
		HouseCut:      houseCut,
		Called:        calledDisplays,
		MyCards:       myCards,
		MaxCards:      MaxCardsPerPlayer,
		ReservedCards: reservedCards,
	}, nil
}