package bot

import (
	"math/rand"
	"strings"
	"time"
)

func generateReferralCode() string {

	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	rand.Seed(time.Now().UnixNano())

	code := make([]byte, 6)

	for i := range code {
		code[i] = letters[rand.Intn(len(letters))]
	}

	return string(code)
}


func cleanCommand(text string) string {

	text = strings.TrimSpace(text)

	text = strings.TrimPrefix(
		text,
		"/",
	)

	if index := strings.Index(
		text,
		" ",
	); index != -1 {

		text = text[:index]
	}

	return text
}