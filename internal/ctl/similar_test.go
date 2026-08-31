package ctl

import (
	"bytes"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// similarBody is a realistic {"similar": […]} envelope: photo rows, each with the
// cosine distance to the source.
const similarBody = `{"similar":[
	{"uid":"pht02","file_name":"b.jpg","title":"Jezero ráno","taken_at":"2024-05-01T10:22:33Z",
	 "file_size":2097152,"distance":0.0821},
	{"uid":"pht03","file_name":"c.jpg","title":"","distance":0.2134}
]}`

// TestClient_ListSimilar verifies the path, the limit and the decoded distances.
func TestClient_ListSimilar(t *testing.T) {
	t.Parallel()

	var gotPath, gotQuery string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		w.Write([]byte(similarBody))
	})

	raw, err := client.ListSimilar(t.Context(), "pht01", 10)
	if err != nil {
		t.Fatalf("ListSimilar returned %v", err)
	}
	if gotPath != "/api/v1/photos/pht01/similar" || gotQuery != "limit=10" {
		t.Errorf("request = %s?%s, want the similar path with the limit", gotPath, gotQuery)
	}
	similar, err := DecodeSimilar(raw)
	if err != nil {
		t.Fatalf("DecodeSimilar returned %v", err)
	}
	if len(similar) != 2 || similar[0].UID != "pht02" || similar[0].Distance != 0.0821 {
		t.Errorf("similar = %+v, want the neighbours with their distances", similar)
	}
}

// TestClient_ListSimilar_defaultLimit verifies a zero limit sends no parameter, so
// the server's own default applies.
func TestClient_ListSimilar_defaultLimit(t *testing.T) {
	t.Parallel()

	var gotQuery string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{"similar":[]}`))
	})
	if _, err := client.ListSimilar(t.Context(), "pht01", 0); err != nil {
		t.Fatalf("ListSimilar returned %v", err)
	}
	if gotQuery != "" {
		t.Errorf("query = %q, want none so the server defaults", gotQuery)
	}
}

// TestClient_ListSimilar_invalid verifies a limit the server would silently clamp
// is refused instead, so a caller asking for 500 learns it cannot have them.
func TestClient_ListSimilar_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid input")
	})
	if _, err := client.ListSimilar(t.Context(), "pht01", 500); !errors.Is(err, ErrInvalidSimilarLimit) {
		t.Errorf("oversized limit error = %v, want ErrInvalidSimilarLimit", err)
	}
	if _, err := client.ListSimilar(t.Context(), " ", 10); !errors.Is(err, ErrEmptyUID) {
		t.Errorf("blank uid error = %v, want ErrEmptyUID", err)
	}
}

// TestWriteSimilar verifies the distance is on every row — without it a
// neighbour list is just a list — and that an empty answer says why it may be
// empty.
func TestWriteSimilar(t *testing.T) {
	t.Parallel()

	similar, err := DecodeSimilar([]byte(similarBody))
	if err != nil {
		t.Fatalf("DecodeSimilar returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteSimilar(&buf, similar); err != nil {
		t.Fatalf("WriteSimilar returned %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "0.082") || !strings.Contains(out, "0.213") {
		t.Errorf("output %q does not print both distances", out)
	}

	buf.Reset()
	if err := WriteSimilar(&buf, nil); err != nil {
		t.Fatalf("WriteSimilar returned %v", err)
	}
	if !strings.Contains(buf.String(), "not be embedded yet") {
		t.Errorf("empty output = %q, want it to name the likelier cause", buf.String())
	}
}

// TestDecodeSimilar_invalid verifies malformed JSON surfaces as an error.
func TestDecodeSimilar_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodeSimilar([]byte(`{"similar":`)); err == nil {
		t.Error("DecodeSimilar of malformed JSON returned no error")
	}
}
