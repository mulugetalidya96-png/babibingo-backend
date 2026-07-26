package game

import (
	"fmt"

	"babibingo/internal/models"

	"github.com/google/uuid"
)

// getUserByTelegramID gets a user by their Telegram ID
func (e *Engine) getUserByTelegramID(telegramID int64) (*models.User, error) {
	var user models.User
	if err := e.db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		return nil, fmt.Errorf("user with telegram_id %d not found: %w", telegramID, err)
	}
	return &user, nil
}

// getPlayerCount returns the number of players in a game
func (e *Engine) getPlayerCount(gameID uuid.UUID) int {
	var count int64
	e.db.Model(&models.GamePlayer{}).Where("game_id = ?", gameID).Count(&count)
	return int(count)
}

// getBoardCount returns the number of cards in a game
func (e *Engine) getBoardCount(gameID uuid.UUID) int {
	var count int64
	e.db.Model(&models.Card{}).Where("game_id = ?", gameID).Count(&count)
	return int(count)
}

// getCalledDisplays returns formatted called numbers
func (e *Engine) getCalledDisplays(nums []int) []string {
	displays := make([]string, len(nums))
	for i, n := range nums {
		displays[i] = fmt.Sprintf("%s%d", getBingoLetter(n), n)
	}
	return displays
}