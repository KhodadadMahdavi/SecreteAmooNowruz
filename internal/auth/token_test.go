package auth

import "testing"

func TestGenerateSessionToken(t *testing.T) {
	t.Helper()

	token, hash, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	if hash == "" {
		t.Fatal("hash is empty")
	}
	if HashSessionToken(token) != hash {
		t.Fatal("HashSessionToken(token) does not match generated hash")
	}
}
