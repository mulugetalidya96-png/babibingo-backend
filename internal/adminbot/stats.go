package adminbot

import (
	"context"
	"fmt"
	"time"

	"babibingo/internal/models"
)

func (b *Bot) handleStats(ctx context.Context, chatID int64, args []string) {
    if len(args) == 0 {
        b.showDailyStats(ctx, chatID)
        return
    }

    switch args[0] {
    case "daily":
        b.showDailyStats(ctx, chatID)
    case "weekly":
        b.showWeeklyStats(ctx, chatID)
    case "revenue":
        b.showRevenueStats(ctx, chatID)
    case "agents":
        b.showAgentStats(ctx, chatID)
    case "bots":
        b.showBotStats(ctx, chatID)
    default:
        b.sendText(ctx, chatID, "❌ Usage: /stats [daily|weekly|revenue|agents|bots]")
    }
}

func (b *Bot) showDailyStats(ctx context.Context, chatID int64) {
    // Get today's stats
    today := time.Now().Truncate(24 * time.Hour)

    var deposits, withdrawals float64
    var depositCount, withdrawCount int64

    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "deposit", "completed", today).
        Select("COALESCE(SUM(amount), 0)").Scan(&deposits)
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "deposit", "completed", today).
        Count(&depositCount)

    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "withdraw", "completed", today).
        Select("COALESCE(SUM(amount), 0)").Scan(&withdrawals)
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "withdraw", "completed", today).
        Count(&withdrawCount)

    b.sendMarkdown(
        ctx,
        chatID,
        fmt.Sprintf(
            "📊 *Daily Statistics*\n📅 %s\n\n"+
                "💰 *Financial:*\n"+
                "• Deposits: %.2f ETB (%d txs)\n"+
                "• Withdrawals: %.2f ETB (%d txs)\n"+
                "• Net Flow: %.2f ETB\n\n"+
                "📋 *Activity:*\n"+
                "• New Users: %d\n"+
                "• Active Users: %d\n"+
                "• Games Played: %d",
            today.Format("Jan 2, 2006"),
            deposits,
            depositCount,
            withdrawals,
            withdrawCount,
            deposits-withdrawals,
            0, // new users
            0, // active users
            0, // games played
        ),
    )
}
// stats.go - Add these functions

func (b *Bot) showWeeklyStats(ctx context.Context, chatID int64) {
    // Get weekly stats (last 7 days)
    weekAgo := time.Now().AddDate(0, 0, -7)

    var deposits, withdrawals float64
    var depositCount, withdrawCount int64
    var newUsers int64

    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "deposit", "completed", weekAgo).
        Select("COALESCE(SUM(amount), 0)").Scan(&deposits)
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "deposit", "completed", weekAgo).
        Count(&depositCount)

    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "withdraw", "completed", weekAgo).
        Select("COALESCE(SUM(amount), 0)").Scan(&withdrawals)
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ? AND created_at >= ?", "withdraw", "completed", weekAgo).
        Count(&withdrawCount)

    b.db.Model(&models.User{}).
        Where("created_at >= ?", weekAgo).
        Count(&newUsers)

    // Get games played in the last week
    var gamesPlayed int64
    b.db.Model(&models.Game{}).
        Where("status = ? AND ended_at >= ?", "finished", weekAgo).
        Count(&gamesPlayed)

    b.sendMarkdown(
        ctx,
        chatID,
        fmt.Sprintf(
            "📊 *Weekly Statistics*\n📅 %s - %s\n\n"+
                "💰 *Financial:*\n"+
                "• Deposits: %.2f ETB (%d txs)\n"+
                "• Withdrawals: %.2f ETB (%d txs)\n"+
                "• Net Flow: %.2f ETB\n\n"+
                "👥 *Users:*\n"+
                "• New Users: %d\n\n"+
                "🎱 *Games:*\n"+
                "• Games Played: %d",
            weekAgo.Format("Jan 2"),
            time.Now().Format("Jan 2"),
            deposits,
            depositCount,
            withdrawals,
            withdrawCount,
            deposits-withdrawals,
            newUsers,
            gamesPlayed,
        ),
    )
}

