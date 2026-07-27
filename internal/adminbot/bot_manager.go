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

// ✅ Default robot settings
var defaultRobotSettings = RobotSettings{
	DesiredCount: 20,
}

// handleBots - Simplified main bot handler
func (b *Bot) handleBots(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 {
		b.showBotStatus(ctx, chatID)
		return
	}

	switch args[0] {
	case "set":
		if len(args) > 1 {
			count, err := strconv.Atoi(args[1])
			if err != nil || count < 0 || count > 100 {
				b.sendText(ctx, chatID, "❌ Invalid count. Use: /bots set <1-100>")
				return
			}
			b.setBotCount(ctx, chatID, count)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /bots set <count>")
		}
	case "status":
		b.showBotStatus(ctx, chatID)
	default:
		b.sendText(ctx, chatID, "❌ Usage: /bots set <count>  or  /bots status")
	}
}

// ✅ showBotStatus - Simple status (no complex stats)
func (b *Bot) showBotStatus(ctx context.Context, chatID int64) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🔴 Panic in showBotStatus: %v", r)
			b.sendText(ctx, chatID, "❌ Error loading status. Please try again.")
		}
	}()

	// Get basic counts
	var totalBots int64
	b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&totalBots)

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
				"👥 *Total Bots:* %d\n"+
				"🎯 *Target Count:* %d\n\n"+
				"💡 Use /bots set <count> to change target",
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

// ✅ setBotCount - Set desired number of bots per game
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

	// ✅ Set the desired count in the bot manager
	botManager.SetDesiredCount(count)

	// Log action
	b.logAdminAction(ctx, chatID, "set_bot_count", 0, "bots", fmt.Sprintf("Set desired bot count to %d", count))

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"✅ *Bot Count Updated*\n\n"+
				"Target bot count per game set to: *%d*\n\n"+
				"📊 Current bots: %d\n"+
				"⚠️ Bots will automatically adjust to reach this target.",
			count,
			b.getCurrentBotCount(),
		),
	)
}

// ✅ getCurrentBotCount - Helper to get current bot count
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

// ✅ getBotStatusText - Get bot status text
func (b *Bot) getBotStatusText() string {
	if b.engine != nil && b.engine.GetBotManager() != nil {
		return "✅ Running"
	}
	return "⏹️ Stopped"
}