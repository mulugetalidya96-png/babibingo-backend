package game

import (
	"babibingo/internal/models"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
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

// callNextNumber calls the next random number and checks for winners
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

	// Broadcast the number
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

	// ✅ NEW: Check for winners after marking
	winners := e.checkAllCardsForWinners(state.Game.ID, state)
	
	if len(winners) > 0 {
		log.Printf("🎉 Found %d winner(s)!", len(winners))
		
		// Handle multiple winners (split the prize)
		e.handleWinners(state, winners)
	}
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
	e.db.Where("game_id = ? AND status = ?", gameID, "active").Find(&cards)

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

// ✅ NEW: Check all cards for winners
func (e *Engine) checkAllCardsForWinners(gameID uuid.UUID, state *GameState) []WinnerInfo {
	var cards []models.Card
	e.db.Where("game_id = ? AND status = ? AND is_winner = ?", gameID, "active", false).Find(&cards)

	var winners []WinnerInfo

	for _, card := range cards {
		// Check if card has a winning pattern
		pattern := checkWinPattern(card.CardData, int64SliceToInt(card.MarkedNumbers))
		if pattern != "" {
			// Get user details
			var user models.User
			if err := e.db.First(&user, card.UserID).Error; err != nil {
				log.Printf("⚠️ Failed to find user for card %s: %v", card.ID, err)
				continue
			}

			// Mark card as winner
			card.IsWinner = true
			if err := e.db.Save(&card).Error; err != nil {
				log.Printf("⚠️ Failed to mark card %s as winner: %v", card.ID, err)
			}

			winners = append(winners, WinnerInfo{
				UserID:     user.TelegramID,
				Name:       user.FirstName + " " + user.LastName,
				Phone:      maskPhone(user.PhoneNumber),
				CardNumber: card.CardNumber,
				Pattern:    pattern,
			})

			log.Printf("🎯 Winner found! User: %d, Card: %d, Pattern: %s", 
				user.TelegramID, card.CardNumber, pattern)
		}
	}

	return winners
}

// ✅ NEW: Handle winners (single or multiple)
func (e *Engine) handleWinners(state *GameState, winners []WinnerInfo) {
	if len(winners) == 0 {
		return
	}

	grossPool := state.Game.TotalPool
	totalPrize := CalculateNetPool(grossPool)
	
	// Split prize among all winners
	prizePerWinner := totalPrize / float64(len(winners))

	log.Printf("💰 Total Prize: $%.2f, Winners: %d, Each: $%.2f", 
		totalPrize, len(winners), prizePerWinner)

	// Update each winner's prize
	for i := range winners {
		winners[i].Prize = prizePerWinner
	}

	// Update game with winner info
	state.Game.Status = GameStatusFinished
	now := time.Now()
	state.Game.EndedAt = &now

	// Use first winner as the primary winner for game record
	firstWinner := &winners[0]
	state.Game.WinnerUserID = &firstWinner.UserID
	state.Game.WinnerPrize = prizePerWinner

	e.db.Save(state.Game)

	// Process each winner
	for _, winner := range winners {
		// Get user by Telegram ID
		var user models.User
		if err := e.db.Where("telegram_id = ?", winner.UserID).First(&user).Error; err != nil {
			log.Printf("⚠️ Failed to find user %d: %v", winner.UserID, err)
			continue
		}

		// Update winner balance
		e.db.Model(&models.User{}).Where("id = ?", user.ID).
			UpdateColumn("balance", gorm.Expr("balance + ?", prizePerWinner))

		// Create win transaction
		e.db.Create(&models.Transaction{
			UserID:    user.ID,
			Type:      "win",
			Amount:    prizePerWinner,
			Status:    "completed",
			Method:    "system",
			CreatedAt: time.Now(),
		})

		log.Printf("💰 Winner %d (Telegram: %d) awarded $%.2f", 
			user.ID, winner.UserID, prizePerWinner)
	}

	// Broadcast winner info to all clients
	netPool, houseCut := GetPoolBreakdown(grossPool)
	
	// Send individual winner events
	for _, winner := range winners {
		e.broadcast(GameEvent{
			Type: "game.winner",
			Winner: &WinnerInfo{
				UserID:     winner.UserID,
				Name:       winner.Name,
				Phone:      winner.Phone,
				Prize:      winner.Prize,
				CardNumber: winner.CardNumber,
				Pattern:    winner.Pattern,
			},
			Pool:      netPool,
			GrossPool: grossPool,
			HouseCut:  houseCut,
		})
	}

	// Also send a summary event
	e.broadcast(GameEvent{
		Type:      "game.winners_summary",
		Winners:   winners,
		Pool:      netPool,
		GrossPool: grossPool,
		HouseCut:  houseCut,
		Message:   fmt.Sprintf("🎉 %d winner(s)! Total Prize: $%.2f", len(winners), totalPrize),
	})

	log.Printf("🏁 Game ended with %d winners", len(winners))

	// Reset after delay
	go func() {
		time.Sleep(10 * time.Second)
		e.currentGame = nil
		log.Println("🔄 Game reset complete")
	}()
}