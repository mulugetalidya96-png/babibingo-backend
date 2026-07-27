package adminbot

import (
	"babibingo/internal/models"
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
)

func (b *Bot) handleCallback(ctx context.Context, callback *telego.CallbackQuery) {
	if callback == nil {
		return
	}

	// Admin check
	if !b.isAdmin(callback.From.ID) {
		b.api.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
			CallbackQueryID: callback.ID,
			Text:            "⛔ Unauthorized",
			ShowAlert:       true,
		})
		return
	}

	log.Printf("Callback received: %s", callback.Data)

	chatID := callback.Message.GetChat().ID
	data := callback.Data

	// Answer callback
	b.api.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: callback.ID,
	})

	// Menu navigation
	if strings.HasPrefix(data, "menu_") {
		b.handleMenuNavigation(ctx, chatID, data)
		return
	}

	// ✅ Deposits menu navigation
	if strings.HasPrefix(data, "deposits_") {
		b.handleDepositsCallback(ctx, chatID, data)
		return
	}

	// ✅ Games menu navigation
	if strings.HasPrefix(data, "games_") {
		b.handleGamesCallback(ctx, chatID, data)
		return
	}

	// ✅ Bots menu navigation
	if strings.HasPrefix(data, "bots_") {
		b.handleBotsCallback(ctx, chatID, data)
		return
	}

	// ✅ Back to menu
	if data == "back_to_menu" {
		b.showDashboard(ctx, chatID)
		return
	}

	// Agent actions
	if strings.HasPrefix(data, "approve_") || strings.HasPrefix(data, "reject_") || strings.HasPrefix(data, "view_") {
		parts := strings.Split(data, "_")
		if len(parts) < 2 {
			return
		}
		action := parts[0]
		id, _ := strconv.Atoi(parts[1])

		switch action {
		case "approve":
			b.approveAgent(ctx, chatID, uint(id))
		case "reject":
			b.rejectAgent(ctx, chatID, uint(id))
		case "view":
			b.viewAgent(ctx, chatID, uint(id))
		}
		return
	}

	// Transaction actions
	if strings.HasPrefix(data, "tx_") {
		parts := strings.Split(data, "_")
		if len(parts) < 4 {
			return
		}
		action := parts[1]
		txType := parts[2]
		txID := parts[3]

		switch action {
		case "approve":
			b.approveTransaction(ctx, chatID, txType, txID)
		case "reject":
			b.rejectTransaction(ctx, chatID, txType, txID)
		}
		return
	}
}

// ✅ handleMenuNavigation - Main menu navigation
func (b *Bot) handleMenuNavigation(ctx context.Context, chatID int64, data string) {
	switch data {
	case "menu_agents":
		b.handleAgents(ctx, chatID, []string{})
	case "menu_deposits":
		b.showDepositMenu(ctx, chatID)
	case "menu_withdrawals":
		b.handleWithdrawals(ctx, chatID, []string{})
	case "menu_games":
		b.showGameMenu(ctx, chatID)
	case "menu_bots":
		b.showBotStatus(ctx, chatID)
	case "menu_users":
		b.handleUsers(ctx, chatID, []string{})
	case "menu_stats":
		b.handleStats(ctx, chatID, []string{})
	case "menu_settings":
		b.handleSettings(ctx, chatID, []string{})
	default:
		b.sendText(ctx, chatID, "❌ Unknown menu option.")
	}
}

// ✅ handleDepositsCallback - Deposits menu callbacks
func (b *Bot) handleDepositsCallback(ctx context.Context, chatID int64, data string) {
	switch data {
	case "deposits_menu":
		b.showDepositMenu(ctx, chatID)
	case "deposits_pending":
		b.showPendingDeposits(ctx, chatID)
	case "deposits_all":
		b.showAllDeposits(ctx, chatID)
	case "deposits_stats":
		b.showDepositStats(ctx, chatID)
	case "deposits_search":
		// Prompt for search query
		b.sendText(ctx, chatID, "🔍 Please send the user or reference to search for.\n\nExample: @username or reference123")
	default:
		b.sendText(ctx, chatID, "❌ Unknown deposits action.")
	}
}

