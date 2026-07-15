//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// stackDetailResp is the slice of the detail response the stack tests read.
type stackDetailResp struct {
	UID          string `json:"uid"`
	StackUID     string `json:"stack_uid"`
	StackCount   int    `json:"stack_count"`
	StackMembers []struct {
		UID       string `json:"uid"`
		IsPrimary bool   `json:"is_primary"`
	} `json:"stack_members"`
}

// stackListResp is the slice of the list response the stack tests read.
type stackListResp struct {
	Photos []struct {
		UID        string `json:"uid"`
		StackCount int    `json:"stack_count"`
	} `json:"photos"`
	Total int `json:"total"`
}

// primaryUID returns the uid of the member flagged primary, or "" if none.
func (d stackDetailResp) primaryUID() string {
	for _, m := range d.StackMembers {
		if m.IsPrimary {
			return m.UID
		}
	}
	return ""
}

func TestIntegration_StackHTTPEndpoints(t *testing.T) {
	e := newEnv(t)
	editor, _ := e.login(t, "editor", auth.RoleEditor)
	// A RAW next to its JPEG: the .CR2 extension makes the JPEG the primary.
	raw := e.seedPhoto(t, photos.Photo{Title: "shot"}, "IMG_9.CR2", 10, 20, 30)
	jpg := e.seedPhoto(t, photos.Photo{Title: "shot"}, "IMG_9.jpg", 40, 50, 60)

	// Manual stack of the selection answers with the primary's detail + strip.
	body, _ := json.Marshal(map[string][]string{"photo_uids": {raw.UID, jpg.UID}})
	detail := postStack(t, editor, e.server.URL+"/api/v1/photos/stack", body)
	if detail.UID != jpg.UID {
		t.Errorf("stacked primary detail uid = %s, want %s (jpeg)", detail.UID, jpg.UID)
	}
	if len(detail.StackMembers) != 2 || detail.primaryUID() != jpg.UID {
		t.Errorf("stack members = %+v, want 2 with jpeg primary", detail.StackMembers)
	}

	// The grid returns one tile carrying the member-count badge.
	list := getStackList(t, editor, e.server.URL+"/api/v1/photos")
	if list.Total != 1 || len(list.Photos) != 1 || list.Photos[0].UID != jpg.UID {
		t.Fatalf("list = %+v, want only the jpeg primary", list)
	}
	if list.Photos[0].StackCount != 2 {
		t.Errorf("stack_count badge = %d, want 2", list.Photos[0].StackCount)
	}

	// Set-primary moves the primary onto the RAW; its detail then shows it primary.
	moved := postStack(t, editor, e.server.URL+"/api/v1/photos/"+raw.UID+"/stack/primary", nil)
	if moved.UID != raw.UID || moved.primaryUID() != raw.UID {
		t.Errorf("after set-primary detail = %+v, want raw primary", moved)
	}
	if got := getStackList(t, editor, e.server.URL+"/api/v1/photos"); got.Total != 1 || got.Photos[0].UID != raw.UID {
		t.Errorf("after set-primary list = %+v, want only the raw", got)
	}

	// Unstacking the JPEG leaves the RAW alone, so the stack dissolves and both
	// photos stand on their own again.
	if resp := mustDo(t, editor, http.MethodPost, e.server.URL+"/api/v1/photos/"+jpg.UID+"/unstack", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("unstack status = %d, want 200", resp.StatusCode)
	}
	if got := getStackList(t, editor, e.server.URL+"/api/v1/photos"); got.Total != 2 {
		t.Errorf("after unstack list total = %d, want 2 standalone photos", got.Total)
	}
}

func TestIntegration_StackForbiddenForViewer(t *testing.T) {
	e := newEnv(t)
	viewer, _ := e.login(t, "viewer", auth.RoleViewer)
	a := e.seedPhoto(t, photos.Photo{Title: "a"}, "A.jpg", 1, 2, 3)
	b := e.seedPhoto(t, photos.Photo{Title: "b"}, "B.jpg", 4, 5, 6)

	body, _ := json.Marshal(map[string][]string{"photo_uids": {a.UID, b.UID}})
	resp := mustDo(t, viewer, http.MethodPost, e.server.URL+"/api/v1/photos/stack", body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer stack status = %d, want 403", resp.StatusCode)
	}
}

// postStack POSTs to a stack endpoint and decodes the primary detail response,
// failing on a non-200 status.
func postStack(t *testing.T, client *http.Client, url string, body []byte) stackDetailResp {
	t.Helper()
	resp := mustDo(t, client, http.MethodPost, url, body)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s status = %d, want 200", url, resp.StatusCode)
	}
	var detail stackDetailResp
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode stack detail: %v", err)
	}
	return detail
}

// getList GETs the photo list and decodes the stack-relevant fields.
func getStackList(t *testing.T, client *http.Client, url string) stackListResp {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, url, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, resp.StatusCode)
	}
	var list stackListResp
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	return list
}
