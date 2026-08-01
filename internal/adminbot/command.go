package adminbot

import (
	"babibingo/internal/models"
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mymmrac/telego"
)

func (b *Bot) handleMessage(ctx context.Context, msg *telego.Message) {
    if msg.From == nil {
        return
    }

    chatID := msg.Chat.ID
    user := msg.From
    text := msg.Text

    // ✅ Admin check
    if !b.checkAdminAccess(ctx, chatID, user.ID) {
        return
    }
    // ✅ Check if we're waiting for bot count input
	if state, ok := b.tempState.Load(chatID); ok && state == "awaiting_bot_count" {
		b.handleBotCountInput(ctx, chatID, text)
		return
	}
    // Log admin action
    log.Printf("📋 Admin %d (%s): %s", user.ID, user.Username, text)

    // Commands
    if strings.HasPrefix(text, "/") {
        b.handleCommand(ctx, chatID, user, text)
        return
    }

  

     b.handleUserTextInput(ctx, chatID, text)
    
    // Also check for agent text input
    b.handleAgentTextInput(ctx, chatID, text)

      b.sendAdminMenu(ctx, chatID)
    
}

func (b *Bot) handleCommand(ctx context.Context, chatID int64, user *telego.User, text string) {
    parts := strings.Split(strings.TrimPrefix(text, "/"), " ")
    command := parts[0]
    args := parts[1:]

    switch command {
    case "start":
        b.handleStart(ctx, chatID, user)
    case "help":
        b.handleHelp(ctx, chatID)
    case "agents":
        b.handleAgents(ctx, chatID, args)
    case "deposits":
        b.handleDeposits(ctx, chatID, args)
    case "withdrawals":
        b.handleWithdrawals(ctx, chatID, args)
    case "dashboard":  // ✅ Add this case
        b.showDashboard(ctx, chatID)
    case "games":
        b.handleGames(ctx, chatID, args)
    case "bots":
        b.handleBots(ctx, chatID, args)
    case "users":
        b.handleUsers(ctx, chatID, args)
    case "stats":
        b.handleStats(ctx, chatID, args)
    case "/health":
		b.handleHealth(ctx, chatID)
		
	case "/memory":
		b.handleMemory(ctx, chatID)
		
	case "/monitor":
		b.handleMonitor(ctx, chatID)
    case "settings":
        b.handleSettings(ctx, chatID, args)
    default:
        b.sendText(ctx, chatID, "❌ Unknown command. Use /help for available commands.")
    }
}

func (b *Bot) handleStart(ctx context.Context, chatID int64, user *telego.User) {
    b.sendAdminMenu(ctx, chatID)
}
// command.go - Add this function

func (b *Bot) handleHelp(ctx context.Context, chatID int64) {
    b.sendMarkdown(
        ctx,
        chatID,
        "🔐 *Admin Bot Commands*\n\n"+
            "👥 *Agent Management:*\n"+
            "/agents - List all agents\n"+
            "/agents pending - View pending\n"+
            "/agents approve <id> - Approve\n"+
            "/agents reject <id> - Reject\n"+
            "/agents view <id> - View details\n"+
            "/agents revoke <id> - Revoke status\n"+
            "/agents commissions - View commissions\n\n"+
            "💳 *Deposit Management:*\n"+
            "/deposits - View pending\n"+
            "/deposits all - View all\n"+
            "/deposits approve <id> - Approve\n"+
            "/deposits reject <id> - Reject\n"+
            "/deposits search <query> - Search\n\n"+
            "🏧 *Withdrawal Management:*\n"+
            "/withdrawals - View pending\n"+
            "/withdrawals all - View all\n"+
            "/withdrawals approve <id> - Approve\n"+
            "/withdrawals reject <id> - Reject\n\n"+
            "🎱 *Game Monitoring:*\n"+
            "/games - View active\n"+
            "/games current - Current game\n"+
            "/games stats - Game stats\n"+
            "/games end <id> - Force end\n\n"+
            "🤖 *Bot Management:*\n"+
            "/bots - View status\n"+
            "/bots start - Start bots\n"+
            "/bots stop - Stop bots\n"+
            "/bots count - Bot count\n"+
            "/bots speed <n> - Set speed\n"+
            "/bots max <n> - Set max bots\n\n"+
            "👤 *User Management:*\n"+
            "/users - List users\n"+
            "/users search <query> - Search\n"+
            "/users view <id> - View\n"+
            "/users balance <id> <amt> - Adjust\n\n"+
            "📊 *Statistics:*\n"+
            "/stats - Daily stats\n"+
            "/stats weekly - Weekly\n"+
            "/stats revenue - Revenue\n"+
            "/stats agents - Agent report\n"+
            "/stats bots - Bot report\n\n"+
            "⚙️ *Settings:*\n"+
            "/settings - View settings\n"+
            "/settings admins - Manage admins\n"+
            "/settings autoapprove - Toggle\n"+
            "/settings notifications - Toggle",
    )
}
// command.go - Add this function

