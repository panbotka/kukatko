package ctl

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

// archivedBody is a photo as the archive endpoint answers with it: the whole
// refreshed row, of which only the two lifecycle fields are the result.
const archivedBody = `{"uid":"pht01","file_name":"a.jpg","title":"Lake","file_size":2097152,
	"archived_at":"2026-08-01T10:22:33Z","hidden_from_library":false}`

// TestClient_SetPhotoState verifies each reversible state posts to its own
// endpoint and that the refreshed photo comes back decodable.
func TestClient_SetPhotoState(t *testing.T) {
	t.Parallel()

	states := []string{StateArchive, StateUnarchive, StateHide, StateUnhide}
	for _, state := range states {
		t.Run(state, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				w.Write([]byte(archivedBody))
			})

			raw, err := client.SetPhotoState(t.Context(), "pht01", state)
			if err != nil {
				t.Fatalf("SetPhotoState(%q) returned %v", state, err)
			}
			if gotMethod != http.MethodPost || gotPath != "/api/v1/photos/pht01/"+state {
				t.Errorf("request = %s %s, want POST the %s endpoint", gotMethod, gotPath, state)
			}
			decoded, err := DecodePhotoState(raw)
			if err != nil {
				t.Fatalf("DecodePhotoState returned %v", err)
			}
			if decoded.UID != "pht01" || !decoded.Archived() {
				t.Errorf("state = %+v, want the archived photo", decoded)
			}
		})
	}
}

// TestClient_SetPhotoState_blankUID verifies a missing uid is refused locally,
// before a request is spent on it.
func TestClient_SetPhotoState_blankUID(t *testing.T) {
	t.Parallel()

	called := false
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.Write([]byte(archivedBody))
	})

	if _, err := client.SetPhotoState(t.Context(), "  ", StateArchive); !errors.Is(err, ErrEmptyUID) {
		t.Fatalf("SetPhotoState(blank) error = %v, want ErrEmptyUID", err)
	}
	if called {
		t.Error("a blank uid still reached the server")
	}
}

// TestWritePhotoState verifies the line reports both lifecycle flags, not only
// the one the command changed.
func TestWritePhotoState(t *testing.T) {
	t.Parallel()

	archived := time.Date(2026, time.August, 1, 10, 22, 0, 0, time.UTC)
	tests := []struct {
		name  string
		state PhotoState
		want  []string
	}{
		{
			name:  "live photo named by its title",
			state: PhotoState{UID: "pht01", FileName: "a.jpg", Title: "Lake"},
			want:  []string{"Lake (pht01)", "in the library"},
		},
		{
			name:  "untitled photo falls back to its file",
			state: PhotoState{UID: "pht02", FileName: "b.jpg"},
			want:  []string{"b.jpg (pht02)", "in the library"},
		},
		{
			name:  "archived and hidden reports both",
			state: PhotoState{UID: "pht03", FileName: "c.jpg", ArchivedAt: &archived, Hidden: true},
			want:  []string{"in the trash since 2026-08-01 10:22", "hidden from the library grid"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := WritePhotoState(&buf, tt.state); err != nil {
				t.Fatalf("WritePhotoState returned %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(buf.String(), want) {
					t.Errorf("output %q is missing %q", buf.String(), want)
				}
			}
		})
	}
}

// TestDescribePurgeTarget verifies the rehearsal says outright when a photo
// cannot be purged at all, so nobody confirms a command the server will refuse.
func TestDescribePurgeTarget(t *testing.T) {
	t.Parallel()

	archived := time.Date(2026, time.August, 1, 10, 22, 0, 0, time.UTC)
	live := PhotoDetail{Photo: Photo{FileSize: 2097152}}
	if got := DescribePurgeTarget(live); !strings.Contains(got, "purging is refused") {
		t.Errorf("DescribePurgeTarget(live) = %q, want the refusal spelled out", got)
	}
	inTrash := PhotoDetail{Photo: Photo{FileSize: 2097152, ArchivedAt: &archived}}
	got := DescribePurgeTarget(inTrash)
	if !strings.Contains(got, "2.0 MiB") || !strings.Contains(got, "in the trash since") {
		t.Errorf("DescribePurgeTarget(archived) = %q, want the size and the archive date", got)
	}
}
