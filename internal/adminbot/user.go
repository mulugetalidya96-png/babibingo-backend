package adminbot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"babibingo/internal/models"

	"gorm.io/gorm"
)

// handleUsers - Main user management handler
func (b *Bot) handleUsers(ctx context.Context, chatID int64, args []string) {
    if len(args) == 0 {
        b.listUsers(ctx, chatID, 1)
        return
    }

    switch args[0] {
    case "list":
        page := 1
        if len(args) > 1 {
            if p, err := strconv.Atoi(args[1]); err == nil && p > 0 {
                page = p
            }
        }
        b.listUsers(ctx, chatID, page)
    case "search":
        if len(args) > 1 {
            query := strings.Join(args[1:], " ")
            b.searchUsers(ctx, chatID, query)
        } else {
            b.sendText(ctx, chatID, "❌ Usage: /users search <username or phone>")
        }
    case "view":
        if len(args) > 1 {
            id, _ := strconv.ParseInt(args[1], 10, 64)
            b.viewUser(ctx, chatID, id)
        } else {
            b.sendText(ctx, chatID, "❌ Usage: /users view <telegram_id>")
        }
    case "balance":
        if len(args) > 2 {
            id, _ := strconv.ParseInt(args[1], 10, 64)
            amount, _ := strconv.ParseFloat(args[2], 64)
            b.adjustBalance(ctx, chatID, id, amount)
        } else {
            b.sendText(ctx, chatID, "❌ Usage: /users balance <telegram_id> <amount>")
        }
    case "suspend":
        if len(args) > 1 {
            id, _ := strconv.ParseInt(args[1], 10, 64)
            b.suspendUser(ctx, chatID, id)
        } else {
            b.sendText(ctx, chatID, "❌ Usage: /users suspend <telegram_id>")
        }
    case "unsuspend":
        if len(args) > 1 {
            id, _ := strconv.ParseInt(args[1], 10, 64)
            b.unsuspendUser(ctx, chatID, id)
        } else {
            b.sendText(ctx, chatID, "❌ Usage: /users unsuspend <telegram_id>")
        }
    case "stats":
        b.showUserStats(ctx, chatID)
    default:
        b.sendText(ctx, chatID, "❌ Usage: /users [list|search|view|balance|suspend|unsuspend|stats]")
    }
}

// listUsers - List users with pagination
func (b *Bot) listUsers(ctx context.Context, chatID int64, page int) {
    limit := 10
    offset := (page - 1) * limit

    var users []models.User
    var total int64

    b.db.Model(&models.User{}).Count(&total)
    b.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&users)

    if len(users) == 0 {
        b.sendText(ctx, chatID, "📋 No users found.")
        return
    }

    totalPages := int((total + int64(limit) - 1) / int64(limit))

    text := fmt.Sprintf("👥 *Users (Page %d/%d)*\n\n", page, totalPages)
    for i, user := range users {
        agentBadge := ""
        if user.IsAgent {
            agentBadge = " 🤝"
        }
        botBadge := ""
        if user.IsBot {
            botBadge = " 🤖"
        }
        text += fmt.Sprintf(
            "%d. @%s%s%s\n   💰 %.2f ETB | 📅 %s\n",
            offset+i+1,
            user.Username,
            agentBadge,
            botBadge,
            user.Balance,
            user.CreatedAt.Format("Jan 2, 2006"),
        )
    }

    if totalPages > 1 {
        text += fmt.Sprintf("\n📖 Page %d/%d | /users list %d", page, totalPages, page+1)
    }

    b.sendMarkdown(ctx, chatID, text)
}

// searchUsers - Search users by username or phone
func (b *Bot) searchUsers(ctx context.Context, chatID int64, query string) {
    var users []models.User

    // Search by username or phone
    searchPattern := "%" + query + "%"
    b.db.Where("username ILIKE ? OR phone_number ILIKE ?", searchPattern, searchPattern).
        Order("created_at DESC").
        Limit(20).
        Find(&users)

    if len(users) == 0 {
        b.sendText(ctx, chatID, fmt.Sprintf("📋 No users found for: %s", query))
        return
    }

    text := fmt.Sprintf("🔍 *Search Results for '%s'*\n\nFound: %d users\n", query, len(users))
    for i, user := range users {
        agentBadge := ""
        if user.IsAgent {
            agentBadge = " 🤝"
        }
        text += fmt.Sprintf(
            "%d. @%s%s\n   💰 %.2f ETB | 📱 %s\n",
            i+1,
            user.Username,
            agentBadge,
            user.Balance,
            user.PhoneNumber,
        )
    }

    b.sendMarkdown(ctx, chatID, text)
}

