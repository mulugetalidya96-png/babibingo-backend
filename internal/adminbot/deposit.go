package adminbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"babibingo/internal/models"

	"github.com/mymmrac/telego"
	"gorm.io/gorm"
)

// handleDeposits - Main deposit handler
func (b *Bot) handleDeposits(ctx context.Context, chatID int64, args []string) {
    if len(args) == 0 {
        b.showPendingDeposits(ctx, chatID)
        return
    }

    switch args[0] {
    case "pending":
        b.showPendingDeposits(ctx, chatID)
    case "all":
        b.showAllDeposits(ctx, chatID)
    case "approve":
        if len(args) > 1 {
            id := args[1]
            b.approveDeposit(ctx, chatID, id)
        } else {
            b.sendText(ctx, chatID, "❌ Usage: /deposits approve <transaction_id>")
        }
    case "reject":
        if len(args) > 1 {
            id := args[1]
            b.rejectDeposit(ctx, chatID, id)
        } else {
            b.sendText(ctx, chatID, "❌ Usage: /deposits reject <transaction_id>")
        }
    case "search":
        if len(args) > 1 {
            query := strings.Join(args[1:], " ")
            b.searchDeposits(ctx, chatID, query)
        } else {
            b.sendText(ctx, chatID, "❌ Usage: /deposits search <user or reference>")
        }
    case "stats":
        b.showDepositStats(ctx, chatID)
    default:
        b.sendText(ctx, chatID, "❌ Usage: /deposits [pending|all|approve <id>|reject <id>|search <query>|stats]")
    }
}

// showPendingDeposits - Show all pending deposits
func (b *Bot) showPendingDeposits(ctx context.Context, chatID int64) {
    var deposits []models.Transaction
    b.db.Where("type = ? AND status = ?", "deposit", "pending").
        Order("created_at ASC").
        Find(&deposits)

    if len(deposits) == 0 {
        b.sendText(ctx, chatID, "📋 No pending deposits.")
        return
    }

    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "💳 *Pending Deposits*\n\nTotal: %d", len(deposits),
    ))

    for _, deposit := range deposits {
        b.sendDepositCard(ctx, chatID, deposit)
    }
}

// showAllDeposits - Show all deposits (paginated)
func (b *Bot) showAllDeposits(ctx context.Context, chatID int64) {
    var deposits []models.Transaction
    b.db.Where("type = ?", "deposit").
        Order("created_at DESC").
        Limit(20).
        Find(&deposits)

    if len(deposits) == 0 {
        b.sendText(ctx, chatID, "📋 No deposits found.")
        return
    }

    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "💳 *Recent Deposits (Last 20)*\n\n"+
            "🟡 Pending | ✅ Completed | ❌ Failed",
    ))

    for _, deposit := range deposits {
        statusEmoji := "🟡"
        if deposit.Status == "completed" {
            statusEmoji = "✅"
        } else if deposit.Status == "failed" {
            statusEmoji = "❌"
        }

        // Get user info
        var user models.User
        b.db.First(&user, deposit.UserID)

        b.sendText(
            ctx,
            chatID,
            fmt.Sprintf(
                "%s %.2f ETB | @%s | %s | Ref: %s",
                statusEmoji,
                deposit.Amount,
                user.Username,
                deposit.CreatedAt.Format("Jan 2, 15:04"),
                deposit.Reference,
            ),
        )
    }
}

