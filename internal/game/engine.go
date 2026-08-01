package game

import (
	"babibingo/internal/models"
	"context"
	"fmt"
	"log"
	"runtime"
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

	LobbyDuration      = 60 * time.Second
	CallInterval       = 5 * time.Second
	MaxCalls           = 75
	StakeAmount        = 20.0
	MaxCardsPerPlayer  = 4
	MaxPlayers         = 400
	HouseCutPercent    = 0.20 // 20% house cut
)

// Engine is the main game engine
type Engine struct {
	db          *gorm.DB
	rdb         *redis.Client
	clients     map[string]*Client
	mu          sync.RWMutex
	currentGame *GameState
	botManager  *BotManager
	ctx         context.Context
	cancel      context.CancelFunc
	gameHistory []*GameState
	historyMu   sync.RWMutex
}

// NewEngine creates a new game engine
func NewEngine(db *gorm.DB, rdb *redis.Client) *Engine {
	InitCardCache()

	// Create context for the engine
	ctx, cancel := context.WithCancel(context.Background())

	if err := db.AutoMigrate(&models.RobotBotSettings{}); err != nil {
		log.Printf("Failed to migrate BotSettings: %v", err)
	}

	engine := &Engine{
		db:          db,
		rdb:         rdb,
		clients:     make(map[string]*Client),
		ctx:         ctx,
		cancel:      cancel,
		gameHistory: make([]*GameState, 0),
	}

	// Initialize bot manager
	engine.botManager = NewBotManager(engine)

	// Start cleanup routine
	go engine.cleanupRoutine()

	return engine
}

// GetBotManager returns the bot manager
func (e *Engine) GetBotManager() *BotManager {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.botManager
}

// StartBots starts the bot system
func (e *Engine) StartBots() {
	bm := e.GetBotManager()
	if bm != nil {
		bm.StartBotRoutine()
	}
}

// StopBots stops the bot system
func (e *Engine) StopBots() {
	bm := e.GetBotManager()
	if bm != nil {
		bm.StopBotRoutine()
	}
}

// GetCurrentGame returns the current game state
func (e *Engine) GetCurrentGame() (*models.Game, int, int, float64, float64, float64, error) {
	state := e.GetCurrentGameState()

	if state == nil {
		return nil, 0, 0, 0, 0, 0, fmt.Errorf("no active game")
	}

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
	state := e.GetCurrentGameState()
	if state == nil {
		return "", fmt.Errorf("no active game")
	}
	return state.Game.Status, nil
}

// GetGameState returns the game state for a user
func (e *Engine) GetGameState(userID int64) (*GameStateResponse, error) {
	state := e.GetCurrentGameState()

	if state == nil {
		return nil, fmt.Errorf("no active game")
	}

	state.mu.RLock()
	defer state.mu.RUnlock()

	// Use context with timeout for DB operations
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()

	// Get user by Telegram ID
	user, err := e.getUserByTelegramIDWithContext(ctx, userID)
	if err != nil {
		return nil, err
	}

	var myCards []models.Card
	if err := e.db.WithContext(ctx).Where("game_id = ? AND user_id = ?", state.Game.ID, user.ID).Find(&myCards).Error; err != nil {
		log.Printf("⚠️ Failed to get user cards: %v", err)
	}

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
		Balance:       user.Balance,
	}, nil
}

// GetGameStats returns game statistics for admin dashboard
func (e *Engine) GetGameStats() map[string]interface{} {
	state := e.GetCurrentGameState()

	stats := make(map[string]interface{})

	if state == nil {
		stats["has_active_game"] = false
		stats["goroutines"] = runtime.NumGoroutine()
		return stats
	}

	state.mu.RLock()
	defer state.mu.RUnlock()

	stats["has_active_game"] = true
	stats["game_id"] = state.Game.ID.String()
	stats["status"] = state.Game.Status
	stats["total_pool"] = state.Game.TotalPool
	stats["players"] = len(state.UserCards)
	stats["reserved_cards"] = len(state.ReservedCards)
	stats["called_numbers"] = len(state.CalledNums)
	stats["timer"] = int(state.Timer.Seconds())
	stats["call_index"] = state.CallIndex
	stats["goroutines"] = runtime.NumGoroutine()

	// Get bot count in game
	botCount := 0
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()

	for _, userID := range state.ReservedCards {
		var user models.User
		if err := e.db.WithContext(ctx).Where("telegram_id = ?", userID).First(&user).Error; err == nil {
			if user.IsBot {
				botCount++
			}
		}
	}
	stats["bot_count"] = botCount

	return stats
}

// GetActiveGamesCount returns number of active games
func (e *Engine) GetActiveGamesCount() int64 {
	var count int64
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()

	e.db.WithContext(ctx).Model(&models.Game{}).Where("status IN (?)", []string{GameStatusWaiting, GameStatusCalling}).Count(&count)
	return count
}

// GetTotalGamesCount returns total games played
func (e *Engine) GetTotalGamesCount() int64 {
	var count int64
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()

	e.db.WithContext(ctx).Model(&models.Game{}).Count(&count)
	return count
}

