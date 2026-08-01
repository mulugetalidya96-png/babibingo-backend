package game

import (
	"context"
	"fmt"
	"log"
	"time"

	"babibingo/internal/models"
)

// collectAllStakes collects stakes from all players with reservations (including bots)
// OPTIMIZED: Batch operations, reduced queries, and parallel processing
// BOTS: No balance deduction, but their stakes contribute to the pool
// HOUSE CUT: Applied to the total pool
func (e *Engine) collectAllStakes(state *GameState) error {
	log.Println("💰💰💰 COLLECT ALL STAKES STARTED 💰💰💰")
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Lock state and copy data
	state.mu.RLock()
	telegramIDs := make(map[int64]bool)
	for _, telegramID := range state.ReservedCards {
		telegramIDs[telegramID] = true
	}
	userCardsCopy := make(map[int64][]int)
	for k, v := range state.UserCards {
		userCardsCopy[k] = v
	}
	state.mu.RUnlock()

	if len(telegramIDs) == 0 {
		log.Println("⚠️ No players with reservations, cancelling game...")
		return fmt.Errorf("no players to collect stakes from")
	}

	log.Printf("📊 Collecting stakes from %d players", len(telegramIDs))

	// Batch fetch users
	var telegramIDList []int64
	for id := range telegramIDs {
		telegramIDList = append(telegramIDList, id)
	}

	var users []models.User
	if err := e.db.WithContext(ctx).Where("telegram_id IN ?", telegramIDList).Find(&users).Error; err != nil {
		return fmt.Errorf("failed to fetch users: %w", err)
	}

	userMap := make(map[int64]*models.User)
	for i := range users {
		userMap[users[i].TelegramID] = &users[i]
	}

	// Start transaction
	tx := e.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return fmt.Errorf("failed to begin transaction: %w", tx.Error)
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("🔴 Panic recovered in collectAllStakes: %v", r)
		}
	}()

	type PlayerData struct {
		User       *models.User
		CardCount  int
		TotalStake float64
		IsBot      bool
		TelegramID int64
	}

	var realPlayers []PlayerData
	var botPlayers []PlayerData
	var realPool float64
	var botPool float64
	totalPool := 0.0
	commissionEarned := make(map[int64]float64)

	// Process players
	for telegramID := range telegramIDs {
		user, exists := userMap[telegramID]
		if !exists {
			continue
		}
		cardCount := len(userCardsCopy[telegramID])
		if cardCount == 0 {
			continue
		}
		totalStake := float64(cardCount) * StakeAmount

		playerData := PlayerData{
			User:       user,
			CardCount:  cardCount,
			TotalStake: totalStake,
			IsBot:      user.IsBot,
			TelegramID: telegramID,
		}

		if user.IsBot {
			botPlayers = append(botPlayers, playerData)
			botPool += totalStake
		} else {
			realPlayers = append(realPlayers, playerData)
			realPool += totalStake
		}
	}

	log.Printf("📊 Found %d real players and %d bot players", len(realPlayers), len(botPlayers))

	// ✅ Process bots - NO BALANCE DEDUCTION, but they contribute to the pool
	if len(botPlayers) > 0 {
		log.Printf("🤖 Processing %d bot players (no balance deduction, pool contribution: %.2f ETB)...", 
			len(botPlayers), botPool)
		
		var botTransactions []models.Transaction
		var botGamePlayers []models.GamePlayer
		var botUserIDs []uint

		for _, bp := range botPlayers {
			totalPool += bp.TotalStake
			// ✅ Convert int64 to uint
			botUserIDs = append(botUserIDs, uint(bp.User.ID))

			// Prepare bot transactions (amount is 0 since no balance deduction)
			for _, cardNumber := range userCardsCopy[bp.TelegramID] {
				_ = cardNumber // ✅ Mark as used to avoid unused warning
				reference := fmt.Sprintf("bot_stake_%s_%d_%d",
					state.Game.ID.String()[:8],
					bp.User.ID,
					time.Now().UnixNano(),
				)
				botTransactions = append(botTransactions, models.Transaction{
					UserID:      bp.User.ID,
					Type:        "stake",
					Amount:      0, // Bots don't pay
					Status:      "completed",
					Method:      "system",
					Reference:   reference,
					Description: fmt.Sprintf("Bot card for game %s (free)", state.Game.ID.String()[:8]),
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

		// Bulk insert bot transactions
		if len(botTransactions) > 0 {
			if err := tx.CreateInBatches(botTransactions, 500).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to create bot transactions: %w", err)
			}
			log.Printf("  ✅ Created %d bot transactions (amount: 0)", len(botTransactions))
		}

		// Bulk upsert bot game players
		if len(botGamePlayers) > 0 {
			for _, gp := range botGamePlayers {
				if err := tx.Save(&gp).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to save bot game player: %w", err)
				}
			}
			log.Printf("  ✅ Created %d bot game players", len(botGamePlayers))
		}

		// Bulk update bot card statuses
		if len(botUserIDs) > 0 {
			if err := tx.Model(&models.Card{}).
				Where("game_id = ? AND user_id IN ?", state.Game.ID, botUserIDs).
				Update("status", "active").Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update bot card status: %w", err)
			}
			log.Printf("  ✅ Updated card status for %d bot players", len(botUserIDs))
		}
	}

	// ✅ Process real players - DEDUCT BALANCE
	if len(realPlayers) > 0 {
		log.Printf("👤 Processing %d real players (deducting balance)...", len(realPlayers))

		// Check balances
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

		var userUpdates []models.User
		var transactions []models.Transaction
		var gamePlayers []models.GamePlayer
		var commissionTransactions []models.Transaction
		var realUserIDs []uint

		for _, rp := range realPlayers {
			totalPool += rp.TotalStake
			// ✅ Convert int64 to uint
			realUserIDs = append(realUserIDs, uint(rp.User.ID))

			// Deduct balance from real players only
			rp.User.Balance -= rp.TotalStake
			userUpdates = append(userUpdates, *rp.User)

			// Prepare transactions
			for _, cardNumber := range userCardsCopy[rp.TelegramID] {
				_ = cardNumber // ✅ Mark as used to avoid unused warning
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
					Description: fmt.Sprintf("Card for game %s", state.Game.ID.String()[:8]),
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

			// Agent Commission (only for real players)
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
							commissionTransactions = append(commissionTransactions, models.Transaction{
								UserID:      agent.ID,
								Type:        "agent_commission",
								Amount:      commission,
								Status:      "completed",
								Method:      "system",
								Reference:   fmt.Sprintf("comm_%d_%d_%d", agent.ID, rp.User.ID, time.Now().UnixNano()),
								Description: fmt.Sprintf("Commission from user %d", rp.User.ID),
								CreatedAt:   time.Now(),
							})
						}
					}
				}
			}
		}

		// Bulk update user balances
		if len(userUpdates) > 0 {
			for _, user := range userUpdates {
				if err := tx.Save(&user).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to update user balance: %w", err)
				}
			}
			log.Printf("  ✅ Updated balances for %d real users", len(userUpdates))
		}

		// Bulk insert transactions
		if len(transactions) > 0 {
			if err := tx.CreateInBatches(transactions, 500).Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to create transactions: %w", err)
			}
			log.Printf("  ✅ Created %d transactions", len(transactions))
		}

		// Bulk upsert game players
		if len(gamePlayers) > 0 {
			for _, gp := range gamePlayers {
				if err := tx.Save(&gp).Error; err != nil {
					tx.Rollback()
					return fmt.Errorf("failed to save game player: %w", err)
				}
			}
			log.Printf("  ✅ Created %d game players", len(gamePlayers))
		}

		// Bulk insert commission transactions
		if len(commissionTransactions) > 0 {
			if err := tx.CreateInBatches(commissionTransactions, 500).Error; err != nil {
				log.Printf("⚠️ Failed to create commission transactions: %v", err)
			}
			log.Printf("  ✅ Created %d commission transactions", len(commissionTransactions))
		}

		// Bulk update real player card statuses
		if len(realUserIDs) > 0 {
			if err := tx.Model(&models.Card{}).
				Where("game_id = ? AND user_id IN ?", state.Game.ID, realUserIDs).
				Update("status", "active").Error; err != nil {
				tx.Rollback()
				return fmt.Errorf("failed to update card status: %w", err)
			}
			log.Printf("  ✅ Updated card status for %d real players", len(realUserIDs))
		}
	}

	// ✅ Calculate final pool breakdown
	grossPool := totalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	log.Printf("📊 Game has %d real players and %d bot players", len(realPlayers), len(botPlayers))
	log.Printf("💰 Gross Pool: %.2f ETB (Real: %.2f, Bots: %.2f)", grossPool, realPool, botPool)
	log.Printf("🏠 House Cut: %.2f ETB (%.0f%%)", houseCut, HouseCutPercent*100)
	log.Printf("🎯 Net Pool (for winners): %.2f ETB", netPool)

	// Update game with gross pool
	state.mu.Lock()
	state.Game.TotalPool = grossPool
	state.mu.Unlock()

	if err := tx.Save(state.Game).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("failed to update game pool: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Broadcast pool update with house cut info
	e.broadcast(GameEvent{
		Type:      "pool.update",
		GameID:    state.Game.ID.String(),
		Pool:      netPool,
		GrossPool: grossPool,
		HouseCut:  houseCut,
		Message:   fmt.Sprintf("💰 Total pool: %.2f ETB (House: %.2f ETB)", netPool, houseCut),
	})

	log.Printf("✅ Collected total pool: Gross: %.2f ETB, Net: %.2f ETB, House: %.2f ETB", 
		grossPool, netPool, houseCut)
	log.Println("💰💰💰 COLLECT ALL STAKES COMPLETED SUCCESSFULLY 💰💰💰")
	return nil
}