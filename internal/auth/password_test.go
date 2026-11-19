package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct-horse") {
		t.Fatal("expected password to match")
	}
	if VerifyPassword(hash, "wrong-password") {
		t.Fatal("expected password mismatch")
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Fatal("expected short password error")
	}
	if err := ValidatePassword("long-enough"); err != nil {
		t.Fatal(err)
	}
}
