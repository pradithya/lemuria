// Copyright 2026 Lemuria Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
