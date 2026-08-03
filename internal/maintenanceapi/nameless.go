package maintenanceapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/auth"
	"github.com/panbotka/kukatko/internal/namelessjob"
	"github.com/panbotka/kukatko/internal/people"
)

// maxUndoBytes caps the undo file accepted by the restore endpoint. The
// production catch-all's undo is a few megabytes (16 531 marker uids and 111 155
// (photo, slot) pairs), so the limit is generous enough for it and still bounds
// what an upload can make the server hold.
const maxUndoBytes = 64 << 20

// undoFilenameLayout stamps the downloaded undo file so consecutive runs do not
// land on the same name in the operator's downloads folder.
const undoFilenameLayout = "20060102T150405Z"

// enqueueTimeout bounds the scheduling that follows a delivered undo file. It
// runs on a context detached from the request, so it needs a deadline of its own.
const enqueueTimeout = 30 * time.Second

// The headers that describe the detach the downloaded undo file covers. The body
// is the file itself — the browser saves it rather than parsing it — so the plan
// travels in headers the client can read before it hands the blob to the user.
const (
	headerNamelessSubjects = "X-Kukatko-Nameless-Subjects"
	headerNamelessMarkers  = "X-Kukatko-Nameless-Markers"
	headerNamelessFaces    = "X-Kukatko-Nameless-Faces"
)

// NamelessRepair is the nameless-subject repair the API drives. It is satisfied
// by *namelessjob.Service; a nil NamelessRepair makes the three endpoints answer
// 503.
type NamelessRepair interface {
	// List reports the subjects whose name identifies nobody (read-only).
	List(ctx context.Context) ([]people.NamelessSubject, error)
	// Snapshot reads the undo file a detach would need, changing nothing.
	Snapshot(ctx context.Context) (namelessjob.Undo, error)
	// EnqueueDetach schedules the detach of everything the undo file covers.
	EnqueueDetach(ctx context.Context, undo namelessjob.Undo, meta audit.Meta) (int, error)
	// EnqueueRestore schedules the replay of an undo file.
	EnqueueRestore(ctx context.Context, undo namelessjob.Undo, meta audit.Meta) (int, error)
}

// namelessReportResponse is the read-only report: every nameless subject with the
// markers and cached faces pointing at it, plus the totals across them.
type namelessReportResponse struct {
	Subjects    []people.NamelessSubject `json:"subjects"`
	MarkerTotal int                      `json:"marker_total"`
	FaceTotal   int                      `json:"face_total"`
}

// namelessRestoreResponse reports how many restore jobs an undo file scheduled.
type namelessRestoreResponse struct {
	Queued int `json:"queued"`
}

// handleNamelessReport runs the read-only report: which subjects identify nobody
// and how much of the catalogue currently hangs off them. It is safe to click —
// it never writes — and is what tells an operator whether the repair applies at
// all. It answers 503 when the repair is not wired and 500 when the read fails.
func (a *API) handleNamelessReport(w http.ResponseWriter, r *http.Request) {
	if a.nameless == nil {
		writeError(w, http.StatusServiceUnavailable, "nameless-subject repair not available")
		return
	}
	found, err := a.nameless.List(r.Context())
	if err != nil {
		log.Printf("maintenanceapi: listing nameless subjects: %v", err)
		writeError(w, http.StatusInternalServerError, "listing nameless subjects failed")
		return
	}
	out := namelessReportResponse{Subjects: found}
	for _, ns := range found {
		out.MarkerTotal += ns.MarkerCount
		out.FaceTotal += ns.FaceCount
	}
	writeJSON(w, http.StatusOK, out)
}

