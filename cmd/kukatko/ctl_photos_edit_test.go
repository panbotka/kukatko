package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// resolveEditFlags parses argv against a fresh edit command's flag set and
// returns the request body the command would send, decoded key by key so a test
// can tell an absent field from an explicit null.
func resolveEditFlags(t *testing.T, argv ...string) (map[string]json.RawMessage, error) {
	t.Helper()

	var flags photoEditFlags
	cmd := &cobra.Command{Use: "edit"}
	flags.register(cmd)
	if err := cmd.Flags().Parse(argv); err != nil {
		t.Fatalf("parsing %v: %v", argv, err)
	}
	edit, err := flags.resolve(cmd.Flags())
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(edit)
	if err != nil {
		t.Fatalf("marshalling the edit: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("edit body %q does not parse: %v", encoded, err)
	}
	return body, nil
}

// TestPhotoEditFlags_onlyWhatWasPassed verifies the command's central rule: a
// flag nobody wrote does not appear in the request body at all.
//
// It matters most for taken_at. Re-sending the date that is already on the photo
// would make the server stamp it "manual" and the library would lose the fact
// that the date came out of the file — a silent, permanent loss on a photo the
// caller only meant to retitle.
func TestPhotoEditFlags_onlyWhatWasPassed(t *testing.T) {
	t.Parallel()

	body, err := resolveEditFlags(t, "--title", "Lake")
	if err != nil {
		t.Fatalf("resolve returned %v", err)
	}
	if len(body) != 1 {
		t.Fatalf("body = %v, want only the title", body)
	}
	if string(body["title"]) != `"Lake"` {
		t.Errorf("title = %s, want the value", body["title"])
	}
	for _, absent := range []string{"taken_at", "description", "scan", "lat", "taken_at_estimated"} {
		if _, ok := body[absent]; ok {
			t.Errorf("untouched %q appeared in the body", absent)
		}
	}
}

// TestPhotoEditFlags_clearingIsNotOmission verifies each way of saying "make this
// empty": the empty string for a text column, and an explicit null for the three
// nullable ones.
func TestPhotoEditFlags_clearingIsNotOmission(t *testing.T) {
	t.Parallel()

	body, err := resolveEditFlags(t, "--description", "", "--clear-taken-at", "--clear-location")
	if err != nil {
		t.Fatalf("resolve returned %v", err)
	}
	if string(body["description"]) != `""` {
		t.Errorf("description = %s, want an empty string", body["description"])
	}
	for _, key := range []string{"taken_at", "lat", "lng"} {
		raw, ok := body[key]
		if !ok {
			t.Errorf("cleared %q was omitted; the server would leave it alone", key)
			continue
		}
		if string(raw) != "null" {
			t.Errorf("cleared %q = %s, want null", key, raw)
		}
	}
}

// TestPhotoEditFlags_wholeSurface verifies every editable field the API accepts
// has a flag and lands under the API's own field name.
func TestPhotoEditFlags_wholeSurface(t *testing.T) {
	t.Parallel()

	body, err := resolveEditFlags(t,
		"--title", "Lake", "--description", "an evening", "--notes", "n", "--ai-note", "a boat",
		"--subject", "s", "--keywords", "k1,k2", "--artist", "Josef", "--copyright", "c",
		"--license", "CC0", "--scan",
		"--taken-at", "1978-06-03", "--taken-at-estimated", "--taken-at-note", "kolem roku 1978",
		"--lat", "50.1", "--lng", "14.4", "--accept-location",
	)
	if err != nil {
		t.Fatalf("resolve returned %v", err)
	}
	want := map[string]string{
		"title": `"Lake"`, "description": `"an evening"`, "notes": `"n"`, "ai_note": `"a boat"`,
		"subject": `"s"`, "keywords": `"k1,k2"`, "artist": `"Josef"`, "copyright": `"c"`,
		"license": `"CC0"`, "scan": "true",
		"taken_at": `"1978-06-03T00:00:00Z"`, "taken_at_estimated": "true",
		"taken_at_note": `"kolem roku 1978"`,
		"lat":           "50.1", "lng": "14.4", "location_source": `"manual"`,
	}
	for key, value := range want {
		if got := string(body[key]); got != value {
			t.Errorf("%s = %s, want %s", key, got, value)
		}
	}
	if len(body) != len(want) {
		t.Errorf("body carries %d fields, want %d: %v", len(body), len(want), body)
	}
}

// TestPhotoEditFlags_noFlagsForRefusedFields verifies the fields the API serves
// but refuses to edit get no flags at all — offering one the server would reject
// is worse than offering none.
func TestPhotoEditFlags_noFlagsForRefusedFields(t *testing.T) {
	t.Parallel()

	var flags photoEditFlags
	cmd := &cobra.Command{Use: "edit"}
	flags.register(cmd)
	for _, name := range []string{
		"software", "color-profile", "image-codec", "camera-serial", "original-name", "projection",
	} {
		if cmd.Flags().Lookup(name) != nil {
			t.Errorf("--%s exists, but the API refuses to edit that field", name)
		}
	}
}

// TestPhotoEditFlags_conflicts verifies the contradictions that are caught before
// a request is made, and the half coordinate pair that would silently move a
// photo onto the prime meridian.
func TestPhotoEditFlags_conflicts(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		argv []string
		want error
	}{
		"set and clear the date": {
			[]string{"--taken-at", "1978-06-03", "--clear-taken-at"}, ctl.ErrConflictingEdits},
		"set and clear the location": {
			[]string{"--lat", "50.1", "--lng", "14.4", "--clear-location"}, ctl.ErrConflictingEdits},
		"clear and accept the location": {
			[]string{"--clear-location", "--accept-location"}, ctl.ErrConflictingEdits},
		"only a latitude":  {[]string{"--lat", "50.1"}, ctl.ErrIncompleteLocation},
		"only a longitude": {[]string{"--lng", "14.4"}, ctl.ErrIncompleteLocation},
		"an unreadable date": {
			[]string{"--taken-at", "last summer"}, ctl.ErrInvalidTimestamp},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := resolveEditFlags(t, tc.argv...); !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestCtlPhotosEdit_dryRun verifies --dry-run prints the exact body and contacts
// nothing. This runs against a live family archive; showing intent first has to
// be free of side effects.
func TestCtlPhotosEdit_dryRun(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted during a dry run")
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "edit", "pht01",
		"--title", "Lake", "--clear-taken-at", "--dry-run")
	if err != nil {
		t.Fatalf("photos edit --dry-run returned %v", err)
	}
	if !strings.Contains(out, "PATCH /api/v1/photos/pht01") {
		t.Errorf("dry run does not name the request:\n%s", out)
	}
	if !strings.Contains(out, `"title": "Lake"`) || !strings.Contains(out, `"taken_at": null`) {
		t.Errorf("dry run does not print the body it would send:\n%s", out)
	}
}

// TestCtlPhotosEdit_sendsPatch verifies the command PATCHes the photo and renders
// the refreshed detail the server answers with.
func TestCtlPhotosEdit_sendsPatch(t *testing.T) {
	var (
		gotMethod string
		gotBody   string
	)
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Write([]byte(`{"uid":"pht01","title":"Lake","ocr_text":"ZAVŘENO"}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "edit", "pht01",
		"--title", "Lake")
	if err != nil {
		t.Fatalf("photos edit returned %v", err)
	}
	if gotMethod != http.MethodPatch || gotBody != `{"title":"Lake"}` {
		t.Errorf("request = %s %s, want a PATCH carrying only the title", gotMethod, gotBody)
	}
	if !strings.Contains(out, "Lake") || !strings.Contains(out, "ZAVŘENO") {
		t.Errorf("output does not render the refreshed detail:\n%s", out)
	}
}

// TestCtlPhotosEdit_noFields verifies an edit that would change nothing is
// refused locally, so the server records no audit entry for a change nobody made.
func TestCtlPhotosEdit_noFields(t *testing.T) {
	configPath := ctlServer(t, func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted for an empty edit")
	})

	_, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "edit", "pht01")
	if !errors.Is(err, ctl.ErrNoEdits) {
		t.Errorf("error = %v, want ErrNoEdits", err)
	}
}
