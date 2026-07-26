package verify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// VerifyClient handles communication with verify.et API
type VerifyClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// VerifyRequest represents the request to verify.et API
type VerifyRequest struct {
	TransactionID string  `json:"transaction_id"`
	Amount        float64 `json:"amount"`
	PhoneNumber   string  `json:"phone_number,omitempty"`
}

// VerifyResponse represents the response from verify.et API
type VerifyResponse struct {
	Success      bool    `json:"success"`
	TransactionID string `json:"transaction_id"`
	Amount       float64 `json:"amount"`
	Status       string  `json:"status"`
	SenderName   string  `json:"sender_name"`
	ReceiverName string  `json:"receiver_name"`
	Timestamp    string  `json:"timestamp"`
	Message      string  `json:"message"`
}

// NewVerifyClient creates a new verify.et client
func NewVerifyClient(apiKey string) *VerifyClient {
	return &VerifyClient{
		BaseURL: "https://api.verify.et/v1",
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// VerifyTransaction verifies a Telebirr transaction
func (c *VerifyClient) VerifyTransaction(txnID string, amount float64) (*VerifyResponse, error) {
	url := fmt.Sprintf("%s/verify/telebirr", c.BaseURL)

	reqBody := VerifyRequest{
		TransactionID: txnID,
		Amount:        amount,
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.APIKey))

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var verifyResp VerifyResponse
	if err := json.Unmarshal(body, &verifyResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &verifyResp, nil
}

// VerifyAndUpdateTransaction verifies and updates user balance
func (c *VerifyClient) VerifyAndUpdateTransaction(txnID string, amount float64, userID int64) error {
	resp, err := c.VerifyTransaction(txnID, amount)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("transaction verification failed: %s", resp.Message)
	}

	if resp.Status != "completed" {
		return fmt.Errorf("transaction not completed: %s", resp.Status)
	}

	// Amount mismatch check
	if resp.Amount != amount {
		return fmt.Errorf("amount mismatch: expected %.2f, got %.2f", amount, resp.Amount)
	}

	return nil
}