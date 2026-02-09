package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	t.Helper()

	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	valid, err := VerifyPassword(hash, "password123")
	if err != nil {
		t.Fatalf("VerifyPassword(valid) error = %v", err)
	}
	if !valid {
		t.Fatal("VerifyPassword(valid) = false, want true")
	}

	valid, err = VerifyPassword(hash, "wrong-password")
	if err != nil {
		t.Fatalf("VerifyPassword(wrong) error = %v", err)
	}
	if valid {
		t.Fatal("VerifyPassword(wrong) = true, want false")
	}
}

func TestHashPasswordRequiresInput(t *testing.T) {
	t.Helper()

	_, err := HashPassword("")
	if err == nil {
		t.Fatal("HashPassword(\"\") error = nil, want error")
	}
}
