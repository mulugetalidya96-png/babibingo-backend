package bot

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"babibingo/internal/models"
	"babibingo/internal/sms"
	"babibingo/internal/verify"

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

	// Check if it's a command
	if strings.HasPrefix(text, "/") {
		b.handleCommand(ctx, chatID, user, text)
		return
	}

	// ✅ Check if it's a Telebirr SMS
	if sms.IsTelebirrSMS(text) {
		b.handleTelebirrSMS(ctx, chatID, user, text)
		return
	}

	// Menu buttons
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

// ✅ Handle Telebirr SMS
func (b *Bot) handleTelebirrSMS(
	ctx context.Context,
	chatID int64,
	user *telego.User,
	smsText string,
) {
	// Parse SMS
	txnInfo := sms.ParseTelebirrSMS(smsText)
	if !txnInfo.IsValid {
		b.sendText(ctx, chatID, "❌ Could not parse transaction details.\n\nPlease send the confirmation code manually or use the WebApp.")
		return
	}

	// Get user from database
	var dbUser models.User
	if err := b.db.Where("telegram_id = ?", user.ID).First(&dbUser).Error; err != nil {
		b.sendText(ctx, chatID, "❌ Please register first with /start")
		return
	}

	// Send processing message
	b.sendText(ctx, chatID, "⏳ Verifying transaction...")

	// Verify with verify.et
	verifyClient := verify.NewVerifyClient(b.cfg.VerifyAPIKey)
	if err := verifyClient.VerifyAndUpdateTransaction(
		txnInfo.TransactionID,
		txnInfo.Amount,
		user.ID,
	); err != nil {
		log.Printf("Verification failed: %v", err)
		b.sendText(ctx, chatID, fmt.Sprintf(
			"❌ Verification failed: %v\n\nPlease contact support @babibingo_support",
			err,
		))
		return
	}

	// Update user balance
	dbUser.Balance += txnInfo.Amount
	if err := b.db.Save(&dbUser).Error; err != nil {
		log.Printf("Failed to update balance: %v", err)
		b.sendText(ctx, chatID, "❌ Failed to update balance. Please contact support.")
		return
	}

	// Create transaction record
	transaction := models.Transaction{
		UserID:    dbUser.ID,
		Type:      "deposit",
		Amount:    txnInfo.Amount,
		Status:    "completed",
		Method:    "telebirr",
		Reference: txnInfo.TransactionID,
		CreatedAt: time.Now(),
	}
	if err := b.db.Create(&transaction).Error; err != nil {
		log.Printf("Failed to create transaction record: %v", err)
	}

	// Send success message
	b.sendMarkdown(ctx, chatID, fmt.Sprintf(
		"✅ *Deposit Successful!*\n\n"+
			"💰 Amount: %.2f ETB\n"+
			"🆔 Transaction: `%s`\n"+
			"💳 New Balance: %.2f ETB\n\n"+
			"🎮 Play now from the menu!",
		txnInfo.Amount,
		txnInfo.TransactionID,
		dbUser.Balance,
	))
}