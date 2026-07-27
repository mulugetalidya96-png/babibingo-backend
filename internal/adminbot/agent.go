package adminbot

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"babibingo/internal/models"

	"github.com/mymmrac/telego"
)

func (b *Bot) handleAgents(ctx context.Context, chatID int64, args []string) {
    if len(args) == 0 {
        b.showPendingAgents(ctx, chatID)
        return
    }

    switch args[0] {
    case "pending":
        b.showPendingAgents(ctx, chatID)
    case "all":
        b.showAllAgents(ctx, chatID)
    case "approve":
        if len(args) > 1 {
            id, _ := strconv.Atoi(args[1])
            b.approveAgent(ctx, chatID, uint(id))
        }
    case "reject":
        if len(args) > 1 {
            id, _ := strconv.Atoi(args[1])
            b.rejectAgent(ctx, chatID, uint(id))
        }
    case "view":
        if len(args) > 1 {
            id, _ := strconv.Atoi(args[1])
            b.viewAgent(ctx, chatID, uint(id))
        }
    case "revoke":
        if len(args) > 1 {
            id, _ := strconv.Atoi(args[1])
            b.revokeAgent(ctx, chatID, int64(id))
        }
    case "commissions":
        b.showAgentCommissions(ctx, chatID)
    default:
        b.sendText(ctx, chatID, "❌ Usage: /agents [pending|all|approve <id>|reject <id>|view <id>|revoke <id>|commissions]")
    }
}

func (b *Bot) showPendingAgents(ctx context.Context, chatID int64) {
    var requests []AgentRequest
    b.db.Where("status = ?", "pending").Order("created_at ASC").Find(&requests)

    if len(requests) == 0 {
        b.sendText(ctx, chatID, "📋 No pending agent applications.")
        return
    }

    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "📋 *Pending Applications*\n\nTotal: %d", len(requests),
    ))

    for _, req := range requests {
        b.sendAgentRequestCard(ctx, chatID, req)
    }
}

func (b *Bot) sendAgentRequestCard(ctx context.Context, chatID int64, req AgentRequest) {
    text := fmt.Sprintf(
        "📋 *Application #%d*\n\n"+
            "👤 User: @%s\n"+
            "🆔 ID: %d\n"+
            "📱 Phone: %s\n"+
            "📅 Submitted: %s\n"+
            "📊 Status: 🟡 Pending",
        req.ID,
        req.Username,
        req.UserID,
        req.PhoneNumber,
        req.CreatedAt.Format("Jan 2, 2006 15:04"),
    )

    msg := telego.SendMessageParams{
        ChatID:      telego.ChatID{ID: chatID},
        Text:        text,
        ParseMode:   "Markdown",
        ReplyMarkup: b.requestActionKeyboard(req.ID),
    }
    b.sendMessage(ctx, &msg)
}

func (b *Bot) approveAgent(ctx context.Context, chatID int64, requestID uint) {
    var request AgentRequest
    if err := b.db.First(&request, requestID).Error; err != nil {
        b.sendText(ctx, chatID, "❌ Request not found.")
        return
    }

    // Update request status
    request.Status = "approved"
    now := time.Now()
    request.ReviewedAt = &now
    b.db.Save(&request)

    // Update user in main database
    if err := b.db.Model(&models.User{}).Where("telegram_id = ?", request.UserID).Update("is_agent", true).Error; err != nil {
        b.sendText(ctx, chatID, "⚠️ Approved but failed to update user status.")
        return
    }

    // Notify user
    b.sendMarkdown(
        ctx,
        request.UserID,
        "🎉 *Congratulations!*\n\n"+
            "Your agent application has been *approved*! 🎉\n\n"+
            "💰 You are now a BabiBingo agent!\n"+
            "• Earn 1 ETB per card played by your referrals\n"+
            "• Use /agent in the main bot to view your dashboard",
    )

    // Log action
    b.logAdminAction(ctx, chatID, "approve_agent", request.UserID, "agent", fmt.Sprintf("Approved agent request #%d", requestID))

    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "✅ *Agent #%d Approved*\n\nUser @%s is now an agent.",
        request.ID, request.Username,
    ))
}

func (b *Bot) rejectAgent(ctx context.Context, chatID int64, requestID uint) {
    var request AgentRequest
    if err := b.db.First(&request, requestID).Error; err != nil {
        b.sendText(ctx, chatID, "❌ Request not found.")
        return
    }

    request.Status = "rejected"
    now := time.Now()
    request.ReviewedAt = &now
    b.db.Save(&request)

    // Notify user
    b.sendMarkdown(
        ctx,
        request.UserID,
        "❌ *Application Rejected*\n\n"+
            "Your agent application has been rejected.\n\n"+
            "Contact support for more information.",
    )

    b.logAdminAction(ctx, chatID, "reject_agent", request.UserID, "agent", fmt.Sprintf("Rejected agent request #%d", requestID))

    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "❌ *Agent #%d Rejected*\n\nUser @%s has been rejected.",
        request.ID, request.Username,
    ))
}

