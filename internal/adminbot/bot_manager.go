package adminbot

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"babibingo/internal/models"

	"github.com/mymmrac/telego"
)

// BotStats - Statistics structure
type BotStats struct {
	IsRunning          bool
	TotalBots          int
	ActiveBots         int
	ActiveInGame       int
	CardsReserved      int
	CardsHeld          int
	GamesWon           int
	GamesPlayed        int
	WinRate            float64
	TotalStaked        float64
	TotalWon           float64
	BotsPerTick        int
	MaxBotsPerGame     int
	ReserveInterval    int
	TodayReserved      int
	NewBotsToday       int
	TotalBotsCreated   int
	DesiredCount       int // ✅ New: desired bot count per game
}

// handleBots - Main bot handler
func (b *Bot) handleBots(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 {
		b.showBotStatus(ctx, chatID)
		return
	}

	switch args[0] {
	case "start":
		b.startBots(ctx, chatID)
	case "stop":
		b.stopBots(ctx, chatID)
	case "status":
		b.showBotStatus(ctx, chatID)
	case "count":
		b.showBotCount(ctx, chatID)
	case "speed":
		if len(args) > 1 {
			speed, _ := strconv.Atoi(args[1])
			b.setBotSpeed(ctx, chatID, speed)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /bots speed <1-10>")
		}
	case "max":
		if len(args) > 1 {
			max, _ := strconv.Atoi(args[1])
			b.setMaxBots(ctx, chatID, max)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /bots max <5-100>")
		}
	case "set":
		if len(args) > 1 {
			count, _ := strconv.Atoi(args[1])
			b.setBotCount(ctx, chatID, count)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /bots set <count>")
		}
	case "reset":
		b.resetBots(ctx, chatID)
	case "stats":
		b.showDetailedBotStats(ctx, chatID)
	default:
		b.sendText(ctx, chatID, "❌ Usage: /bots [start|stop|status|count|speed <n>|max <n>|set <count>|reset|stats]")
	}
}

// ✅ getBotStats - Get real bot stats from database and game engine
func (b *Bot) getBotStats() BotStats {
	stats := BotStats{}

	// Get total bots
	var totalBots int64
	b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&totalBots)
	stats.TotalBots = int(totalBots)

	// Get active bots (last hour)
	var activeBots int64
	b.db.Model(&models.User{}).Where("is_bot = ? AND last_active > ?", true, time.Now().Add(-1*time.Hour)).Count(&activeBots)
	stats.ActiveBots = int(activeBots)

	// Get cards reserved by bots
	var cardsReserved int64
	b.db.Model(&models.Card{}).Where("user_id IN (?)", 
		b.db.Table("users").Select("id").Where("is_bot = ?", true),
	).Count(&cardsReserved)
	stats.CardsReserved = int(cardsReserved)

	// Get bot wins
	var botWins int64
	b.db.Model(&models.Card{}).Where("is_winner = ? AND user_id IN (?)", true,
		b.db.Table("users").Select("id").Where("is_bot = ?", true),
	).Count(&botWins)
	stats.GamesWon = int(botWins)

	// Get total bot games
	var botGames int64
	b.db.Model(&models.GamePlayer{}).Where("user_id IN (?)", 
		b.db.Table("users").Select("id").Where("is_bot = ?", true),
	).Count(&botGames)
	stats.GamesPlayed = int(botGames)

	if stats.GamesPlayed > 0 {
		stats.WinRate = float64(stats.GamesWon) / float64(stats.GamesPlayed) * 100
	}

	// Get today's reserved
	today := time.Now().Truncate(24 * time.Hour)
	var todayReserved int64
	b.db.Model(&models.Card{}).Where("user_id IN (?) AND created_at >= ?", 
		b.db.Table("users").Select("id").Where("is_bot = ?", true),
		today,
	).Count(&todayReserved)
	stats.TodayReserved = int(todayReserved)

	// Total bots created
	stats.TotalBotsCreated = int(totalBots)

	// Get financial stats
	b.db.Model(&models.Transaction{}).
		Where("user_id IN (?) AND type = ? AND status = ?", 
			b.db.Table("users").Select("id").Where("is_bot = ?", true),
			"stake", "completed",
		).
		Select("COALESCE(SUM(amount), 0)").Scan(&stats.TotalStaked)

	b.db.Model(&models.Transaction{}).
		Where("user_id IN (?) AND type = ? AND status = ?", 
			b.db.Table("users").Select("id").Where("is_bot = ?", true),
			"win", "completed",
		).
		Select("COALESCE(SUM(amount), 0)").Scan(&stats.TotalWon)

	// ✅ Get desired count from bot manager
	if b.engine != nil && b.engine.GetBotManager() != nil {
		stats.DesiredCount = b.engine.GetBotManager().GetDesiredCount()
	}

	// ✅ Active in game
	if b.engine != nil {
		currentGame, _, _, _, _, _, err := b.engine.GetCurrentGame()
		if err == nil && currentGame != nil {
			var botPlayers int64
			b.db.Model(&models.GamePlayer{}).
				Where("game_id = ? AND user_id IN (?)", currentGame.ID,
					b.db.Table("users").Select("id").Where("is_bot = ?", true),
				).
				Count(&botPlayers)
			stats.ActiveInGame = int(botPlayers)
		}
	}

	// Get settings from config
	stats.BotsPerTick = b.botSettings.Speed
	stats.MaxBotsPerGame = b.botSettings.MaxBots
	stats.ReserveInterval = 3

	return stats
}

