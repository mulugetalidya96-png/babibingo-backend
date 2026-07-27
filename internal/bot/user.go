package bot

import (
	"context"
	"fmt"
	"log"
	"time"

	"babibingo/internal/models"

	"github.com/mymmrac/telego"
)

// internal/bot/user.go - handlePhoneShare

func (b *Bot) handlePhoneShare(
	ctx context.Context,
	chatID int64,
	tgUser *telego.User,
	contact *telego.Contact,
) {
	var existing models.User

	err := b.db.
		Where("telegram_id = ?", tgUser.ID).
		First(&existing).
		Error

	if err == nil {
		b.sendMainMenu(ctx, chatID)
		return
	}

	// ✅ Generate referral code for this user
	referralCode := generateReferralCode()

	// ✅ Check if there's a referral code from the start command
	var referredBy *int64
	if cachedCode, ok := b.tempReferralCache.Load(chatID); ok {
		referrerCode := cachedCode.(string)
		var referrer models.User
		if err := b.db.Where("referral_code = ?", referrerCode).First(&referrer).Error; err == nil {
			referredBy = &referrer.ID
			log.Printf("✅ User %d referred by agent %d", tgUser.ID, referrer.ID)
		}
		b.tempReferralCache.Delete(chatID) // Clean up
	}

	// ✅ Create user with referral info
	user := models.User{
		TelegramID:   int64(tgUser.ID),
		PhoneNumber:  contact.PhoneNumber,
		Username:     tgUser.Username,
		FirstName:    tgUser.FirstName,
		LastName:     tgUser.LastName,
		ReferralCode: referralCode,
		ReferredBy:   referredBy, // ✅ Store who referred this user
		Balance:      0,
		AgentBalance: 0,
		IsAgent:      false,
		LastActive:   time.Now(),
		CreatedAt:    time.Now(),
	}

	if err := b.db.Create(&user).Error; err != nil {
		log.Printf("failed creating user: %v", err)
		b.sendText(ctx, chatID, "❌ Registration failed. Please try again.")
		return
	}

	// ✅ If user was referred, notify the referrer (agent)
	if referredBy != nil {
		var referrer models.User
		if err := b.db.First(&referrer, *referredBy).Error; err == nil {
			b.sendMarkdown(
				ctx,
				referrer.TelegramID, // Send to agent
				fmt.Sprintf(
					"🎉 *New Referral!*\n\n"+
						"User @%s has registered using your referral link!\n\n"+
						"💳 When they play, you'll earn 1 ETB per card.",
					tgUser.Username,
				),
			)
		}
	}

	// ✅ Send welcome message with referral info
	b.sendMarkdown(
		ctx,
		chatID,
		fmt.Sprintf(
			"✅ *Registration successful!*\n\n"+
				"🎱 Welcome to BabiBingo!\n"+
				"🔑 Your Referral Code: `%s`\n\n"+
				"Share this code with friends to earn rewards!\n"+
				"📤 /invite to share your referral link.\n"+
				"📊 /referrals to track your referrals.",
			referralCode,
		),
	)

	b.sendMainMenu(ctx, chatID)
}