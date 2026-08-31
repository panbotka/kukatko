package ctl

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"
)

// subjectsBody is a realistic bare {"subjects": […]} envelope. Anna's two counts
// differ, as they do for anyone photographed twice in one frame.
const subjectsBody = `{"subjects":[
	{"uid":"sub01","slug":"anna","name":"Anna","type":"person","favorite":true,
	 "marker_count":128,"photo_count":120},
	{"uid":"sub02","slug":"rex","name":"Rex","type":"pet","marker_count":9,"photo_count":9}
]}`

// TestClient_ListSubjects verifies the bare subject envelope decodes with its own
// decoder, both counts included and kept apart.
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
	if subjects[0].Name != "Anna" || !subjects[0].Favorite ||
		subjects[0].MarkerCount != 128 || subjects[0].PhotoCount != 120 {
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
	if subject.MarkerCount != 0 || subject.PhotoCount != 0 {
		t.Errorf("counts = %d markers / %d photos, want zero: the detail endpoint sends neither",
			subject.MarkerCount, subject.PhotoCount)
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

// storedSubjectBody is a fully populated GET /subjects/{uid} body: the record a
// rename must carry back untouched.
const storedSubjectBody = `{"uid":"sub01","slug":"micka","name":"Micka","type":"pet","favorite":true,
	"private":true,"notes":"the tabby","cover_photo_uid":"pht07","birth_year":2012,"death_year":2024}`

// TestClient_CreateSubject verifies a create reaches POST /subjects and that the
// optional fields left alone are not sent at all.
func TestClient_CreateSubject(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"uid":"sub09","slug":"anna","name":"Anna","type":"person"}`))
	})

	raw, err := client.CreateSubject(t.Context(), SubjectInput{Name: "Anna"})
	if err != nil {
		t.Fatalf("CreateSubject returned %v", err)
	}
	if gotPath != "/api/v1/subjects" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want POST /api/v1/subjects", gotMethod, gotPath)
	}
	if gotBody["name"] != "Anna" || len(gotBody) != 1 {
		t.Errorf("body = %v, want the name and nothing else", gotBody)
	}
	subject, err := DecodeSubject(raw)
	if err != nil || subject.UID != "sub09" {
		t.Errorf("created subject = %+v, %v, want the server's new record", subject, err)
	}
}

// TestSubjectInput_validate verifies the two inputs the CLI can reject without a
// round trip.
func TestSubjectInput_validate(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("an invalid subject reached the server")
	})
	if _, err := client.CreateSubject(t.Context(), SubjectInput{Name: "  "}); !errors.Is(err, ErrEmptySubjectName) {
		t.Errorf("blank name = %v, want ErrEmptySubjectName", err)
	}
	_, err := client.CreateSubject(t.Context(), SubjectInput{Name: "Rex", Type: "dog"})
	if !errors.Is(err, ErrInvalidSubjectType) {
		t.Errorf("unknown type = %v, want ErrInvalidSubjectType", err)
	}
}

// TestClient_RenameSubject verifies a rename reads the record first and sends the
// whole of it back with only the name changed — the PATCH rewrites everything, so
// anything the body omits would be erased.
func TestClient_RenameSubject(t *testing.T) {
	t.Parallel()

	var methods []string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodGet {
			w.Write([]byte(storedSubjectBody))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"uid":"sub01","slug":"micinka","name":"Mičinka","type":"pet"}`))
	})

	if _, err := client.RenameSubject(t.Context(), "sub01", " Mičinka "); err != nil {
		t.Fatalf("RenameSubject returned %v", err)
	}
	if len(methods) != 2 || methods[0] != http.MethodGet || methods[1] != http.MethodPatch {
		t.Fatalf("methods = %v, want a read then a patch", methods)
	}
	if gotBody["name"] != "Mičinka" {
		t.Errorf("body name = %v, want the trimmed new name", gotBody["name"])
	}
	for key, want := range map[string]any{
		"type": "pet", "favorite": true, "private": true, "notes": "the tabby",
		"cover_photo_uid": "pht07", "birth_year": float64(2012), "death_year": float64(2024),
	} {
		if gotBody[key] != want {
			t.Errorf("body[%q] = %v, want %v carried over untouched", key, gotBody[key], want)
		}
	}
}

