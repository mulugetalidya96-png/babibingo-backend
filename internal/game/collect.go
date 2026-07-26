package game

import (
	"fmt"
	"log"
	"time"

	"babibingo/internal/models"
)

// collectAllStakes collects stakes from all players with reservations
func (e *Engine) collectAllStakes(state *GameState) error {
	// Get all unique Telegram IDs with reservations
	telegramIDs := make(map[int64]bool)
	for _, telegramID := range state.ReservedCards {
		telegramIDs[telegramID] = true
	}

	if len(telegramIDs) == 0 {
		log.Println("⚠️ No players with reservations, cancelling game...")
		return fmt.Errorf("no players to collect stakes from")
	}

	log.Printf("🟡 Collecting stakes from %d players", len(telegramIDs))

	// Use a database transaction for atomicity
	tx := e.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	totalPool := 0.0

	for telegramID := range telegramIDs {
		cardCount := len(state.UserCards[telegramID])
		totalStake := float64(cardCount) * StakeAmount

		log.Printf("🟡 User %d has %d cards, total stake: %.2f", telegramID, cardCount, totalStake)

		// Get user by Telegram ID
		var user models.User
		if err := tx.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("user with telegram_id %d not found: %w", telegramID, err)
		}

		// Check balance
		if user.Balance < totalStake {
			tx.Rollback()
			return fmt.Errorf("user %d has insufficient balance: need %.2f, have %.2f",
				telegramID, totalStake, user.Balance)
		}

		// Deduct balance
		user.Balance -= totalStake
		if err := tx.Save(&user).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to deduct balance for user %d: %w", telegramID, err)
		}

		// Create transaction records
		for _, cardNumber := range state.UserCards[telegramID] {
			transaction := models.Transaction{
				UserID: user.ID,
				Type:   "stake",
				Amount: StakeAmount,
				Status: "completed",
				Method: "system",
				Description: fmt.Sprintf("Card #%d for game %s", cardNumber, state.Game.ID.String()),
				CreatedAt: time.Now(),
			}
			if err := tx.Create(&transaction).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to create transaction: %w", err)
			}
		}

		// Update GamePlayer record
		var gamePlayer models.GamePlayer
		result := tx.Where("game_id = ? AND user_id = ?", state.Game.ID, user.ID).First(&gamePlayer)
		if result.Error != nil {
			gamePlayer = models.GamePlayer{
				GameID:     state.Game.ID,
				UserID:     user.ID,
				CardsCount: cardCount,
				TotalStake: totalStake,
				CreatedAt:  time.Now(),
			}
			if err := tx.Create(&gamePlayer).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to create game player: %w", err)
			}
		} else {
			gamePlayer.CardsCount = cardCount
			gamePlayer.TotalStake = totalStake
			gamePlayer.UpdatedAt = time.Now()
			if err := tx.Save(&gamePlayer).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update game player: %w", err)
			}
		}

		// Update card status from "reserved" to "active"
		if err := tx.Model(&models.Card{}).
			Where("game_id = ? AND user_id = ?", state.Game.ID, user.ID).
			Update("status", "active").Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update card status: %w", err)
		}

		totalPool += totalStake
	}

	// Update the game's total pool
	state.Game.TotalPool = totalPool
	if err := tx.Save(state.Game).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update game pool: %w", err)
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("✅ Collected total pool: %.2f", totalPool)
	return nil
}