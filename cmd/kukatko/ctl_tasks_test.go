package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestCtlTasks_createHandsOver verifies the repeatable --assign reaches the wire
// as `participants` beside --state, so handing work over is one command.
func TestCtlTasks_createHandsOver(t *testing.T) {
	var bodies []map[string]json.RawMessage
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"uid":"tk2","title":"Štítky","state":"working","photo_count":0}`))
	})

	if _, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "tasks", "create",
		"--title", "Štítky", "--state", "working", "--assign", "us-agent", "--assign", "us-anna"); err != nil {
		t.Fatalf("tasks create --assign: %v", err)
	}
	if got := string(bodies[0]["participants"]); got != `["us-agent","us-anna"]` {
		t.Errorf("create sent participants %s", got)
	}
	if got := string(bodies[0]["state"]); got != `"working"` {
		t.Errorf("create sent state %s", got)
	}
}

// TestCtlTasks_createPhotos verifies where the group comes from: arguments win,
// a piped list is read, and an empty pipe opens a task over nothing — saying so
// in the result, so an accidental empty pipe is visible.
func TestCtlTasks_createPhotos(t *testing.T) {
	tests := []struct {
		name       string
		stdin      string
		args       []string
		wantPhotos string
	}{
		{name: "arguments", args: []string{"ph1", "ph2"}, wantPhotos: `["ph1","ph2"]`},
		{name: "piped list", stdin: "ph3\nph4\n", wantPhotos: `["ph3","ph4"]`},
		{name: "empty pipe", stdin: ""},
		{name: "blank pipe", stdin: "\n\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body map[string]json.RawMessage
			configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&body)
				// The stub answers with the count it was sent, as the server would.
				count := "0"
				if tt.wantPhotos != "" {
					count = "2"
				}
				_, _ = w.Write([]byte(`{"uid":"tk2","title":"Nová","state":"question","photo_count":` + count + `}`))
			})
			args := append([]string{"ctl", "--ctl-config", configPath, "tasks", "create", "--title", "Nová"},
				tt.args...)
			out, err := runCtl(t, tt.stdin, args...)
			if err != nil {
				t.Fatalf("tasks create: %v", err)
			}
			got, sent := body["photo_uids"]
			if tt.wantPhotos == "" {
				if sent {
					t.Errorf("an empty group was sent as %s, want the field left out", got)
				}
				if !strings.Contains(out, "no photos") {
					t.Errorf("the result does not say \"no photos\":\n%s", out)
				}
				return
			}
			if string(got) != tt.wantPhotos {
				t.Errorf("create sent photo_uids %s, want %s", got, tt.wantPhotos)
			}
			if strings.Contains(out, "no photos") {
				t.Errorf("the result says \"no photos\" for a task with some:\n%s", out)
			}
		})
	}
}

// TestIsTerminal verifies only a character device counts as a terminal: a test
// buffer and a pipe are read, /dev/null (a character device, as a tty is) is not.
func TestIsTerminal(t *testing.T) {
	t.Parallel()

	if isTerminal(strings.NewReader("")) {
		t.Error("a strings.Reader was taken for a terminal")
	}
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = readEnd.Close() }()
	_ = writeEnd.Close()
	if isTerminal(readEnd) {
		t.Error("a pipe was taken for a terminal")
	}
	var reader io.Reader = readEnd
	if isTerminal(reader) {
		t.Error("a pipe behind an io.Reader was taken for a terminal")
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("opening %s: %v", os.DevNull, err)
	}
	defer func() { _ = null.Close() }()
	if !isTerminal(null) {
		t.Errorf("%s is a character device and should count as one", os.DevNull)
	}
}
