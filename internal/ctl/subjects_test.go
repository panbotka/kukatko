package ctl

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// subjectsBody is a realistic bare {"subjects": […]} envelope.
const subjectsBody = `{"subjects":[
	{"uid":"sub01","slug":"anna","name":"Anna","type":"person","favorite":true,"marker_count":128},
	{"uid":"sub02","slug":"rex","name":"Rex","type":"pet","marker_count":9}
]}`

// TestClient_ListSubjects verifies the bare subject envelope decodes with its own
// decoder, marker counts included.
func TestClient_ListSubjects(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(subjectsBody))
	})

	raw, err := client.ListSubjects(t.Context())
	if err != nil {
		t.Fatalf("ListSubjects returned %v", err)
	}
	if gotPath != "/api/v1/subjects" {
		t.Errorf("path = %q, want /api/v1/subjects", gotPath)
	}
	subjects, err := DecodeSubjects(raw)
	if err != nil {
		t.Fatalf("DecodeSubjects returned %v", err)
	}
	if len(subjects) != 2 {
		t.Fatalf("subjects = %+v, want two rows", subjects)
	}
	if subjects[0].Name != "Anna" || !subjects[0].Favorite || subjects[0].MarkerCount != 128 {
		t.Errorf("first subject = %+v, want the decoded Anna subject", subjects[0])
	}
	if subjects[1].Type != "pet" {
		t.Errorf("second subject = %+v, want a pet", subjects[1])
	}
}

// TestClient_ListSubjects_empty verifies an empty library decodes to no subjects.
func TestClient_ListSubjects_empty(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"subjects":[]}`))
	})
	raw, err := client.ListSubjects(t.Context())
	if err != nil {
		t.Fatalf("ListSubjects returned %v", err)
	}
	subjects, err := DecodeSubjects(raw)
	if err != nil || len(subjects) != 0 {
		t.Errorf("DecodeSubjects = %+v, %v, want an empty list", subjects, err)
	}
}

// TestClient_GetSubject verifies the uid is escaped into the path and one subject
// decodes, notes and cover included.
func TestClient_GetSubject(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"uid":"sub01","slug":"anna","name":"Anna","type":"person",
			"favorite":true,"private":false,"notes":"sister","cover_photo_uid":"pht09"}`))
	})

	raw, err := client.GetSubject(t.Context(), "sub 01")
	if err != nil {
		t.Fatalf("GetSubject returned %v", err)
	}
	if gotPath != "/api/v1/subjects/sub 01" {
		t.Errorf("path = %q, want the escaped uid", gotPath)
	}
	subject, err := DecodeSubject(raw)
	if err != nil {
		t.Fatalf("DecodeSubject returned %v", err)
	}
	if subject.Name != "Anna" || subject.Notes != "sister" {
		t.Errorf("subject = %+v, want the decoded subject", subject)
	}
	if subject.CoverPhotoUID == nil || *subject.CoverPhotoUID != "pht09" {
		t.Errorf("cover = %v, want pht09", subject.CoverPhotoUID)
	}
	if subject.MarkerCount != 0 {
		t.Errorf("marker_count = %d, want zero: the detail endpoint does not send one", subject.MarkerCount)
	}
}

// TestClient_GetSubject_emptyUID verifies a blank uid never reaches the network.
func TestClient_GetSubject_emptyUID(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted with a blank uid")
	})
	if _, err := client.GetSubject(t.Context(), ""); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("GetSubject(\"\") error = %v, want ErrEmptyUID", err)
	}
}

// TestClient_SubjectPhotos verifies the gallery endpoint is paged with limit and
// offset, and that its envelope is the /photos one, so DecodePhotoPage reads it.
func TestClient_SubjectPhotos(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotQuery url.Values
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query()
		w.Write([]byte(`{"photos":[{"uid":"pht01","file_name":"a.jpg"}],
			"total":9,"limit":1,"offset":2,"next_offset":3}`))
	})

	raw, err := client.SubjectPhotos(t.Context(), "sub01", PageOptions{Limit: 1, Offset: 2})
	if err != nil {
		t.Fatalf("SubjectPhotos returned %v", err)
	}
	if gotPath != "/api/v1/subjects/sub01/photos" {
		t.Errorf("path = %q, want the subject gallery path", gotPath)
	}
	if gotQuery.Get("limit") != "1" || gotQuery.Get("offset") != "2" {
		t.Errorf("query = %v, want the paging parameters", gotQuery)
	}
	page, err := DecodePhotoPage(raw)
	if err != nil {
		t.Fatalf("DecodePhotoPage returned %v", err)
	}
	if page.Total != 9 || len(page.Photos) != 1 || page.NextOffset == nil || *page.NextOffset != 3 {
		t.Errorf("page = %+v, want one row of nine with next_offset 3", page)
	}
}

// TestClient_SubjectPhotos_paging verifies zero paging sends no parameters at all,
// leaving the server's own default page size, and that a negative one is refused.
func TestClient_SubjectPhotos_paging(t *testing.T) {
	t.Parallel()

	var gotQuery string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"photos":[],"total":0,"limit":100,"offset":0,"next_offset":null}`))
	})

	if _, err := client.SubjectPhotos(t.Context(), "sub01", PageOptions{}); err != nil {
		t.Fatalf("SubjectPhotos returned %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want no parameters for a zero page", gotQuery)
	}
	_, err := client.SubjectPhotos(t.Context(), "sub01", PageOptions{Limit: -1})
	if !errors.Is(err, ErrInvalidPaging) {
		t.Errorf("negative limit error = %v, want ErrInvalidPaging", err)
	}
}

// TestDecodeSubjects_invalid verifies malformed JSON surfaces as an error.
func TestDecodeSubjects_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodeSubjects([]byte(`{"subjects":`)); err == nil {
		t.Error("DecodeSubjects of malformed JSON returned no error")
	}
	if _, err := DecodeSubject([]byte(`not json`)); err == nil {
		t.Error("DecodeSubject of malformed JSON returned no error")
	}
}