// GetTotalPoolAllGames returns total pool from all finished games
func (e *Engine) GetTotalPoolAllGames() float64 {
	var total float64
	ctx, cancel := context.WithTimeout(e.ctx, 5*time.Second)
	defer cancel()

	e.db.WithContext(ctx).Model(&models.Game{}).Where("status = ?", GameStatusFinished).Select("COALESCE(SUM(total_pool), 0)").Scan(&total)
	return total
}

// GetCurrentGameState safely returns current game
func (e *Engine) GetCurrentGameState() *GameState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.currentGame
}

// SetCurrentGame safely updates current game
func (e *Engine) SetCurrentGame(state *GameState) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// If there was a previous game, archive it
	if e.currentGame != nil {
		e.archiveGame(e.currentGame)
	}

	e.currentGame = state
}

// archiveGame moves a game to history for cleanup
func (e *Engine) archiveGame(state *GameState) {
	if state == nil {
		return
	}

	e.historyMu.Lock()
	defer e.historyMu.Unlock()

	// Keep only last 50 games in history
	if len(e.gameHistory) >= 50 {
		// Remove oldest
		e.gameHistory = e.gameHistory[1:]
	}
	e.gameHistory = append(e.gameHistory, state)
}

// cleanupRoutine cleans up old games and resources
func (e *Engine) cleanupRoutine() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
			e.cleanupOldGames()
			e.cleanupStaleClients()
		}
	}
}

// cleanupOldGames removes old games from memory
func (e *Engine) cleanupOldGames() {
	e.historyMu.Lock()
	defer e.historyMu.Unlock()

	// Games older than 24 hours get removed
	cutoff := time.Now().Add(-24 * time.Hour)
	newHistory := make([]*GameState, 0)

	for _, game := range e.gameHistory {
		if game.Game.CreatedAt.After(cutoff) {
			newHistory = append(newHistory, game)
		}
	}

	if len(newHistory) != len(e.gameHistory) {
		e.gameHistory = newHistory
		log.Printf("🧹 Cleaned up old games, remaining: %d", len(e.gameHistory))
	}
}

// cleanupStaleClients removes disconnected clients
// cleanupStaleClients removes disconnected clients
func (e *Engine) cleanupStaleClients() {
	e.mu.Lock()
	defer e.mu.Unlock()

	staleCount := 0
	for id, client := range e.clients {
		// Check if client is still connected
		if client == nil || client.Conn == nil {
			delete(e.clients, id)
			staleCount++
			continue
		}
		
		// Try to close the connection if it's a websocket.Conn
		if conn, ok := client.Conn.(interface{ Close() error }); ok {
			conn.Close()
		}
		
		delete(e.clients, id)
		staleCount++
	}

	if staleCount > 0 {
		log.Printf("🧹 Cleaned up %d stale clients", staleCount)
	}
}

// Shutdown gracefully shuts down the engine
func (e *Engine) Shutdown() {
	log.Println("🛑 Shutting down game engine...")

	// Cancel context
	e.cancel()

	// Stop bots
	e.StopBots()

	// Close all client connections
	e.mu.Lock()
	for id, client := range e.clients {
		if client.Conn != nil {
			// Type assert to close the connection
			if conn, ok := client.Conn.(interface{ Close() error }); ok {
				conn.Close()
			}
		}
		delete(e.clients, id)
	}
	e.mu.Unlock()

	log.Println("✅ Game engine shutdown complete")
}


// HealthCheck - returns engine health status
func (e *Engine) HealthCheck() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	state := e.currentGame

	health := map[string]interface{}{
		"status":     "healthy",
		"goroutines": runtime.NumGoroutine(),
		"clients":    len(e.clients),
		"has_game":   state != nil,
	}

	if state != nil {
		health["game_status"] = state.Game.Status
		health["game_id"] = state.Game.ID.String()
		health["players"] = len(state.UserCards)
	}

	// Check database connection
	ctx, cancel := context.WithTimeout(e.ctx, 2*time.Second)
	defer cancel()

	sqlDB, err := e.db.DB()
	if err == nil {
		if err := sqlDB.PingContext(ctx); err != nil {
			health["db_status"] = "error: " + err.Error()
		} else {
			health["db_status"] = "connected"
		}
	}

	return health
}

// GetMemoryStats - returns memory statistics
func (e *Engine) GetMemoryStats() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"alloc":       m.Alloc / 1024 / 1024,      // MB
		"total_alloc": m.TotalAlloc / 1024 / 1024, // MB
		"sys":         m.Sys / 1024 / 1024,        // MB
		"num_gc":      m.NumGC,
		"goroutines":  runtime.NumGoroutine(),
	}
}

// GetGameHistoryCount returns the number of games in history
func (e *Engine) GetGameHistoryCount() int {
	e.historyMu.RLock()
	defer e.historyMu.RUnlock()
	return len(e.gameHistory)
}

// ClearGameHistory clears all archived games from memory
func (e *Engine) ClearGameHistory() {
	e.historyMu.Lock()
	defer e.historyMu.Unlock()
	e.gameHistory = make([]*GameState, 0)
	log.Println("🧹 Game history cleared")
}

// SendError sends an error event to the frontend
func (e *Engine) SendError(userID int64, message string) {
	e.broadcast(GameEvent{
		Type:    "error",
		UserID:  userID,
		Message: message,
	})
}