// handleNamelessDetach applies the repair: it hands the undo file to the browser
// as a download and only then schedules the detach.
//
// That order is the contract, not a convenience. On the command line `--apply`
// refuses without `--undo-file`, because a detach is otherwise irreversible —
// the marker→subject links are set NULL and nothing else records what they were.
// Over HTTP the operator's copy is the download, so the response body is written
// and flushed first and the jobs are enqueued only once the client has actually
// taken it: a failed write, or a client that hung up, leaves the catalogue
// untouched. An enqueue that fails after the file went out is logged and leaves
// the catalogue untouched too — the operator holds an undo for a detach that
// never happened, which replays as a no-op.
//
// The body carries no Content-Length, so the response stream ends only when this
// handler returns — after the scheduling. A client that reads the download to EOF
// (any browser taking it as a Blob) therefore knows the jobs are queued by the
// time it has the file.
//
// It answers 503 when the repair is not wired and 409 when there is no nameless
// subject to detach, both before anything is written.
func (a *API) handleNamelessDetach(w http.ResponseWriter, r *http.Request) {
	if a.nameless == nil {
		writeError(w, http.StatusServiceUnavailable, "nameless-subject repair not available")
		return
	}
	undo, err := a.nameless.Snapshot(r.Context())
	if err != nil {
		log.Printf("maintenanceapi: snapshotting nameless subjects: %v", err)
		writeError(w, http.StatusInternalServerError, "reading the undo snapshot failed")
		return
	}
	if len(undo.Subjects) == 0 {
		writeError(w, http.StatusConflict, "no nameless subjects: nothing to detach")
		return
	}
	raw, err := json.MarshalIndent(undo, "", "  ")
	if err != nil {
		log.Printf("maintenanceapi: encoding the nameless undo file: %v", err)
		writeError(w, http.StatusInternalServerError, "encoding the undo file failed")
		return
	}
	meta := auditMeta(r)
	if !deliverUndo(w, append(raw, '\n'), undo) {
		log.Printf("maintenanceapi: the nameless undo file was not delivered; nothing detached")
		return
	}
	// Detached from the request: a client that has the file and closes the body
	// cancels r.Context() the instant delivery succeeds, and the scheduling this
	// response has already promised must not die with it.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), enqueueTimeout)
	defer cancel()
	if queued, err := a.nameless.EnqueueDetach(ctx, undo, meta); err != nil {
		log.Printf("maintenanceapi: scheduling the nameless detach: %v (queued %d)", err, queued)
	}
}

// handleNamelessRestore takes an undo file back and schedules the replay: every
// subject it holds is re-created under its original uid and the markers and faces
// it owned are re-assigned. It answers 503 when the repair is not wired, 400 for
// a body that is not a readable undo file or records no subject, and 500 when the
// scheduling fails.
func (a *API) handleNamelessRestore(w http.ResponseWriter, r *http.Request) {
	if a.nameless == nil {
		writeError(w, http.StatusServiceUnavailable, "nameless-subject repair not available")
		return
	}
	undo, err := decodeUndo(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	queued, err := a.nameless.EnqueueRestore(r.Context(), undo, auditMeta(r))
	if err != nil {
		log.Printf("maintenanceapi: scheduling the nameless restore: %v", err)
		writeError(w, http.StatusInternalServerError, "scheduling the restore failed")
		return
	}
	writeJSON(w, http.StatusAccepted, namelessRestoreResponse{Queued: queued})
}

// decodeUndo reads the uploaded undo file. Unknown fields are *not* rejected: the
// file is a durable artefact an operator may have been holding since an older
// version wrote it, and refusing to replay it over a field the reader does not
// know would strand exactly the data the file exists to protect.
func decodeUndo(r *http.Request) (namelessjob.Undo, error) {
	var undo namelessjob.Undo
	if err := json.NewDecoder(io.LimitReader(r.Body, maxUndoBytes)).Decode(&undo); err != nil {
		return namelessjob.Undo{}, errors.New("invalid undo file")
	}
	if len(undo.Subjects) == 0 {
		return namelessjob.Undo{}, errors.New("the undo file records no detached subject")
	}
	return undo, nil
}

// deliverUndo writes the undo file to the response as a download and reports
// whether the client actually received it. Flushing is what makes the answer
// meaningful: it pushes the buffered body onto the connection and surfaces the
// connection's write error, so a client that hung up mid-transfer fails here
// rather than silently leaving the operator without an undo. A ResponseWriter
// that cannot flush at all (a recorder, a wrapper without Unwrap) is not treated
// as a failure — it buffers rather than drops.
//
// The request context is deliberately *not* consulted: a client that received the
// whole file and closed the body immediately cancels it, which would look like a
// failure at the exact moment delivery succeeded.
func deliverUndo(w http.ResponseWriter, raw []byte, undo namelessjob.Undo) bool {
	markers, faces := 0, 0
	for _, snap := range undo.Subjects {
		markers += len(snap.MarkerUIDs)
		faces += len(snap.Faces)
	}
	name := "kukatko-nameless-undo-" + time.Now().UTC().Format(undoFilenameLayout) + ".json"
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set(headerNamelessSubjects, strconv.Itoa(len(undo.Subjects)))
	h.Set(headerNamelessMarkers, strconv.Itoa(markers))
	h.Set(headerNamelessFaces, strconv.Itoa(faces))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(raw); err != nil {
		return false
	}
	if err := http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return false
	}
	return true
}

// auditMeta builds the audit provenance of the requesting maintainer, which the
// scheduled job stamps onto the audit row it writes inside its own transaction.
func auditMeta(r *http.Request) audit.Meta {
	user, _ := auth.UserFromContext(r.Context())
	return audit.FromRequest(r, user.UID)
}
