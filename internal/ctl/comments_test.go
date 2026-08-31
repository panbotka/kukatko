package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// commentsBody is a realistic {"comments": […]} thread: two entries, the second
// edited, both with a resolved author name.
const commentsBody = `{"comments":[
	{"uid":"cmt01","photo_uid":"pht01","author_uid":"usr01","author_name":"Anna Nováková",
	 "body":"Babička na dvoře,\nléto 1978.","created_at":"2024-05-01T10:22:33Z"},
	{"uid":"cmt02","photo_uid":"pht01","author_uid":"usr02","author_name":"Tomáš",
	 "body":"Spíš 1979 — to už stál nový plot.","created_at":"2024-05-02T09:00:00Z",
	 "edited_at":"2024-05-02T09:05:00Z"}
]}`

// TestClient_ListComments verifies the thread path and that the bodies decode
// whole, newlines and all.
func TestClient_ListComments(t *testing.T) {
	t.Parallel()

	var gotPath, gotMethod string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.Write([]byte(commentsBody))
	})

	raw, err := client.ListComments(t.Context(), "pht01")
	if err != nil {
		t.Fatalf("ListComments returned %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/photos/pht01/comments" {
		t.Errorf("request = %s %s, want GET on the thread path", gotMethod, gotPath)
	}
	thread, err := DecodeComments(raw)
	if err != nil {
		t.Fatalf("DecodeComments returned %v", err)
	}
	if len(thread) != 2 || !strings.Contains(thread[0].Body, "léto 1978") {
		t.Errorf("thread = %+v, want both comments with their bodies", thread)
	}
	if thread[1].EditedAt == nil {
		t.Errorf("second comment = %+v, want its edited_at kept", thread[1])
	}
}

// TestClient_AddComment verifies the body is trimmed, posted, and echoed back.
func TestClient_AddComment(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotBody map[string]string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"uid":"cmt09","photo_uid":"pht01","author_uid":"usr09",
			"author_name":"Agent","body":"Podle EXIF 1978.","created_at":"2024-05-03T08:00:00Z"}`))
	})

	raw, err := client.AddComment(t.Context(), "pht01", "  Podle EXIF 1978.  ")
	if err != nil {
		t.Fatalf("AddComment returned %v", err)
	}
	if gotMethod != http.MethodPost || gotBody["body"] != "Podle EXIF 1978." {
		t.Errorf("request = %s with %v, want a POST carrying the trimmed body", gotMethod, gotBody)
	}
	if _, sent := gotBody["author_uid"]; sent {
		t.Errorf("body = %v, want no author: the server takes it from the token", gotBody)
	}
	comment, err := DecodeComment(raw)
	if err != nil || comment.UID != "cmt09" {
		t.Errorf("DecodeComment = %+v, %v, want the created comment", comment, err)
	}
}

// TestClient_AddComment_invalid verifies a blank and an over-long body are both
// refused before a request is spent on a guaranteed 400.
func TestClient_AddComment_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite an invalid body")
	})
	if _, err := client.AddComment(t.Context(), "pht01", "   "); !errors.Is(err, ErrEmptyComment) {
		t.Errorf("blank body error = %v, want ErrEmptyComment", err)
	}
	long := strings.Repeat("á", MaxCommentLen+1)
	if _, err := client.AddComment(t.Context(), "pht01", long); !errors.Is(err, ErrCommentTooLong) {
		t.Errorf("over-long body error = %v, want ErrCommentTooLong", err)
	}
	if _, err := client.AddComment(t.Context(), " ", "ahoj"); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank uid error = %v, want ErrEmptyUID", err)
	}
}

// TestWriteComments verifies the thread prints as prose: every body whole, with
// its author and its edited marker, because the text is the answer somebody came
// for and a column width would cut it off.
func TestWriteComments(t *testing.T) {
	t.Parallel()

	thread, err := DecodeComments([]byte(commentsBody))
	if err != nil {
		t.Fatalf("DecodeComments returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteComments(&buf, thread); err != nil {
		t.Fatalf("WriteComments returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"Anna Nováková (usr01)", "  Babička na dvoře,", "  léto 1978.",
		"Spíš 1979 — to už stál nový plot.", "(edited",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q does not contain %q", out, want)
		}
	}

	buf.Reset()
	if err := WriteComments(&buf, nil); err != nil {
		t.Fatalf("WriteComments returned %v", err)
	}
	if strings.TrimSpace(buf.String()) != "no comments on this photo" {
		t.Errorf("empty output = %q, want the one-line message", buf.String())
	}
}

// TestDecodeComments_invalid verifies malformed JSON surfaces as an error.
func TestDecodeComments_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodeComments([]byte(`{"comments":`)); err == nil {
		t.Error("DecodeComments of malformed JSON returned no error")
	}
	if _, err := DecodeComment([]byte(`nope`)); err == nil {
		t.Error("DecodeComment of malformed JSON returned no error")
	}
}