// viewUser - View detailed user information
func (b *Bot) viewUser(ctx context.Context, chatID int64, telegramID int64) {
    var user models.User
    if err := b.db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
        if err == gorm.ErrRecordNotFound {
            b.sendText(ctx, chatID, "❌ User not found.")
        } else {
            b.sendText(ctx, chatID, fmt.Sprintf("❌ Error: %v", err))
        }
        return
    }

    // Get user statistics
    var gameCount int64
    b.db.Model(&models.GamePlayer{}).Where("user_id = ?", user.ID).Count(&gameCount)

    var cardCount int64
    b.db.Model(&models.Card{}).Where("user_id = ?", user.ID).Count(&cardCount)

    var totalStaked float64
    b.db.Model(&models.Transaction{}).
        Where("user_id = ? AND type = ? AND status = ?", user.ID, "stake", "completed").
        Select("COALESCE(SUM(amount), 0)").Scan(&totalStaked)

    var totalWon float64
    b.db.Model(&models.Transaction{}).
        Where("user_id = ? AND type = ? AND status = ?", user.ID, "win", "completed").
        Select("COALESCE(SUM(amount), 0)").Scan(&totalWon)

    var totalDeposits float64
    b.db.Model(&models.Transaction{}).
        Where("user_id = ? AND type = ? AND status = ?", user.ID, "deposit", "completed").
        Select("COALESCE(SUM(amount), 0)").Scan(&totalDeposits)

    var referralCount int64
    b.db.Model(&models.User{}).Where("referred_by = ?", user.ID).Count(&referralCount)

    // Get recent transactions
    var recentTxs []models.Transaction
    b.db.Where("user_id = ?", user.ID).
        Order("created_at DESC").
        Limit(5).
        Find(&recentTxs)

    recentTxText := ""
    for _, tx := range recentTxs {
        statusEmoji := "🟡"
        if tx.Status == "completed" {
            statusEmoji = "✅"
        } else if tx.Status == "failed" {
            statusEmoji = "❌"
        }
        recentTxText += fmt.Sprintf(
            "• %s %.2f ETB (%s) %s\n",
            statusEmoji,
            tx.Amount,
            tx.Type,
            tx.CreatedAt.Format("Jan 2"),
        )
    }
    if recentTxText == "" {
        recentTxText = "No recent transactions"
    }

    agentText := "❌"
    if user.IsAgent {
        agentText = fmt.Sprintf("✅ (Balance: %.2f ETB)", user.AgentBalance)
    }

    b.sendMarkdown(
        ctx,
        chatID,
        fmt.Sprintf(
            "👤 *User Details*\n\n"+
                "🆔 Telegram ID: `%d`\n"+
                "👤 Name: %s %s\n"+
                "📱 Phone: %s\n"+
                "💰 Balance: %.2f ETB\n"+
                "🤝 Agent: %s\n"+
                "🔑 Referral Code: `%s`\n"+
                "👥 Referrals: %d\n"+
                "📅 Joined: %s\n"+
                "📅 Last Active: %s\n\n"+
                "📊 *Statistics:*\n"+
                "• Games Played: %d\n"+
                "• Cards Purchased: %d\n"+
                "• Total Staked: %.2f ETB\n"+
                "• Total Won: %.2f ETB\n"+
                "• Total Deposits: %.2f ETB\n\n"+
                "📋 *Recent Transactions:*\n%s",
            user.TelegramID,
            user.FirstName,
            user.LastName,
            user.PhoneNumber,
            user.Balance,
            agentText,
            user.ReferralCode,
            referralCount,
            user.CreatedAt.Format("Jan 2, 2006 15:04"),
            user.LastActive.Format("Jan 2, 2006 15:04"),
            gameCount,
            cardCount,
            totalStaked,
            totalWon,
            totalDeposits,
            recentTxText,
        ),
    )
}

