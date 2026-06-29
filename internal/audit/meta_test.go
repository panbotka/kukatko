package audit

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

// TestClientIP verifies the client IP is taken from proxy headers first
// (X-Forwarded-For first hop, then X-Real-IP) and falls back to the RemoteAddr
// host with the port stripped.
func TestClientIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		xff        string
		realIP     string
		remoteAddr string
		want       string
	}{
		{name: "forwarded-for first hop wins", xff: "203.0.113.7, 10.0.0.1", remoteAddr: "10.0.0.9:5555", want: "203.0.113.7"},
		{name: "real-ip used when no forwarded-for", realIP: "198.51.100.4", remoteAddr: "10.0.0.9:5555", want: "198.51.100.4"},
		{name: "remote addr host fallback", remoteAddr: "192.0.2.5:42000", want: "192.0.2.5"},
		{name: "remote addr without port", remoteAddr: "192.0.2.6", want: "192.0.2.6"},
		{name: "empty everything", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &http.Request{Header: http.Header{}, RemoteAddr: tt.remoteAddr}
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.realIP != "" {
				r.Header.Set("X-Real-IP", tt.realIP)
			}
			if got := clientIP(r); got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestFromRequestAndEntry verifies FromRequest captures actor, IP and User-Agent
// and that Meta.Entry stamps them onto a new entry with the given action,
// target and details.
func TestFromRequestAndEntry(t *testing.T) {
	t.Parallel()

	r := &http.Request{Header: http.Header{}, RemoteAddr: "192.0.2.10:1234"}
	r.Header.Set("User-Agent", "test-agent/1.0")
	meta := FromRequest(r, "us-actor")
	if meta.ActorUID != "us-actor" || meta.IP != "192.0.2.10" || meta.UserAgent != "test-agent/1.0" {
		t.Fatalf("FromRequest() = %+v, want actor/ip/ua populated", meta)
	}

	details := map[string]any{"fields": []string{"title"}}
	entry := meta.Entry(ActionPhotoUpdate, "photos", "ph-1", details)
	want := Entry{
		ActorUID:   "us-actor",
		Action:     ActionPhotoUpdate,
		TargetType: "photos",
		TargetUID:  "ph-1",
		Details:    details,
		IP:         "192.0.2.10",
		UserAgent:  "test-agent/1.0",
	}
	if !reflect.DeepEqual(entry, want) {
		t.Errorf("Meta.Entry() = %+v, want %+v", entry, want)
	}
}

// TestFilterBuildWhere verifies the dynamic WHERE clause and positional
// arguments are built only from the non-empty filter fields, in order.
func TestFilterBuildWhere(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	t.Run("empty filter has no clause", func(t *testing.T) {
		t.Parallel()
		where, args := Filter{}.buildWhere()
		if where != "" || args != nil {
			t.Errorf("buildWhere() = (%q, %v), want empty", where, args)
		}
	})

	t.Run("populated filter builds ordered clause", func(t *testing.T) {
		t.Parallel()
		where, args := Filter{ActorUID: "us-1", Action: ActionPhotoArchive, Since: &since}.buildWhere()
		wantWhere := " WHERE actor_uid = $1 AND action = $2 AND created_at >= $3"
		if where != wantWhere {
			t.Errorf("buildWhere() where = %q, want %q", where, wantWhere)
		}
		if len(args) != 3 || args[0] != "us-1" || args[1] != ActionPhotoArchive || args[2] != since {
			t.Errorf("buildWhere() args = %v, want [us-1 %s %v]", args, ActionPhotoArchive, since)
		}
	})
}
