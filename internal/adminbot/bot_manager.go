package adminbot

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"babibingo/internal/models"

	"github.com/mymmrac/telego"
)

// ✅ RobotSettings - Simple robot settings
type RobotSettings struct {
	DesiredCount int
}

var defaultRobotSettings = RobotSettings{
	DesiredCount: 20,
}

// handleBots - Main bot handler
func (b *Bot) handleBots(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 {
		b.showBotStatus(ctx, chatID)
		return
	}

	switch args[0] {
	case "set":
		if len(args) > 1 {
			count, err := strconv.Atoi(args[1])
			if err != nil || count < 1 || count > 100 {
				b.sendText(ctx, chatID, "❌ Invalid count. Please enter a number between 1 and 100.\n\nExample: /bots set 30")
				return
			}
			b.setBotCount(ctx, chatID, count)
		} else {
			b.sendText(ctx, chatID, "📝 Please enter the desired bot count (1-100):\n\nExample: /bots set 30")
		}
	case "status":
		b.showBotStatus(ctx, chatID)
	default:
		b.sendText(ctx, chatID, "❌ Unknown command.\n\nAvailable:\n/bots status - Show bot status\n/bots set <count> - Set bot count (1-100)")
	}
}

// ✅ showBotStatus - With interactive buttons
func (b *Bot) showBotStatus(ctx context.Context, chatID int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🔴 Panic in showBotStatus: %v", r)
			b.sendText(ctx, chatID, "❌ Error loading status. Please try again.")
		}
	}()

	// Get total bots
	var totalBots int64
	if err := b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&totalBots).Error; err != nil {
		log.Printf("DB Error: %v", err)
		b.sendText(ctx, chatID, "❌ Database error")
		return
	}

	desiredCount := b.getDesiredBotCount()

	// Check if bot manager is running
	isRunning := false
	if b.engine != nil && b.engine.GetBotManager() != nil {
		isRunning = true
	}

	statusEmoji := "✅"
	statusText := "Running"
	if !isRunning {
		statusEmoji = "⏹️"
		statusText = "Stopped"
	}

	msg := telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text: fmt.Sprintf(
			"🤖 *Bot Manager*\n\n"+
				"📊 *Status:* %s %s\n"+
				"👥 *Current Bots:* %d\n"+
				"🎯 *Target Count:* %d\n\n"+
				"Select an option below:",
			statusEmoji,
			statusText,
			totalBots,
			desiredCount,
		),
		ParseMode: "Markdown",
		ReplyMarkup: &telego.InlineKeyboardMarkup{
			InlineKeyboard: [][]telego.InlineKeyboardButton{
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
						Text:         "➕ Add 10 Bots",
						CallbackData: "bots_add_10",
					},
					{
						Text:         "➖ Remove 10 Bots",
						CallbackData: "bots_remove_10",
					},
				},
				{
					{
						Text:         "🎯 Set Count",
						CallbackData: "bots_set_prompt",
					},
				},
				{
					{
						Text:         "🔄 Refresh",
						CallbackData: "bots_status",
					},
				},
				{
					{
						Text:         "🔙 Back",
						CallbackData: "back_to_menu",
					},
				},
			},
		},
	}

	b.sendMessage(ctx, &msg)
}

// ✅ setBotCount - Set desired number of bots
func (b *Bot) setBotCount(ctx context.Context, chatID int64, count int) {
	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	botManager := b.engine.GetBotManager()
	if botManager == nil {
		b.sendText(ctx, chatID, "❌ Bot manager not available.")
		return
	}

	// Validate count
	if count < 1 {
		count = 1
	}
	if count > 100 {
		count = 100
	}

	botManager.SetDesiredCount(count)

	b.logAdminAction(ctx, chatID, "set_bot_count", 0, "bots", fmt.Sprintf("Set desired bot count to %d", count))

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"✅ *Bot Count Updated*\n\n"+
				"🎯 Target bot count per game set to: *%d*\n\n"+
				"📊 Current bots: %d\n"+
				"⚠️ Bots will automatically adjust to reach this target.",
			count,
			b.getCurrentBotCount(),
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

	currentCount := b.getCurrentBotCount()
	newCount := currentCount + count
	if newCount > 100 {
		newCount = 100
		b.sendText(ctx, chatID, fmt.Sprintf("⚠️ Max bots is 100. Setting to 100."))
	}

	botManager.SetDesiredCount(newCount)

	b.logAdminAction(ctx, chatID, "add_bots", 0, "bots", fmt.Sprintf("Added %d bots, new count: %d", count, newCount))

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"➕ *Bots Added*\n\n"+
				"Added *%d* bots.\n\n"+
				"🎯 New target: *%d* bots\n"+
				"📊 Current bots: %d",
			count,
			newCount,
			b.getCurrentBotCount(),
		),
	)
}

// ✅ removeBots - Remove a specific number of bots
func (b *Bot) removeBots(ctx context.Context, chatID int64, count int) {
	if b.engine == nil {
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	botManager := b.engine.GetBotManager()
	if botManager == nil {
		b.sendText(ctx, chatID, "❌ Bot manager not available.")
		return
	}

	currentCount := b.getCurrentBotCount()
	newCount := currentCount - count
	if newCount < 1 {
		newCount = 1
		b.sendText(ctx, chatID, fmt.Sprintf("⚠️ Minimum bots is 1. Setting to 1."))
	}

	botManager.SetDesiredCount(newCount)

	b.logAdminAction(ctx, chatID, "remove_bots", 0, "bots", fmt.Sprintf("Removed %d bots, new count: %d", count, newCount))

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"➖ *Bots Removed*\n\n"+
				"Removed *%d* bots.\n\n"+
				"🎯 New target: *%d* bots\n"+
				"📊 Current bots: %d",
			count,
			newCount,
			b.getCurrentBotCount(),
		),
	)
}

// ✅ getCurrentBotCount - Get current bot count
func (b *Bot) getCurrentBotCount() int {
	var count int64
	b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&count)
	return int(count)
}

// ✅ getDesiredBotCount - Get desired bot count
func (b *Bot) getDesiredBotCount() int {
	if b.engine != nil && b.engine.GetBotManager() != nil {
		return b.engine.GetBotManager().GetDesiredCount()
	}
	return defaultRobotSettings.DesiredCount
}