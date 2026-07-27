package game

import (
	"babibingo/internal/models"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// ReserveCard reserves a card for a user
// package game - Add error broadcasting

// ReserveCard reserves a card for a user
func (e *Engine) ReserveCard(telegramID int64, cardNumber int) error {
	log.Printf("🔵 ReserveCard: telegram_id=%d, card=%d", telegramID, cardNumber)

	if e.currentGame == nil {
		err := fmt.Errorf("no active game")
		e.sendError(telegramID, err.Error())
		return err
	}

	state := e.currentGame
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.Game.Status != GameStatusWaiting {
		err := fmt.Errorf("game already started")
		e.sendError(telegramID, err.Error())
		return err
	}

	// Get user by Telegram ID
	user, err := e.getUserByTelegramID(telegramID)
	if err != nil {
		errMsg := fmt.Sprintf("user not found: %v", err)
		e.sendError(telegramID, errMsg)
		return fmt.Errorf("user not found: %w", err)
	}

	// Check balance
	if user.Balance < StakeAmount {
		err := fmt.Errorf("insufficient balance: need %.2f ETB, have %.2f ETB", StakeAmount, user.Balance)
		e.sendError(telegramID, err.Error())
		return err
	}

	// Check reservation
	if reservedBy, ok := state.ReservedCards[cardNumber]; ok {
		if reservedBy == telegramID {
			err := fmt.Errorf("card already reserved by you")
			e.sendError(telegramID, err.Error())
			return err
		}
		err := fmt.Errorf("card already reserved by another player")
		e.sendError(telegramID, err.Error())
		return err
	}

	if len(state.UserCards[telegramID]) >= MaxCardsPerPlayer {
		err := fmt.Errorf("maximum %d cards allowed per player", MaxCardsPerPlayer)
		e.sendError(telegramID, err.Error())
		return err
	}

	// Reserve in memory
	state.ReservedCards[cardNumber] = telegramID
	state.UserCards[telegramID] = append(state.UserCards[telegramID], cardNumber)
	e.UpdatePool(state)

	// Get card data
	cardData, found := GetCardByID(cardNumber)
	if !found {
		e.rollbackReservationLocked(state, telegramID, cardNumber)
		err := fmt.Errorf("card data not found")
		e.sendError(telegramID, err.Error())
		return err
	}

	// Create card record
	card := models.Card{
		ID:            uuid.New(),
		GameID:        state.Game.ID,
		UserID:        user.ID,
		CardNumber:    cardNumber,
		CardData:      cardData,
		MarkedNumbers: pq.Int64Array{},
		IsWinner:      false,
		Status:        "reserved",
	}

	if err := e.db.Create(&card).Error; err != nil {
		e.rollbackReservationLocked(state, telegramID, cardNumber)
		errMsg := fmt.Sprintf("failed saving card: %v", err)
		e.sendError(telegramID, errMsg)
		return fmt.Errorf("failed saving card: %w", err)
	}

	// Broadcast success
	grossPool := state.Game.TotalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	e.broadcast(GameEvent{
		Type:       "card.reserved",
		GameID:     state.Game.ID.String(),
		CardNumber: cardNumber,
		UserID:     telegramID,
		Card:       &card,
		Players:    len(state.UserCards),
		Pool:       netPool,
		GrossPool:  grossPool,
		HouseCut:   houseCut,
		Stake:      StakeAmount,
		Message:    fmt.Sprintf("Card #%d reserved! Prize Pool: $%.2f", cardNumber, netPool),
	})

	log.Printf("🟢 Card %d reserved for user %d", cardNumber, telegramID)
	return nil
}

// ✅ sendError - Send error event to frontend


// ✅ Internal rollback - assumes lock is already held
func (e *Engine) rollbackReservationLocked(state *GameState, telegramID int64, cardNumber int) {
	log.Printf("🔴 Rolling back reservation for user %d, card %d", telegramID, cardNumber)
	delete(state.ReservedCards, cardNumber)
	userCards := state.UserCards[telegramID]
	for i, num := range userCards {
		if num == cardNumber {
			state.UserCards[telegramID] = append(userCards[:i], userCards[i+1:]...)
			break
		}
	}
	e.UpdatePool(state)
}

// ✅ rollbackReservation - Public version (acquires lock)
func (e *Engine) rollbackReservation(state *GameState, telegramID int64, cardNumber int) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	e.rollbackReservationLocked(state, telegramID, cardNumber)
}

// CancelReservation cancels a card reservation
func (e *Engine) CancelReservation(telegramID int64, cardNumber int) error {
	if e.currentGame == nil {
		return fmt.Errorf("no active game")
	}

	state := e.currentGame
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.Game.Status != GameStatusWaiting {
		return fmt.Errorf("game already started - cannot cancel")
	}

	reservedBy, ok := state.ReservedCards[cardNumber]
	if !ok || reservedBy != telegramID {
		return fmt.Errorf("card not reserved by you")
	}

	user, err := e.getUserByTelegramID(telegramID)
	if err != nil {
		return err
	}

	// Remove from memory
	delete(state.ReservedCards, cardNumber)
	userCards := state.UserCards[telegramID]
	for i, num := range userCards {
		if num == cardNumber {
			state.UserCards[telegramID] = append(userCards[:i], userCards[i+1:]...)
			break
		}
	}

	// Delete from database
	if err := e.db.Where("game_id = ? AND card_number = ? AND user_id = ?",
		state.Game.ID, cardNumber, user.ID).Delete(&models.Card{}).Error; err != nil {
		return fmt.Errorf("failed to delete card: %w", err)
	}

	e.UpdatePool(state)
	grossPool := state.Game.TotalPool
	netPool, houseCut := GetPoolBreakdown(grossPool)

	e.broadcast(GameEvent{
		Type:       "card.cancelled",
		GameID:     state.Game.ID.String(),
		CardNumber: cardNumber,
		UserID:     telegramID,
		Pool:       netPool,
		GrossPool:  grossPool,
		HouseCut:   houseCut,
		Message:    fmt.Sprintf("Card #%d cancelled. Prize Pool: $%.2f", cardNumber, netPool),
	})

	return nil
}