// ✅ showBotStatus - Show real bot status with count controls
func (b *Bot) showBotStatus(ctx context.Context, chatID int64) {
	stats := b.getBotStats()

	statusEmoji := "✅"
	statusText := "Running"
	if !stats.IsRunning {
		statusEmoji = "⏹️"
		statusText = "Stopped"
	}

	msg := telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text: fmt.Sprintf(
			"🤖 *Bot Manager*\n\n"+
				"📊 *Status:* %s %s\n"+
				"👥 *Total Bots:* %d\n"+
				"🎯 *Target Count:* %d\n"+
				"🟢 *Active Bots:* %d\n"+
				"🔴 *Inactive Bots:* %d\n"+
				"🃏 *Cards Reserved:* %d\n"+
				"🏆 *Games Won:* %d\n"+
				"📈 *Win Rate:* %.1f%%\n\n"+
				"⚙️ *Settings:*\n"+
				"• Bots per tick: %d\n"+
				"• Max bots/game: %d\n"+
				"• Interval: %ds\n\n"+
				"📅 *Today's Activity:*\n"+
				"• Reserved: %d cards\n\n"+
				"⏱️ Uptime: %s\n\n"+
				"💡 Use /bots set <count> to set desired bot count",
			statusEmoji,
			statusText,
			stats.TotalBots,
			stats.DesiredCount,
			stats.ActiveBots,
			stats.TotalBots-stats.ActiveBots,
			stats.CardsReserved,
			stats.GamesWon,
			stats.WinRate,
			stats.BotsPerTick,
			stats.MaxBotsPerGame,
			stats.ReserveInterval,
			stats.TodayReserved,
			b.getUptime(),
		),
		ParseMode: "Markdown",
		ReplyMarkup: &telego.InlineKeyboardMarkup{
			InlineKeyboard: [][]telego.InlineKeyboardButton{
				{
					{
						Text:         "▶️ Start",
						CallbackData: "bots_start",
					},
					{
						Text:         "⏹️ Stop",
						CallbackData: "bots_stop",
					},
				},
				{
					{
						Text:         "📊 Stats",
						CallbackData: "bots_stats",
					},
					{
						Text:         "🔄 Reset",
						CallbackData: "bots_reset",
					},
				},
				{
					{
						Text:         "➕ Add 5 Bots",
						CallbackData: "bots_add_5",
					},
					{
						Text:         "➖ Remove 5 Bots",
						CallbackData: "bots_remove_5",
					},
				},
				{
					{
						Text:         "⚙️ Settings",
						CallbackData: "bots_settings",
					},
				},
			},
		},
	}

	b.sendMessage(ctx, &msg)
}

