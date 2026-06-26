//go:build integration

package facematch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/database/dbtest"
	"github.com/panbotka/kukatko/internal/facematch"
	"github.com/panbotka/kukatko/internal/people"
	"github.com/panbotka/kukatko/internal/photoapi"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
	"github.com/panbotka/kukatko/internal/vectors"
)

// These tests run only under `make test-integration` against the database named by
// KUKATKO_TEST_DATABASE_URL. They share one database and truncate per case, so they
// do not run in parallel. They exercise the face endpoints end-to-end (photoapi
// HTTP layer over the real facematch.Service and stores).

const testPassword = "correct horse battery staple"

// env wires the auth and photo APIs (with the face service) behind an httptest
// server over the integration database.
type env struct {
	server  *httptest.Server
	authSvc *auth.Service
	photos  *photos.Store
	vectors *vectors.Store
	people  *people.Store
}

// newEnv builds the HTTP test environment over a freshly truncated database.
func newEnv(t *testing.T) *env {
	t.Helper()
	db := dbtest.New(t)
	dbtest.TruncateAll(t, db)

	authStore := auth.NewStore(db.Pool())
	authSvc := auth.NewService(authStore, auth.SessionPolicy{TTL: time.Hour, MaxLifetime: 3 * time.Hour})
	authAPI := auth.NewAPI(auth.APIConfig{Service: authSvc, Limiter: auth.NewLimiter(100, time.Minute)})

	fs, err := storage.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewFS: %v", err)
	}
	photoStore := photos.NewStore(db.Pool())
	vectorStore := vectors.NewStore(db.Pool())
	peopleStore := people.NewStore(db.Pool())
	faceSvc := facematch.New(facematch.Config{Photos: photoStore, Faces: vectorStore, People: peopleStore})

	api := photoapi.NewAPI(photoapi.Config{
		Store:           photoStore,
		Storage:         fs,
		Thumbnailer:     thumb.New(fs, t.TempDir()),
		Similar:         vectorStore,
		Faces:           faceSvc,
		RequireAuth:     authAPI.RequireAuth,
		RequireWrite:    authAPI.RequireWrite,
		RequireDownload: authAPI.RequireAuthOrDownloadToken,
	})

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		authAPI.RegisterRoutes(r)
		api.RegisterRoutes(r)
	})
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return &env{server: server, authSvc: authSvc, photos: photoStore, vectors: vectorStore, people: peopleStore}
}

// login creates a user with the given role and returns a cookie-bearing client.
func (e *env) login(t *testing.T, username string, role auth.Role) *http.Client {
	t.Helper()
	if _, err := e.authSvc.CreateUser(t.Context(), auth.CreateUserInput{
		Username: username, Password: testPassword, Role: role,
	}); err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": username, "password": testPassword})
	resp := mustDo(t, client, http.MethodPost, e.server.URL+"/api/v1/auth/login", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status = %d, want 200", resp.StatusCode)
	}
	return client
}

// mustDo issues an HTTP request with an optional JSON body and returns the response.
func mustDo(t *testing.T, client *http.Client, method, urlStr string, body []byte) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, urlStr, reader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, urlStr, err)
	}
	return resp
}

// getFaces fetches and decodes the faces response for a photo.
func (e *env) getFaces(t *testing.T, client *http.Client, photoUID string) facematch.FacesResponse {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, e.server.URL+"/api/v1/photos/"+photoUID+"/faces", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET faces status = %d, want 200", resp.StatusCode)
	}
	var out facematch.FacesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode faces: %v", err)
	}
	return out
}

// assign posts a face-assignment request and returns the raw response.
func (e *env) assign(t *testing.T, client *http.Client, photoUID string, req facematch.AssignRequest) *http.Response {
	t.Helper()
	body, _ := json.Marshal(req)
	return mustDo(t, client, http.MethodPost, e.server.URL+"/api/v1/photos/"+photoUID+"/faces/assign", body)
}

// makePhoto inserts a photo with stored dimensions and returns its uid.
func (e *env) makePhoto(t *testing.T, hash string) string {
	t.Helper()
	created, err := e.photos.Create(t.Context(), photos.Photo{
		FileHash: hash, FilePath: "2024/01/" + hash + ".jpg", FileName: hash + ".jpg",
		FileWidth: 1000, FileHeight: 1000, FileOrientation: 1,
	})
	if err != nil {
		t.Fatalf("create photo %s: %v", hash, err)
	}
	return created.UID
}

// faceVec builds a FaceDim unit vector with the given index set to 1.
func faceVec(index int) []float32 {
	v := make([]float32, vectors.FaceDim)
	v[index] = 1
	return v
}

