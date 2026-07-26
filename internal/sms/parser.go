package sms

import (
	"regexp"
	"strconv"
	"strings"
)

// TransactionInfo represents parsed transaction details
type TransactionInfo struct {
	Amount          float64
	TransactionID   string
	Sender          string
	Receiver        string
	Date            string
	Balance         float64
	IsValid         bool
}

// ParseTelebirrSMS parses Telebirr SMS messages
func ParseTelebirrSMS(smsText string) *TransactionInfo {
	result := &TransactionInfo{
		IsValid: false,
	}

	// Extract transaction number
	txnRegex := regexp.MustCompile(`transaction number is ([A-Z0-9]+)`)
	if matches := txnRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		result.TransactionID = matches[1]
	}

	// Extract amount
	amountRegex := regexp.MustCompile(`transferred ETB ([0-9.]+) to`)
	if matches := amountRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		if amount, err := strconv.ParseFloat(matches[1], 64); err == nil {
			result.Amount = amount
		}
	}

	// Extract sender
	senderRegex := regexp.MustCompile(`Dear ([A-Za-z ]+)`)
	if matches := senderRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		result.Sender = strings.TrimSpace(matches[1])
	}

	// Extract receiver
	receiverRegex := regexp.MustCompile(`to ([A-Za-z ]+) \([0-9]+\*+[0-9]+\)`)
	if matches := receiverRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		result.Receiver = strings.TrimSpace(matches[1])
	}

	// Extract balance
	balanceRegex := regexp.MustCompile(`balance is ETB ([0-9.]+)`)
	if matches := balanceRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		if balance, err := strconv.ParseFloat(matches[1], 64); err == nil {
			result.Balance = balance
		}
	}

	// Check if we have enough info
	if result.TransactionID != "" && result.Amount > 0 {
		result.IsValid = true
	}

	return result
}

// IsTelebirrSMS checks if the SMS is from Telebirr
func IsTelebirrSMS(smsText string) bool {
	keywords := []string{
		"telebirr",
		"Ethio telecom",
		"transferred ETB",
		"transaction number is",
	}
	
	lowerText := strings.ToLower(smsText)
	for _, keyword := range keywords {
		if strings.Contains(lowerText, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}