// ✅ setBotCount - Set desired number of bots per game
func (b *Bot) setBotCount(ctx context.Context, chatID int64, count int) {
	if count < 0 || count > 100 {
		b.sendText(ctx, chatID, "❌ Bot count must be between 0 and 100.")
		return
	}

	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	botManager := b.engine.GetBotManager()
	if botManager == nil {
		b.sendText(ctx, chatID, "❌ Bot manager not available.")
		return
	}

	// ✅ Set the desired count in the bot manager
	botManager.SetDesiredCount(count)

	// Update local settings
	b.botSettings.DesiredCount = count

	// Log action
	b.logAdminAction(ctx, chatID, "set_bot_count", 0, "bots", fmt.Sprintf("Set desired bot count to %d", count))

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"🎯 *Bot Count Updated*\n\n"+
				"Desired bot count per game set to: *%d*\n\n"+
				"📊 Current bots: %d\n"+
				"📈 Bots to add/remove: %d\n\n"+
				"⚠️ Bots will automatically adjust to reach this target.",
			count,
			b.getCurrentBotCount(),
			count-b.getCurrentBotCount(),
		),
	)
}

// ✅ addBots - Add a specific number of bots
func (b *Bot) addBots(ctx context.Context, chatID int64, count int) {
	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	botManager := b.engine.GetBotManager()
	if botManager == nil {
		b.sendText(ctx, chatID, "❌ Bot manager not available.")
		return
	}

	// Reserve cards for bots
	botManager.ReserveCardsForBots(count)

	// Log action
	b.logAdminAction(ctx, chatID, "add_bots", 0, "bots", fmt.Sprintf("Added %d bots", count))

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"➕ *Bots Added*\n\n"+
				"Added *%d* new bots.\n\n"+
				"📊 New total: %d bots\n"+
				"🃏 Cards reserved: %d\n\n"+
				"Use /bots status to view updated stats.",
			count,
			b.getCurrentBotCount(),
			b.getBotCardCount(),
		),
	)
}

// ✅ removeBots - Remove a specific number of bots
func (b *Bot) removeBots(ctx context.Context, chatID int64, count int) {
	currentCount := b.getCurrentBotCount()
	if count > currentCount {
		count = currentCount
		b.sendText(ctx, chatID, fmt.Sprintf("⚠️ Only %d bots available to remove.", currentCount))
	}

	if count <= 0 {
		b.sendText(ctx, chatID, "❌ No bots to remove.")
		return
	}

	// Find bots to remove (oldest inactive bots first)
	var bots []models.User
	b.db.Where("is_bot = ?", true).
		Order("last_active ASC").
		Limit(count).
		Find(&bots)

	if len(bots) == 0 {
		b.sendText(ctx, chatID, "❌ No bots found to remove.")
		return
	}

	// Delete the bots
	for _, bot := range bots {
		b.db.Delete(&bot)
	}

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"➖ *Bots Removed*\n\n"+
				"Removed *%d* bots.\n\n"+
				"📊 New total: %d bots\n\n"+
				"Use /bots status to view updated stats.",
			len(bots),
			b.getCurrentBotCount(),
		),
	)
}

// ✅ showBotCount - Show bot count details
func (b *Bot) showBotCount(ctx context.Context, chatID int64) {
	stats := b.getBotStats()

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"📊 *Bot Count Details*\n\n"+
				"👥 *Total Bot Users:* %d\n"+
				"🎯 *Target Count:* %d\n"+
				"🟢 *Active in Game:* %d\n"+
				"💤 *Idle Bots:* %d\n"+
				"🃏 *Cards Held:* %d\n\n"+
				"📈 *Today:*\n"+
				"• Cards Reserved: %d\n"+
				"• New Bots: %d",
			stats.TotalBots,
			stats.DesiredCount,
			stats.ActiveInGame,
			stats.TotalBots-stats.ActiveInGame,
			stats.CardsReserved,
			stats.TodayReserved,
			stats.NewBotsToday,
		),
	)
}

// ✅ Helper methods for bot count management
func (b *Bot) getCurrentBotCount() int {
	var count int64
	b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&count)
	return int(count)
}

func (b *Bot) getBotCardCount() int {
	var count int64
	b.db.Model(&models.Card{}).Where("user_id IN (?)", 
		b.db.Table("users").Select("id").Where("is_bot = ?", true),
	).Count(&count)
	return int(count)
}

