package audit

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/clientip"
	"github.com/panbotka/kukatko/internal/obs"
)

// TestFromRequest_ip verifies the audited IP is the address internal/clientip
// resolved — the socket peer for an untrusted caller, however it decorates its
// request with forwarding headers, and the forwarded client only when a trusted
// proxy is what dialled us (SEC-001: the trail must record where a request came
// from, not what its sender claimed).
func TestFromRequest_ip(t *testing.T) {
	t.Parallel()

	trusted, err := clientip.ParseSet([]string{"loopback"})
	if err != nil {
		t.Fatalf("ParseSet: %v", err)
	}

	tests := []struct {
		name       string
		xff        string
		realIP     string
		remoteAddr string
		resolve    bool // run the request through the client-IP middleware
		want       string
	}{
		{name: "forged forwarded-for ignored", xff: "203.0.113.7, 10.0.0.1", remoteAddr: "192.0.2.9:5555",
			resolve: true, want: "192.0.2.9"},
		{name: "forged real-ip ignored", realIP: "198.51.100.4", remoteAddr: "192.0.2.9:5555",
			resolve: true, want: "192.0.2.9"},
		{name: "trusted proxy names the client", xff: "203.0.113.7", remoteAddr: "127.0.0.1:5555",
			resolve: true, want: "203.0.113.7"},
		{name: "without the middleware the peer wins", xff: "203.0.113.7", remoteAddr: "192.0.2.5:42000",
			want: "192.0.2.5"},
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
				r.Header.Set("X-Real-Ip", tt.realIP)
			}
			got := ""
			if tt.resolve {
				clientip.Middleware(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
					got = FromRequest(req, "us-actor").IP
				})).ServeHTTP(httptest.NewRecorder(), r)
			} else {
				got = FromRequest(r, "us-actor").IP
			}
			if got != tt.want {
				t.Errorf("FromRequest().IP = %q, want %q", got, tt.want)
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
		wantWhere := " WHERE actor_uid = $1 AND action = $2 AND audit_log.created_at >= $3"
		if where != wantWhere {
			t.Errorf("buildWhere() where = %q, want %q", where, wantWhere)
		}
		if len(args) != 3 || args[0] != "us-1" || args[1] != ActionPhotoArchive || args[2] != since {
			t.Errorf("buildWhere() args = %v, want [us-1 %s %v]", args, ActionPhotoArchive, since)
		}
	})

	t.Run("project comes first", func(t *testing.T) {
		t.Parallel()
		where, args := Filter{ProjectUID: "pj1", ActorUID: "us-1"}.buildWhere()
		if want := " WHERE project_id = $1 AND actor_uid = $2"; where != want {
			t.Errorf("buildWhere() where = %q, want %q", where, want)
		}
		if len(args) != 2 || args[0] != "pj1" || args[1] != "us-1" {
			t.Errorf("buildWhere() args = %v, want [pj1 us-1]", args)
		}
	})

	t.Run("review-only appends the literal via predicate", func(t *testing.T) {
		t.Parallel()
		where, args := Filter{ActorUID: "us-1", ReviewOnly: true}.buildWhere()
		wantWhere := " WHERE actor_uid = $1 AND details ->> 'via' = 'review'"
		if where != wantWhere {
			t.Errorf("buildWhere() where = %q, want %q", where, wantWhere)
		}
		// The via predicate is a literal, not a bound parameter, so it matches the
		// partial index idx_audit_log_review_actor and adds no argument.
		if len(args) != 1 || args[0] != "us-1" {
			t.Errorf("buildWhere() args = %v, want [us-1]", args)
		}
	})

	t.Run("action set builds an ANY clause", func(t *testing.T) {
		t.Parallel()
		actions := []string{ActionFaceAssign, ActionLabelAttach}
		where, args := Filter{ReviewOnly: true, Actions: actions}.buildWhere()
		wantWhere := " WHERE action = ANY($1) AND details ->> 'via' = 'review'"
		if where != wantWhere {
			t.Errorf("buildWhere() where = %q, want %q", where, wantWhere)
		}
		got, ok := args[0].([]string)
		if len(args) != 1 || !ok || len(got) != 2 || got[0] != ActionFaceAssign || got[1] != ActionLabelAttach {
			t.Errorf("buildWhere() args = %v, want [[face.assign label.attach]]", args)
		}
	})
}

