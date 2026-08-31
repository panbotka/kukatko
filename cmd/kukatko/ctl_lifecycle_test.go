package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// recorder collects every request a command made, so a test about a gate can
// assert the thing it is guarding never happened.
type recorder struct {
	calls []string
}

// record appends one request in "METHOD /path" form.
func (r *recorder) record(req *http.Request) {
	r.calls = append(r.calls, req.Method+" "+req.URL.Path)
}

// touched reports whether any request matched "METHOD /path".
func (r *recorder) touched(call string) bool {
	return slices.Contains(r.calls, call)
}

// archivedPhotoBody is a photo detail in the trash, as `photos purge` reads it
// before destroying anything.
const archivedPhotoBody = `{"uid":"pht01","file_name":"a.jpg","title":"Lake","file_size":2097152,
	"archived_at":"2026-08-01T10:22:33Z","hidden_from_library":false}`

// trashHandler answers the endpoints the trash commands read, recording every
// call, and serves the archived photos as one page.
func trashHandler(rec *recorder, rows string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		switch {
		case r.URL.Path == "/api/v1/trash/info":
			w.Write([]byte(`{"retention_days":30}`))
		case r.URL.Path == "/api/v1/photos":
			fmt.Fprintf(w, `{"photos":[%s],"total":2,"limit":500,"offset":0,"next_offset":null}`, rows)
		case strings.HasSuffix(r.URL.Path, "/purge"), r.URL.Path == "/api/v1/trash/empty",
			r.URL.Path == "/api/v1/trash/purge-older":
			w.Write([]byte(`{"purged":2,"failed":0}`))
		default:
			w.Write([]byte(archivedPhotoBody))
		}
	}
}

// trashRows are two archived photos, one long past a 30-day retention window and
// one archived moments ago.
const trashRows = `{"uid":"pht01","file_name":"old.jpg","file_size":2097152,
		"archived_at":"2026-01-02T10:00:00Z"},
	{"uid":"pht02","file_name":"fresh.jpg","file_size":1024,
		"archived_at":"2099-01-02T10:00:00Z"}`

// TestCtlPhotosUpload verifies the files reach the server and the report names
// what became of each of them.
func TestCtlPhotosUpload(t *testing.T) {
	var gotPath string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"results":[{"filename":"a.jpg","status":201,"outcome":"created","photo_uid":"pht01"},` +
			`{"filename":"b.jpg","status":409,"outcome":"duplicate","photo_uid":"pht02"}]}`))
	})

	dir := t.TempDir()
	first := filepath.Join(dir, "a.jpg")
	second := filepath.Join(dir, "b.jpg")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "upload", first, second)
	if err != nil {
		t.Fatalf("photos upload returned %v (%s)", err, out)
	}
	if gotPath != "/api/v1/upload" {
		t.Errorf("path = %q, want the upload endpoint", gotPath)
	}
	for _, want := range []string{"created", "pht01", "duplicate", "1 created", "1 already in the library"} {
		if !strings.Contains(out, want) {
			t.Errorf("upload output is missing %q:\n%s", want, out)
		}
	}
}

// TestCtlPhotosUpload_failedFileExitsNonzero verifies a file that could not be
// catalogued is reported and makes the command fail, while a duplicate does not.
func TestCtlPhotosUpload_failedFileExitsNonzero(t *testing.T) {
	configPath := ctlServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"results":[{"filename":"a.txt","status":500,"outcome":"error",` +
			`"error":"unsupported media type"}]}`))
	})

	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("not a photo"), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "upload", path)
	if err == nil {
		t.Fatalf("photos upload succeeded for a file that failed:\n%s", out)
	}
	if !strings.Contains(out, "unsupported media type") {
		t.Errorf("output does not report why the file failed:\n%s", out)
	}
}

// TestCtlPhotosState verifies the four reversible commands post to their own
// endpoint and report where the photo now stands — without any confirmation,
// because none of them destroys anything.
func TestCtlPhotosState(t *testing.T) {
	for _, state := range []string{"archive", "unarchive", "hide", "unhide"} {
		t.Run(state, func(t *testing.T) {
			var gotPath string
			configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Write([]byte(archivedPhotoBody))
			})

			out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", state, "pht01")
			if err != nil {
				t.Fatalf("photos %s returned %v (%s)", state, err, out)
			}
			if gotPath != "/api/v1/photos/pht01/"+state {
				t.Errorf("path = %q, want the %s endpoint", gotPath, state)
			}
			if !strings.Contains(out, "Lake (pht01)") || !strings.Contains(out, "in the trash since") {
				t.Errorf("output does not report the photo's state:\n%s", out)
			}
		})
	}
}

