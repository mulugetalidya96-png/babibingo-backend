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

// handleBots - With debug logging
func (b *Bot) handleBots(ctx context.Context, chatID int64, args []string) {
	log.Printf("🟢 handleBots called with args: %v", args)
	
	if len(args) == 0 {
		log.Printf("🟢 No args, showing status")
		b.showBotStatus(ctx, chatID)
		return
	}

	switch args[0] {
	case "set":
		log.Printf("🟢 Set command detected")
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
		log.Printf("🟢 Status command detected")
		b.showBotStatus(ctx, chatID)
	default:
		log.Printf("🟢 Unknown command: %s", args[0])
		b.sendText(ctx, chatID, "❌ Usage: /bots set <count>  or  /bots status")
	}
}

// ✅ showBotStatus - With extensive debug logging
func (b *Bot) showBotStatus(ctx context.Context, chatID int64) {
	log.Printf("🔵 showBotStatus: STARTED")
	
	defer func() {
		if r := recover(); r != nil {
			log.Printf("🔴 PANIC in showBotStatus: %v", r)
			b.sendText(ctx, chatID, fmt.Sprintf("❌ Error loading status: %v", r))
		}
	}()

	log.Printf("🔵 showBotStatus: Getting total bots from DB...")
	var totalBots int64
	if err := b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&totalBots).Error; err != nil {
		log.Printf("🔴 DB Error: %v", err)
		b.sendText(ctx, chatID, "❌ Database error")
		return
	}
	log.Printf("🔵 showBotStatus: Total bots = %d", totalBots)

	log.Printf("🔵 showBotStatus: Getting desired count...")
	desiredCount := b.getDesiredBotCount()
	log.Printf("🔵 showBotStatus: Desired count = %d", desiredCount)

	log.Printf("🔵 showBotStatus: Checking if bot manager is running...")
	isRunning := false
	if b.engine != nil {
		log.Printf("🔵 showBotStatus: engine is not nil")
		botManager := b.engine.GetBotManager()
		if botManager != nil {
			log.Printf("🔵 showBotStatus: botManager is not nil")
			isRunning = true
		} else {
			log.Printf("🔵 showBotStatus: botManager is nil")
		}
	} else {
		log.Printf("🔵 showBotStatus: engine is nil")
	}

	statusEmoji := "✅"
	statusText := "Running"
	if !isRunning {
		statusEmoji = "⏹️"
		statusText = "Stopped"
	}
	log.Printf("🔵 showBotStatus: Status = %s %s", statusEmoji, statusText)

	log.Printf("🔵 showBotStatus: Building message...")
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

	log.Printf("🔵 showBotStatus: Sending message...")
	b.sendMessage(ctx, &msg)
	log.Printf("🔵 showBotStatus: COMPLETED SUCCESSFULLY")
}

// ✅ setBotCount - With debug logging
func (b *Bot) setBotCount(ctx context.Context, chatID int64, count int) {
	log.Printf("🟡 setBotCount: count=%d", count)
	
	if b.engine == nil {
		log.Printf("🔴 setBotCount: engine is nil")
		b.sendText(ctx, chatID, "❌ Game engine not available.")
		return
	}

	log.Printf("🟡 setBotCount: Getting bot manager...")
	botManager := b.engine.GetBotManager()
	if botManager == nil {
		log.Printf("🔴 setBotCount: botManager is nil")
		b.sendText(ctx, chatID, "❌ Bot manager not available.")
		return
	}

	log.Printf("🟡 setBotCount: Setting desired count...")
	botManager.SetDesiredCount(count)

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
	log.Printf("🟡 setBotCount: COMPLETED")
}

// ✅ getCurrentBotCount
func (b *Bot) getCurrentBotCount() int {
	var count int64
	b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&count)
	return int(count)
}

// ✅ getDesiredBotCount - With debug logging
func (b *Bot) getDesiredBotCount() int {
	log.Printf("🟣 getDesiredBotCount: Called")
	
	if b.engine != nil {
		log.Printf("🟣 getDesiredBotCount: engine is not nil")
		botManager := b.engine.GetBotManager()
		if botManager != nil {
			log.Printf("🟣 getDesiredBotCount: botManager is not nil")
			count := botManager.GetDesiredCount()
			log.Printf("🟣 getDesiredBotCount: count = %d", count)
			return count
		}
		log.Printf("🟣 getDesiredBotCount: botManager is nil")
	} else {
		log.Printf("🟣 getDesiredBotCount: engine is nil")
	}
	
	log.Printf("🟣 getDesiredBotCount: Returning default: %d", defaultRobotSettings.DesiredCount)
	return defaultRobotSettings.DesiredCount
}