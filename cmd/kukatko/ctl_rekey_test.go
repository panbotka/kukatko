package main

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// rekeyRunBody is a realistic answer of the storage-move routes: a running run
// a quarter through with two errors.
const rekeyRunBody = `{"uid":"rk1","project_uid":"pjclub","state":"running","target_prefix":"projects/pjclub/",` +
	`"total_photos":200,"total_bytes":4294967296,"done_photos":50,"done_bytes":1073741824,"last_key":"",` +
	`"errors":[{"key":"2023/01/a.jpg","message":"copy does not match","at":"2026-10-13T10:00:00Z"}],` +
	`"error_count":2,"started_at":"2026-10-13T09:00:00Z","updated_at":"2026-10-13T10:00:00Z"}`

// rekeyServer starts a fake of GET /projects and the storage-move routes,
// recording every request as "METHOD path".
func rekeyServer(t *testing.T) (string, func() []string) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []string
	)
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/projects":
			_, _ = w.Write([]byte(projectsListBody))
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/projects/pjclub/rekey":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"already_prefixed"}`))
		default:
			_, _ = w.Write([]byte(rekeyRunBody))
		}
	})
	return configPath, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

// TestCtlProjectsRekey verifies each flag sends its request to the resolved
// project, the run renders as a table, and a refusal reports the server's code.
func TestCtlProjectsRekey(t *testing.T) {
	configPath, seen := rekeyServer(t)
	const veselice = "/api/v1/projects/pj0000000000000000veselice/rekey"
	tests := []struct {
		flag string
		want string
	}{
		{"", "POST " + veselice},
		{"--status", "GET " + veselice},
		{"--pause", "POST " + veselice + "/pause"},
		{"--resume", "POST " + veselice + "/resume"},
		{"--retry", "POST " + veselice + "/retry"},
	}
	for _, tt := range tests {
		args := []string{"ctl", "--ctl-config", configPath, "projects", "rekey", "v"}
		if tt.flag != "" {
			args = append(args, tt.flag)
		}
		out, err := runCtl(t, "", args...)
		if err != nil {
			t.Fatalf("projects rekey %s: %v", tt.flag, err)
		}
		all := seen()
		if got := all[len(all)-1]; got != tt.want {
			t.Errorf("projects rekey %s sent %q, want %q", tt.flag, got, tt.want)
		}
		for _, want := range []string{"running", "50 of 200 (25.0%)", "1.0 GiB of 4.0 GiB",
			"2023/01/a.jpg: copy does not match"} {
			if !strings.Contains(out, want) {
				t.Errorf("projects rekey %s output lacks %q:\n%s", tt.flag, want, out)
			}
		}
	}

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "rekey", "club"); err == nil ||
		!strings.Contains(err.Error(), "already_prefixed") {
		t.Errorf("projects rekey club = %v, want the server's already_prefixed", err)
	}
	before := len(seen())
	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "projects", "rekey", "v", "--pause", "--retry")
	if !errors.Is(err, errCtlRekeyFlagsExclusive) {
		t.Errorf("projects rekey --pause --retry = %v, want errCtlRekeyFlagsExclusive", err)
	}
	if len(seen()) != before {
		t.Error("conflicting flags still sent a request")
	}
}
