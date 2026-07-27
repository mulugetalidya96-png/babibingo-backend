// internal/sms/cbebirr_parser.go

package sms

import (
	"regexp"
	"strconv"
	"strings"
)

// CBEBirrTransaction represents parsed CBE Birr transaction details
type CBEBirrTransaction struct {
	Amount          float64
	TransactionID   string // Txn ID
	ReferenceNumber string // Reference number (same as Txn ID)
	Sender          string
	Receiver        string
	Date            string
	PhoneNumber     string
	ReceiptURL      string
	IsValid         bool
}

// ParseCBEBirrSMS parses CBE Birr SMS messages
func ParseCBEBirrSMS(smsText string) *CBEBirrTransaction {
	result := &CBEBirrTransaction{
		IsValid: false,
	}

	// Extract transaction ID / Reference Number
	txnRegex := regexp.MustCompile(`(?:Txn ID|TID[=\s]+)([A-Z0-9]+)`)
	if matches := txnRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		result.TransactionID = matches[1]
		result.ReferenceNumber = matches[1]
	}

	// Extract amount
	amountRegex := regexp.MustCompile(`sent\s+([0-9.]+)\s*Br`)
	if matches := amountRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		if amount, err := strconv.ParseFloat(matches[1], 64); err == nil {
			result.Amount = amount
		}
	}

	// Extract sender
	senderRegex := regexp.MustCompile(`Dear\s+([A-Za-z ]+)`)
	if matches := senderRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		result.Sender = strings.TrimSpace(matches[1])
	}

	// Extract receiver
	receiverRegex := regexp.MustCompile(`to\s+([A-Za-z ]+)`)
	if matches := receiverRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		result.Receiver = strings.TrimSpace(matches[1])
	}

	// Extract date
	dateRegex := regexp.MustCompile(`(\d{2}/\d{2}/\d{2}\s+\d{2}:\d{2})`)
	if matches := dateRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		result.Date = matches[1]
	}

	// ✅ Extract phone number from URL
	phoneRegex := regexp.MustCompile(`PH=([0-9]+)`)
	if matches := phoneRegex.FindStringSubmatch(smsText); len(matches) > 1 {
		result.PhoneNumber = matches[1]
	}

	// ✅ Extract receipt URL
	urlRegex := regexp.MustCompile(`https://cbepay1\.cbe\.com\.et/aureceipt\?TID=[A-Z0-9]+&PH=[0-9]+`)
	if matches := urlRegex.FindString(smsText); matches != "" {
		result.ReceiptURL = matches
	}

	// ✅ Validate: need transaction ID, amount, and phone number
	if result.TransactionID != "" && result.Amount > 0 && result.PhoneNumber != "" {
		result.IsValid = true
	}

	return result
}

// IsCBEBirrSMS checks if the SMS is from CBE Birr
func IsCBEBirrSMS(smsText string) bool {
	keywords := []string{
		"CBE Birr",
		"cbepay1.cbe.com.et",
		"sent.*Br",
		"Txn ID",
	}

	lowerText := strings.ToLower(smsText)
	for _, keyword := range keywords {
		if strings.Contains(lowerText, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}