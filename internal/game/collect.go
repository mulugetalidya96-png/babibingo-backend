package game

import (
	"errors"
	"fmt"
	"log"
	"time"

	"babibingo/internal/models"

	"gorm.io/gorm"
)

// collectAllStakes collects stakes from all players with reservations (including bots)
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

	log.Printf("🟡 Collecting stakes from %d players (including bots)", len(telegramIDs))

	tx := e.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("🔴 Panic recovered in collectAllStakes: %v", r)
		}
	}()

	totalPool := 0.0
	commissionEarned := make(map[int64]float64) // Track commissions per agent (by Telegram ID)
	realPlayerCount := 0
	botPlayerCount := 0
	var realUsers []models.User // Track real users for notifications

	for telegramID := range telegramIDs {
		cardCount := len(state.UserCards[telegramID])
		totalStake := float64(cardCount) * StakeAmount

		log.Printf("🟡 User %d has %d cards, total stake: %.2f", telegramID, cardCount, totalStake)

		// ✅ Get user by Telegram ID (include bots)
		var user models.User
		if err := tx.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				log.Printf("⚠️ User %d not found, skipping", telegramID)
				continue
			}
			tx.Rollback()
			return fmt.Errorf("user with telegram_id %d not found: %w", telegramID, err)
		}

		log.Printf("  ✅ Found user: ID=%d, TelegramID=%d, IsBot=%v", user.ID, user.TelegramID, user.IsBot)

		// ✅ Check if user is a bot
		if user.IsBot {
			botPlayerCount++
			log.Printf("  🤖 Bot user %d, adding to pool without balance deduction", telegramID)
			
			// ✅ Bots don't need balance deduction (they have virtual balance)
			// Just track their stake in the pool
			
			// Create transaction records for bots (for tracking)
			for _, cardNumber := range state.UserCards[telegramID] {
				reference := fmt.Sprintf("bot_stake_%s_%d_%d", 
					state.Game.ID.String()[:8], 
					user.ID, 
					time.Now().UnixNano(),
				)
				transaction := models.Transaction{
					UserID:      user.ID,
					Type:        "stake",
					Amount:      StakeAmount,
					Status:      "completed",
					Method:      "system",
					Reference:   reference,
					Description: fmt.Sprintf("Bot card #%d for game %s", cardNumber, state.Game.ID.String()),
					CreatedAt:   time.Now(),
				}
				if err := tx.Create(&transaction).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to create bot transaction: %w", err)
				}
				log.Printf("  ✅ Created bot transaction for card #%d", cardNumber)
			}

			// ✅ Add bot's stake to pool
			totalPool += totalStake

			// Handle game_player record for bot
			var gamePlayer models.GamePlayer
			result := tx.Where("game_id = ? AND user_id = ?", state.Game.ID, user.ID).First(&gamePlayer)
			
			if result.Error != nil {
				if errors.Is(result.Error, gorm.ErrRecordNotFound) {
					gamePlayer = models.GamePlayer{
						GameID:     state.Game.ID,
						UserID:     user.ID,
						CardsCount: cardCount,
						TotalStake: totalStake,
						IsBot:      true,
						CreatedAt:  time.Now(),
					}
					if err := tx.Create(&gamePlayer).Error; err != nil {
						tx.Rollback()
						return fmt.Errorf("failed to create bot game player: %w", err)
					}
					log.Printf("  ✅ Created new game_player for bot %d", user.ID)
				} else {
					tx.Rollback()
					return fmt.Errorf("failed to query bot game player: %w", result.Error)
				}
			} else {
				gamePlayer.CardsCount = cardCount
				gamePlayer.TotalStake = totalStake
				gamePlayer.IsBot = true
				gamePlayer.UpdatedAt = time.Now()
				if err := tx.Save(&gamePlayer).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to update bot game player: %w", err)
				}
				log.Printf("  ✅ Updated existing game_player for bot %d", user.ID)
			}

			// Update card status from "reserved" to "active"
			if err := tx.Model(&models.Card{}).
				Where("game_id = ? AND user_id = ?", state.Game.ID, user.ID).
				Update("status", "active").Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update bot card status: %w", err)
			}
			log.Printf("  ✅ Updated card status to 'active' for bot ID: %d", user.ID)

			continue // Skip balance deduction for bots
		}

		// ✅ Real user - deduct balance
		realPlayerCount++
		realUsers = append(realUsers, user)

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

		// Create transaction records with unique reference
		for _, cardNumber := range state.UserCards[telegramID] {
			reference := fmt.Sprintf("stake_%s_%d_%d", 
				state.Game.ID.String()[:8], 
				user.ID, 
				time.Now().UnixNano(),
			)
			transaction := models.Transaction{
				UserID:      user.ID,
				Type:        "stake",
				Amount:      StakeAmount,
				Status:      "completed",
				Method:      "system",
				Reference:   reference,
				Description: fmt.Sprintf("Card #%d for game %s", cardNumber, state.Game.ID.String()),
				CreatedAt:   time.Now(),
			}
			if err := tx.Create(&transaction).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to create transaction: %w", err)
			}
			log.Printf("  ✅ Created transaction for card #%d", cardNumber)
		}

		// ✅ Agent Commission Logic - Only for real users
		if user.ReferredBy != nil {
			var agent models.User
			if err := tx.Where("id = ? AND is_agent = ?", *user.ReferredBy, true).First(&agent).Error; err == nil {
				// ✅ Skip if agent is a bot
				if !agent.IsBot {
					// Commission: 1 ETB per card
					commission := float64(cardCount) * 1.0
					
					// Update BOTH AgentBalance AND Balance (regular balance)
					agent.AgentBalance += commission
					agent.Balance += commission
					
					// Track commission for this agent
					commissionEarned[agent.TelegramID] = commissionEarned[agent.TelegramID] + commission
					
					if err := tx.Save(&agent).Error; err != nil {
						log.Printf("⚠️ Failed to update agent balance: %v", err)
					} else {
						log.Printf("💰 Agent %d (Telegram: %d) earned %.2f ETB commission from user %d", 
							agent.ID, agent.TelegramID, commission, user.ID)
						log.Printf("   📊 Agent Balance: %.2f, Regular Balance: %.2f", 
							agent.AgentBalance, agent.Balance)
						
						// Create commission transaction
						commissionReference := fmt.Sprintf("comm_%d_%d_%d", 
							agent.ID, 
							user.ID, 
							time.Now().UnixNano(),
						)
						commissionTx := models.Transaction{
							UserID:      agent.ID,
							Type:        "agent_commission",
							Amount:      commission,
							Status:      "completed",
							Method:      "system",
							Reference:   commissionReference,
							Description: fmt.Sprintf("Commission from user %d playing %d cards", user.ID, cardCount),
							CreatedAt:   time.Now(),
						}
						if err := tx.Create(&commissionTx).Error; err != nil {
							log.Printf("⚠️ Failed to create commission transaction: %v", err)
						}
					}
				}
			} else {
				log.Printf("⚠️ Agent %d not found or not an agent", *user.ReferredBy)
			}
		}

		// Handle game_player record - create if not exists
		var gamePlayer models.GamePlayer
		result := tx.Where("game_id = ? AND user_id = ?", state.Game.ID, user.ID).First(&gamePlayer)
		
		if result.Error != nil {
			if errors.Is(result.Error, gorm.ErrRecordNotFound) {
				gamePlayer = models.GamePlayer{
					GameID:     state.Game.ID,
					UserID:     user.ID,
					CardsCount: cardCount,
					TotalStake: totalStake,
					IsBot:      false,
					CreatedAt:  time.Now(),
				}
				if err := tx.Create(&gamePlayer).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to create game player: %w", err)
				}
				log.Printf("  ✅ Created new game_player for user %d (primary ID: %d)", telegramID, user.ID)
			} else {
				tx.Rollback()
				return fmt.Errorf("failed to query game player: %w", result.Error)
			}
		} else {
			gamePlayer.CardsCount = cardCount
			gamePlayer.TotalStake = totalStake
			gamePlayer.IsBot = false
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

	// ✅ Log the final composition
	log.Printf("📊 Game has %d real players and %d bot players", realPlayerCount, botPlayerCount)
	log.Printf("💰 Total pool including bots: %.2f ETB", totalPool)

	// ✅ Even if no real players, we should still show the total stake
	// But we should not start the game if there are no real players
	if realPlayerCount == 0 {
		log.Printf("⚠️ No real players in the game, but total pool is %.2f ETB (all bots)", totalPool)
		
		// ✅ Still update the game with the pool
		state.Game.TotalPool = totalPool
		if err := tx.Save(state.Game).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to update game pool: %w", err)
		}
		
		if err := tx.Commit().Error; err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
		
		// ✅ Broadcast the pool update with bot-only pool
		e.broadcast(GameEvent{
			Type:      "pool.update",
			GameID:    state.Game.ID.String(),
			Pool:      totalPool,
			GrossPool: totalPool,
			HouseCut:  0,
			Message:   fmt.Sprintf("💰 Total pool: %.2f ETB (bots only)", totalPool),
		})
		
		// Return error to prevent game from starting
		return fmt.Errorf("no real players found, game cancelled (pool: %.2f ETB)", totalPool)
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

	log.Printf("✅ Collected total pool: %.2f (including bots)", totalPool)

	// ✅ Send balance updates to agents who earned commissions
	for agentTelegramID, commissionAmount := range commissionEarned {
		if commissionAmount > 0 {
			// Get the agent's updated balance
			var agent models.User
			if err := e.db.Where("telegram_id = ? AND is_bot = ?", agentTelegramID, false).First(&agent).Error; err == nil {
				// Send balance update via WebSocket
				e.broadcast(GameEvent{
					Type:    "balance.update",
					UserID:  agentTelegramID,
					Balance: agent.Balance,
				})
				log.Printf("💰 Sent balance update to agent %d: %.2f ETB (Agent Balance: %.2f)", 
					agentTelegramID, agent.Balance, agent.AgentBalance)
			}
		}
	}

	// ✅ Broadcast final pool update
	e.broadcast(GameEvent{
		Type:      "pool.update",
		GameID:    state.Game.ID.String(),
		Pool:      totalPool,
		GrossPool: totalPool,
		HouseCut:  0,
		Message:   fmt.Sprintf("💰 Total pool: %.2f ETB", totalPool),
	})

	return nil
}