// approveDeposit - Approve a pending deposit
func (b *Bot) approveDeposit(ctx context.Context, chatID int64, transactionID string) {
    var deposit models.Transaction
    err := b.db.Where("id = ? AND type = ? AND status = ?", transactionID, "deposit", "pending").
        First(&deposit).Error

    if err != nil {
        if err == gorm.ErrRecordNotFound {
            b.sendText(ctx, chatID, "❌ Deposit not found or already processed.")
        } else {
            b.sendText(ctx, chatID, fmt.Sprintf("❌ Error: %v", err))
        }
        return
    }

    // Get user
    var user models.User
    if err := b.db.First(&user, deposit.UserID).Error; err != nil {
        b.sendText(ctx, chatID, "❌ User not found.")
        return
    }

    // Start transaction
    tx := b.db.Begin()

    // Update deposit status
    deposit.Status = "completed"
    if err := tx.Save(&deposit).Error; err != nil {
        tx.Rollback()
        b.sendText(ctx, chatID, "❌ Failed to update deposit status.")
        return
    }

    // Update user balance
    user.Balance += deposit.Amount
    if err := tx.Save(&user).Error; err != nil {
        tx.Rollback()
        b.sendText(ctx, chatID, "❌ Failed to update user balance.")
        return
    }

    // Commit transaction
    if err := tx.Commit().Error; err != nil {
        b.sendText(ctx, chatID, "❌ Failed to commit transaction.")
        return
    }

    // ✅ Notify the user
    b.sendMarkdown(
        ctx,
        user.TelegramID,
        fmt.Sprintf(
            "✅ *Deposit Approved!*\n\n"+
                "💰 Amount: %.2f ETB\n"+
                "🆔 Reference: `%s`\n"+
                "💳 New Balance: %.2f ETB\n\n"+
                "🎮 Play now from the menu!",
            deposit.Amount,
            deposit.Reference,
            user.Balance,
        ),
    )

    // Log admin action
    b.logAdminAction(ctx, chatID, "approve_deposit", deposit.UserID, "deposit",
        fmt.Sprintf("Approved deposit %.2f ETB for user %d", deposit.Amount, deposit.UserID))

    // Confirm to admin
    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "✅ *Deposit Approved*\n\n"+
            "💰 Amount: %.2f ETB\n"+
            "👤 User: @%s\n"+
            "🆔 Reference: `%s`\n"+
            "💳 New Balance: %.2f ETB",
        deposit.Amount,
        user.Username,
        deposit.Reference,
        user.Balance,
    ))
}

// rejectDeposit - Reject a pending deposit
func (b *Bot) rejectDeposit(ctx context.Context, chatID int64, transactionID string) {
    var deposit models.Transaction
    err := b.db.Where("id = ? AND type = ? AND status = ?", transactionID, "deposit", "pending").
        First(&deposit).Error

    if err != nil {
        if err == gorm.ErrRecordNotFound {
            b.sendText(ctx, chatID, "❌ Deposit not found or already processed.")
        } else {
            b.sendText(ctx, chatID, fmt.Sprintf("❌ Error: %v", err))
        }
        return
    }

    // Get user
    var user models.User
    if err := b.db.First(&user, deposit.UserID).Error; err != nil {
        b.sendText(ctx, chatID, "❌ User not found.")
        return
    }

    // Update deposit status
    deposit.Status = "failed"
    if err := b.db.Save(&deposit).Error; err != nil {
        b.sendText(ctx, chatID, "❌ Failed to update deposit status.")
        return
    }

    // ✅ Notify the user
    b.sendMarkdown(
        ctx,
        user.TelegramID,
        fmt.Sprintf(
            "❌ *Deposit Rejected*\n\n"+
                "💰 Amount: %.2f ETB\n"+
                "🆔 Reference: `%s`\n\n"+
                "Please contact support for more information.",
            deposit.Amount,
            deposit.Reference,
        ),
    )

    // Log admin action
    b.logAdminAction(ctx, chatID, "reject_deposit", deposit.UserID, "deposit",
        fmt.Sprintf("Rejected deposit %.2f ETB for user %d", deposit.Amount, deposit.UserID))

    // Confirm to admin
    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "❌ *Deposit Rejected*\n\n"+
            "💰 Amount: %.2f ETB\n"+
            "👤 User: @%s\n"+
            "🆔 Reference: `%s`",
        deposit.Amount,
        user.Username,
        deposit.Reference,
    ))
}

// searchDeposits - Search deposits by user or reference
func (b *Bot) searchDeposits(ctx context.Context, chatID int64, query string) {
    var deposits []models.Transaction

    // Try to find user by username or telegram_id
    var user models.User
    userFound := false
    if strings.HasPrefix(query, "@") {
        username := strings.TrimPrefix(query, "@")
        if err := b.db.Where("username = ?", username).First(&user).Error; err == nil {
            userFound = true
        }
    } else if id, err := strconv.ParseInt(query, 10, 64); err == nil {
        if err := b.db.Where("telegram_id = ?", id).First(&user).Error; err == nil {
            userFound = true
        }
    }

    if userFound {
        b.db.Where("type = ? AND user_id = ?", "deposit", user.ID).
            Order("created_at DESC").
            Limit(20).
            Find(&deposits)
    } else {
        // Search by reference
        b.db.Where("type = ? AND reference ILIKE ?", "deposit", "%"+query+"%").
            Order("created_at DESC").
            Limit(20).
            Find(&deposits)
    }

    if len(deposits) == 0 {
        b.sendText(ctx, chatID, fmt.Sprintf("📋 No deposits found for: %s", query))
        return
    }

    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "🔍 *Search Results for '%s'*\n\nFound: %d deposits",
        query, len(deposits),
    ))

    for _, deposit := range deposits {
        var u models.User
        b.db.First(&u, deposit.UserID)

        statusEmoji := "🟡"
        if deposit.Status == "completed" {
            statusEmoji = "✅"
        } else if deposit.Status == "failed" {
            statusEmoji = "❌"
        }

        b.sendText(
            ctx,
            chatID,
            fmt.Sprintf(
                "%s %.2f ETB | @%s | %s | Ref: %s",
                statusEmoji,
                deposit.Amount,
                u.Username,
                deposit.CreatedAt.Format("Jan 2, 15:04"),
                deposit.Reference,
            ),
        )
    }
}

