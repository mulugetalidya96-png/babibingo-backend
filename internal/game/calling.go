package game

import (
	"babibingo/internal/models"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// startCalling starts the calling phase
func (e *Engine) startCalling(state *GameState) {
	// Collect stakes from all players
	if err := e.collectAllStakes(state); err != nil {
		e.broadcast(GameEvent{
			Type:    "game.error",
			Message: fmt.Sprintf("Failed to collect stakes: %v", err),
		})
		return
	}

	state.Game.Status = GameStatusCalling
	now := time.Now()
	state.Game.StartedAt = &now
	state.Timer = CallInterval

	e.db.Save(state.Game)

	grossPool := state.Game.TotalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	e.broadcast(GameEvent{
		Type:       "game.started",
		GameID:     state.Game.ID.String(),
		Status:     GameStatusCalling,
		Players:    e.getPlayerCount(state.Game.ID),
		BoardCount: e.getBoardCount(state.Game.ID),
		Pool:       netPool,
		GrossPool:  grossPool,
		HouseCut:   houseCut,
	})

	log.Printf("🚀 Game %s started calling!", state.Game.ID.String())
}

// callNextNumber calls the next random number
func (e *Engine) callNextNumber(state *GameState) {
	available := e.getAvailableNumbers(state)
	if len(available) == 0 {
		e.endGame(state, nil)
		return
	}

	num := available[rand.Intn(len(available))]
	state.CalledNums = append(state.CalledNums, num)
	state.CallIndex++

	// Update database
	called := make([]int64, len(state.CalledNums))
	for i, n := range state.CalledNums {
		called[i] = int64(n)
	}
	state.Game.CalledNumbers = pq.Int64Array(called)
	state.Timer = CallInterval
	e.db.Save(state.Game)

	// Broadcast
	letter := getBingoLetter(num)
	display := fmt.Sprintf("%s%d", letter, num)
	grossPool := state.Game.TotalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	e.broadcast(GameEvent{
		Type:        "number.called",
		GameID:      state.Game.ID.String(),
		CallNumber:  num,
		CallDisplay: display,
		Called:      e.getCalledDisplays(state.CalledNums),
		Players:     e.getPlayerCount(state.Game.ID),
		Pool:        netPool,
		GrossPool:   grossPool,
		HouseCut:    houseCut,
	})

	log.Printf("🔢 Number called: %s (Pool: $%.2f)", display, netPool)

	// Auto-mark cards
	e.autoMarkCards(state.Game.ID, num)
}

// getAvailableNumbers returns numbers that haven't been called
func (e *Engine) getAvailableNumbers(state *GameState) []int {
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
	return available
}

// autoMarkCards automatically marks cards that have the called number
func (e *Engine) autoMarkCards(gameID uuid.UUID, number int) {
	var cards []models.Card
	e.db.Where("game_id = ?", gameID).Find(&cards)

	markedCount := 0
	for _, card := range cards {
		if containsNumber(card.CardData, number) {
			card.MarkedNumbers = append(card.MarkedNumbers, int64(number))
			e.db.Save(&card)
			markedCount++
		}
	}

	if markedCount > 0 {
		log.Printf("✅ Auto-marked %d cards for number %d", markedCount, number)
	}
}