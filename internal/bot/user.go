package bot

import (
	"context"
	"log"
	"time"

	"babibingo/internal/models"

	"github.com/mymmrac/telego"
)
func (b *Bot) handlePhoneShare(
	ctx context.Context,
	chatID int64,
	tgUser *telego.User,
	contact *telego.Contact,
) {

	var existing models.User

	err := b.db.
		Where(
			"telegram_id = ?",
			tgUser.ID,
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


	referralCode := generateReferralCode()


	user := models.User{
		TelegramID: int64(tgUser.ID),

		PhoneNumber: contact.PhoneNumber,

		Username: tgUser.Username,

		FirstName: tgUser.FirstName,

		LastName: tgUser.LastName,

		ReferralCode: referralCode,

		Balance: 0,

		LastActive: time.Now(),
	}


	if err := b.db.Create(&user).Error; err != nil {

		log.Printf(
			"failed creating user: %v",
			err,
		)

		b.sendText(
			ctx,
			chatID,
			"❌ Registration failed. Please try again.",
		)

		return
	}


	b.sendMarkdown(
		ctx,
		chatID,

		"✅ *Registration successful!*\n\n"+
			"🎱 Enjoy playing BabiBingo.",
	)


	b.sendMainMenu(
		ctx,
		chatID,
	)
}