func (b *Bot) getDesiredBotCount() int {
	if b.engine != nil && b.engine.GetBotManager() != nil {
		return b.engine.GetBotManager().GetDesiredCount()
	}
	return b.botSettings.DesiredCount
}

// ✅ BotSettings structure
type BotSettings struct {
	DesiredCount int
	Speed        int
	MaxBots      int
	AutoApprove  bool
}

var defaultBotSettings = BotSettings{
	DesiredCount: 20,
	Speed:        2,
	MaxBots:      50,
}

// ✅ startBots - Start bot routine
func (b *Bot) startBots(ctx context.Context, chatID int64) {
	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	botManager := b.engine.GetBotManager()
	if botManager == nil {
		b.sendText(ctx, chatID, "❌ Bot manager not available.")
		return
	}

	// Check if already running
	if b.isBotRunning() {
		b.sendMarkdown(ctx, chatID, "⚠️ *Bots Already Running*\n\nBot routine is already active.\n\nUse /bots stop to stop them.")
		return
	}

	// Start bot routine
	botManager.StartBotRoutine()

	// Log action
	b.logAdminAction(ctx, chatID, "start_bots", 0, "bots", "Started bot routine")

	b.sendMarkdown(ctx, chatID, fmt.Sprintf(
		"✅ *Bots Started*\n\n"+
			"🤖 Bot routine has been started.\n\n"+
			"📊 Bots will now automatically reserve cards.\n"+
			"🎯 Target bot count: %d\n"+
			"Use /bots status to monitor.",
		b.getDesiredBotCount(),
	))
}

// ✅ stopBots - Stop bot routine
func (b *Bot) stopBots(ctx context.Context, chatID int64) {
	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	botManager := b.engine.GetBotManager()
	if botManager == nil {
		b.sendText(ctx, chatID, "❌ Bot manager not available.")
		return
	}

	// Check if already stopped
	if !b.isBotRunning() {
		b.sendMarkdown(ctx, chatID, "⚠️ *Bots Already Stopped*\n\nBot routine is already stopped.")
		return
	}

	// Stop bot routine
	botManager.StopBotRoutine()

	// Log action
	b.logAdminAction(ctx, chatID, "stop_bots", 0, "bots", "Stopped bot routine")

	b.sendMarkdown(
		ctx,
		chatID,
		"⏹️ *Bots Stopped*\n\n"+
			"🤖 Bot routine has been stopped.\n\n"+
			"📊 No new bots will be created.\n\n"+
			"Use /bots start to resume.",
	)
}

// ✅ setBotSpeed - Set bot speed
func (b *Bot) setBotSpeed(ctx context.Context, chatID int64, speed int) {
	if speed < 1 || speed > 10 {
		b.sendText(ctx, chatID, "❌ Speed must be between 1 and 10.\n\n💡 1 = Slow, 10 = Fast")
		return
	}

	// Update speed in config/settings
	b.botSettings.Speed = speed

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"⚡ *Bot Speed Updated*\n\n"+
				"Bots per tick set to: *%d*\n\n"+
				"📊 Estimated bots per minute: %d\n",
			speed,
			speed*20,
		),
	)

	b.logAdminAction(ctx, chatID, "set_bot_speed", 0, "bots", fmt.Sprintf("Set bot speed to %d", speed))
}

// ✅ setMaxBots - Set max bots
func (b *Bot) setMaxBots(ctx context.Context, chatID int64, max int) {
	if max < 5 || max > 100 {
		b.sendText(ctx, chatID, "❌ Max bots must be between 5 and 100.\n\n💡 Recommended: 30-50 for optimal gameplay")
		return
	}

	b.botSettings.MaxBots = max

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"🎯 *Max Bots Updated*\n\n"+
				"Max bots per game set to: *%d*\n",
			max,
		),
	)

	b.logAdminAction(ctx, chatID, "set_max_bots", 0, "bots", fmt.Sprintf("Set max bots to %d", max))
}