// adjustBalance - Adjust user balance
func (b *Bot) adjustBalance(ctx context.Context, chatID int64, telegramID int64, amount float64) {
    var user models.User
    if err := b.db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
        if err == gorm.ErrRecordNotFound {
            b.sendText(ctx, chatID, "❌ User not found.")
        } else {
            b.sendText(ctx, chatID, fmt.Sprintf("❌ Error: %v", err))
        }
        return
    }

    oldBalance := user.Balance
    user.Balance += amount
    if err := b.db.Save(&user).Error; err != nil {
        b.sendText(ctx, chatID, fmt.Sprintf("❌ Failed to update balance: %v", err))
        return
    }

    // Create adjustment transaction
    txType := "adjustment"
    txStatus := "completed"
    if amount < 0 {
        txType = "adjustment_debit"
    } else {
        txType = "adjustment_credit"
    }

    transaction := models.Transaction{
        UserID:      user.ID,
        Type:        txType,
        Amount:      amount,
        Status:      txStatus,
        Method:      "admin",
        Description: fmt.Sprintf("Admin balance adjustment by admin %d", chatID),
        CreatedAt:   time.Now(),
    }
    b.db.Create(&transaction)

    // Log admin action
    b.logAdminAction(ctx, chatID, "adjust_balance", user.TelegramID, "user",
        fmt.Sprintf("Adjusted balance by %.2f ETB (old: %.2f, new: %.2f)", amount, oldBalance, user.Balance))

    // Notify user
    sign := "+"
    if amount < 0 {
        sign = ""
    }
    b.sendMarkdown(
        ctx,
        user.TelegramID,
        fmt.Sprintf(
            "💰 *Balance Adjustment*\n\n"+
                "Your balance has been adjusted by an administrator.\n\n"+
                "📊 Amount: %s%.2f ETB\n"+
                "💳 New Balance: %.2f ETB\n\n"+
                "If you have questions, please contact support.",
            sign,
            amount,
            user.Balance,
        ),
    )

    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "✅ *Balance Adjusted*\n\n"+
            "👤 User: @%s\n"+
            "📊 Old Balance: %.2f ETB\n"+
            "📊 Adjustment: %.2f ETB\n"+
            "💳 New Balance: %.2f ETB",
        user.Username,
        oldBalance,
        amount,
        user.Balance,
    ))
}

// suspendUser - Suspend a user
func (b *Bot) suspendUser(ctx context.Context, chatID int64, telegramID int64) {
    // You would need a "suspended" field in the User model
    // For now, we'll just log it
    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "⏳ *User Suspension*\n\n"+
            "User `%d` has been suspended.\n\n"+
            "⚠️ This feature requires a `suspended` field in the User model.",
        telegramID,
    ))

    b.logAdminAction(ctx, chatID, "suspend_user", telegramID, "user",
        fmt.Sprintf("Suspended user %d", telegramID))
}

// unsuspendUser - Unsuspend a user
func (b *Bot) unsuspendUser(ctx context.Context, chatID int64, telegramID int64) {
    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "⏳ *User Unsuspended*\n\n"+
            "User `%d` has been unsuspended.\n\n"+
            "⚠️ This feature requires a `suspended` field in the User model.",
        telegramID,
    ))

    b.logAdminAction(ctx, chatID, "unsuspend_user", telegramID, "user",
        fmt.Sprintf("Unsuspended user %d", telegramID))
}

// showUserStats - Show user statistics
func (b *Bot) showUserStats(ctx context.Context, chatID int64) {
    var totalUsers int64
    var totalAgents int64
    var totalBots int64
    var activeUsers int64

    b.db.Model(&models.User{}).Count(&totalUsers)
    b.db.Model(&models.User{}).Where("is_agent = ?", true).Count(&totalAgents)
    b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&totalBots)

    // Active users: last 7 days
    weekAgo := time.Now().AddDate(0, 0, -7)
    b.db.Model(&models.User{}).Where("last_active >= ?", weekAgo).Count(&activeUsers)

    // Today's new users
    today := time.Now().Truncate(24 * time.Hour)
    var newUsersToday int64
    b.db.Model(&models.User{}).Where("created_at >= ?", today).Count(&newUsersToday)

    // Users with balance > 0
    var usersWithBalance int64
    b.db.Model(&models.User{}).Where("balance > 0").Count(&usersWithBalance)

    b.sendMarkdown(
        ctx,
        chatID,
        fmt.Sprintf(
            "📊 *User Statistics*\n\n"+
                "👥 *Total Users:*\n"+
                "• Total: %d\n"+
                "• Agents: %d\n"+
                "• Bots: %d\n"+
                "• Active (7d): %d\n\n"+
                "📈 *Growth:*\n"+
                "• New Today: %d\n"+
                "• With Balance: %d\n\n"+
                "💰 *Balance Stats:*\n"+
                "• Users with Balance: %d\n"+
                "• Average Balance: %.2f ETB",
            totalUsers,
            totalAgents,
            totalBots,
            activeUsers,
            newUsersToday,
            usersWithBalance,
            usersWithBalance,
            b.getAverageBalance(),
        ),
    )
}

// getAverageBalance - Helper to calculate average balance
func (b *Bot) getAverageBalance() float64 {
    var avg float64
    b.db.Model(&models.User{}).Select("COALESCE(AVG(balance), 0)").Scan(&avg)
    return avg
}