// TestCtlPhotosPurge_refusesWithoutConfirmation verifies the gate: a bare purge
// destroys nothing, says why, and names the way to see what is at stake.
func TestCtlPhotosPurge_refusesWithoutConfirmation(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, trashHandler(rec, trashRows))

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "purge", "pht01")
	if err == nil {
		t.Fatalf("photos purge ran without --yes:\n%s", out)
	}
	if !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), "--dry-run") {
		t.Errorf("error = %q, want it to name --yes and --dry-run", err)
	}
	if rec.touched("POST /api/v1/photos/pht01/purge") {
		t.Errorf("the photo was purged without confirmation: %v", rec.calls)
	}
}

// TestCtlPhotosPurge_dryRunNamesThePhoto verifies a rehearsal works without
// --yes, names what would be destroyed, and destroys nothing.
func TestCtlPhotosPurge_dryRunNamesThePhoto(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, trashHandler(rec, trashRows))

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "purge", "pht01", "--dry-run")
	if err != nil {
		t.Fatalf("photos purge --dry-run returned %v (%s)", err, out)
	}
	for _, want := range []string{"dry run", "Lake (pht01)", "2.0 MiB", "in the trash since", "nothing was changed"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run output is missing %q:\n%s", want, out)
		}
	}
	if rec.touched("POST /api/v1/photos/pht01/purge") {
		t.Errorf("a dry run purged the photo: %v", rec.calls)
	}
}

// TestCtlPhotosPurge_confirmed verifies the confirmed purge actually runs and
// says what it destroyed.
func TestCtlPhotosPurge_confirmed(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, trashHandler(rec, trashRows))

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "photos", "purge", "pht01", "--yes")
	if err != nil {
		t.Fatalf("photos purge --yes returned %v (%s)", err, out)
	}
	if !rec.touched("POST /api/v1/photos/pht01/purge") {
		t.Errorf("the confirmed purge never reached the server: %v", rec.calls)
	}
	if !strings.Contains(out, "permanently deleted") {
		t.Errorf("output does not confirm the deletion:\n%s", out)
	}
}

// TestCtlTrashInfo verifies the informed-gate read: what is in the trash, oldest
// first, with the date retention will take each photo on.
func TestCtlTrashInfo(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, trashHandler(rec, trashRows))

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "trash", "info")
	if err != nil {
		t.Fatalf("trash info returned %v (%s)", err, out)
	}
	for _, want := range []string{"old.jpg", "fresh.jpg", "2026-02-01 10:00", "retention 30 days", "2 of 2 photos"} {
		if !strings.Contains(out, want) {
			t.Errorf("trash info is missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "old.jpg") > strings.Index(out, "fresh.jpg") {
		t.Errorf("the trash is not listed oldest first:\n%s", out)
	}
	if rec.touched("POST /api/v1/trash/empty") {
		t.Errorf("reading the trash emptied it: %v", rec.calls)
	}
}

// TestCtlTrashEmpty_refusesWithoutConfirmation verifies emptying the trash is
// gated and that the refusal costs the library nothing.
func TestCtlTrashEmpty_refusesWithoutConfirmation(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, trashHandler(rec, trashRows))

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "trash", "empty")
	if err == nil {
		t.Fatalf("trash empty ran without --yes:\n%s", out)
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error = %q, want it to name --yes", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("a refused empty still called the server: %v", rec.calls)
	}
}

// TestCtlTrashEmpty_dryRunListsWhatWouldGo verifies the rehearsal needs no --yes,
// names every photo at risk, and empties nothing.
func TestCtlTrashEmpty_dryRunListsWhatWouldGo(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, trashHandler(rec, trashRows))

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "trash", "empty", "--dry-run")
	if err != nil {
		t.Fatalf("trash empty --dry-run returned %v (%s)", err, out)
	}
	for _, want := range []string{"would permanently delete", "old.jpg", "fresh.jpg", "nothing was deleted"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run is missing %q:\n%s", want, out)
		}
	}
	if rec.touched("POST /api/v1/trash/empty") {
		t.Errorf("a dry run emptied the trash: %v", rec.calls)
	}
}

