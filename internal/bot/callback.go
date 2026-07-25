package bot

import (
	"context"
	"log"

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

	// Future callback actions:
	//
	// switch callback.Data {
	//
	// case "play":
	//     ...
	//
	// case "deposit":
	//     ...
	//
	// }

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