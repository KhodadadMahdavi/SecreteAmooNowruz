package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const sessionTokenBytes = 32

func GenerateSessionToken() (plainToken string, tokenHash string, err error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}

	plainToken = base64.RawURLEncoding.EncodeToString(raw)
	tokenHash = HashSessionToken(plainToken)
	return plainToken, tokenHash, nil
}

func HashSessionToken(plainToken string) string {
	sum := sha256.Sum256([]byte(plainToken))
	return hex.EncodeToString(sum[:])
}
