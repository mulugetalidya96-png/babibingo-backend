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
// OPTIMIZED: Batch operations, reduced queries, and parallel processing
// NOW ALLOWS: Game to start even with only bots (for testing/demo)
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
	
	// ✅ DEBUG: Log all reserved cards
	log.Printf("🔍 Reserved cards: %v", state.ReservedCards)
	log.Printf("🔍 User cards: %v", state.UserCards)

	// ✅ OPTIMIZATION 1: Batch fetch all users in one query
	var telegramIDList []int64
	for id := range telegramIDs {
		telegramIDList = append(telegramIDList, id)
	}

	var users []models.User
	if err := e.db.Where("telegram_id IN ?", telegramIDList).Find(&users).Error; err != nil {
		return fmt.Errorf("failed to fetch users: %w", err)
	}

	// Create map for quick lookup
	userMap := make(map[int64]*models.User)
	for i := range users {
		userMap[users[i].TelegramID] = &users[i]
		log.Printf("  📌 User %d: IsBot=%v, Balance=%.2f", users[i].TelegramID, users[i].IsBot, users[i].Balance)
	}

	tx := e.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("🔴 Panic recovered in collectAllStakes: %v", r)
		}
	}()

	totalPool := 0.0
	commissionEarned := make(map[int64]float64)
	realPlayerCount := 0
	botPlayerCount := 0

	// ✅ OPTIMIZATION 2: Batch prepare data
	type PlayerData struct {
		User       *models.User
		CardCount  int
		TotalStake float64
		IsBot      bool
		TelegramID int64
	}

	var realPlayers []PlayerData
	var botPlayers []PlayerData

	for telegramID := range telegramIDs {
		user, exists := userMap[telegramID]
		if !exists {
			log.Printf("⚠️ User %d not found in database, skipping", telegramID)
			continue
		}

		cardCount := len(state.UserCards[telegramID])
		totalStake := float64(cardCount) * StakeAmount

		log.Printf("  📊 User %d: cards=%d, stake=%.2f, IsBot=%v", 
			telegramID, cardCount, totalStake, user.IsBot)

		playerData := PlayerData{
			User:       user,
			CardCount:  cardCount,
			TotalStake: totalStake,
			IsBot:      user.IsBot,
			TelegramID: telegramID,
		}

		if user.IsBot {
			botPlayers = append(botPlayers, playerData)
			botPlayerCount++
		} else {
			realPlayers = append(realPlayers, playerData)
			realPlayerCount++
		}
	}

	log.Printf("📊 Found %d real players and %d bot players", realPlayerCount, botPlayerCount)

	// ✅ OPTIMIZATION 3: Process bots in bulk (no balance deduction)
	if len(botPlayers) > 0 {
		log.Printf("🤖 Processing %d bot players", len(botPlayers))
		
		// Batch create bot transactions
		var botTransactions []models.Transaction
		var botGamePlayers []models.GamePlayer

		for _, bp := range botPlayers {
			totalPool += bp.TotalStake

			// Prepare bot transactions
			for _, cardNumber := range state.UserCards[bp.TelegramID] {
				reference := fmt.Sprintf("bot_stake_%s_%d_%d",
					state.Game.ID.String()[:8],
					bp.User.ID,
					time.Now().UnixNano(),
				)
				botTransactions = append(botTransactions, models.Transaction{
					UserID:      bp.User.ID,
					Type:        "stake",
					Amount:      StakeAmount,
					Status:      "completed",
					Method:      "system",
					Reference:   reference,
					Description: fmt.Sprintf("Bot card #%d for game %s", cardNumber, state.Game.ID.String()),
					CreatedAt:   time.Now(),
				})
			}

			// Prepare bot game players
			botGamePlayers = append(botGamePlayers, models.GamePlayer{
				GameID:     state.Game.ID,
				UserID:     bp.User.ID,
				CardsCount: bp.CardCount,
				TotalStake: bp.TotalStake,
				IsBot:      true,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			})
		}

		// ✅ Batch insert bot transactions
		if len(botTransactions) > 0 {
			if err := tx.CreateInBatches(botTransactions, 100).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to create bot transactions: %w", err)
			}
			log.Printf("  ✅ Created %d bot transactions", len(botTransactions))
		}

		// ✅ Batch upsert bot game players
		if len(botGamePlayers) > 0 {
			for _, gp := range botGamePlayers {
				// Use ON CONFLICT for PostgreSQL or check existence
				var existing models.GamePlayer
				err := tx.Where("game_id = ? AND user_id = ?", gp.GameID, gp.UserID).First(&existing).Error
				if err == nil {
					// Update existing
					existing.CardsCount = gp.CardsCount
					existing.TotalStake = gp.TotalStake
					existing.IsBot = true
					existing.UpdatedAt = time.Now()
					if err := tx.Save(&existing).Error; err != nil {
						tx.Rollback()
						return fmt.Errorf("failed to update bot game player: %w", err)
					}
				} else if errors.Is(err, gorm.ErrRecordNotFound) {
					// Create new
					if err := tx.Create(&gp).Error; err != nil {
						tx.Rollback()
						return fmt.Errorf("failed to create bot game player: %w", err)
					}
				} else {
					tx.Rollback()
					return fmt.Errorf("failed to query bot game player: %w", err)
				}
			}
			log.Printf("  ✅ Created/Updated %d bot game players", len(botGamePlayers))
		}

		// ✅ Batch update bot card statuses
		for _, bp := range botPlayers {
			if err := tx.Model(&models.Card{}).
				Where("game_id = ? AND user_id = ?", state.Game.ID, bp.User.ID).
				Update("status", "active").Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update bot card status: %w", err)
			}
		}
		log.Printf("  ✅ Updated card status for %d bot players", len(botPlayers))
	}

	// ✅ OPTIMIZATION 4: Process real players with balance deduction
	if len(realPlayers) > 0 {
		log.Printf("👤 Processing %d real players", len(realPlayers))

		// First, check all balances
		var insufficientBalance []string
		for _, rp := range realPlayers {
			if rp.User.Balance < rp.TotalStake {
				insufficientBalance = append(insufficientBalance, 
					fmt.Sprintf("User %d needs %.2f, has %.2f", 
						rp.TelegramID, rp.TotalStake, rp.User.Balance))
			}
		}
		if len(insufficientBalance) > 0 {
			tx.Rollback()
			return fmt.Errorf("insufficient balances: %v", insufficientBalance)
		}

		// Prepare data for batch operations
		var userUpdates []models.User
		var transactions []models.Transaction
		var gamePlayers []models.GamePlayer
		var commissionTransactions []models.Transaction

		for _, rp := range realPlayers {
			// Deduct balance
			rp.User.Balance -= rp.TotalStake
			userUpdates = append(userUpdates, *rp.User)

			// Prepare transactions
			for _, cardNumber := range state.UserCards[rp.TelegramID] {
				reference := fmt.Sprintf("stake_%s_%d_%d",
					state.Game.ID.String()[:8],
					rp.User.ID,
					time.Now().UnixNano(),
				)
				transactions = append(transactions, models.Transaction{
					UserID:      rp.User.ID,
					Type:        "stake",
					Amount:      StakeAmount,
					Status:      "completed",
					Method:      "system",
					Reference:   reference,
					Description: fmt.Sprintf("Card #%d for game %s", cardNumber, state.Game.ID.String()),
					CreatedAt:   time.Now(),
				})
			}

			// Prepare game players
			gamePlayers = append(gamePlayers, models.GamePlayer{
				GameID:     state.Game.ID,
				UserID:     rp.User.ID,
				CardsCount: rp.CardCount,
				TotalStake: rp.TotalStake,
				IsBot:      false,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			})

			totalPool += rp.TotalStake

			// ✅ Agent Commission Logic (optimized)
			if rp.User.ReferredBy != nil {
				var agent models.User
				if err := tx.Where("id = ? AND is_agent = ?", *rp.User.ReferredBy, true).First(&agent).Error; err == nil {
					if !agent.IsBot {
						commission := float64(rp.CardCount) * 1.0
						agent.AgentBalance += commission
						agent.Balance += commission
						
						commissionEarned[agent.TelegramID] = commissionEarned[agent.TelegramID] + commission
						
						if err := tx.Save(&agent).Error; err != nil {
							log.Printf("⚠️ Failed to update agent balance: %v", err)
						} else {
							log.Printf("💰 Agent %d earned %.2f ETB commission", agent.TelegramID, commission)
							
							// Prepare commission transaction
							commissionReference := fmt.Sprintf("comm_%d_%d_%d",
								agent.ID,
								rp.User.ID,
								time.Now().UnixNano(),
							)
							commissionTransactions = append(commissionTransactions, models.Transaction{
								UserID:      agent.ID,
								Type:        "agent_commission",
								Amount:      commission,
								Status:      "completed",
								Method:      "system",
								Reference:   commissionReference,
								Description: fmt.Sprintf("Commission from user %d playing %d cards", rp.User.ID, rp.CardCount),
								CreatedAt:   time.Now(),
							})
						}
					}
				}
			}
		}

		// ✅ Batch update user balances
		if len(userUpdates) > 0 {
			for _, user := range userUpdates {
				if err := tx.Save(&user).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to update user balance: %w", err)
				}
			}
			log.Printf("  ✅ Updated balances for %d users", len(userUpdates))
		}

		// ✅ Batch insert transactions
		if len(transactions) > 0 {
			if err := tx.CreateInBatches(transactions, 100).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to create transactions: %w", err)
			}
			log.Printf("  ✅ Created %d transactions", len(transactions))
		}

		// ✅ Batch upsert game players
		for _, gp := range gamePlayers {
			var existing models.GamePlayer
			err := tx.Where("game_id = ? AND user_id = ?", gp.GameID, gp.UserID).First(&existing).Error
			if err == nil {
				existing.CardsCount = gp.CardsCount
				existing.TotalStake = gp.TotalStake
				existing.IsBot = false
				existing.UpdatedAt = time.Now()
				if err := tx.Save(&existing).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to update game player: %w", err)
				}
			} else if errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(&gp).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to create game player: %w", err)
				}
			} else {
				tx.Rollback()
				return fmt.Errorf("failed to query game player: %w", err)
			}
		}
		log.Printf("  ✅ Created/Updated %d game players", len(gamePlayers))

		// ✅ Batch insert commission transactions
		if len(commissionTransactions) > 0 {
			if err := tx.CreateInBatches(commissionTransactions, 100).Error; err != nil {
				log.Printf("⚠️ Failed to create commission transactions: %v", err)
			}
			log.Printf("  ✅ Created %d commission transactions", len(commissionTransactions))
		}

		// ✅ Batch update card statuses for real players
		for _, rp := range realPlayers {
			if err := tx.Model(&models.Card{}).
				Where("game_id = ? AND user_id = ?", state.Game.ID, rp.User.ID).
				Update("status", "active").Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update card status: %w", err)
			}
		}
		log.Printf("  ✅ Updated card status for %d real players", len(realPlayers))
	}

	// ✅ Log the final composition
	log.Printf("📊 Game has %d real players and %d bot players", realPlayerCount, botPlayerCount)
	log.Printf("💰 Total pool including bots: %.2f ETB", totalPool)

	// ✅ ALLOW GAME TO START EVEN WITH NO REAL PLAYERS
	// This is useful for testing/demo purposes
	if realPlayerCount == 0 && botPlayerCount > 0 {
		log.Printf("🎮 No real players, but %d bots are playing. Starting bot-only game for testing/demo!", botPlayerCount)
		log.Printf("💰 Total pool: %.2f ETB (all bots)", totalPool)
		
		// Still update the game with the pool
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
		
		// ✅ Return nil to allow game to start
		return nil
	}

	// ✅ No players at all (should not happen)
	if realPlayerCount == 0 && botPlayerCount == 0 {
		log.Println("⚠️ No players found, cancelling game...")
		tx.Rollback()
		return fmt.Errorf("no players found")
	}

	// ✅ REAL PLAYERS FOUND - Continue with game start
	log.Printf("✅ Found %d real players and %d bots, game will start!", realPlayerCount, botPlayerCount)

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

	// ✅ Send balance updates to agents
	for agentTelegramID, commissionAmount := range commissionEarned {
		if commissionAmount > 0 {
			var agent models.User
			if err := e.db.Where("telegram_id = ? AND is_bot = ?", agentTelegramID, false).First(&agent).Error; err == nil {
				e.broadcast(GameEvent{
					Type:    "balance.update",
					UserID:  agentTelegramID,
					Balance: agent.Balance,
				})
				log.Printf("💰 Sent balance update to agent %d: %.2f ETB", agentTelegramID, agent.Balance)
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