// showDepositStats - Show deposit statistics
func (b *Bot) showDepositStats(ctx context.Context, chatID int64) {
    var totalDeposits float64
    var totalCount int64
    var pendingDeposits float64
    var pendingCount int64
    var completedDeposits float64
    var completedCount int64

    b.db.Model(&models.Transaction{}).
        Where("type = ?", "deposit").
        Select("COALESCE(SUM(amount), 0)").Scan(&totalDeposits)
    b.db.Model(&models.Transaction{}).
        Where("type = ?", "deposit").
        Count(&totalCount)

    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ?", "deposit", "pending").
        Select("COALESCE(SUM(amount), 0)").Scan(&pendingDeposits)
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ?", "deposit", "pending").
        Count(&pendingCount)

    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ?", "deposit", "completed").
        Select("COALESCE(SUM(amount), 0)").Scan(&completedDeposits)
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ?", "deposit", "completed").
        Count(&completedCount)

    // Get today's deposits
    today := time.Now().Truncate(24 * time.Hour)
    var todayDeposits float64
    var todayCount int64
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "deposit", "completed", today).
        Select("COALESCE(SUM(amount), 0)").Scan(&todayDeposits)
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "deposit", "completed", today).
        Count(&todayCount)

    b.sendMarkdown(
        ctx,
        chatID,
        fmt.Sprintf(
            "📊 *Deposit Statistics*\n\n"+
                "💰 *Total Deposits:*\n"+
                "• Amount: %.2f ETB\n"+
                "• Count: %d\n\n"+
                "🟡 *Pending Deposits:*\n"+
                "• Amount: %.2f ETB\n"+
                "• Count: %d\n\n"+
                "✅ *Completed Deposits:*\n"+
                "• Amount: %.2f ETB\n"+
                "• Count: %d\n\n"+
                "📅 *Today's Deposits:*\n"+
                "• Amount: %.2f ETB\n"+
                "• Count: %d",
            totalDeposits,
            totalCount,
            pendingDeposits,
            pendingCount,
            completedDeposits,
            completedCount,
            todayDeposits,
            todayCount,
        ),
    )
}

// sendDepositCard - Send a detailed deposit card
func (b *Bot) sendDepositCard(ctx context.Context, chatID int64, deposit models.Transaction) {
    var user models.User
    b.db.First(&user, deposit.UserID)

    statusEmoji := "🟡"
    statusText := "Pending"
    if deposit.Status == "completed" {
        statusEmoji = "✅"
        statusText = "Completed"
    } else if deposit.Status == "failed" {
        statusEmoji = "❌"
        statusText = "Failed"
    }

    text := fmt.Sprintf(
        "💳 *Deposit #%s*\n\n"+
            "👤 User: @%s\n"+
            "🆔 ID: %d\n"+
            "💰 Amount: %.2f ETB\n"+
            "📱 Method: %s\n"+
            "🆔 Reference: `%s`\n"+
            "📅 Date: %s\n"+
            "📊 Status: %s %s",
        deposit.ID.String()[:8],
        user.Username,
        user.TelegramID,
        deposit.Amount,
        deposit.Method,
        deposit.Reference,
        deposit.CreatedAt.Format("Jan 2, 2006 15:04"),
        statusEmoji,
        statusText,
    )

    if deposit.Status == "pending" {
        msg := telego.SendMessageParams{
            ChatID:      telego.ChatID{ID: chatID},
            Text:        text,
            ParseMode:   "Markdown",
            ReplyMarkup: b.transactionActionKeyboard(deposit.ID.String(), "deposit"),
        }
        b.sendMessage(ctx, &msg)
    } else {
        b.sendMarkdown(ctx, chatID, text)
    }
}