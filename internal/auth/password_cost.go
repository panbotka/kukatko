//go:build !integration

package auth

// hashCost is the bcrypt work factor HashPassword mints at. This file is
// compiled into every build that is not the integration-test build — that is,
// into everything that ever runs as a server — and pins the cost to the
// production constant.
//
// There is deliberately no way to change it at run time: the cheaper cost lives
// in password_cost_integration.go behind the `integration` build tag and is not
// part of this binary at all. See bcryptCost for the full reasoning.
const hashCost = bcryptCost
