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

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSignAndVerifySessionID(t *testing.T) {
	secret := []byte("test-secret-key-that-is-long-enough-for-hmac")
	sessionID := "session-abc-123"

	// Sign the session ID
	signed := signSessionID(secret, sessionID)

	// Verify and extract
	extractedID, err := verifyAndExtractSessionID(secret, signed)
	if err != nil {
		t.Fatalf("verifyAndExtractSessionID() error = %v", err)
	}
	if extractedID != sessionID {
		t.Errorf("extracted session ID = %q, want %q", extractedID, sessionID)
	}
}

func TestVerifyAndExtractSessionID_ValidSignature(t *testing.T) {
	secret := []byte("my-secret-key")
	sessionID := "test-session-id"

	// Compute valid HMAC
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sessionID))
	sig := hex.EncodeToString(mac.Sum(nil))
	cookieValue := sessionID + "." + sig

	got, err := verifyAndExtractSessionID(secret, cookieValue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != sessionID {
		t.Errorf("got session ID %q, want %q", got, sessionID)
	}
}

func TestVerifyAndExtractSessionID_InvalidSignature(t *testing.T) {
	secret := []byte("my-secret-key")
	sessionID := "test-session-id"

	// Create a valid-length but wrong signature (64 hex chars = 32 bytes = sha256.Size)
	wrongSig := strings.Repeat("ab", sha256.Size)
	cookieValue := sessionID + "." + wrongSig

	_, err := verifyAndExtractSessionID(secret, cookieValue)
	if err == nil {
		t.Fatal("expected error for wrong signature, got nil")
	}
	if !strings.Contains(err.Error(), "invalid session cookie signature") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestVerifyAndExtractSessionID_MalformedHex(t *testing.T) {
	secret := []byte("my-secret-key")
	cookieValue := "session-id.not-valid-hex!!!"

	_, err := verifyAndExtractSessionID(secret, cookieValue)
	if err == nil {
		t.Fatal("expected error for malformed hex, got nil")
	}
	if !strings.Contains(err.Error(), "malformed hex") {
		t.Errorf("expected 'malformed hex' in error, got: %v", err)
	}
}

func TestVerifyAndExtractSessionID_WrongLength(t *testing.T) {
	secret := []byte("my-secret-key")
	// Valid hex but too short (only 4 bytes instead of 32)
	cookieValue := "session-id.aabbccdd"

	_, err := verifyAndExtractSessionID(secret, cookieValue)
	if err == nil {
		t.Fatal("expected error for wrong length signature, got nil")
	}
	if !strings.Contains(err.Error(), "wrong length") {
		t.Errorf("expected 'wrong length' in error, got: %v", err)
	}
}

func TestVerifyAndExtractSessionID_NoDot(t *testing.T) {
	secret := []byte("my-secret-key")
	cookieValue := "no-dot-in-this-value"

	_, err := verifyAndExtractSessionID(secret, cookieValue)
	if err == nil {
		t.Fatal("expected error for missing dot, got nil")
	}
	if !strings.Contains(err.Error(), "invalid signed cookie format") {
		t.Errorf("expected 'invalid signed cookie format' in error, got: %v", err)
	}
}

func TestVerifyAndExtractSessionID_WrongSecret(t *testing.T) {
	secret := []byte("my-secret-key")
	wrongSecret := []byte("wrong-secret-key")
	sessionID := "test-session-id"

	// Sign with the correct secret
	signed := signSessionID(secret, sessionID)

	// Verify with the wrong secret
	_, err := verifyAndExtractSessionID(wrongSecret, signed)
	if err == nil {
		t.Fatal("expected error for wrong secret, got nil")
	}
}

func TestVerifyAndExtractSessionID_TruncatedHex(t *testing.T) {
	secret := []byte("my-secret-key")
	sessionID := "test-session-id"

	// Compute valid HMAC then truncate the hex signature
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sessionID))
	fullSig := hex.EncodeToString(mac.Sum(nil))
	truncatedSig := fullSig[:32] // 16 bytes instead of 32
	cookieValue := sessionID + "." + truncatedSig

	_, err := verifyAndExtractSessionID(secret, cookieValue)
	if err == nil {
		t.Fatal("expected error for truncated signature, got nil")
	}
	if !strings.Contains(err.Error(), "wrong length") {
		t.Errorf("expected 'wrong length' in error, got: %v", err)
	}
}
