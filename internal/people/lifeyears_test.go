package people

import (
	"errors"
	"testing"
	"time"
)

// nowYear is the fixed "today" the table below is written against, so the cases
// mean the same thing whatever year the suite runs in.
const nowYear = 2026

// TestValidateLifeYears covers the boundaries of the birth/death rule: an
// unknown year is always fine, both years must sit inside [MinLifeYear, nowYear],
// and a death may not precede the birth.
func TestValidateLifeYears(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		birth   *int
		death   *int
		wantErr error
	}{
		{name: "both unknown", birth: nil, death: nil, wantErr: nil},
		{name: "birth only", birth: new(1923), death: nil, wantErr: nil},
		{name: "death only", birth: nil, death: new(1998), wantErr: nil},
		{name: "full life span", birth: new(1923), death: new(1998), wantErr: nil},
		{name: "died in the birth year", birth: new(1923), death: new(1923), wantErr: nil},
		{name: "earliest allowed birth", birth: new(MinLifeYear), death: nil, wantErr: nil},
		{name: "born this year", birth: new(nowYear), death: nil, wantErr: nil},
		{
			name:    "birth before the lower bound",
			birth:   new(MinLifeYear - 1),
			wantErr: ErrInvalidLifeYears,
		},
		{name: "mistyped three-digit birth", birth: new(198), wantErr: ErrInvalidLifeYears},
		{name: "birth in the future", birth: new(nowYear + 1), wantErr: ErrInvalidLifeYears},
		{name: "death in the future", death: new(nowYear + 1), wantErr: ErrInvalidLifeYears},
		{
			name:    "death below the lower bound",
			death:   new(MinLifeYear - 1),
			wantErr: ErrInvalidLifeYears,
		},
		{
			name:    "death precedes birth",
			birth:   new(1998),
			death:   new(1923),
			wantErr: ErrInvalidLifeYears,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateLifeYears(tt.birth, tt.death, nowYear)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validateLifeYears(%v, %v, %d) error = %v, want %v",
					tt.birth, tt.death, nowYear, err, tt.wantErr)
			}
		})
	}
}

// TestCheckLifeYears_usesTheCurrentYear pins the one thing the table above
// cannot: the exported write paths bound a year by the real calendar, so next
// year is rejected and this year is not.
func TestCheckLifeYears_usesTheCurrentYear(t *testing.T) {
	t.Parallel()

	thisYear := time.Now().Year()
	if err := checkLifeYears(new(thisYear), nil); err != nil {
		t.Errorf("checkLifeYears(%d) = %v, want nil — the current year is a valid birth year",
			thisYear, err)
	}
	if err := checkLifeYears(new(thisYear+1), nil); !errors.Is(err, ErrInvalidLifeYears) {
		t.Errorf("checkLifeYears(%d) = %v, want ErrInvalidLifeYears", thisYear+1, err)
	}
}
