package game

import (
	"babibingo/internal/models"
	"encoding/json"
	"log"
	"math/rand"
	"os"
	"sort"
	"sync"
)

var (
	predefinedCards []models.CardJSON
	cardCacheOnce   sync.Once
)

// InitCardCache initializes the card cache
func InitCardCache() {
	cardCacheOnce.Do(func() {
		// Try to load from JSON file
		if err := LoadCardsFromJSON("data/cards.json"); err != nil {
			log.Printf("⚠️ Failed to load cards from JSON: %v, generating random cards", err)
			// Generate all 75 cards as fallback
			predefinedCards = make([]models.CardJSON, 0, 400)
			for i := 1; i <= 75; i++ {
				predefinedCards = append(predefinedCards, generateRandomCard(i))
			}
		}
		log.Printf("✅ Cards loaded: %d cards available", len(predefinedCards))
	})
}

// LoadCardsFromJSON loads cards from JSON file
func LoadCardsFromJSON(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &predefinedCards)
}

// GetCardByID returns a card by its ID (1-75)
func GetCardByID(id int) (models.CardJSON, bool) {
	// Ensure cache is initialized
	InitCardCache()
	
	for _, card := range predefinedCards {
		if card.CardID == id {
			return card, true
		}
	}
	
	// If not found, generate a random card as fallback
	log.Printf("⚠️ Card %d not found in cache, generating random card", id)
	return generateRandomCard(id), true
}

// GetAllCards returns all predefined cards
func GetAllCards() []models.CardJSON {
	InitCardCache()
	return predefinedCards
}

// checkWinPattern checks if a card has a winning pattern
func checkWinPattern(card models.CardJSON, marked []int) string {
	markedSet := make(map[int]bool)
	for _, n := range marked {
		markedSet[n] = true
	}

	// Build 5x5 grid
	grid := make([][]*int, 5)
	for i := range grid {
		grid[i] = make([]*int, 5)
	}

	// Fill grid
	for i, n := range card.B {
		grid[i][0] = &n
	}
	for i, n := range card.I {
		grid[i][1] = &n
	}
	grid[0][2] = card.N[0]
	grid[1][2] = card.N[1]
	grid[2][2] = nil // Free space
	grid[3][2] = card.N[3]
	grid[4][2] = card.N[4]
	for i, n := range card.G {
		grid[i][3] = &n
	}
	for i, n := range card.O {
		grid[i][4] = &n
	}

	// Check horizontal
	for row := 0; row < 5; row++ {
		win := true
		for col := 0; col < 5; col++ {
			if grid[row][col] == nil {
				continue
			}
			if !markedSet[*grid[row][col]] {
				win = false
				break
			}
		}
		if win {
			return "horizontal"
		}
	}

	// Check vertical
	for col := 0; col < 5; col++ {
		win := true
		for row := 0; row < 5; row++ {
			if grid[row][col] == nil {
				continue
			}
			if !markedSet[*grid[row][col]] {
				win = false
				break
			}
		}
		if win {
			return "vertical"
		}
	}

	// Check diagonal (top-left to bottom-right)
	win := true
	for i := 0; i < 5; i++ {
		if grid[i][i] == nil {
			continue
		}
		if !markedSet[*grid[i][i]] {
			win = false
			break
		}
	}
	if win {
		return "diagonal"
	}

	// Check diagonal (top-right to bottom-left)
	win = true
	for i := 0; i < 5; i++ {
		if grid[i][4-i] == nil {
			continue
		}
		if !markedSet[*grid[i][4-i]] {
			win = false
			break
		}
	}
	if win {
		return "diagonal"
	}

	return ""
}

// generateRandomCard generates a random bingo card
func generateRandomCard(cardID int) models.CardJSON {
	return models.CardJSON{
		B:      pickRandom(1, 15, 5),
		I:      pickRandom(16, 30, 5),
		N:      appendNFreeSpace(pickRandom(31, 45, 4)),
		G:      pickRandom(46, 60, 5),
		O:      pickRandom(61, 75, 5),
		CardID: cardID,
	}
}

// pickRandom picks random numbers
func pickRandom(min, max, count int) []int {
	nums := make([]int, max-min+1)
	for i := range nums {
		nums[i] = min + i
	}
	rand.Shuffle(len(nums), func(i, j int) { nums[i], nums[j] = nums[j], nums[i] })
	result := nums[:count]
	sort.Ints(result)
	return result
}

// getBingoLetter returns the letter for a bingo number
func getBingoLetter(num int) string {
	switch {
	case num >= 1 && num <= 15:
		return "B"
	case num >= 16 && num <= 30:
		return "I"
	case num >= 31 && num <= 45:
		return "N"
	case num >= 46 && num <= 60:
		return "G"
	case num >= 61 && num <= 75:
		return "O"
	default:
		return ""
	}
}

// containsNumber checks if a number is in a card
func containsNumber(card models.CardJSON, num int) bool {
	for _, n := range card.B {
		if n == num {
			return true
		}
	}
	for _, n := range card.I {
		if n == num {
			return true
		}
	}
	for _, n := range card.N {
		if n != nil && *n == num {
			return true
		}
	}
	for _, n := range card.G {
		if n == num {
			return true
		}
	}
	for _, n := range card.O {
		if n == num {
			return true
		}
	}
	return false
}

// appendNFreeSpace appends free space to N column
func appendNFreeSpace(nums []int) []*int {
	result := make([]*int, 5)
	result[0] = &nums[0]
	result[1] = &nums[1]
	result[2] = nil // Free space
	result[3] = &nums[2]
	result[4] = &nums[3]
	return result
}

// maskPhone masks a phone number
func maskPhone(phone string) string {
	if len(phone) < 8 {
		return phone
	}
	return phone[:4] + "****" + phone[len(phone)-2:]
}