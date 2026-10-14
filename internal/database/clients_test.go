package database

import (
	"strings"
	"testing"
	"time"
)

func TestApplicationName_shape(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("9", 80)
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"release", "0.32.0", "kukatko/0.32.0 1a2b3c4d"},
		{"dev build", "dev", "kukatko/dev 1a2b3c4d"},
		{"git describe", "v0.32.0-3-gabc+dirty", "kukatko/v0.32.0-3-gabc+dirty 1a2b3c4d"},
		{"non-ascii and spaces become dashes", "1.0 β", "kukatko/1.0-- 1a2b3c4d"},
		{"empty version", "", "kukatko/unknown 1a2b3c4d"},
		{"too long is cut to fit", long, "kukatko/" + long[:63-len("kukatko/")-len(" 1a2b3c4d")] + " 1a2b3c4d"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := applicationName(tc.version, "1a2b3c4d")
			if got != tc.want {
				t.Errorf("applicationName(%q) = %q, want %q", tc.version, got, tc.want)
			}
			if len(got) > maxApplicationName {
				t.Errorf("applicationName(%q) is %d bytes, over the server's %d", tc.version, len(got), maxApplicationName)
			}
		})
	}
}

func TestApplicationName_stablePerProcess(t *testing.T) {
	t.Parallel()
	first, second := ApplicationName(), ApplicationName()
	if first != second {
		t.Errorf("ApplicationName() = %q then %q, want one name per process", first, second)
	}
	if !strings.HasPrefix(first, ApplicationNamePrefix) {
		t.Errorf("ApplicationName() = %q, want the %q prefix", first, ApplicationNamePrefix)
	}
	if !(Client{ApplicationName: first}).Kukatko() {
		t.Errorf("a client announcing %q is not recognised as Kukátko", first)
	}
}

func TestClient_String(t *testing.T) {
	t.Parallel()
	since := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		client Client
		want   string
	}{
		{
			"named kukatko",
			Client{PID: 42, ApplicationName: "kukatko/0.32.0 1a2b3c4d", User: "kukatko", Address: "172.18.0.5", Since: since},
			`pid 42 "kukatko/0.32.0 1a2b3c4d" user kukatko from 172.18.0.5 since 2026-10-14T09:00:00Z`,
		},
		{
			"unnamed old release over a socket",
			Client{PID: 7, User: "kukatko"},
			"pid 7 (no application_name — Kukátko 0.31.0 or older, or another tool) user kukatko",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.client.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
	if (Client{ApplicationName: "psql"}).Kukatko() || (Client{}).Kukatko() {
		t.Error("a psql/unnamed client was recognised as Kukátko")
	}
}

func TestSchemaStatusFrom(t *testing.T) {
	t.Parallel()
	migs := []migration{
		{version: 1, filename: "0001_a.sql"},
		{version: 2, filename: "0002_b.sql"},
		{version: 5, filename: "0005_c.sql"},
	}
	tests := []struct {
		name        string
		applied     map[int64]bool
		wantCurrent int64
		wantPending []string
	}{
		{"empty database", map[int64]bool{}, 0, []string{"0001_a.sql", "0002_b.sql", "0005_c.sql"}},
		{"one behind", map[int64]bool{1: true, 2: true}, 2, []string{"0005_c.sql"}},
		{"gap in the middle", map[int64]bool{1: true, 5: true}, 5, []string{"0002_b.sql"}},
		{"up to date", map[int64]bool{1: true, 2: true, 5: true}, 5, nil},
		{"ahead of the binary", map[int64]bool{1: true, 2: true, 5: true, 9: true}, 9, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := schemaStatusFrom(migs, tc.applied)
			if got.Current != tc.wantCurrent || got.Expected != 5 {
				t.Errorf("Current/Expected = %d/%d, want %d/5", got.Current, got.Expected, tc.wantCurrent)
			}
			if strings.Join(got.Pending, ",") != strings.Join(tc.wantPending, ",") {
				t.Errorf("Pending = %v, want %v", got.Pending, tc.wantPending)
			}
			if got.Behind() != (len(tc.wantPending) > 0) {
				t.Errorf("Behind() = %v with pending %v", got.Behind(), got.Pending)
			}
		})
	}
}
