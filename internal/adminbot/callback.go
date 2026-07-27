package adminbot

import (
	"context"
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

func (b *Bot) handleMenuNavigation(ctx context.Context, chatID int64, data string) {
    switch data {
    case "menu_agents":
        b.handleAgents(ctx, chatID, []string{})
    case "menu_deposits":
        b.handleDeposits(ctx, chatID, []string{})
    case "menu_withdrawals":
        b.handleWithdrawals(ctx, chatID, []string{})
    case "menu_games":
        b.handleGames(ctx, chatID, []string{})
    case "menu_bots":
        b.handleBots(ctx, chatID, []string{})
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



// callback.go - Add these functions

func (b *Bot) approveTransaction(ctx context.Context, chatID int64, txType string, txID string) {
    switch txType {
    case "deposit":
        b.approveDeposit(ctx, chatID, txID)
    case "withdraw":
        b.approveWithdrawal(ctx, chatID, txID)
    default:
        b.sendText(ctx, chatID, "❌ Unknown transaction type.")
    }
}

func (b *Bot) rejectTransaction(ctx context.Context, chatID int64, txType string, txID string) {
    switch txType {
    case "deposit":
        b.rejectDeposit(ctx, chatID, txID)
    case "withdraw":
        b.rejectWithdrawal(ctx, chatID, txID)
    default:
        b.sendText(ctx, chatID, "❌ Unknown transaction type.")
    }
}
// callback.go - Add these functions

// callback.go - Add/Update these functions

func (b *Bot) approveWithdrawal(ctx context.Context, chatID int64, txID string) {
    b.approveWithdraw(ctx, chatID, txID)
}

func (b *Bot) rejectWithdrawal(ctx context.Context, chatID int64, txID string) {
    b.rejectWithdraw(ctx, chatID, txID)
}