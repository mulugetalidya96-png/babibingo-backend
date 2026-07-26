package bot

import (
	"context"
	"log"
	"strings"

	"github.com/mymmrac/telego"
)

func (b *Bot) handleCallback(
	ctx context.Context,
	callback *telego.CallbackQuery,
) {

	if callback == nil {
		return
	}

	log.Printf(
		"callback received: %s",
		callback.Data,
	)

	chatID := callback.Message.GetChat().ID

	// Handle deposit bank selection
	if strings.HasPrefix(callback.Data, "deposit_") {
		bank := strings.TrimPrefix(callback.Data, "deposit_")
		b.handleDepositBankSelection(ctx, chatID, bank)
	}

	// Handle back to menu
	if callback.Data == "back_to_menu" {
		b.sendMainMenu(ctx, chatID)
	}

	// Telegram requires answering callback queries.
	err := b.api.AnswerCallbackQuery(
		ctx,
		&telego.AnswerCallbackQueryParams{
			CallbackQueryID: callback.ID,
		},
	)

	if err != nil {
		log.Printf(
			"failed answering callback: %v",
			err,
		)
	}
}

// handleDepositBankSelection sends deposit info based on bank selection
func (b *Bot) handleDepositBankSelection(
	ctx context.Context,
	chatID int64,
	bank string,
) {
	switch bank {
	case "telebirr":
		b.sendMarkdown(
			ctx,
			chatID,
			"📱 *Telebirr Deposit*\n\n"+
				"Send money to:\n"+
				"`0940072277` — BabiBingo\n\n"+
				"Copy confirmation code and submit in WebApp.\n\n"+
				"⏱️ Processing: 1-2 minutes",
		)
	case "cbebirr":
		b.sendMarkdown(
			ctx,
			chatID,
			"🏦 *CBE Birr Deposit*\n\n"+
				"Send money to:\n"+
				"Account: `1000123456789`\n"+
				"Name: BabiBingo\n"+
				"Bank: CBE\n\n"+
				"Copy transaction reference and submit in WebApp.\n\n"+
				"⏱️ Processing: 1-5 minutes",
		)
	default:
		b.sendText(ctx, chatID, "❌ Invalid selection. Please try again.")
		b.handleDeposit(ctx, chatID)
	}
}