// TestClient_DeleteSubject verifies the delete reaches its path and treats the
// server's 204 as success.
func TestClient_DeleteSubject(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.DeleteSubject(t.Context(), "sub01"); err != nil {
		t.Fatalf("DeleteSubject returned %v", err)
	}
	if gotPath != "/api/v1/subjects/sub01" || gotMethod != http.MethodDelete {
		t.Errorf("request = %s %s, want DELETE /api/v1/subjects/sub01", gotMethod, gotPath)
	}
}

// TestClient_MergeSubjects verifies the merge posts the keeper to the source's
// path — the subject in the path is the one that disappears — and decodes the
// counts it answers with.
func TestClient_MergeSubjects(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &gotBody)
		w.Write([]byte(`{"keeper_uid":"sub02","source_uid":"sub01","markers_moved":17,
			"faces_moved":17,"confirmations_moved":2,"rejections_moved":1,
			"rejections_dropped":1,"dismissals_moved":0,"shared_photos":3}`))
	})

	raw, err := client.MergeSubjects(t.Context(), "sub01", "sub02")
	if err != nil {
		t.Fatalf("MergeSubjects returned %v", err)
	}
	if gotPath != "/api/v1/subjects/sub01/merge" || gotMethod != http.MethodPost {
		t.Errorf("request = %s %s, want the source's merge endpoint", gotMethod, gotPath)
	}
	if gotBody["keeper_uid"] != "sub02" {
		t.Errorf("body = %v, want the keeper named", gotBody)
	}
	result, err := DecodeMergeResult(raw)
	if err != nil {
		t.Fatalf("DecodeMergeResult returned %v", err)
	}
	if result.MarkersMoved != 17 || result.SharedPhotos != 3 || result.RejectionsDropped != 1 {
		t.Errorf("merge result = %+v, want the server's counts", result)
	}
}

// TestClient_MergeSubjects_intoSelf verifies a merge of a subject into itself is
// refused locally, before a request destroys anything.
func TestClient_MergeSubjects_intoSelf(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("a self-merge reached the server")
	})
	if _, err := client.MergeSubjects(t.Context(), "sub01", "sub01"); !errors.Is(err, ErrMergeIntoSelf) {
		t.Errorf("self-merge = %v, want ErrMergeIntoSelf", err)
	}
}

// TestSubjectRefFromArgs verifies the uid and the name are alternatives: one of
// them is required and both together are refused, because the API would silently
// resolve by uid and ignore the name.
func TestSubjectRefFromArgs(t *testing.T) {
	t.Parallel()

	ref, err := SubjectRefFromArgs(" sub01 ", "")
	if err != nil || ref.UID != "sub01" || ref.Name != "" {
		t.Errorf("uid only = %+v, %v, want the trimmed uid", ref, err)
	}
	ref, err = SubjectRefFromArgs("", " Anna ")
	if err != nil || ref.Name != "Anna" || ref.UID != "" {
		t.Errorf("name only = %+v, %v, want the trimmed name", ref, err)
	}
	if _, err := SubjectRefFromArgs("", ""); !errors.Is(err, ErrSubjectRequired) {
		t.Errorf("neither = %v, want ErrSubjectRequired", err)
	}
	if _, err := SubjectRefFromArgs("sub01", "Anna"); !errors.Is(err, ErrSubjectAmbiguous) {
		t.Errorf("both = %v, want ErrSubjectAmbiguous", err)
	}
}

// TestClient_DescribeSubject verifies a subject is named for a confirmation line,
// and that a 404 surfaces rather than being papered over with the bare uid.
func TestClient_DescribeSubject(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"uid":"sub01","name":"Anna","type":"person"}`))
	})
	label, err := client.DescribeSubject(t.Context(), "sub01")
	if err != nil || label != "Anna (sub01)" {
		t.Errorf("DescribeSubject = %q, %v, want the name beside the uid", label, err)
	}

	missing := testClient(t, "kkt_a_b", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"subject not found"}`))
	})
	var status *StatusError
	if _, err := missing.DescribeSubject(t.Context(), "sub09"); !errors.As(err, &status) {
		t.Errorf("DescribeSubject of a missing subject = %v, want a StatusError", err)
	}
}
