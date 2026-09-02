package auth

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// ceremonyNow is the fixed instant these tests reason from.
var ceremonyNow = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

// mustPut stores cer and returns its id, failing the test if the store refused.
func mustPut(t *testing.T, store *ceremonyStore, cer ceremony) string {
	t.Helper()
	id, err := store.put(cer, ceremonyNow)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	return id
}

// TestCeremonyStore_takeIsOneShot pins the property the whole flow's replay
// resistance rests on: a challenge can be answered exactly once, whether the
// answer verified or not.
func TestCeremonyStore_takeIsOneShot(t *testing.T) {
	t.Parallel()
	store := newCeremonyStore(time.Minute)
	id := mustPut(t, store, ceremony{session: webauthn.SessionData{Challenge: "c"}, userUID: "us1"})

	got, ok := store.take(id, ceremonyNow)
	if !ok || got.userUID != "us1" || got.session.Challenge != "c" {
		t.Fatalf("take() = %+v, %v; want the stored ceremony", got, ok)
	}
	if _, ok := store.take(id, ceremonyNow); ok {
		t.Error("take() succeeded twice; a spent challenge must not be answerable again")
	}
	if store.len() != 0 {
		t.Errorf("store holds %d ceremonies, want 0", store.len())
	}
}

func TestCeremonyStore_take(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   func(t *testing.T, store *ceremonyStore) string
		at   time.Time
		want bool
	}{
		{
			name: "inside the window",
			id:   func(t *testing.T, s *ceremonyStore) string { return mustPut(t, s, ceremony{}) },
			at:   ceremonyNow.Add(59 * time.Second),
			want: true,
		},
		{
			name: "exactly at the expiry is too late",
			id:   func(t *testing.T, s *ceremonyStore) string { return mustPut(t, s, ceremony{}) },
			at:   ceremonyNow.Add(time.Minute),
		},
		{
			name: "past the expiry",
			id:   func(t *testing.T, s *ceremonyStore) string { return mustPut(t, s, ceremony{}) },
			at:   ceremonyNow.Add(2 * time.Minute),
		},
		{
			name: "an unknown id",
			id:   func(_ *testing.T, _ *ceremonyStore) string { return "nonesuch" },
			at:   ceremonyNow,
		},
		{
			name: "no id at all — the cookie was missing",
			id:   func(_ *testing.T, _ *ceremonyStore) string { return "" },
			at:   ceremonyNow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := newCeremonyStore(time.Minute)
			if _, ok := store.take(tt.id(t, store), tt.at); ok != tt.want {
				t.Errorf("take() ok = %v, want %v", ok, tt.want)
			}
		})
	}
}

// TestCeremonyStore_cleanup pins the maintenance tick: what has expired goes and
// what has not stays, so an unanswered ceremony is not a permanent leak and a
// live one is not thrown away underneath its owner.
func TestCeremonyStore_cleanup(t *testing.T) {
	t.Parallel()
	store := newCeremonyStore(time.Minute)
	stale := mustPut(t, store, ceremony{})
	fresh, err := store.put(ceremony{}, ceremonyNow.Add(90*time.Second))
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	store.cleanup(ceremonyNow.Add(2 * time.Minute))
	if store.len() != 1 {
		t.Fatalf("store holds %d ceremonies, want 1", store.len())
	}
	if _, ok := store.take(stale, ceremonyNow.Add(2*time.Minute)); ok {
		t.Error("the expired ceremony survived the cleanup")
	}
	if _, ok := store.take(fresh, ceremonyNow.Add(2*time.Minute)); !ok {
		t.Error("the live ceremony was cleaned up")
	}
}

// TestCeremonyStore_boundedByExpiryFirst pins the order the cap is defended in:
// a full store first drops what has expired, and only refuses if that frees
// nothing. The begin endpoint is anonymous, so an unbounded map here is how it
// would become a memory leak.
func TestCeremonyStore_boundedByExpiryFirst(t *testing.T) {
	t.Parallel()
	store := newCeremonyStore(time.Minute)
	for range maxCeremonies {
		mustPut(t, store, ceremony{})
	}
	if _, err := store.put(ceremony{}, ceremonyNow); err == nil {
		t.Error("put() into a full store of live ceremonies succeeded, want a refusal")
	}
	if _, err := store.put(ceremony{}, ceremonyNow.Add(2*time.Minute)); err != nil {
		t.Errorf("put() after everything expired failed: %v", err)
	}
}

// TestCeremonyStore_idsAreDistinct pins that two ceremonies never collide, which
// is what keeps one caller from consuming another's challenge.
func TestCeremonyStore_idsAreDistinct(t *testing.T) {
	t.Parallel()
	store := newCeremonyStore(time.Minute)
	seen := make(map[string]struct{}, 128)
	for range 128 {
		id := mustPut(t, store, ceremony{})
		if len(id) != ceremonyIDLen {
			t.Fatalf("ceremony id %q has length %d, want %d", id, len(id), ceremonyIDLen)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("ceremony id %q was minted twice", id)
		}
		seen[id] = struct{}{}
	}
}