func (b *Bot) showRevenueStats(ctx context.Context, chatID int64) {
    // Get total revenue (house cut from games)
    var totalPool float64
    b.db.Model(&models.Game{}).
        Where("status = ?", "finished").
        Select("COALESCE(SUM(total_pool), 0)").Scan(&totalPool)

    // House cut is 10%
    houseRevenue := totalPool * 0.10

    // Get total agent commissions paid
    var totalCommissions float64
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ?", "agent_commission", "completed").
        Select("COALESCE(SUM(amount), 0)").Scan(&totalCommissions)

    // Get total deposits
    var totalDeposits float64
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ?", "deposit", "completed").
        Select("COALESCE(SUM(amount), 0)").Scan(&totalDeposits)

    // Get total withdrawals
    var totalWithdrawals float64
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ?", "withdraw", "completed").
        Select("COALESCE(SUM(amount), 0)").Scan(&totalWithdrawals)

    b.sendMarkdown(
        ctx,
        chatID,
        fmt.Sprintf(
            "💰 *Revenue Report*\n\n"+
                "📊 *Game Revenue:*\n"+
                "• Total Pool: %.2f ETB\n"+
                "• House Cut (10%%): %.2f ETB\n\n"+
                "🤝 *Agent Commissions:*\n"+
                "• Total Paid: %.2f ETB\n\n"+
                "💳 *Transactions:*\n"+
                "• Total Deposits: %.2f ETB\n"+
                "• Total Withdrawals: %.2f ETB\n"+
                "• Net Balance: %.2f ETB",
            totalPool,
            houseRevenue,
            totalCommissions,
            totalDeposits,
            totalWithdrawals,
            totalDeposits-totalWithdrawals,
        ),
    )
}

func (b *Bot) showAgentStats(ctx context.Context, chatID int64) {
    var totalAgents int64
    var totalReferrals int64
    var totalCommissions float64

    b.db.Model(&models.User{}).Where("is_agent = ?", true).Count(&totalAgents)
    
    // Count total referrals (users with referred_by not null)
    b.db.Model(&models.User{}).Where("referred_by IS NOT NULL").Count(&totalReferrals)
    
    b.db.Model(&models.Transaction{}).
        Where("type = ? AND status = ?", "agent_commission", "completed").
        Select("COALESCE(SUM(amount), 0)").Scan(&totalCommissions)

    // Get top agent
    var topAgent models.User
    b.db.Where("is_agent = ?", true).Order("agent_balance DESC").First(&topAgent)

    b.sendMarkdown(
        ctx,
        chatID,
        fmt.Sprintf(
            "🤝 *Agent Statistics*\n\n"+
                "📊 *Agents:*\n"+
                "• Total Agents: %d\n"+
                "• Total Referrals: %d\n"+
                "• Total Commissions Paid: %.2f ETB\n\n"+
                "🏆 *Top Agent:*\n"+
                "• @%s - %.2f ETB",
            totalAgents,
            totalReferrals,
            totalCommissions,
            topAgent.Username,
            topAgent.AgentBalance,
        ),
    )
}

func (b *Bot) showBotStats(ctx context.Context, chatID int64) {
    var totalBots int64
    b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&totalBots)

    // Get bots that have played (have cards)
    var activeBots int64
    b.db.Model(&models.Card{}).Distinct("user_id").Where("user_id IN (?)", 
        b.db.Table("users").Select("id").Where("is_bot = ?", true),
    ).Count(&activeBots)

    // Get total cards reserved by bots
    var botCards int64
    b.db.Model(&models.Card{}).Where("user_id IN (?)", 
        b.db.Table("users").Select("id").Where("is_bot = ?", true),
    ).Count(&botCards)

    // Get bot wins
    var botWins int64
    b.db.Model(&models.Card{}).Where("is_winner = ? AND user_id IN (?)", true,
        b.db.Table("users").Select("id").Where("is_bot = ?", true),
    ).Count(&botWins)

    b.sendMarkdown(
        ctx,
        chatID,
        fmt.Sprintf(
            "🤖 *Bot Statistics*\n\n"+
                "📊 *Bot Users:*\n"+
                "• Total Bots Created: %d\n"+
                "• Active Bots: %d\n\n"+
                "🃏 *Bot Activity:*\n"+
                "• Cards Reserved: %d\n"+
                "• Games Won: %d\n"+
                "• Win Rate: %.1f%%",
            totalBots,
            activeBots,
            botCards,
            botWins,
            float64(botWins)/float64(botCards)*100,
        ),
    )
}