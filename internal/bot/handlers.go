package bot

import (
	"context"
	"strings"

	"github.com/mymmrac/telego"
)





func (b *Bot) handleMessage(ctx context.Context, msg *telego.Message) {
	if msg.From == nil {
		return
	}

	chatID := msg.Chat.ID
	user := msg.From

	// Contact sharing
	if msg.Contact != nil {
		b.handlePhoneShare(ctx, chatID, user, msg.Contact)
		return
	}

	text := strings.TrimSpace(msg.Text)

	if strings.HasPrefix(text, "/") {
		b.handleCommand(ctx, chatID, user, text)
		return
	}

	switch text {

	case "🎮 Start play":
		b.handlePlay(ctx, chatID)

	case "💰 Balance":
		b.handleBalance(ctx, chatID, user)

	case "💳 Deposit":
		b.handleDeposit(ctx, chatID)

	case "🏧 Withdraw":
		b.handleWithdraw(ctx, chatID)

	case "🤝 Agent":
		b.handleAgent(ctx, chatID, user)

	case "📨 Invite":
		b.handleInvite(ctx, chatID, user)

	case "🆘 Support":
		b.handleSupport(ctx, chatID)

	default:
		b.sendMainMenu(ctx, chatID)
	}
}