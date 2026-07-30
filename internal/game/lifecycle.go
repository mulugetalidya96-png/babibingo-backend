package game

import (
	"babibingo/internal/models"
	"log"
	"math/rand"
	"time"

	"github.com/lib/pq"
	"gorm.io/gorm"
)

// Run starts the game engine ticker
func (e *Engine) Run() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		e.tick()
	}
}

// tick handles the game tick
func (e *Engine) tick() {
	if e.currentGame == nil {
		log.Println("🟡 No active game, starting new one...")
		e.startNewGame()
		return
	}

	state := e.currentGame
	state.mu.Lock()
	defer state.mu.Unlock()

	switch state.Game.Status {
	case GameStatusWaiting:
		e.handleWaitingState(state)
	case GameStatusCalling:
		e.handleCallingState(state)
	}
}

// handleWaitingState handles the waiting/lobby state
func (e *Engine) handleWaitingState(state *GameState) {
	state.Timer -= 1 * time.Second
	if state.Timer < 0 {
		state.Timer = 0
	}

	if state.Timer <= 0 {
		if len(state.ReservedCards) == 0 {
			log.Println("⚠️ No cards reserved, cancelling game...")
			e.endGame(state, nil)
			return
		}
		log.Println("🚀 Timer reached 0! Starting game...")
		e.startCalling(state)
		return
	}

	grossPool := state.Game.TotalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	e.broadcast(GameEvent{
		Type:       "timer.tick",
		GameID:     state.Game.ID.String(),
		Status:     GameStatusWaiting,
		Timer:      int(state.Timer.Seconds()),
		Players:    e.getPlayerCount(state.Game.ID),
		BoardCount: e.getBoardCount(state.Game.ID),
		Pool:       netPool,
		GrossPool:  grossPool,
		HouseCut:   houseCut,
		Stake:      StakeAmount,
	})
}

// handleCallingState handles the active calling state
func (e *Engine) handleCallingState(state *GameState) {
	state.Timer -= time.Second
	if state.Timer < 0 {
		state.Timer = 0
	}

	if state.Timer <= 0 {
		if state.CallIndex >= MaxCalls {
			log.Println("🏁 Max calls reached, ending game...")
			e.endGame(state, nil)
			return
		}
		e.callNextNumber(state)
	}
}

// startNewGame creates a new game
func (e *Engine) startNewGame() {
	game := &models.Game{
		Status:            GameStatusWaiting,
		StakeAmount:       StakeAmount,
		MaxCardsPerPlayer: MaxCardsPerPlayer,
		MaxPlayers:        MaxPlayers,
		CalledNumbers:     pq.Int64Array{},
		TotalPool:         0,
	}

	if err := e.db.Create(game).Error; err != nil {
		log.Printf("🔴 Failed to create game: %v", err)
		return
	}
	if e.botManager != nil {
	e.botManager.ResetGameBots()
}

	e.currentGame = &GameState{
		Game:          game,
		Timer:         LobbyDuration,
		CallIndex:     0,
		CalledNums:    []int{},
		ReservedCards: make(map[int]int64),
		UserCards:     make(map[int64][]int),
	}

	// ✅ Start bots in background - don't block the timer
	if e.botManager != nil {
		go func() {
			// Wait a bit for game to initialize
			time.Sleep(1 * time.Second)
			// Add initial bots (5-10 bots randomly)
			initialBots := rand.Intn(6) + 5 // 5-10 bots
			e.botManager.ReserveCardsForBots(initialBots)
		}()
		
		// Start the bot routine in background
		go e.botManager.StartBotRoutine()
	}

	grossPool := 0.0
	netPool, houseCut := GetPoolBreakdown(grossPool)

	e.broadcast(GameEvent{
		Type:       "game.new",
		GameID:     game.ID.String(),
		Status:     GameStatusWaiting,
		Timer:      int(LobbyDuration.Seconds()),
		Players:    0,
		BoardCount: 0,
		Pool:       netPool,
		GrossPool:  grossPool,
		HouseCut:   houseCut,
		Stake:      StakeAmount,
	})

	log.Printf("🟢 New game started: %s", game.ID.String())
}
// endGame ends the current game
func (e *Engine) endGame(state *GameState, winner *WinnerInfo) {
	log.Printf("🏁 Ending game - Winner: %v", winner != nil)
    if e.botManager != nil {
		e.botManager.StopBotRoutine()
	}
	state.Game.Status = GameStatusFinished
	now := time.Now()
	state.Game.EndedAt = &now

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

		log.Printf("💰 Winner %d won $%.2f", winner.UserID, winner.Prize)
	}

	e.db.Save(state.Game)

	grossPool := state.Game.TotalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	e.broadcast(GameEvent{
		Type:      "game.ended",
		GameID:    state.Game.ID.String(),
		Status:    GameStatusFinished,
		Winner:    winner,
		Pool:      netPool,
		GrossPool: grossPool,
		HouseCut:  houseCut,
	})

	// Reset after delay
	go func() {
		time.Sleep(10 * time.Second)
		e.currentGame = nil
		log.Println("🔄 Game reset complete")
	}()
}