package ctl

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// uploadLinksBody is a realistic {"links": […]} envelope: one link with a known
// code and one created before codes were stored readably.
const uploadLinksBody = `{"links":[
	{"uid":"ul1","title":"Pouť 2026","state":"active","expires_at":"2026-10-31T10:00:00Z",
	 "upload_count":3,"created_by_name":"Kurátor","code":"Ab3dEf7h","path":"/u/Ab3dEf7h"},
	{"uid":"ul2","title":"Hody","state":"active","expires_at":"2026-11-30T10:00:00Z","upload_count":0}
],"default_days":30,"max_days":365}`

// TestClient_ListUploadLinks verifies the listing renders each link's absolute
// URL, and a link without a known code points at restore-code.
func TestClient_ListUploadLinks(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(uploadLinksBody))
	})
	raw, err := client.ListUploadLinks(t.Context())
	if err != nil {
		t.Fatalf("ListUploadLinks returned %v", err)
	}
	if gotPath != "/api/v1/upload-links" {
		t.Errorf("path = %q, want the upload link list", gotPath)
	}
	links, err := DecodeUploadLinks(raw)
	if err != nil || len(links) != 2 {
		t.Fatalf("DecodeUploadLinks = %+v, %v", links, err)
	}
	var buf bytes.Buffer
	if err := WriteUploadLinks(&buf, "https://fotky.example/", links); err != nil {
		t.Fatalf("WriteUploadLinks: %v", err)
	}
	for _, want := range []string{"URL", "https://fotky.example/u/Ab3dEf7h", unknownLinkURL, "Pouť 2026"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("table does not contain %q:\n%s", want, buf.String())
		}
	}
}

// TestClient_RestoreUploadLinkCode verifies the code — read out of a pasted URL
// when need be — goes in the body, and a blank one costs no round trip.
func TestClient_RestoreUploadLinkCode(t *testing.T) {
	t.Parallel()

	var (
		gotPath string
		gotBody map[string]string
	)
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`{"link":{"uid":"ul2","state":"active","code":"Ab3dEf7h","path":"/u/Ab3dEf7h"}}`))
	})
	raw, err := client.RestoreUploadLinkCode(t.Context(), "ul2", " https://fotky.example/u/Ab3dEf7h/ ")
	if err != nil {
		t.Fatalf("RestoreUploadLinkCode returned %v", err)
	}
	if gotPath != "/api/v1/upload-links/ul2/restore-code" || gotBody["code"] != "Ab3dEf7h" {
		t.Errorf("request = %s %v, want the restore route with the bare code", gotPath, gotBody)
	}
	if link, err := DecodeUploadLink(raw); err != nil || link.Path != "/u/Ab3dEf7h" {
		t.Errorf("DecodeUploadLink = %+v, %v", link, err)
	}
	if _, err := client.RestoreUploadLinkCode(t.Context(), "ul2", "  "); !errors.Is(err, ErrEmptyLinkCode) {
		t.Errorf("blank code error = %v, want ErrEmptyLinkCode", err)
	}
}

// TestClient_NewUploadLinkCode verifies the new-code route is a bodiless POST.
func TestClient_NewUploadLinkCode(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Write([]byte(`{"link":{"uid":"ul1","code":"Nw5cDe9k","path":"/u/Nw5cDe9k"}}`))
	})
	if _, err := client.NewUploadLinkCode(t.Context(), "ul1"); err != nil {
		t.Fatalf("NewUploadLinkCode returned %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/upload-links/ul1/new-code" {
		t.Errorf("request = %s %s, want POST to the new-code route", gotMethod, gotPath)
	}
}

// TestParseUploadLinkCode verifies the shapes somebody pastes.
func TestParseUploadLinkCode(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{"Ab3dEf7h", "Ab3dEf7h"},
		{"  Ab3dEf7h ", "Ab3dEf7h"},
		{"https://fotky.example/u/Ab3dEf7h", "Ab3dEf7h"},
		{"https://fotky.example/u/Ab3dEf7h/?x=1#top", "Ab3dEf7h"},
		{"/u/Ab3dEf7h", "Ab3dEf7h"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ParseUploadLinkCode(tt.in); got != tt.want {
			t.Errorf("ParseUploadLinkCode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
