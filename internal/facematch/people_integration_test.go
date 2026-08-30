//go:build integration

package facematch_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/facematch"
)

// getDetail fetches a photo's detail with the given query string and returns the
// raw keys, so a test can tell an absent people block from an empty one.
func (e *env) getDetail(t *testing.T, client *http.Client, photoUID, query string) map[string]json.RawMessage {
	t.Helper()
	url := e.server.URL + "/api/v1/photos/" + photoUID + query
	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	return body
}

// decodePeople decodes the detail response's people block.
func decodePeople(t *testing.T, raw json.RawMessage) []facematch.PersonOnPhoto {
	t.Helper()
	var onPhoto []facematch.PersonOnPhoto
	if err := json.Unmarshal(raw, &onPhoto); err != nil {
		t.Fatalf("decode people %s: %v", raw, err)
	}
	return onPhoto
}

// TestDetail_peopleOptIn checks the widened detail payload: with people=true it
// reports the named subjects and the still-unassigned detections with their
// detection score, so an agent reads the whole photo in one request instead of
// having to know that /faces exists.
func TestDetail_peopleOptIn(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "alice-admin", auth.RoleAdmin)

	box := [4]float64{0.2, 0.2, 0.3, 0.3}
	uid := env.makePhoto(t, "roll-call")
	alice := env.createSubject(t, "Alice")
	marker := env.createMarker(t, uid, alice.UID, box)
	env.saveFace(t, uid, 0, faceVec(0), box, "", "")
	env.saveFace(t, uid, 1, faceVec(1), [4]float64{0.6, 0.6, 0.2, 0.2}, "", "")

	body := env.getDetail(t, client, uid, "?people=true")
	raw, ok := body["people"]
	if !ok {
		t.Fatal("people is absent although the request asked for it")
	}
	onPhoto := decodePeople(t, raw)
	if len(onPhoto) != 2 {
		t.Fatalf("people = %+v, want the named face and the unassigned one", onPhoto)
	}
	named := onPhoto[0]
	if named.SubjectUID != alice.UID || named.SubjectName != "Alice" || named.MarkerUID != marker.UID {
		t.Errorf("first = %+v, want Alice on %s", named, marker.UID)
	}
	if onPhoto[1].SubjectUID != "" {
		t.Errorf("second = %+v, want an unassigned detection", onPhoto[1])
	}
}

// TestDetail_peopleAbsentWithoutTheParameter checks a plain detail read is
// unchanged: the block does not appear, so the server never pays for the
// face↔marker match nobody asked for.
func TestDetail_peopleAbsentWithoutTheParameter(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "alice-admin", auth.RoleAdmin)

	box := [4]float64{0.2, 0.2, 0.3, 0.3}
	uid := env.makePhoto(t, "quiet")
	alice := env.createSubject(t, "Alice")
	env.createMarker(t, uid, alice.UID, box)
	env.saveFace(t, uid, 0, faceVec(0), box, "", "")

	if _, ok := env.getDetail(t, client, uid, "")["people"]; ok {
		t.Error("people is present although nobody asked for it")
	}
	// A malformed value is treated as "not asked" rather than failing the detail:
	// losing the photo over the list of who is on it would be a bad trade.
	if _, ok := env.getDetail(t, client, uid, "?people=maybe")["people"]; ok {
		t.Error("people is present for an unparseable parameter")
	}
}

// TestDetail_peopleEmptyIsNotAbsent checks a photo with nobody on it answers with
// an empty list rather than no key at all, so "we looked, nobody is marked" stays
// distinguishable from "you did not ask".
func TestDetail_peopleEmptyIsNotAbsent(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "alice-admin", auth.RoleAdmin)

	uid := env.makePhoto(t, "nobody")
	body := env.getDetail(t, client, uid, "?people=true")
	raw, ok := body["people"]
	if !ok {
		t.Fatal("people is absent although the request asked for it")
	}
	if len(decodePeople(t, raw)) != 0 {
		t.Errorf("people = %s, want an empty list", raw)
	}
}

// TestUpdate_returnsPeopleWhenAsked checks the same block rides the PATCH
// response, so an agent that writes an evaluation can read back the whole photo —
// who is on it included — without a second request.
func TestUpdate_returnsPeopleWhenAsked(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "alice-admin", auth.RoleAdmin)

	box := [4]float64{0.2, 0.2, 0.3, 0.3}
	uid := env.makePhoto(t, "edited")
	alice := env.createSubject(t, "Alice")
	env.createMarker(t, uid, alice.UID, box)
	env.saveFace(t, uid, 0, faceVec(0), box, "", "")

	patch, _ := json.Marshal(map[string]any{"title": "Alice by the lake"})
	resp := mustDo(t, client, http.MethodPatch,
		env.server.URL+"/api/v1/photos/"+uid+"?people=true", patch)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Title  string                    `json:"title"`
		People []facematch.PersonOnPhoto `json:"people"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if body.Title != "Alice by the lake" {
		t.Errorf("title = %q, want the edited title", body.Title)
	}
	if len(body.People) != 1 || body.People[0].SubjectName != "Alice" {
		t.Errorf("people = %+v, want Alice", body.People)
	}
}