// saveFace stores one face on a photo, optionally with a cached subject assignment.
func (e *env) saveFace(t *testing.T, photoUID string, index int, vec []float32, bbox [4]float64, subjectUID, subjectName string) {
	t.Helper()
	face := vectors.Face{FaceIndex: index, Vector: vec, BBox: bbox, SubjectName: subjectName}
	if subjectUID != "" {
		face.SubjectUID = &subjectUID
	}
	// Replace would clobber other faces; append by reading then re-saving all.
	existing, err := e.vectors.ListFaces(t.Context(), photoUID)
	if err != nil {
		t.Fatalf("ListFaces: %v", err)
	}
	if err := e.vectors.SaveFaces(t.Context(), photoUID, append(existing, face)); err != nil {
		t.Fatalf("SaveFaces: %v", err)
	}
}

// createSubject inserts a subject and returns it.
func (e *env) createSubject(t *testing.T, name string) people.Subject {
	t.Helper()
	subj, err := e.people.CreateSubject(t.Context(), people.Subject{Name: name})
	if err != nil {
		t.Fatalf("CreateSubject %s: %v", name, err)
	}
	return subj
}

// createMarker inserts a face marker for a photo/subject and returns it.
func (e *env) createMarker(t *testing.T, photoUID, subjectUID string, bbox [4]float64) people.Marker {
	t.Helper()
	m := people.Marker{PhotoUID: photoUID, Type: people.MarkerFace, X: bbox[0], Y: bbox[1], W: bbox[2], H: bbox[3]}
	if subjectUID != "" {
		m.SubjectUID = &subjectUID
	}
	created, err := e.people.CreateMarker(t.Context(), m)
	if err != nil {
		t.Fatalf("CreateMarker: %v", err)
	}
	return created
}

// TestFaces_iouMatchCachesAndAssigns checks a face overlapping an assigned marker is
// reported as already_done and the match is cached on the face row.
func TestFaces_iouMatchCachesAndAssigns(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "alice-admin", auth.RoleAdmin)
	ctx := context.Background()

	box := [4]float64{0.2, 0.2, 0.3, 0.3}
	uid := env.makePhoto(t, "match")
	alice := env.createSubject(t, "Alice")
	marker := env.createMarker(t, uid, alice.UID, box)
	env.saveFace(t, uid, 0, faceVec(0), box, "", "")

	resp := env.getFaces(t, client, uid)
	if len(resp.Faces) != 1 {
		t.Fatalf("got %d faces, want 1", len(resp.Faces))
	}
	face := resp.Faces[0]
	if face.Action != facematch.ActionAlreadyDone || face.MarkerUID != marker.UID ||
		face.SubjectUID != alice.UID || face.SubjectName != "Alice" {
		t.Fatalf("face = %+v, want already_done linked to Alice/%s", face, marker.UID)
	}
	if face.IoU < 0.99 {
		t.Errorf("IoU = %v, want ~1 for coincident boxes", face.IoU)
	}

	// The match must be cached on the face row.
	faces, err := env.vectors.ListFaces(ctx, uid)
	if err != nil || len(faces) != 1 {
		t.Fatalf("ListFaces = %d, %v", len(faces), err)
	}
	if faces[0].MarkerUID == nil || *faces[0].MarkerUID != marker.UID ||
		faces[0].SubjectUID == nil || *faces[0].SubjectUID != alice.UID {
		t.Errorf("face cache not persisted: %+v", faces[0])
	}
}

// TestFaces_suggestions checks an unnamed face is suggested the nearest assigned
// subject, excluding people already on the photo and faces below the size floor.
func TestFaces_suggestions(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "bob-admin", auth.RoleAdmin)

	bob := env.createSubject(t, "Bob")
	carol := env.createSubject(t, "Carol")
	dan := env.createSubject(t, "Dan")

	bigBox := [4]float64{0.1, 0.1, 0.3, 0.3}
	tinyBox := [4]float64{0.1, 0.1, 0.005, 0.005}

	// Neighbour photos: Bob (near, big), Dan (near, too small).
	p2 := env.makePhoto(t, "neighbour_bob")
	env.saveFace(t, p2, 0, faceVec(0), bigBox, bob.UID, "Bob")
	p3 := env.makePhoto(t, "neighbour_dan")
	env.saveFace(t, p3, 0, faceVec(0), tinyBox, dan.UID, "Dan")

	// Query photo p1: Carol already assigned (her own face) + an unnamed face whose
	// vector is closest to Bob and Dan.
	p1 := env.makePhoto(t, "query")
	carolBox := [4]float64{0.6, 0.6, 0.3, 0.3}
	carolMarker := env.createMarker(t, p1, carol.UID, carolBox)
	env.saveFace(t, p1, 0, faceVec(1), carolBox, carol.UID, "Carol")
	_ = carolMarker
	env.saveFace(t, p1, 1, faceVec(0), bigBox, "", "") // unnamed, no overlapping marker

	resp := env.getFaces(t, client, p1)

	var unnamed *facematch.FaceView
	for i := range resp.Faces {
		if resp.Faces[i].FaceIndex == 1 {
			unnamed = &resp.Faces[i]
		}
	}
	if unnamed == nil {
		t.Fatalf("unnamed face not found in %+v", resp.Faces)
	}
	if unnamed.Action != facematch.ActionCreateMarker {
		t.Errorf("unnamed action = %s, want create_marker", unnamed.Action)
	}
	if len(unnamed.Suggestions) != 1 || unnamed.Suggestions[0].SubjectUID != bob.UID {
		t.Fatalf("suggestions = %+v, want only Bob (Carol excluded on-photo, Dan too small)", unnamed.Suggestions)
	}
	if unnamed.Suggestions[0].Confidence <= 0 {
		t.Errorf("Bob confidence = %v, want > 0", unnamed.Suggestions[0].Confidence)
	}
}

