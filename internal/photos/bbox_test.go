package photos

import (
	"errors"
	"testing"
)

func TestParseBBox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    BBox
		wantErr bool
	}{
		{name: "plain box", raw: "49.1,16.5,49.4,16.9", want: BBox{South: 49.1, West: 16.5, North: 49.4, East: 16.9}},
		{name: "spaces tolerated", raw: " 49.1, 16.5 ,49.4,16.9", want: BBox{South: 49.1, West: 16.5, North: 49.4, East: 16.9}},
		{name: "antimeridian box", raw: "-10,170,10,-170", want: BBox{South: -10, West: 170, North: 10, East: -170}},
		{name: "whole world", raw: "-90,-180,90,180", want: BBox{South: -90, West: -180, North: 90, East: 180}},
		{name: "three parts", raw: "1,2,3", wantErr: true},
		{name: "five parts", raw: "1,2,3,4,5", wantErr: true},
		{name: "not a number", raw: "a,2,3,4", wantErr: true},
		{name: "nan", raw: "NaN,2,3,4", wantErr: true},
		{name: "south above north", raw: "50,16,49,17", wantErr: true},
		{name: "latitude out of range", raw: "-91,16,49,17", wantErr: true},
		{name: "longitude out of range", raw: "49,16,50,181", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseBBox(tt.raw)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidBBox) {
					t.Fatalf("ParseBBox(%q) error = %v, want ErrInvalidBBox", tt.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBBox(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("ParseBBox(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestBBoxClauses(t *testing.T) {
	t.Parallel()

	bind := func() func(any) string {
		n := 0
		return func(any) string {
			n++
			return "$" + string(rune('0'+n))
		}
	}
	if got := bboxClauses(ListParams{}, bind()); got != nil {
		t.Errorf("no box: got %v, want nil", got)
	}
	plain := bboxClauses(ListParams{BBox: &BBox{South: 1, West: 2, North: 3, East: 4}}, bind())
	want := []string{"lat BETWEEN $1 AND $2", "lng BETWEEN $3 AND $4"}
	if len(plain) != 2 || plain[0] != want[0] || plain[1] != want[1] {
		t.Errorf("plain box: got %v, want %v", plain, want)
	}
	wrapped := bboxClauses(ListParams{BBox: &BBox{South: -1, West: 170, North: 1, East: -170}}, bind())
	if len(wrapped) != 2 || wrapped[1] != "(lng >= $3 OR lng <= $4)" {
		t.Errorf("antimeridian box: got %v", wrapped)
	}
}