// ✅ showDashboard - Show admin dashboard with summary
func (b *Bot) showDashboard(ctx context.Context, chatID int64) {
	// Get pending counts
	var pendingAgents int64
	b.db.Model(&AgentRequest{}).Where("status = ?", "pending").Count(&pendingAgents)

	var pendingDeposits int64
	b.db.Model(&models.Transaction{}).Where("type = ? AND status = ?", "deposit", "pending").Count(&pendingDeposits)

	var pendingWithdrawals int64
	b.db.Model(&models.Transaction{}).Where("type = ? AND status = ?", "withdraw", "pending").Count(&pendingWithdrawals)

	var activeGames int64
	b.db.Model(&models.Game{}).Where("status IN (?)", []string{"waiting", "calling"}).Count(&activeGames)

	var totalUsers int64
	b.db.Model(&models.User{}).Count(&totalUsers)

	var totalAgents int64
	b.db.Model(&models.User{}).Where("is_agent = ?", true).Count(&totalAgents)

	// Get today's stats
	today := time.Now().Truncate(24 * time.Hour)
	var todayDeposits float64
	b.db.Model(&models.Transaction{}).
		Where("type = ? AND status = ? AND created_at >= ?", "deposit", "completed", today).
		Select("COALESCE(SUM(amount), 0)").Scan(&todayDeposits)

	var todayWithdrawals float64
	b.db.Model(&models.Transaction{}).
		Where("type = ? AND status = ? AND created_at >= ?", "withdraw", "completed", today).
		Select("COALESCE(SUM(amount), 0)").Scan(&todayWithdrawals)

	msg := telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text: fmt.Sprintf(
			"📊 *Admin Dashboard*\n\n"+
				"🟡 *Pending Actions:*\n"+
				"• 👥 Agents: %d\n"+
				"• 💳 Deposits: %d\n"+
				"• 🏧 Withdrawals: %d\n\n"+
				"🎱 *Active Games:* %d\n\n"+
				"👥 *Users:* %d (🤝 Agents: %d)\n\n"+
				"💰 *Today's Activity:*\n"+
				"• Deposits: %.2f ETB\n"+
				"• Withdrawals: %.2f ETB\n\n"+
				"📋 Use /help for commands",
			pendingAgents,
			pendingDeposits,
			pendingWithdrawals,
			activeGames,
			totalUsers,
			totalAgents,
			todayDeposits,
			todayWithdrawals,
		),
		ParseMode: "Markdown",
		ReplyMarkup: &telego.InlineKeyboardMarkup{
			InlineKeyboard: [][]telego.InlineKeyboardButton{
				{
					{Text: "👥 Agents", CallbackData: "menu_agents"},
					{Text: "💳 Deposits", CallbackData: "menu_deposits"},
				},
				{
					{Text: "🏧 Withdrawals", CallbackData: "menu_withdrawals"},
					{Text: "🎱 Games", CallbackData: "menu_games"},
				},
				{
					{Text: "🤖 Bots", CallbackData: "menu_bots"},
					{Text: "👤 Users", CallbackData: "menu_users"},
				},
				{
					{Text: "📊 Stats", CallbackData: "menu_stats"},
					{Text: "⚙️ Settings", CallbackData: "menu_settings"},
				},
			},
		},
	}

	b.sendMessage(ctx, &msg)
}
func (b *Bot) handleHealth(ctx context.Context, chatID int64) {
	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	b.sendMarkdown(ctx, chatID, "🏥 *Checking Engine Health...*")

	health := b.engine.HealthCheck()
	
	statusEmoji := "✅"
	if health["status"] != "healthy" {
		statusEmoji = "❌"
	}

	text := fmt.Sprintf(
		"🏥 *Engine Health*\n\n"+
		"Status: %s %s\n"+
		"Goroutines: %d\n"+
		"Clients: %d\n"+
		"Has Game: %v\n"+
		"DB Status: %s\n",
		statusEmoji,
		health["status"],
		health["goroutines"],
		health["clients"],
		health["has_game"],
		health["db_status"],
	)

	if health["has_game"].(bool) {
		text += fmt.Sprintf(
			"\n📊 *Game Details*\n"+
			"Game ID: %s\n"+
			"Status: %s\n"+
			"Players: %d\n",
			health["game_id"],
			health["game_status"],
			health["players"],
		)
	}

	keyboard := [][]telego.InlineKeyboardButton{
		{
			{Text: "🔄 Refresh", CallbackData: "health_refresh"},
			{Text: "🔙 Back", CallbackData: "back_to_menu"},
		},
	}

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}

