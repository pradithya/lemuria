package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Validator validates GitHub webhook signatures.
type Validator struct {
	secret []byte
}

// NewValidator creates a new webhook validator.
func NewValidator(secret string) *Validator {
	return &Validator{
		secret: []byte(secret),
	}
}

// Validate checks the HMAC-SHA256 signature of a webhook payload.
func (v *Validator) Validate(payload []byte, signature string) bool {
	if signature == "" {
		return false
	}

	// Expected format: "sha256=<hex>"
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	expectedMAC, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, v.secret)
	mac.Write(payload)
	actualMAC := mac.Sum(nil)

	return hmac.Equal(expectedMAC, actualMAC)
}
