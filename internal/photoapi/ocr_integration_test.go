//go:build integration

package photoapi_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/photos"
)

// detailOCR fetches a photo's detail and returns the text the response carries
// for it, together with whether the key was present at all.
func detailOCR(t *testing.T, env *env, client *http.Client, uid string) (string, bool) {
	t.Helper()
	resp := mustDo(t, client, http.MethodGet, env.server.URL+"/api/v1/photos/"+uid, nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	raw, ok := body["ocr_text"]
	if !ok {
		return "", false
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		t.Fatalf("ocr_text %s is not a string: %v", raw, err)
	}
	return text, true
}

// TestDetail_servesOCRText verifies the text the recogniser read *in* a photo
// reaches the detail response, which is the only endpoint that carries it: it was
// stored and searchable (`text:`) but served nowhere, so an agent could find a
// photo by what is written on it and then not read the writing.
func TestDetail_servesOCRText(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "reader", auth.RoleViewer)

	photo := env.seedPhoto(t, photos.Photo{Title: "Shop"}, "shop.jpg", 10, 20, 30)
	const read = "ZAVŘENO\nOtevíráme v pondělí"
	if err := env.store.SaveOCR(t.Context(), photo.UID, photos.OCR{
		Text: read, Model: "PP-OCRv5_mobile",
	}); err != nil {
		t.Fatalf("SaveOCR: %v", err)
	}

	got, present := detailOCR(t, env, client, photo.UID)
	if !present {
		t.Fatal("the detail carries no ocr_text at all")
	}
	if got != read {
		t.Errorf("ocr_text = %q, want the recognised text verbatim", got)
	}
}

// TestDetail_omitsOCRTextWhenThereIsNone verifies a photo the recogniser has never
// seen — and one it looked at and found nothing on — costs no bytes at all. The
// key is simply absent, which is what makes serving it unconditionally cheap
// enough not to need an opt-in parameter.
func TestDetail_omitsOCRTextWhenThereIsNone(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "reader", auth.RoleViewer)

	never := env.seedPhoto(t, photos.Photo{Title: "Never read"}, "never.jpg", 40, 50, 60)
	if _, present := detailOCR(t, env, client, never.UID); present {
		t.Error("ocr_text is present for a photo the recogniser never saw")
	}

	blank := env.seedPhoto(t, photos.Photo{Title: "Nothing on it"}, "blank.jpg", 70, 80, 90)
	if err := env.store.SaveOCR(t.Context(), blank.UID, photos.OCR{Model: "PP-OCRv5_mobile"}); err != nil {
		t.Fatalf("SaveOCR: %v", err)
	}
	if _, present := detailOCR(t, env, client, blank.UID); present {
		t.Error("ocr_text is present for a photo with nothing written on it")
	}
}

// TestList_neverServesOCRText verifies the text stays off the list and search
// pages. A hundred scanned documents in one response is exactly the payload the
// detail endpoint is allowed to pay for and a listing is not.
func TestList_neverServesOCRText(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "reader", auth.RoleViewer)

	photo := env.seedPhoto(t, photos.Photo{Title: "Shop"}, "shop.jpg", 10, 20, 30)
	if err := env.store.SaveOCR(t.Context(), photo.UID, photos.OCR{
		Text: "ZAVŘENO", Model: "PP-OCRv5_mobile",
	}); err != nil {
		t.Fatalf("SaveOCR: %v", err)
	}

	for _, path := range []string{"/api/v1/photos", "/api/v1/search?q=Shop"} {
		resp := mustDo(t, client, http.MethodGet, env.server.URL+path, nil)
		var page struct {
			Photos []map[string]json.RawMessage `json:"photos"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if len(page.Photos) == 0 {
			t.Fatalf("%s returned no photos", path)
		}
		for _, row := range page.Photos {
			if _, ok := row["ocr_text"]; ok {
				t.Errorf("%s carries ocr_text on a list row", path)
			}
		}
	}
}

// TestDetail_omitsPeopleWithoutAFaceBackend verifies the people block stays
// absent when no face service is wired, whether or not the caller asked — the
// photo is worth showing either way.
func TestDetail_omitsPeopleWithoutAFaceBackend(t *testing.T) {
	env := newEnv(t)
	client, _ := env.login(t, "reader", auth.RoleViewer)

	photo := env.seedPhoto(t, photos.Photo{Title: "Shop"}, "shop.jpg", 10, 20, 30)
	resp := mustDo(t, client, http.MethodGet,
		env.server.URL+"/api/v1/photos/"+photo.UID+"?people=true", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status = %d, want 200", resp.StatusCode)
	}
	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if _, ok := body["people"]; ok {
		t.Error("people is present with no face backend wired")
	}
}
