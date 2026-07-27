package bot

import (
	"context"
	"fmt"
	"strings"

	"babibingo/internal/models"

	"github.com/mymmrac/telego"
)

// internal/bot/command.go - handleCommand

func (b *Bot) handleCommand(
    ctx context.Context,
    chatID int64,
    user *telego.User,
    text string,
) {
    // Parse command and arguments
    parts := strings.Split(strings.TrimPrefix(text, "/"), " ")
    command := parts[0]
    
    // ✅ Extract referral code if present (e.g., /start ABC123)
    referralCode := ""
    if len(parts) > 1 {
        referralCode = parts[1]
    }

    switch command {
    case "start":
        b.handleStart(ctx, chatID, user, referralCode) // ✅ Pass referral code

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
        b.sendText(ctx, chatID, "Unknown command. Use the menu below.")
        b.sendMainMenu(ctx, chatID)
    }
}
// internal/bot/command.go - handleStart

func (b *Bot) handleStart(
    ctx context.Context,
    chatID int64,
    user *telego.User,
    referralCode string, // ✅ Add referral code parameter
) {
    var existing models.User
    err := b.db.Where("telegram_id = ?", user.ID).First(&existing).Error

    if err == nil {
        b.sendMainMenu(ctx, chatID)
        return
    }

    // ✅ Store referral code temporarily (will be used after phone registration)
    // We'll store in context or a map
    if referralCode != "" {
        b.tempReferralCache.Store(chatID, referralCode)
    }

    // Ask for phone number
    msg := telego.SendMessageParams{
        ChatID: telego.ChatID{
            ID: chatID,
        },
        Text: "📋 *Registration Process*\n\n" +
            "Please share your phone number to register automatically.",
        ParseMode: "Markdown",
        ReplyMarkup: b.contactKeyboard(),
    }

    b.sendMessage(ctx, &msg)
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
// handleDeposit handles deposit requests with bank selection
func (b *Bot) handleDeposit(
	ctx context.Context,
	chatID int64,
) {
	// ✅ First ask user to choose their bank
	msg := telego.SendMessageParams{
		ChatID: telego.ChatID{
			ID: chatID,
		},
		Text: "💳 *Select Deposit Method*\n\n" +
			"Choose your preferred payment method:",
		ParseMode: "Markdown",
		ReplyMarkup: b.bankSelectionKeyboard(),
	}

	b.sendMessage(ctx, &msg)
}

// ✅ New: Bank selection keyboard
func (b *Bot) bankSelectionKeyboard() *telego.InlineKeyboardMarkup {
	return &telego.InlineKeyboardMarkup{
		InlineKeyboard: [][]telego.InlineKeyboardButton{
			{
				{
					Text:         "📱 Telebirr",
					CallbackData: "deposit_telebirr",
				},
				{
					Text:         "🏦 CBE Birr",
					CallbackData: "deposit_cbebirr",
				},
			},
			{
				{
					Text:         "🔙 Back",
					CallbackData: "back_to_menu",
				},
			},
		},
	}
}



// ✅ Telebirr deposit info
func (b *Bot) sendTelebirrDepositInfo(
	ctx context.Context,
	chatID int64,
) {
	b.sendMarkdown(
		ctx,
		chatID,
		"📱 *Telebirr Deposit*\n\n"+
			"1️⃣ Open Telebirr app\n"+
			"2️⃣ Send money to:\n"+
			"   `0940072277` — BabiBingo\n"+
			"3️⃣ Copy the confirmation code\n"+
			"4️⃣ Submit code in WebApp\n\n"+
			"💰 *Minimum Deposit:* 20 ETB\n"+
			"⏱️ *Processing Time:* 1-2 minutes\n\n"+
			"⚠️ Include your Telegram username in the reference.",
	)
}

// ✅ CBE Birr deposit info
func (b *Bot) sendCBEBirrDepositInfo(
	ctx context.Context,
	chatID int64,
) {
	b.sendMarkdown(
		ctx,
		chatID,
		"🏦 *CBE Birr Deposit*\n\n"+
			"1️⃣ Open CBE Birr app\n"+
			"2️⃣ Send money to:\n"+
			"   Account: `1000123456789`\n"+
			"   Name: BabiBingo\n"+
			"   Bank: Commercial Bank of Ethiopia\n"+
			"3️⃣ Copy the transaction reference\n"+
			"4️⃣ Submit reference in WebApp\n\n"+
			"💰 *Minimum Deposit:* 20 ETB\n"+
			"⏱️ *Processing Time:* 1-5 minutes\n\n"+
			"⚠️ Include your Telegram username in the reference.",
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
// handleAgent handles agent-related actions
func (b *Bot) handleAgent(
	ctx context.Context,
	chatID int64,
	user *telego.User,
) {
	var u models.User

	err := b.db.
		Where("telegram_id = ?", user.ID).
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

	// If user is already an agent, show dashboard
	if u.IsAgent {
		b.sendMarkdown(
			ctx,
			chatID,
			fmt.Sprintf(
				"🤝 *Agent Dashboard*\n\n"+
					"💰 Agent Balance: %.2f ETB\n"+
					"🔑 Referral Code: `%s`\n\n"+
					"📤 Share your referral link to earn commissions!\n"+
					"1 ETB per card played by your invited users.",
				u.AgentBalance,
				u.ReferralCode,
			),
		)
		return
	}

	// ✅ Updated Agent Registration Message for BabiBingo
	b.sendMarkdown(
		ctx,
		chatID,
		"💼 *Become a BabiBingo Agent*\n\n"+
			"Earn commissions every time your invited players play!\n\n"+
			"💰 *Commission:* 1 ETB per card played by your invited users\n\n"+
			"የባቢ ቢንጎ ኤጀንት በመሆን እና invite ያደረጉት ሰው 1 ካርቴላ በያዘ ቁጥር 1 ብር ያግኙ \n\n"+
			"ለመመዝገብ :- ከዚህ ስር የሚገኘውን ቁልፍ በመንካት request አድርገው...invite ማድረጊያ link ይላክሎታል \n\n"+
			"እርሱን link በማጋራት invite ያድረጉ።\n\n"+
			"👉 *Register here as an agent:*\n"+
			"@BabiBingoAgent_Bot", // ✅ Updated to BabiBingo Agent Bot
	)
}
// internal/bot/command.go - handleInvite

// internal/bot/command.go - handleInvite

func (b *Bot) handleInvite(
	ctx context.Context,
	chatID int64,
	user *telego.User,
) {
	var u models.User
	if err := b.db.Where("telegram_id = ?", user.ID).First(&u).Error; err != nil {
		b.sendText(ctx, chatID, "❌ Please register first with /start")
		return
	}

	// ✅ Create invite link with user's Telegram ID as referral
	link := fmt.Sprintf(
		"https://t.me/babibingo_bot?start=ref_%d",
		user.ID,
	)

	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"🔗 *Your Invitation Link*\n\n"+
				"Share this link with your friends!\n\n"+
				"Link: %s",
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