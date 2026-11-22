package argocd

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPassword(t *testing.T) {
	pass := "mySecretPass123!"
	hashed, err := HashPassword(pass, 10)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(pass)); err != nil {
		t.Fatalf("Hash verification failed: %v", err)
	}

	wrongPass := "wrongPass"
	if err := bcrypt.CompareHashAndPassword([]byte(hashed), []byte(wrongPass)); err == nil {
		t.Fatalf("Hash verification should have failed for wrong password")
	}
}

func TestCalculateChecksum(t *testing.T) {
	spec1 := UserSpec{
		Username: "jenkins",
		Password: "password123",
		Enabled:  true,
		APIKey:   true,
		Login:    true,
		Roles:    "role:admin",
	}

	spec2 := UserSpec{
		Username: "jenkins",
		Password: "password123",
		Enabled:  true,
		APIKey:   true,
		Login:    true,
		Roles:    "role:admin",
	}

	spec3 := UserSpec{
		Username: "jenkins",
		Password: "password456", // changed password
		Enabled:  true,
		APIKey:   true,
		Login:    true,
		Roles:    "role:admin",
	}

	c1 := CalculateChecksum(spec1)
	c2 := CalculateChecksum(spec2)
	c3 := CalculateChecksum(spec3)

	if c1 != c2 {
		t.Fatalf("Expected checksums for identical specs to match: %s vs %s", c1, c2)
	}

	if c1 == c3 {
		t.Fatalf("Expected checksums for different passwords to differ: %s vs %s", c1, c3)
	}
}
