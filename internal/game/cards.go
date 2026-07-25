package game

import (
	"babibingo/internal/models"
	"encoding/json"
	"os"
)

var predefinedCards []models.CardJSON

func LoadCardsFromJSON(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &predefinedCards)
}

func GetCardByID(id int) (models.CardJSON, bool) {
	for _, card := range predefinedCards {
		if card.CardID == id {
			return card, true
		}
	}
	return models.CardJSON{}, false
}
func GetAllCards() []models.CardJSON {
	return predefinedCards
}