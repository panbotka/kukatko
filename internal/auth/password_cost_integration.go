//go:build integration

package auth

import (
	"fmt"
	"os"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

// EnvTestBcryptCost names the environment variable that sets the bcrypt work
// factor of the integration-test build. It is read only by this file, which is
// compiled only under the `integration` build tag (make test-integration), so a
// shipped binary contains neither the variable lookup nor a cost below
// bcryptCost.
const EnvTestBcryptCost = "KUKATKO_TEST_BCRYPT_COST"

// hashCost is the bcrypt work factor HashPassword mints at while the
// integration suite runs. It defaults to bcrypt.MinCost because roughly fifteen
// packages seed their accounts through the real auth path to exercise RBAC, and
// at the production cost those hashes — not the behaviour under test —
// accounted for almost all of the suite's runtime.
//
// Set EnvTestBcryptCost to run the suite at another cost, e.g.
// KUKATKO_TEST_BCRYPT_COST=12 to reproduce the production timings.
var hashCost = testHashCost(os.Getenv(EnvTestBcryptCost))

// testHashCost converts the raw EnvTestBcryptCost value into a bcrypt work
// factor. An unset (empty) value yields bcrypt.MinCost. A value that is not an
// integer within bcrypt's accepted range panics rather than falling back, so a
// typo fails the suite loudly instead of silently testing at a cost nobody
// intended.
func testHashCost(raw string) int {
	if raw == "" {
		return bcrypt.MinCost
	}
	cost, err := strconv.Atoi(raw)
	if err != nil || cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		panic(fmt.Sprintf("auth: %s=%q must be an integer between %d and %d",
			EnvTestBcryptCost, raw, bcrypt.MinCost, bcrypt.MaxCost))
	}
	return cost
}