// ============ MEMORY COMMAND ============

func (b *Bot) handleMemory(ctx context.Context, chatID int64) {
	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	b.sendMarkdown(ctx, chatID, "💾 *Checking Memory Usage...*")

	mem := b.engine.GetMemoryStats()

	// Calculate used memory percentage
	usedPercent := float64(mem["alloc"].(int)) / float64(mem["sys"].(int)) * 100

	// Status indicators
	memStatus := "🟢"
	if usedPercent > 80 {
		memStatus = "🔴"
	} else if usedPercent > 60 {
		memStatus = "🟡"
	}

	text := fmt.Sprintf(
		"💾 *Memory Statistics*\n\n"+
		"Status: %s %.1f%% used\n\n"+
		"📊 *Memory Usage*\n"+
		"• Allocated: %d MB\n"+
		"• Total Allocated: %d MB\n"+
		"• System Memory: %d MB\n"+
		"• GC Cycles: %d\n"+
		"• Goroutines: %d\n\n"+
		"📈 *Health Indicators*\n"+
		"• Heap Alloc: %.2f MB\n"+
		"• Stack Inuse: %.2f MB\n"+
		"• MSpan Inuse: %.2f MB\n"+
		"• MCache Inuse: %.2f MB\n",
		memStatus,
		usedPercent,
		mem["alloc"],
		mem["total_alloc"],
		mem["sys"],
		mem["num_gc"],
		mem["goroutines"],
		float64(mem["alloc"].(int))*0.001,
		float64(mem["sys"].(int))*0.001,
		float64(mem["sys"].(int))*0.001,
		float64(mem["sys"].(int))*0.001,
	)

	// Warning if memory is high
	if usedPercent > 80 {
		text += "\n⚠️ *High Memory Usage Detected!*\n" +
			"Consider restarting the bot if performance degrades."
	}

	keyboard := [][]telego.InlineKeyboardButton{
		{
			{Text: "🔄 Refresh", CallbackData: "memory_refresh"},
			{Text: "🔙 Back", CallbackData: "back_to_menu"},
		},
	}

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}

// ============ MONITOR COMMAND ============

func (b *Bot) handleMonitor(ctx context.Context, chatID int64) {
	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	b.sendMarkdown(ctx, chatID, "📊 *Fetching Monitoring Data...*")

	health := b.engine.HealthCheck()
	mem := b.engine.GetMemoryStats()
	gameStats := b.engine.GetGameStats()

	// Get database stats
	var userCount int64
	var gameCount int64
	var transactionCount int64
	
	b.db.Model(&models.User{}).Count(&userCount)
	b.db.Model(&models.Game{}).Count(&gameCount)
	b.db.Model(&models.Transaction{}).Count(&transactionCount)

	text := fmt.Sprintf(
		"📊 *System Monitor*\n\n"+
		"🏥 *Engine Health*\n"+
		"• Status: %s\n"+
		"• Goroutines: %d\n"+
		"• Clients: %d\n"+
		"• DB Status: %s\n\n"+
		"💾 *Memory Usage*\n"+
		"• Allocated: %d MB\n"+
		"• Total Alloc: %d MB\n"+
		"• System: %d MB\n"+
		"• GC Cycles: %d\n\n"+
		"🎮 *Game Stats*\n"+
		"• Active Game: %v\n"+
		"• Total Games: %d\n"+
		"• Total Users: %d\n"+
		"• Total Transactions: %d\n",
		health["status"],
		health["goroutines"],
		health["clients"],
		health["db_status"],
		mem["alloc"],
		mem["total_alloc"],
		mem["sys"],
		mem["num_gc"],
		health["has_game"],
		gameCount,
		userCount,
		transactionCount,
	)

	if health["has_game"].(bool) {
		text += fmt.Sprintf(
			"\n📊 *Current Game*\n"+
			"• Status: %s\n"+
			"• Players: %d\n"+
			"• Pool: %.2f ETB\n"+
			"• Called: %d/75\n"+
			"• Bots: %d\n",
			gameStats["status"],
			gameStats["players"],
			gameStats["total_pool"],
			gameStats["called_numbers"],
			gameStats["bot_count"],
		)
	}

	// Add timestamp
	text += fmt.Sprintf("\n🕐 *Last Updated:* %s", time.Now().Format("15:04:05"))

	keyboard := [][]telego.InlineKeyboardButton{
		{
			{Text: "🔄 Refresh", CallbackData: "monitor_refresh"},
			{Text: "🔙 Back", CallbackData: "back_to_menu"},
		},
	}

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}