// ✅ handleGamesCallback - Games menu callbacks
func (b *Bot) handleGamesCallback(ctx context.Context, chatID int64, data string) {
	switch data {
	case "games_menu":
		b.showGameMenu(ctx, chatID)
	case "games_active":
		b.showActiveGames(ctx, chatID)
	case "games_current":
		b.showCurrentGame(ctx, chatID)
	case "games_history":
		b.showGameHistory(ctx, chatID)
	case "games_stats":
		b.showGameStats(ctx, chatID)
	case "games_pool":
		b.showCurrentPool(ctx, chatID)
	case "games_end":
		b.sendText(ctx, chatID, "📝 Please send the game ID to end.\n\nExample: /games end a1b2c3d4-...")
	default:
		// Handle force end confirmation
		if strings.HasPrefix(data, "games_end_confirm_") {
			gameID := strings.TrimPrefix(data, "games_end_confirm_")
			b.forceEndGameConfirm(ctx, chatID, gameID)
			return
		}
		b.sendText(ctx, chatID, "❌ Unknown games action.")
	}
}

// ✅ handleBotsCallback - Bots menu callbacks
// callback.go - Update handleBotsCallback

func (b *Bot) handleBotsCallback(ctx context.Context, chatID int64, data string) {
	switch data {
	case "bots_status":
		b.showBotStatus(ctx, chatID)
	case "bots_refresh":
		b.showBotStatus(ctx, chatID)
	case "bots_set_prompt":
		b.sendText(ctx, chatID, "📝 Please enter the desired bot count (1-100):\n\nExample: /bots set 30")
	case "bots_add_5":
		b.addBots(ctx, chatID, 5)
	case "bots_add_10":
		b.addBots(ctx, chatID, 10)
	case "bots_remove_5":
		b.removeBots(ctx, chatID, 5)
	case "bots_remove_10":
		b.removeBots(ctx, chatID, 10)
	case "bots_reset_confirm":
		b.handleBotsResetConfirm(ctx, chatID)
	case "bots_reset_cancel":
		b.showBotStatus(ctx, chatID)
	case "bots_back":
		b.showBotStatus(ctx, chatID)
	default:
		b.sendText(ctx, chatID, "❌ Unknown bots action.")
	}
}

// ✅ handleBotsResetConfirm - Reset bots confirmation
func (b *Bot) handleBotsResetConfirm(ctx context.Context, chatID int64) {
	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	botManager := b.engine.GetBotManager()
	if botManager == nil {
		b.sendText(ctx, chatID, "❌ Bot manager not available.")
		return
	}

	// Stop bots
	botManager.StopBotRoutine()

	// Delete all bot users
	if err := b.db.Where("is_bot = ?", true).Delete(&models.User{}).Error; err != nil {
		b.sendText(ctx, chatID, fmt.Sprintf("❌ Failed to reset bots: %v", err))
		return
	}

	b.logAdminAction(ctx, chatID, "reset_bots", 0, "bots", "Reset all bot data")

	msg := telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text: "✅ *Bots Reset*\n\n" +
			"🤖 All bot data has been reset.\n\n" +
			"📊 Bot users removed.\n" +
			"📈 Statistics cleared.\n\n" +
			"Use /bots start to restart.",
		ParseMode: "Markdown",
		ReplyMarkup: &telego.InlineKeyboardMarkup{
			InlineKeyboard: [][]telego.InlineKeyboardButton{
				{
					{
						Text:         "🔙 Back to Bots",
						CallbackData: "bots_back",
					},
				},
			},
		},
	}
	b.sendMessage(ctx, &msg)
}

// ✅ approveTransaction - Handle transaction approval
func (b *Bot) approveTransaction(ctx context.Context, chatID int64, txType string, txID string) {
	switch txType {
	case "deposit":
		b.approveDeposit(ctx, chatID, txID)
	case "withdraw":
		b.approveWithdraw(ctx, chatID, txID)
	default:
		b.sendText(ctx, chatID, "❌ Unknown transaction type.")
	}
}

// ✅ rejectTransaction - Handle transaction rejection
func (b *Bot) rejectTransaction(ctx context.Context, chatID int64, txType string, txID string) {
	switch txType {
	case "deposit":
		b.rejectDeposit(ctx, chatID, txID)
	case "withdraw":
		b.rejectWithdraw(ctx, chatID, txID)
	default:
		b.sendText(ctx, chatID, "❌ Unknown transaction type.")
	}
}

// ✅ approveWithdrawal - Handle withdrawal approval
func (b *Bot) approveWithdrawal(ctx context.Context, chatID int64, txID string) {
	b.approveWithdraw(ctx, chatID, txID)
}

// ✅ rejectWithdrawal - Handle withdrawal rejection
func (b *Bot) rejectWithdrawal(ctx context.Context, chatID int64, txID string) {
	b.rejectWithdraw(ctx, chatID, txID)
}