// TestMetaEntry_actorRole verifies a meta carrying an actor role records it as
// the "actor_role" detail on a copy of the details, never in the caller's map,
// and that a meta without one leaves the details alone.
func TestMetaEntry_actorRole(t *testing.T) {
	t.Parallel()

	details := map[string]any{"name": "x"}
	got := Meta{ActorUID: "us1", ActorRole: "maintainer"}.Entry("a", "t", "uid", details)
	if want := map[string]any{"name": "x", "actor_role": "maintainer"}; !reflect.DeepEqual(got.Details, want) {
		t.Errorf("details = %v, want %v", got.Details, want)
	}
	if _, leaked := details["actor_role"]; leaked {
		t.Error("Entry modified the caller's details map")
	}
	if plain := (Meta{ActorUID: "us1"}).Entry("a", "t", "uid", details); !reflect.DeepEqual(plain.Details, details) {
		t.Errorf("details without an actor role = %v, want %v", plain.Details, details)
	}
	if nilDetails := (Meta{ActorRole: "maintainer"}).Entry("a", "t", "", nil); nilDetails.Details["actor_role"] != "maintainer" {
		t.Errorf("nil details with an actor role = %v", nilDetails.Details)
	}
}

// TestMetaEntry_project verifies a meta files a project-scoped action under its
// project and never an instance action, whatever project the request resolved.
func TestMetaEntry_project(t *testing.T) {
	t.Parallel()

	meta := Meta{ActorUID: "us1", ProjectUID: "pj1"}
	tests := []struct {
		action string
		want   string
	}{
		{action: ActionPhotoUpdate, want: "pj1"},
		{action: ActionProjectMemberRole, want: "pj1"},
		{action: ActionUserRename, want: ""},
		{action: ActionProjectCreate, want: ""},
		{action: "not.an.action", want: ""},
	}
	for _, tt := range tests {
		if got := meta.Entry(tt.action, "t", "uid", nil).ProjectUID; got != tt.want {
			t.Errorf("Entry(%s).ProjectUID = %q, want %q", tt.action, got, tt.want)
		}
	}
}

// TestProjectScoped verifies the split: every instance action is a known one
// (so the list cannot hold a typo), and the catalogue's actions are scoped.
func TestProjectScoped(t *testing.T) {
	t.Parallel()

	for action := range instanceActions {
		if !KnownAction(action) {
			t.Errorf("instance action %q is not a known action", action)
		}
		if ProjectScoped(action) {
			t.Errorf("ProjectScoped(%q) = true for an instance action", action)
		}
	}
	for _, action := range []string{ActionPhotoUpdate, ActionAlbumCreate, ActionSettingsUpdate,
		ActionAnnouncementSet, ActionProjectInviteCreate} {
		if !ProjectScoped(action) {
			t.Errorf("ProjectScoped(%q) = false, want true", action)
		}
	}
	if ProjectScoped("nope") {
		t.Error("ProjectScoped of an unknown label = true")
	}
}

// TestProjectScopedActions verifies the listed form of ProjectScoped: byte
// ordered, every known action that is not an instance action, and a fresh
// slice on every call.
func TestProjectScopedActions(t *testing.T) {
	t.Parallel()

	got := ProjectScopedActions()
	if !slices.IsSorted(got) {
		t.Errorf("ProjectScopedActions() is not byte-ordered: %v", got)
	}
	if want := len(knownActions) - len(instanceActions); len(got) != want {
		t.Errorf("ProjectScopedActions() has %d actions, want %d", len(got), want)
	}
	for _, action := range got {
		if !ProjectScoped(action) {
			t.Errorf("ProjectScopedActions() lists %q, which is not project-scoped", action)
		}
	}
	got[0] = "mutated"
	if ProjectScopedActions()[0] == "mutated" {
		t.Error("ProjectScopedActions() returned a shared slice")
	}
}

// TestFromRequest_actorRole verifies FromRequest picks up the actor role the
// request's observability fields carry.
func TestFromRequest_actorRole(t *testing.T) {
	t.Parallel()

	var meta Meta
	handler := obs.AccessLog(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		obs.SetActorRole(r.Context(), "maintainer")
		obs.SetProject(r.Context(), "pj1", "veselice")
		meta = FromRequest(r, "us1")
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x", nil))
	if meta.ActorRole != "maintainer" {
		t.Errorf("ActorRole = %q, want maintainer", meta.ActorRole)
	}
	if meta.ProjectUID != "pj1" {
		t.Errorf("ProjectUID = %q, want pj1", meta.ProjectUID)
	}
}
