// internal/verify/verify.go

package verify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type VerifyClient struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

type VerifyRequest struct {
	Bank              string  `json:"bank"`
	TransactionNumber string  `json:"transactionNumber"` // ✅ Correct field name
	Amount            float64 `json:"amount,omitempty"`
	SettlementAccount string  `json:"settlementAccount,omitempty"` // ✅ For receiver verification
}

type VerifyResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    struct {
		RequestID string `json:"requestId"`
		Result    struct {
			Status       string  `json:"status"`
			Amount       float64 `json:"amount"`
			SenderName   string  `json:"senderName"`
			ReceiverName string  `json:"receiverName"`
			Receiver     string  `json:"receiver"` // ✅ This is the receiver account
			Matched      bool    `json:"matched"`   // ✅ Settlement account match
		} `json:"result"`
	} `json:"data"`
}

func NewVerifyClient(apiKey string) *VerifyClient {
	// ✅ Correct base URL
	return &VerifyClient{
		BaseURL: "https://verify.et",
		APIKey:  apiKey,
		Client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *VerifyClient) VerifyTransaction(txnID string, amount float64, receiverPhone string) (*VerifyResponse, error) {
	// ✅ Correct endpoint
	url := fmt.Sprintf("%s/api/verify", c.BaseURL)

	reqBody := VerifyRequest{
		Bank:              "telebirr",
		TransactionNumber: txnID,
		Amount:            amount,
		SettlementAccount: receiverPhone, // ✅ Verify it was sent to our account
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var verifyResp VerifyResponse
	if err := json.Unmarshal(body, &verifyResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &verifyResp, nil
}