// TestCtlTrashEmpty_confirmed verifies the confirmed empty runs and reports its
// counts.
func TestCtlTrashEmpty_confirmed(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, trashHandler(rec, trashRows))

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "trash", "empty", "--yes")
	if err != nil {
		t.Fatalf("trash empty --yes returned %v (%s)", err, out)
	}
	if !rec.touched("POST /api/v1/trash/empty") {
		t.Errorf("the confirmed empty never reached the server: %v", rec.calls)
	}
	if !strings.Contains(out, "2 photos permanently deleted") {
		t.Errorf("output does not report what was destroyed:\n%s", out)
	}
}

// TestCtlTrashPurgeOlder_dryRunAppliesTheCutoff verifies the rehearsal lists only
// the photos the age window would actually take.
func TestCtlTrashPurgeOlder_dryRunAppliesTheCutoff(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, trashHandler(rec, trashRows))

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"trash", "purge-older", "--days", "30", "--dry-run")
	if err != nil {
		t.Fatalf("trash purge-older --dry-run returned %v (%s)", err, out)
	}
	if !strings.Contains(out, "old.jpg") {
		t.Errorf("the photo past the window is not listed:\n%s", out)
	}
	if strings.Contains(out, "fresh.jpg") {
		t.Errorf("a photo archived inside the window is listed as at risk:\n%s", out)
	}
	if rec.touched("POST /api/v1/trash/purge-older") {
		t.Errorf("a dry run purged the trash: %v", rec.calls)
	}
}

// TestCtlTrashPurgeOlder_refusesWithoutConfirmation verifies the age-bounded
// purge is gated like the rest.
func TestCtlTrashPurgeOlder_refusesWithoutConfirmation(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, trashHandler(rec, trashRows))

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "trash", "purge-older", "--days", "30")
	if err == nil {
		t.Fatalf("trash purge-older ran without --yes:\n%s", out)
	}
	if rec.touched("POST /api/v1/trash/purge-older") {
		t.Errorf("a refused purge still ran: %v", rec.calls)
	}
}

// TestCtlDuplicatesMerge_refusesWithoutConfirmation verifies a merge, which
// archives the copies it does not keep, is gated like a deletion.
func TestCtlDuplicatesMerge_refusesWithoutConfirmation(t *testing.T) {
	rec := &recorder{}
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Write([]byte(`{"keeper_uid":"pht01","archived":1,"dry_run":false}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath, "duplicates", "merge", "pht01", "pht02")
	if err == nil {
		t.Fatalf("duplicates merge ran without --yes:\n%s", out)
	}
	if len(rec.calls) != 0 {
		t.Errorf("a refused merge still called the server: %v", rec.calls)
	}
}

// TestCtlDuplicatesMerge_dryRunPreviewsOnTheServer verifies the rehearsal asks
// the server for its own preview — the same computation, writing nothing — and
// needs no --yes.
func TestCtlDuplicatesMerge_dryRunPreviewsOnTheServer(t *testing.T) {
	var gotBody string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the merge body: %v", err)
		}
		gotBody = string(body)
		w.Write([]byte(`{"keeper_uid":"pht01","archived":1,"albums_added":2,"dry_run":true}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"duplicates", "merge", "pht01", "pht02", "--dry-run")
	if err != nil {
		t.Fatalf("duplicates merge --dry-run returned %v (%s)", err, out)
	}
	if !strings.Contains(gotBody, `"dry_run":true`) {
		t.Errorf("body = %s, want the server asked for a preview", gotBody)
	}
	if !strings.Contains(out, "dry run: nothing was merged") {
		t.Errorf("output is not marked as a rehearsal:\n%s", out)
	}
}

// TestCtlDuplicatesMerge_confirmed verifies the confirmed merge runs and says
// where the copies went — the trash, not oblivion.
func TestCtlDuplicatesMerge_confirmed(t *testing.T) {
	var gotBody string
	configPath := ctlServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading the merge body: %v", err)
		}
		gotBody = string(body)
		w.Write([]byte(`{"keeper_uid":"pht01","archived":2,"dry_run":false}`))
	})

	out, err := runCtl(t, "", "ctl", "--ctl-config", configPath,
		"duplicates", "merge", "pht01", "pht02", "pht03", "--yes")
	if err != nil {
		t.Fatalf("duplicates merge --yes returned %v (%s)", err, out)
	}
	if !strings.Contains(gotBody, `"member_uids":["pht01","pht02","pht03"]`) {
		t.Errorf("body = %s, want the keeper folded into its own group", gotBody)
	}
	if !strings.Contains(out, "in the trash, not deleted") {
		t.Errorf("output does not say where the copies went:\n%s", out)
	}
}
