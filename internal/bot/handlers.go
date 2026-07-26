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
// internal/bot/handlers.go - handleTelebirrSMS

// internal/bot/handlers.go - handleTelebirrSMS

// internal/bot/handlers.go - handleTelebirrSMS

// internal/bot/handlers.go - handleTelebirrSMS

func (b *Bot) handleTelebirrSMS(
	ctx context.Context,
	chatID int64,
	user *telego.User,
	smsText string,
) {
	// 1️⃣ Parse SMS
	txnInfo := sms.ParseTelebirrSMS(smsText)
	if !txnInfo.IsValid {
		b.sendText(ctx, chatID, "❌ Could not parse transaction details. Please send the confirmation code manually.")
		return
	}

	// 2️⃣ Get user from database
	var dbUser models.User
	if err := b.db.Where("telegram_id = ?", user.ID).First(&dbUser).Error; err != nil {
		b.sendText(ctx, chatID, "❌ Please register first with /start")
		return
	}

	// ✅ 3️⃣ Check for duplicate transaction
	var existingTransaction models.Transaction
	err := b.db.Where("reference = ?", txnInfo.TransactionID).First(&existingTransaction).Error
	if err == nil {
		// Transaction already exists
		b.sendMarkdown(ctx, chatID, fmt.Sprintf(
			"⚠️ *Duplicate Transaction Detected*\n\n"+
				"Transaction `%s` has already been processed.\n\n"+
				"📅 Processed: %s\n"+
				"💰 Amount: %.2f ETB\n"+
				"📊 Status: %s\n\n"+
				"If you believe this is an error, please contact support.",
			txnInfo.TransactionID,
			existingTransaction.CreatedAt.Format("2006-01-02 15:04:05"),
			existingTransaction.Amount,
			existingTransaction.Status,
		))
		return
	}

	// 4️⃣ Check config
	if b.cfg == nil || b.cfg.VerifyAPIKey == "" {
		b.sendText(ctx, chatID, "❌ Verify.et API key is not configured. Please contact support.")
		return
	}

	// 5️⃣ Send processing message
	b.sendText(ctx, chatID, "⏳ Verifying transaction with verify.et...")

	// 6️⃣ Get phone number
	babiBingoPhone := "0997325583"

	// 7️⃣ Call verify.et API
	verifyClient := verify.NewVerifyClient(b.cfg.VerifyAPIKey)
	verifyResp, err := verifyClient.VerifyTransaction(
		txnInfo.TransactionID,
		txnInfo.Amount,
		babiBingoPhone,
	)
	if err != nil {
		log.Printf("Verify.et error: %v", err)
		b.sendText(ctx, chatID, fmt.Sprintf(
			"❌ Verification failed: %v\n\nPlease contact support.",
			err,
		))
		return
	}

	// 8️⃣ Check if verification was successful
	if !verifyResp.Success {
		b.sendText(ctx, chatID, fmt.Sprintf(
			"❌ Transaction verification failed: %s",
			verifyResp.Message,
		))
		return
	}

	// 9️⃣ Check data array
	if len(verifyResp.Data) == 0 {
		b.sendText(ctx, chatID, "❌ No transaction data found. Please try again.")
		return
	}

	txnData := verifyResp.Data[0]

	// 🔟 Check if verified
	if !txnData.Verified {
		b.sendText(ctx, chatID, fmt.Sprintf(
			"❌ Transaction not verified. Status: %s",
			txnData.Status,
		))
		return
	}

	// 1️⃣1️⃣ Check settlement account match
	if !txnData.SettlementAccountMatch.Matched {
		b.sendText(ctx, chatID, fmt.Sprintf(
			"❌ Transaction sent to wrong account.\n\n"+
				"Expected: %s\n"+
				"Received: %s\n\n"+
				"Please send to the correct BabiBingo account.",
			babiBingoPhone,
			txnData.ReceiverAccount,
		))
		return
	}

	// ✅ 1️⃣2️⃣ Double-check duplicate before saving (race condition protection)
	var checkDuplicate models.Transaction
	if err := b.db.Where("reference = ?", txnInfo.TransactionID).First(&checkDuplicate).Error; err == nil {
		b.sendText(ctx, chatID, "⚠️ This transaction was already processed by another request.")
		return
	}

	// 1️⃣3️⃣ Update user balance
	dbUser.Balance += txnInfo.Amount
	if err := b.db.Save(&dbUser).Error; err != nil {
		log.Printf("Failed to update balance: %v", err)
		b.sendText(ctx, chatID, "❌ Failed to update balance. Please contact support.")
		return
	}

	// 1️⃣4️⃣ Create transaction record
	transaction := models.Transaction{
		UserID:    dbUser.ID,
		Type:      "deposit",
		Amount:    txnInfo.Amount,
		Status:    "completed",
		Method:    "telebirr",
		Reference: txnInfo.TransactionID,
		Description: fmt.Sprintf("Telebirr deposit via SMS - Transaction: %s", txnInfo.TransactionID),
		CreatedAt: time.Now(),
	}
	if err := b.db.Create(&transaction).Error; err != nil {
		// ✅ If duplicate is created between check and save
		if strings.Contains(err.Error(), "duplicate") {
			b.sendText(ctx, chatID, "⚠️ This transaction was already processed. Please check your balance.")
			return
		}
		log.Printf("Failed to create transaction record: %v", err)
		b.sendText(ctx, chatID, "❌ Failed to create transaction record. Please contact support.")
		return
	}

	// 1️⃣5️⃣ Send success message
	b.sendMarkdown(ctx, chatID, fmt.Sprintf(
		"✅ *Deposit Successful!*\n\n"+
			"💰 Amount: %.2f ETB\n"+
			"🆔 Transaction: `%s`\n"+
			"📱 Sent to: %s\n"+
			"💳 New Balance: %.2f ETB\n\n"+
			"🎮 Play now from the menu!",
		txnInfo.Amount,
		txnInfo.TransactionID,
		babiBingoPhone,
		dbUser.Balance,
	))
}