func (b *Bot) viewAgent(ctx context.Context, chatID int64, requestID uint) {
    var request AgentRequest
    if err := b.db.First(&request, requestID).Error; err != nil {
        b.sendText(ctx, chatID, "❌ Request not found.")
        return
    }

    var user models.User
    b.db.Where("telegram_id = ?", request.UserID).First(&user)

    var referralCount int64
    b.db.Model(&models.User{}).Where("referred_by = ?", user.ID).Count(&referralCount)

    var totalCommission float64
    b.db.Model(&models.Transaction{}).
        Where("user_id = ? AND type = ?", user.ID, "agent_commission").
        Select("COALESCE(SUM(amount), 0)").
        Scan(&totalCommission)

    statusEmoji := "🟡"
    statusText := "Pending"
    if request.Status == "approved" {
        statusEmoji = "✅"
        statusText = "Approved"
    } else if request.Status == "rejected" {
        statusEmoji = "❌"
        statusText = "Rejected"
    }

    b.sendMarkdown(
        ctx,
        chatID,
        fmt.Sprintf(
            "📋 *Agent Details*\n\n"+
                "👤 Name: %s %s\n"+
                "🆔 Telegram: @%s\n"+
                "📱 Phone: %s\n"+
                "📊 Status: %s %s\n"+
                "📅 Joined: %s\n\n"+
                "💰 *Agent Stats:*\n"+
                "• Agent Balance: %.2f ETB\n"+
                "• Total Earned: %.2f ETB\n"+
                "• Referrals: %d",
            request.FirstName,
            request.LastName,
            request.Username,
            request.PhoneNumber,
            statusEmoji,
            statusText,
            user.CreatedAt.Format("Jan 2, 2006"),
            user.AgentBalance,
            totalCommission,
            referralCount,
        ),
    )
}

func (b *Bot) revokeAgent(ctx context.Context, chatID int64, userID int64) {
    // Update user in main database
    if err := b.db.Model(&models.User{}).Where("telegram_id = ?", userID).Update("is_agent", false).Error; err != nil {
        b.sendText(ctx, chatID, "❌ Failed to revoke agent status.")
        return
    }

    // Update agent request status
    b.db.Model(&AgentRequest{}).Where("user_id = ?", userID).Update("status", "rejected")

    // Notify user
    b.sendMarkdown(
        ctx,
        userID,
        "❌ *Agent Status Revoked*\n\n"+
            "Your agent status has been revoked by an administrator.",
    )

    b.logAdminAction(ctx, chatID, "revoke_agent", userID, "agent", fmt.Sprintf("Revoked agent status for user %d", userID))

    b.sendMarkdown(ctx, chatID, fmt.Sprintf(
        "❌ *Agent Revoked*\n\nUser %d is no longer an agent.",
        userID,
    ))
}

func (b *Bot) showAgentCommissions(ctx context.Context, chatID int64) {
    var agents []models.User
    b.db.Where("is_agent = ?", true).Order("agent_balance DESC").Limit(10).Find(&agents)

    if len(agents) == 0 {
        b.sendText(ctx, chatID, "📋 No agents found.")
        return
    }

    text := "📊 *Top Agents by Commission*\n\n"
    for i, agent := range agents {
        var referralCount int64
        b.db.Model(&models.User{}).Where("referred_by = ?", agent.ID).Count(&referralCount)
        text += fmt.Sprintf(
            "%d. @%s - %.2f ETB (%d referrals)\n",
            i+1,
            agent.Username,
            agent.AgentBalance,
            referralCount,
        )
    }

    b.sendMarkdown(ctx, chatID, text)
}
// agent.go - Add this function

func (b *Bot) showAllAgents(ctx context.Context, chatID int64) {
    var agents []models.User
    b.db.Where("is_agent = ?", true).Order("created_at DESC").Find(&agents)

    if len(agents) == 0 {
        b.sendText(ctx, chatID, "📋 No agents found.")
        return
    }

    text := "📋 *All Agents*\n\n"
    for i, agent := range agents {
        // Get referral count
        var referralCount int64
        b.db.Model(&models.User{}).Where("referred_by = ?", agent.ID).Count(&referralCount)

        text += fmt.Sprintf(
            "%d. @%s - %.2f ETB (%d referrals)\n",
            i+1,
            agent.Username,
            agent.AgentBalance,
            referralCount,
        )

        // Truncate if too long (Telegram limit ~4096 chars)
        if len(text) > 3500 {
            text += "\n... and more"
            break
        }
    }

    b.sendMarkdown(ctx, chatID, text)
}