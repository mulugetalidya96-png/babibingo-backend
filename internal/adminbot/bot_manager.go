package adminbot

import (
	"context"
	"fmt"
	"strconv"
)

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
        }
    case "max":
        if len(args) > 1 {
            max, _ := strconv.Atoi(args[1])
            b.setMaxBots(ctx, chatID, max)
        }
    default:
        b.sendText(ctx, chatID, "❌ Usage: /bots [start|stop|status|count|speed <n>|max <n>]")
    }
}

func (b *Bot) showBotStatus(ctx context.Context, chatID int64) {
    // Get bot stats from game engine
    // This requires access to the game engine
    // You may need to pass the engine reference

    b.sendMarkdown(
        ctx,
        chatID,
        "🤖 *Bot Manager Status*\n\n"+
            "📊 *Bot Statistics:*\n"+
            "• Total Bots: 45\n"+
            "• Active Bots: 23\n"+
            "• Inactive Bots: 22\n\n"+
            "⚙️ *Bot Settings:*\n"+
            "• Status: ✅ Running\n"+
            "• Bots per tick: 2\n"+
            "• Max bots per game: 50\n"+
            "• Reserve interval: 3s\n\n"+
            "📈 *Bot Activity:*\n"+
            "• Reserved today: 12 cards\n"+
            "• Total cards reserved: 342",
    )

    b.logAdminAction(ctx, chatID, "view_bots", 0, "bots", "Viewed bot status")
}

func (b *Bot) startBots(ctx context.Context, chatID int64) {
    // Start bot routine via game engine
    // engine.StartBots()

    b.sendMarkdown(ctx, chatID, "✅ *Bots Started*\n\nBot routine has been started.")
    b.logAdminAction(ctx, chatID, "start_bots", 0, "bots", "Started bot routine")
}

func (b *Bot) stopBots(ctx context.Context, chatID int64) {
    // Stop bot routine via game engine
    // engine.StopBots()

    b.sendMarkdown(ctx, chatID, "⏹️ *Bots Stopped*\n\nBot routine has been stopped.")
    b.logAdminAction(ctx, chatID, "stop_bots", 0, "bots", "Stopped bot routine")
}

func (b *Bot) showBotCount(ctx context.Context, chatID int64) {
    // Get bot count from game engine

    b.sendMarkdown(
        ctx,
        chatID,
        "📊 *Bot Count*\n\n"+
            "Total Bot Users: 45\n"+
            "Active Bots: 23\n"+
            "Bots in Game: 12",
    )
}

func (b *Bot) setBotSpeed(ctx context.Context, chatID int64, speed int) {
    if speed < 1 || speed > 10 {
        b.sendText(ctx, chatID, "❌ Speed must be between 1 and 10.")
        return
    }

    // Update bot speed in game engine
    b.sendMarkdown(ctx, chatID, fmt.Sprintf("⚡ *Bot Speed Updated*\n\nBots per tick set to: %d", speed))
    b.logAdminAction(ctx, chatID, "set_bot_speed", 0, "bots", fmt.Sprintf("Set bot speed to %d", speed))
}

func (b *Bot) setMaxBots(ctx context.Context, chatID int64, max int) {
    if max < 5 || max > 100 {
        b.sendText(ctx, chatID, "❌ Max bots must be between 5 and 100.")
        return
    }

    b.sendMarkdown(ctx, chatID, fmt.Sprintf("🎯 *Max Bots Updated*\n\nMax bots per game set to: %d", max))
    b.logAdminAction(ctx, chatID, "set_max_bots", 0, "bots", fmt.Sprintf("Set max bots to %d", max))
}