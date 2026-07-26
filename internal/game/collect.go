package game

import (
	"errors"
	"fmt"
	"log"
	"time"

	"babibingo/internal/models"

	"gorm.io/gorm"
)

// collectAllStakes - Updated with proper error handling

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

	tx := e.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("🔴 Panic recovered in collectAllStakes: %v", r)
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

		log.Printf("  ✅ Found user: ID=%d, TelegramID=%d", user.ID, user.TelegramID)

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
		log.Printf("  ✅ Deducted balance for user %d, new balance: %.2f", telegramID, user.Balance)

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
			log.Printf("  ✅ Created transaction for card #%d", cardNumber)
		}

		// ✅ Handle game_player record - create if not exists
		var gamePlayer models.GamePlayer
		result := tx.Where("game_id = ? AND user_id = ?", state.Game.ID, user.ID).First(&gamePlayer)
		
		if result.Error != nil {
			// ✅ Check if it's a "record not found" error (which is expected)
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				// Create new game player record
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
				log.Printf("  ✅ Created new game_player for user %d (primary ID: %d)", telegramID, user.ID)
			} else {
				// Some other error occurred
				tx.Rollback()
				return fmt.Errorf("failed to query game player: %w", result.Error)
			}
		} else {
			// Update existing game player record
			gamePlayer.CardsCount = cardCount
			gamePlayer.TotalStake = totalStake
			gamePlayer.UpdatedAt = time.Now()
			if err := tx.Save(&gamePlayer).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update game player: %w", err)
			}
			log.Printf("  ✅ Updated existing game_player for user %d (primary ID: %d)", telegramID, user.ID)
		}

		// Update card status from "reserved" to "active"
		if err := tx.Model(&models.Card{}).
			Where("game_id = ? AND user_id = ?", state.Game.ID, user.ID).
			Update("status", "active").Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update card status: %w", err)
		}
		log.Printf("  ✅ Updated card status to 'active' for user ID: %d", user.ID)

		totalPool += totalStake
	}

	// Update the game's total pool
	state.Game.TotalPool = totalPool
	if err := tx.Save(state.Game).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update game pool: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("✅ Collected total pool: %.2f", totalPool)
	return nil
}