// ✅ resetBots - Reset all bots
func (b *Bot) resetBots(ctx context.Context, chatID int64) {
	msg := telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text: "🔄 *Reset Bots*\n\n" +
			"Are you sure you want to reset all bot data?\n\n" +
			"⚠️ This will:\n" +
			"• Stop bot routine\n" +
			"• Remove all bot users\n" +
			"• Reset bot statistics\n\n" +
			"This action cannot be undone!",
		ParseMode: "Markdown",
		ReplyMarkup: &telego.InlineKeyboardMarkup{
			InlineKeyboard: [][]telego.InlineKeyboardButton{
				{
					{
						Text:         "✅ Yes, Reset All",
						CallbackData: "bots_reset_confirm",
					},
					{
						Text:         "❌ Cancel",
						CallbackData: "bots_reset_cancel",
					},
				},
			},
		},
	}

	b.sendMessage(ctx, &msg)
}

// ✅ isBotRunning - Check if bot routine is running
func (b *Bot) isBotRunning() bool {
	if b.engine == nil {
		return false
	}
	botManager := b.engine.GetBotManager()
	if botManager == nil {
		return false
	}
	return true
}

// ✅ showDetailedBotStats - Show detailed stats
func (b *Bot) showDetailedBotStats(ctx context.Context, chatID int64) {
	stats := b.getBotStats()

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"📊 *Detailed Bot Statistics*\n\n"+
				"📈 *Overall:*\n"+
				"• Total Bots Created: %d\n"+
				"• Cards Reserved: %d\n"+
				"• Games Played: %d\n"+
				"• Games Won: %d\n"+
				"• Win Rate: %.2f%%\n\n"+
				"💰 *Financial:*\n"+
				"• Total Staked: %.2f ETB\n"+
				"• Total Won: %.2f ETB\n"+
				"• Net Loss: %.2f ETB",
			stats.TotalBotsCreated,
			stats.CardsReserved,
			stats.GamesPlayed,
			stats.GamesWon,
			stats.WinRate,
			stats.TotalStaked,
			stats.TotalWon,
			stats.TotalStaked-stats.TotalWon,
		),
	)
}



// ✅ showBotSettings - Show bot settings
func (b *Bot) showBotSettings(ctx context.Context, chatID int64) {
	desiredCount := b.getDesiredBotCount()

	msg := telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text: fmt.Sprintf(
			"⚙️ *Bot Settings*\n\n"+
				"🤖 *General:*\n"+
				"• Status: %s\n"+
				"• Target Count: *%d* bots\n"+
				"• Speed: %d bots/tick\n"+
				"• Max Bots: %d\n"+
				"• Interval: 3s\n\n"+
				"📊 *Current:*\n"+
				"• Active Bots: %d\n"+
				"• Total Bots: %d\n\n"+
				"💡 *Commands:*\n"+
				"/bots set <count> - Set target bot count\n"+
				"/bots speed <n> - Change speed (1-10)\n"+
				"/bots max <n> - Change max bots (5-100)",
			b.getBotStatusText(),
			desiredCount,
			b.botSettings.Speed,
			b.botSettings.MaxBots,
			b.getCurrentBotCount(),
			b.getTotalBotCount(),
		),
		ParseMode: "Markdown",
		ReplyMarkup: &telego.InlineKeyboardMarkup{
			InlineKeyboard: [][]telego.InlineKeyboardButton{
				{
					{Text: "⬆️ Speed +1", CallbackData: "bots_speed_up"},
					{Text: "⬇️ Speed -1", CallbackData: "bots_speed_down"},
				},
				{
					{Text: "⬆️ Max +5", CallbackData: "bots_max_up"},
					{Text: "⬇️ Max -5", CallbackData: "bots_max_down"},
				},
				{
					{Text: "➕ Add 5 Bots", CallbackData: "bots_add_5"},
					{Text: "➖ Remove 5 Bots", CallbackData: "bots_remove_5"},
				},
				{
					{Text: "🎯 Set Count", CallbackData: "bots_set_count"},
				},
				{
					{Text: "🔙 Back", CallbackData: "bots_back"},
				},
			},
		},
	}

	b.sendMessage(ctx, &msg)
}

// ✅ getBotStatusText - Get bot status text
func (b *Bot) getBotStatusText() string {
	if b.isBotRunning() {
		return "✅ Running"
	}
	return "⏹️ Stopped"
}

// ✅ getTotalBotCount - Helper to get total bot count
func (b *Bot) getTotalBotCount() int {
	var count int64
	b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&count)
	return int(count)
}

// ✅ getBotSettings - Get current bot settings
