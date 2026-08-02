//go:build !integration

package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestHashPassword_productionCost is the guard on the tunable work factor: a
// build without the `integration` tag is every build that ever serves traffic,
// and it must mint hashes at cost 12. It runs in `make test`, so the quality
// gate fails the moment the default is lowered — deliberately or by a knob that
// leaks out of the test build.
func TestHashPassword_productionCost(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("bcrypt.Cost: %v", err)
	}
	if cost != productionBcryptCost {
		t.Errorf("production build mints bcrypt cost %d, want %d — password hashing has been weakened",
			cost, productionBcryptCost)
	}
}
