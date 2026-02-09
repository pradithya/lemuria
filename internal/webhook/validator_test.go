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
	"testing"
)

func computeHMAC(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestValidatorValidate(t *testing.T) {
	secret := "test-webhook-secret"
	payload := []byte(`{"action":"opened","number":1}`)

	tests := []struct {
		name      string
		secret    string
		payload   []byte
		signature string
		want      bool
	}{
		{
			name:      "valid signature",
			secret:    secret,
			payload:   payload,
			signature: computeHMAC(payload, secret),
			want:      true,
		},
		{
			name:      "empty signature",
			secret:    secret,
			payload:   payload,
			signature: "",
			want:      false,
		},
		{
			name:      "wrong prefix",
			secret:    secret,
			payload:   payload,
			signature: "sha1=abc123",
			want:      false,
		},
		{
			name:      "wrong secret",
			secret:    secret,
			payload:   payload,
			signature: computeHMAC(payload, "wrong-secret"),
			want:      false,
		},
		{
			name:      "tampered payload",
			secret:    secret,
			payload:   []byte(`{"action":"closed"}`),
			signature: computeHMAC(payload, secret),
			want:      false,
		},
		{
			name:      "invalid hex in signature",
			secret:    secret,
			payload:   payload,
			signature: "sha256=not-valid-hex",
			want:      false,
		},
		{
			name:      "empty payload with valid signature",
			secret:    secret,
			payload:   []byte{},
			signature: computeHMAC([]byte{}, secret),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := NewValidator(tt.secret)
			got := v.Validate(tt.payload, tt.signature)
			if got != tt.want {
				t.Errorf("Validate() = %v, want %v", got, tt.want)
			}
		})
	}
}