// TestAssign_createUnassignReuseSubject exercises the full state machine: create a
// marker (auto-creating a subject), unassign it, then reassign by the same name and
// confirm the subject is reused, not duplicated.
func TestAssign_createUnassignReuseSubject(t *testing.T) {
	env := newEnv(t)
	client := env.login(t, "carol-editor", auth.RoleEditor)
	ctx := context.Background()

	box := [4]float64{0.2, 0.2, 0.3, 0.3}
	uid := env.makePhoto(t, "assign")
	idx := 0
	env.saveFace(t, uid, idx, faceVec(0), box, "", "")

	// create_marker auto-creates the subject and links the face.
	created := decodeAssign(t, env.assign(t, client, uid, facematch.AssignRequest{
		Action: facematch.ActionCreateMarker, FaceIndex: &idx, BBox: &box, SubjectName: "Eva Nováková",
	}))
	if created.Action != facematch.ActionCreateMarker || created.Subject == nil || created.Subject.Slug != "eva-novakova" {
		t.Fatalf("create result = %+v", created)
	}
	if !created.Marker.Reviewed {
		t.Errorf("created marker not reviewed: %+v", created.Marker)
	}
	markerUID := created.Marker.UID
	subjectUID := created.Subject.UID

	// The face cache reflects the assignment.
	if faces, _ := env.vectors.ListFaces(ctx, uid); faces[0].SubjectUID == nil || *faces[0].SubjectUID != subjectUID {
		t.Fatalf("face cache after create = %+v", faces[0])
	}

	// unassign clears the subject and the reviewed flag.
	un := decodeAssign(t, env.assign(t, client, uid, facematch.AssignRequest{
		Action: facematch.ActionUnassignPerson, FaceIndex: &idx, MarkerUID: markerUID,
	}))
	if un.Marker.SubjectUID != nil || un.Marker.Reviewed {
		t.Fatalf("unassign result marker = %+v, want no subject and not reviewed", un.Marker)
	}
	if faces, _ := env.vectors.ListFaces(ctx, uid); faces[0].SubjectUID != nil || faces[0].SubjectName != "" {
		t.Errorf("face cache not cleared after unassign: %+v", faces[0])
	}

	// assign_person by the same name reuses the existing subject (find-or-create).
	re := decodeAssign(t, env.assign(t, client, uid, facematch.AssignRequest{
		Action: facematch.ActionAssignPerson, FaceIndex: &idx, MarkerUID: markerUID, SubjectName: "Eva Nováková",
	}))
	if re.Subject == nil || re.Subject.UID != subjectUID {
		t.Fatalf("reassign subject = %+v, want reused %s", re.Subject, subjectUID)
	}
	if !re.Marker.Reviewed {
		t.Errorf("reassigned marker not reviewed: %+v", re.Marker)
	}
	subs, err := env.people.ListSubjects(ctx)
	if err != nil {
		t.Fatalf("ListSubjects: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("got %d subjects, want 1 (no duplicate created)", len(subs))
	}
}

// TestAssign_validationAndAuth checks request validation and that a viewer cannot
// mutate assignments.
func TestAssign_validationAndAuth(t *testing.T) {
	env := newEnv(t)
	uid := env.makePhoto(t, "auth")

	editor := env.login(t, "ed", auth.RoleEditor)
	resp := env.assign(t, editor, uid, facematch.AssignRequest{Action: "bogus"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bogus action status = %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.assign(t, editor, uid, facematch.AssignRequest{Action: facematch.ActionAssignPerson, MarkerUID: "mkmissing", SubjectName: "X"})
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing marker status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	viewer := env.login(t, "vi", auth.RoleViewer)
	box := [4]float64{0, 0, 0.1, 0.1}
	resp = env.assign(t, viewer, uid, facematch.AssignRequest{Action: facematch.ActionCreateMarker, BBox: &box, SubjectName: "X"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer assign status = %d, want 403", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

// decodeAssign decodes a 200 assignment response, failing on any other status.
func decodeAssign(t *testing.T, resp *http.Response) facematch.AssignResult {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("assign status = %d, body = %s", resp.StatusCode, body)
	}
	var out facematch.AssignResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode assign: %v", err)
	}
	return out
}
