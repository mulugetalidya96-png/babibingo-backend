package bot

import (
	"context"
	"fmt"
	"strings"

	"babibingo/internal/models"

	"github.com/mymmrac/telego"
)
func (b *Bot) handleCommand(
	ctx context.Context,
	chatID int64,
	user *telego.User,
	text string,
) {

	command := strings.Split(
		strings.TrimPrefix(text, "/"),
		" ",
	)[0]

	switch command {

	case "start":
		b.handleStart(ctx, chatID, user)

	case "play":
		b.handlePlay(ctx, chatID)

	case "balance":
		b.handleBalance(ctx, chatID, user)

	case "deposit":
		b.handleDeposit(ctx, chatID)

	case "withdraw":
		b.handleWithdraw(ctx, chatID)

	case "agent":
		b.handleAgent(ctx, chatID, user)

	case "invite":
		b.handleInvite(ctx, chatID, user)

	case "support":
		b.handleSupport(ctx, chatID)

	default:
		b.sendText(
			ctx,
			chatID,
			"Unknown command. Use the menu below.",
		)

		b.sendMainMenu(
			ctx,
			chatID,
		)
	}
}
func (b *Bot) handleStart(
	ctx context.Context,
	chatID int64,
	user *telego.User,
) {

	var existing models.User

	err := b.db.
		Where(
			"telegram_id = ?",
			user.ID,
		).
		First(&existing).
		Error


	if err == nil {

		b.sendMainMenu(
			ctx,
			chatID,
		)

		return
	}


	msg := telego.SendMessageParams{
		ChatID: telego.ChatID{
			ID: chatID,
		},

		Text:
		"📋 *Registration Process*\n\n" +
			"Please share your phone number to register automatically.",

		ParseMode: "Markdown",

		ReplyMarkup: b.contactKeyboard(),
	}


	b.sendMessage(
		ctx,
		&msg,
	)
}
func (b *Bot) handlePlay(
	ctx context.Context,
	chatID int64,
) {

	msg := telego.SendMessageParams{
		ChatID: telego.ChatID{
			ID: chatID,
		},

		Text:
		"🎱 *BabiBingo*\n\n" +
			"Click below to open the game.",

		ParseMode: "Markdown",

		ReplyMarkup: b.playKeyboard(),
	}


	b.sendMessage(
		ctx,
		&msg,
	)
}
func (b *Bot) handleBalance(
	ctx context.Context,
	chatID int64,
	user *telego.User,
) {

	var u models.User


	err := b.db.
		Where(
			"telegram_id = ?",
			user.ID,
		).
		First(&u).
		Error


	if err != nil {

		b.sendText(
			ctx,
			chatID,
			"❌ Please register first with /start",
		)

		return
	}


	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"💰 *Your Balance*\n\n%.2f ETB",
			u.Balance,
		),
	)
}
func (b *Bot) handleDeposit(
	ctx context.Context,
	chatID int64,
) {

	b.sendMarkdown(
		ctx,
		chatID,

		"💳 *Deposit via Telebirr*\n\n"+
			"Account: `0940072277` — BabiBingo\n\n"+
			"1️⃣ Open Telebirr\n"+
			"2️⃣ Send money\n"+
			"3️⃣ Copy confirmation code\n"+
			"4️⃣ Submit in WebApp\n\n"+
			"Balance updates within 1-2 minutes.",
	)
}
func (b *Bot) handleWithdraw(
	ctx context.Context,
	chatID int64,
) {

	b.sendMarkdown(
		ctx,
		chatID,

		"🏧 *Withdrawal*\n\n"+
			"Open WebApp to request withdrawal.\n"+
			"Minimum withdrawal: 50 ETB",
	)
}
func (b *Bot) handleAgent(
	ctx context.Context,
	chatID int64,
	user *telego.User,
) {

	var u models.User


	err := b.db.
		Where(
			"telegram_id = ?",
			user.ID,
		).
		First(&u).
		Error


	if err != nil {

		b.sendText(
			ctx,
			chatID,
			"❌ Please register first",
		)

		return
	}


	if !u.IsAgent {

		b.sendMarkdown(
			ctx,
			chatID,

			"🤝 *Become an Agent*\n\n"+
				"• 5% commission on deposits\n"+
				"• 3% commission on referrals\n\n"+
				"Contact support.",
		)

		return
	}


	b.sendMarkdown(
		ctx,
		chatID,

		fmt.Sprintf(
			"🤝 *Agent Dashboard*\n\n"+
				"Balance: %.2f ETB\n"+
				"Code: %s",

			u.AgentBalance,
			u.ReferralCode,
		),
	)
}
func (b *Bot) handleInvite(
	ctx context.Context,
	chatID int64,
	user *telego.User,
) {

	var u models.User


	if err := b.db.
		Where(
			"telegram_id = ?",
			user.ID,
		).
		First(&u).
		Error; err != nil {

		b.sendText(
			ctx,
			chatID,
			"❌ Please register first",
		)

		return
	}


	link := fmt.Sprintf(
		"https://t.me/%s?start=%s",
		b.me.Username,
		u.ReferralCode,
	)


	b.sendMarkdown(
		ctx,
		chatID,

		fmt.Sprintf(
			"📨 *Invite Friends*\n\n"+
				"Code: `%s`\n\n"+
				"%s\n\n"+
				"Earn 10 ETB for referrals.",
			u.ReferralCode,
			link,
		),
	)
}
func (b *Bot) handleSupport(
	ctx context.Context,
	chatID int64,
) {

	b.sendMarkdown(
		ctx,
		chatID,
		"🆘 *Support*\n\nContact: @babibingo_support",
	)
}