package uploadlinkapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/audit"
	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/ingest"
	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/uploadlink"
)

// SessionCookieName is the cookie identifying an anonymous uploader's browser
// session. It is scoped to the API so the registration endpoint
// (/api/v1/auth/register) sees it too, and it carries a random token whose hash
// marks the photos that session uploaded.
const SessionCookieName = "kukatko_upload_session"

// sessionCookiePath scopes the session cookie to the API.
const sessionCookiePath = "/api/v1"

// nameField is the multipart form field carrying the uploader's typed name. It
// must precede the files: the stream is read once, in order.
const nameField = "name"

// maxNameFieldBytes caps how much of the name field is read.
const maxNameFieldBytes = 1024

// maxFilesPerRequest caps the file parts one upload request may carry, refused
// ones included. The page sends one file per request; the cap keeps a script from
// streaming an endless body of parts — junk ones too — through the single token
// a request takes from the rate limiters.
const maxFilesPerRequest = 50

// Per-file refusals the upload reports inside its result list.
var (
	// errUnsupportedType is a file whose type the pipeline does not ingest.
	errUnsupportedType = errors.New("unsupported file type")
	// errLinkFull is a file past the link's lifetime upload cap.
	errLinkFull = errors.New("this link accepts no more uploads")
	// errLinkGone is a file sent after the link was revoked or expired while its
	// request was still running; the text is the 410 a dead link answers with,
	// which the page recognises.
	errLinkGone = errors.New(linkGoneMessage)
	// errTooManyFiles is a file part past maxFilesPerRequest; the request stops
	// there.
	errTooManyFiles = errors.New("too many files in one upload")
	// errNotAccepted is a file whose cap slot could not be taken for a reason
	// other than the link's state; sending it again may work.
	errNotAccepted = errors.New("the file could not be accepted")
	// errNotFiled is a photo that was ingested but could not be filed into the
	// link's targets; sending it again files it.
	errNotFiled = errors.New("the photo could not be added to the album")
)

// recordTimeout bounds the bookkeeping that follows a file's ingest — the
// upload's provenance, its filing into the link's targets, its audit entry and
// the sidecar reschedule — which runs detached from the request (see ingestOne).
const recordTimeout = time.Minute

// linkGoneMessage is the error of a link that no longer accepts uploads, for the
// whole request and for a single file alike.
const linkGoneMessage = "upload link is no longer valid"

// publicLink is what anybody holding a link learns: the curator's title and
// note, the name of the project and the names of the albums and labels the
// photos go to, the expiry, and whether registering through the link could
// succeed. Nothing else — no UIDs, no counts, no creator, no photos.
type publicLink struct {
	Title string `json:"title"`
	// Project is the name of the project the photos go into, so the holder
	// knows whose library they are adding to before they pick a file; "" only
	// without a ProjectResolver wired (tests).
	Project   string    `json:"project"`
	Note      string    `json:"note"`
	Albums    []string  `json:"albums"`
	Labels    []string  `json:"labels"`
	ExpiresAt time.Time `json:"expires_at"`
	// RegistrationEnabled is whether the link's project takes self-service
	// registrations, so the page offers registering through the link only
	// where it can succeed. It is the same flag the project's own public
	// settings publish, so it tells the holder nothing new.
	RegistrationEnabled bool `json:"registration_enabled"`
	// StorageFull is whether the link's project has used up its storage quota,
	// so the page says nothing can be uploaded before anybody picks a file. It
	// is a yes/no only: the holder of a link learns no sizes.
	StorageFull bool `json:"storage_full"`
	// CreditsAccount is whether an upload from this request is credited to the
	// signed-in account: only an approved member of the link's project is. A
	// visitor who is signed in on the instance but no member of the project
	// uploads exactly like an anonymous one — the typed name, never the
	// account — so the page asks for a name instead of saying "uploading as".
	CreditsAccount bool `json:"credits_account"`
}

// goneBody is the 410 of a link that exists but no longer accepts uploads. It
// says which of the two happened, and nothing else about the link.
type goneBody struct {
	Error string           `json:"error"`
	State uploadlink.State `json:"state"`
}

// uploadResponse mirrors the ingest endpoint's response: one result per file.
type uploadResponse struct {
	Results []ingest.FileResult `json:"results"`
}

// uploader is who an upload is attributed to: a signed-in account, or an
// anonymous browser session, plus the name they typed.
type uploader struct {
	userUID     string
	sessionHash string
	name        string
}

// handlePublic describes a live link to whoever holds it: 200 with the title,
// note, target names and expiry, 410 for an expired or revoked link, 404 for an
// unknown one.
//
// It also starts an anonymous visitor's upload session here, before any upload:
// the page uploads several files at once, and if the first of them had to mint
// the session each would mint its own, the browser would keep only the last
// cookie, and a later registration would claim only part of the batch.
func (a *API) handlePublic(w http.ResponseWriter, r *http.Request) {
	link, ok := a.liveLink(w, r)
	if !ok {
		return
	}
	credited, err := a.creditedAccount(r, link.ProjectUID)
	if err != nil {
		a.serverError(w, r, "checking the uploader's membership", err)
		return
	}
	if credited == "" && SessionHash(r) == "" {
		if _, err := a.startSession(w); err != nil {
			a.serverError(w, r, "starting upload session", err)
			return
		}
	}
	writeJSON(w, http.StatusOK, publicLink{
		Title: link.Title, Project: link.project.Name, Note: link.Note,
		Albums: names(link.Albums), Labels: names(link.Labels),
		ExpiresAt: link.ExpiresAt, RegistrationEnabled: a.registrationOpen(r, link.ProjectUID),
		StorageFull: a.storageFull(r, link), CreditsAccount: credited != "",
	})
}

// storageFull reports whether link's project has no room left for another
// original. Without a QuotaChecker wired, or on a failed usage read, it reports
// room: the upload itself is checked again file by file.
func (a *API) storageFull(r *http.Request, link liveTarget) bool {
	if a.quota == nil {
		return false
	}
	_, full := ingest.QuotaRefusal(r.Context(), a.quota, link.project)
	return full
}

// registrationOpen reports whether projectUID, a live link's project, takes
// self-service registrations — the switch a registration through the link is
// judged by. A failed read is reported closed and logged: the page then just
// does not offer the form, and the upload itself is unaffected.
func (a *API) registrationOpen(r *http.Request, projectUID string) bool {
	if a.registration == nil {
		return false
	}
	stored, err := a.registration.Get(r.Context(), projectUID)
	if err != nil {
		a.log.WarnContext(r.Context(), "uploadlinkapi: reading the link's registration switch", "error", err)
		return false
	}
	return stored.RegistrationEnabled
}

// handleUpload ingests the files of a multipart upload through a live link and
// files each resulting photo — new or duplicate — into the link's albums and
// labels. Like the ingest endpoint it answers 200 with one result per file; it
// answers 404/410 for a dead link, 429 when the link's budget is spent, 413
// {"error":"quota_exceeded"} before reading the body when the link's project has
// used up its storage quota, and 400 for a body that is not a multipart upload
// carrying a file.
func (a *API) handleUpload(w http.ResponseWriter, r *http.Request) {
	link, ok := a.liveLink(w, r)
	if !ok {
		return
	}
	if !a.linkLimit.Allow(link.UID) {
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected multipart/form-data upload")
		return
	}
	// Before a byte of the body: a project with no room left refuses every file.
	if a.storageFull(r, link) {
		writeJSON(w, http.StatusRequestEntityTooLarge, quotaFullBody)
		return
	}
	who, err := a.uploaderFor(w, r, link.ProjectUID)
	if err != nil {
		a.serverError(w, r, "resolving the uploader", err)
		return
	}
	results, err := a.ingestParts(r, reader, link, who)
	if err != nil {
		writeError(w, http.StatusBadRequest, "malformed multipart upload")
		return
	}
	if len(results) == 0 {
		writeError(w, http.StatusBadRequest, "no files in upload")
		return
	}
	writeJSON(w, http.StatusOK, uploadResponse{Results: results})
}

// liveTarget is a live link together with the project its uploads go into —
// which the pipeline needs whole, for its uid and its storage prefix.
type liveTarget struct {
	uploadlink.Link
	// project is the link's project. Without a ProjectResolver wired it is the
	// bare uid with the root layout's empty prefix.
	project *project.Project
}

// liveLink resolves the {code} link — instance-wide, by its code alone — and
// returns it with its project's storage prefix when it accepts uploads,
// writing 404 for an unknown code (and for a link whose project is gone or
// deactivated, which is answered exactly like one), 410 for an expired or
// revoked link and 500 for a failed lookup otherwise.
func (a *API) liveLink(w http.ResponseWriter, r *http.Request) (liveTarget, bool) {
	link, err := a.store.ByCode(r.Context(), chi.URLParam(r, "code"))
	if errors.Is(err, uploadlink.ErrNotFound) {
		writeError(w, http.StatusNotFound, uploadlink.ErrNotFound.Error())
		return liveTarget{}, false
	}
	if err != nil {
		a.serverError(w, r, "resolving upload link", err)
		return liveTarget{}, false
	}
	target := liveTarget{Link: link, project: &project.Project{UID: link.ProjectUID, Active: true}}
	if a.projects != nil {
		p, err := a.projects.Get(r.Context(), link.ProjectUID)
		if errors.Is(err, project.ErrNotFound) || (err == nil && !p.Active) {
			writeError(w, http.StatusNotFound, uploadlink.ErrNotFound.Error())
			return liveTarget{}, false
		}
		if err != nil {
			a.serverError(w, r, "resolving the upload link's project", err)
			return liveTarget{}, false
		}
		target.project = p
	}
	if state := link.StateAt(a.now()); state != uploadlink.StateActive {
		writeJSON(w, http.StatusGone, goneBody{Error: linkGoneMessage, State: state})
		return liveTarget{}, false
	}
	return target, true
}

// uploaderFor resolves who an upload through a link of projectUID is
// attributed to: the signed-in account when the project credits it
// (creditedAccount), or else the anonymous session — the one the session
// cookie names, or a fresh one whose cookie is set on w.
func (a *API) uploaderFor(w http.ResponseWriter, r *http.Request, projectUID string) (uploader, error) {
	credited, err := a.creditedAccount(r, projectUID)
	if err != nil {
		return uploader{}, err
	}
	if credited != "" {
		return uploader{userUID: credited}, nil
	}
	if hash := SessionHash(r); hash != "" {
		return uploader{sessionHash: hash}, nil
	}
	hash, err := a.startSession(w)
	if err != nil {
		return uploader{}, err
	}
	return uploader{sessionHash: hash}, nil
}

// creditedAccount returns the uid of the signed-in account an upload through a
// link of projectUID is credited to, or "" for none. Only an approved member of
// the link's project is credited (project.ApprovedMemberSQL): an account signed
// in on the instance that belongs to another project only — or the system
// administrator, who enters every project but belongs to none by that — would
// otherwise be stored as the photo's uploader, and the project would read the
// name of somebody outside it on the photo, in its uploader facet and in its
// audit trail. Such a visitor uploads like an anonymous one instead.
func (a *API) creditedAccount(r *http.Request, projectUID string) (string, error) {
	user, ok := a.currentUser(r)
	if !ok || a.members == nil {
		return "", nil
	}
	member, err := a.members.IsApprovedMember(r.Context(), projectUID, user.UID)
	if err != nil {
		return "", fmt.Errorf("uploadlinkapi: checking %s's membership of %s: %w", user.UID, projectUID, err)
	}
	if !member {
		return "", nil
	}
	return user.UID, nil
}

// startSession mints a fresh anonymous upload session, sets its cookie on w and
// returns the token's hash. The page's GET normally did this already; an upload
// arriving without the cookie (a client that skipped the page) starts one here.
func (a *API) startSession(w http.ResponseWriter) (string, error) {
	token, err := uploadlink.NewSessionToken()
	if err != nil {
		return "", fmt.Errorf("uploadlinkapi: minting session token: %w", err)
	}
	// A session cookie (no expiry): the photos are claimable for as long as this
	// browser session lasts, which is what "uploaded in this session" means.
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookieName, Value: token, Path: sessionCookiePath,
		HttpOnly: true, Secure: a.secureCookies, SameSite: http.SameSiteStrictMode,
	})
	return uploadlink.HashSecret(token), nil
}

// SessionHash returns the hash of the anonymous upload session token r carries,
// or "" when it carries none (or one of an implausible length).
func SessionHash(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || len(cookie.Value) < 32 || len(cookie.Value) > 128 {
		return ""
	}
	return uploadlink.HashSecret(cookie.Value)
}

// ingestParts walks the multipart stream: the name field sets the uploader's
// name, an ingest.SizeField declares the size of the file part after it, every
// part carrying a filename is ingested and filed, other fields are skipped. It
// returns the per-file results in order, or an error for a malformed stream. A
// file part past maxFilesPerRequest is refused and ends the walk: nothing after
// it is read.
func (a *API) ingestParts(
	r *http.Request, reader *multipart.Reader, link liveTarget, who uploader,
) ([]ingest.FileResult, error) {
	var results []ingest.FileResult
	var declared int64
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return results, nil
		}
		if err != nil {
			return nil, fmt.Errorf("uploadlinkapi: reading multipart part: %w", err)
		}
		if part.FileName() == "" {
			switch part.FormName() {
			case nameField:
				who.name = readName(part)
			case ingest.SizeField:
				declared = ingest.ReadSizeField(part)
			}
			_ = part.Close()
			continue
		}
		if len(results) >= maxFilesPerRequest {
			return append(results,
				refused(part.FileName(), http.StatusRequestEntityTooLarge, "", errTooManyFiles)), nil
		}
		results = append(results, a.ingestOne(r, part, ingest.PartSize(part, declared), link, who))
		declared = 0
		_ = part.Close()
	}
}

// readName reads the uploader's typed name from part, capped and normalised.
func readName(part io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(part, maxNameFieldBytes))
	if err != nil {
		return ""
	}
	return uploadlink.NormalizeUploaderName(string(raw))
}

// ingestOne runs one file through the pipeline and, when it resolved to a
// photo, records it against the link — which files it into the link's targets
// and audits it. Before the pipeline sees a byte it takes the file's slot of the
// link's cap in the database, atomically with checking the link is still live,
// so neither concurrent requests nor a revoke or expiry mid-request let a file
// past the cap or the link's end; a file that is then not recorded gives its
// slot back.
//
// Once the pipeline has the file, the client's disconnect no longer cancels
// anything: the pipeline itself detaches after staging, and the recording runs
// on a context detached from the request too. A browser that hung up after its
// last byte used to leave a photo with no record of the upload — no provenance,
// not filed into the link's album, no audit entry — and a slot that was neither
// used nor given back.
//
// The file is held to the smallest of the project's, the instance's and the
// link's per-file caps; declared is the size it announced (0 for none), which
// lets an oversized file be refused before its body is read.
func (a *API) ingestOne(
	r *http.Request, part *multipart.Part, declared int64, link liveTarget, who uploader,
) ingest.FileResult {
	filename := part.FileName()
	if !imgconvert.IsSupportedFormat(path.Ext(filename)) {
		// ingest.CodeUnsupportedType: the code the pipeline gives a type it refuses by
		// content (an AVIF under any name), so both refusals read the same.
		return refused(filename, http.StatusUnsupportedMediaType, ingest.CodeUnsupportedType, errUnsupportedType)
	}
	if err := a.store.ReserveUpload(r.Context(), link.UID, a.maxUploads, a.now()); err != nil {
		return a.reservationRefused(r, link, filename, err)
	}
	// The photo goes into the link's project, under that project's prefix,
	// whoever is uploading: the public route has no project of its own.
	res := a.ingest.IngestFile(r.Context(), link.project, part, ingest.Request{
		Filename: filename, UploadedBy: who.userUID, DeclaredSize: declared, MaxFileSize: a.maxFileSize,
	})
	if res.Outcome == ingest.OutcomeError || res.PhotoUID == "" {
		a.release(r, link)
		return withoutUsage(res)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), recordTimeout)
	defer cancel()
	if err := a.record(ctx, r, link, who, res); err != nil {
		a.log.ErrorContext(ctx, "uploadlinkapi: filing an uploaded photo",
			slog.String("link_uid", link.UID), slog.String("photo_uid", res.PhotoUID),
			slog.String("error", err.Error()))
		a.release(r, link)
		return refused(filename, http.StatusInternalServerError, "", errNotFiled)
	}
	return res
}

// reservationRefused is the per-file result of a file whose cap slot the store
// refused: 429 for a full link, the dead link's 410 for one revoked, expired or
// deleted since the request began, 500 for a failed reservation.
func (a *API) reservationRefused(r *http.Request, link liveTarget, filename string, err error) ingest.FileResult {
	switch {
	case errors.Is(err, uploadlink.ErrFull):
		return refused(filename, http.StatusTooManyRequests, "", errLinkFull)
	case errors.Is(err, uploadlink.ErrRevoked), errors.Is(err, uploadlink.ErrExpired),
		errors.Is(err, uploadlink.ErrNotFound):
		return refused(filename, http.StatusGone, "", errLinkGone)
	default:
		a.log.ErrorContext(r.Context(), "uploadlinkapi: reserving an upload",
			slog.String("link_uid", link.UID), slog.String("error", err.Error()))
		return refused(filename, http.StatusInternalServerError, "", errNotAccepted)
	}
}

// release gives back the cap slot of a file that was not recorded. It outlives
// the request's context — a client that hung up mid-file must not cost the link
// a slot — and a failure only overcounts, so it is logged, not reported.
func (a *API) release(r *http.Request, link liveTarget) {
	ctx := context.WithoutCancel(r.Context())
	if err := a.store.ReleaseUpload(ctx, link.UID); err != nil {
		a.log.WarnContext(ctx, "uploadlinkapi: releasing an upload slot",
			slog.String("link_uid", link.UID), slog.String("error", err.Error()))
	}
}

// record stores the provenance of res, files its photo into the link's targets
// and audits the upload, then reschedules the photo's sidecar, which now names
// new albums and labels. It runs on ctx — detached from the request by the
// caller — and reads only the request's metadata (who, from where) off r.
func (a *API) record(
	ctx context.Context, r *http.Request, link liveTarget, who uploader, res ingest.FileResult,
) error {
	entry := audit.FromRequest(r, who.userUID).Entry(audit.ActionUploadLinkUpload, "", "",
		map[string]any{"filename": res.Filename})
	entry.ProjectUID = link.ProjectUID
	err := a.store.RecordUpload(ctx, uploadlink.Upload{
		LinkUID: link.UID, PhotoUID: res.PhotoUID, Created: res.Outcome == ingest.OutcomeCreated,
		UploaderName: who.name, UploadedBy: who.userUID, SessionHash: who.sessionHash,
	}, entry)
	if err != nil {
		return fmt.Errorf("uploadlinkapi: recording upload: %w", err)
	}
	a.enqueueSidecar(ctx, res.PhotoUID)
	return nil
}

// enqueueSidecar schedules photoUID's sidecar rewrite, best-effort: a missed one
// is caught by the sidecar backfill.
func (a *API) enqueueSidecar(ctx context.Context, photoUID string) {
	if a.sidecar == nil {
		return
	}
	if err := a.sidecar.EnqueueSidecar(ctx, photoUID); err != nil {
		a.log.WarnContext(ctx, "uploadlinkapi: scheduling sidecar",
			slog.String("photo_uid", photoUID), slog.String("error", err.Error()))
	}
}

// refused builds the per-file error result of a file the link turned away
// before (or after) the pipeline; code is its stable identifier, "" for none.
func refused(filename string, status int, code string, err error) ingest.FileResult {
	return ingest.FileResult{
		Filename: filename, Status: status, Outcome: ingest.OutcomeError, Code: code, Error: err.Error(),
	}
}

// names returns the names of targets, in order.
func names(targets []uploadlink.Target) []string {
	out := make([]string, len(targets))
	for i, t := range targets {
		out[i] = t.Name
	}
	return out
}

// quotaFullBody is the request-level 413 of an upload into a project with no
// room left. Unlike the signed-in upload's it names no sizes: whoever holds a
// link learns that the project is full, not how large its library is.
var quotaFullBody = errorBody{Error: ingest.CodeQuotaExceeded}

// withoutUsage strips the numbers off a quota refusal for the public route, for
// the reason quotaFullBody carries none; any other result is returned as is.
func withoutUsage(res ingest.FileResult) ingest.FileResult {
	if res.Code == ingest.CodeQuotaExceeded {
		res.LimitBytes, res.UsedBytes = 0, nil
		res.Error = ingest.ErrQuotaExceeded.Error()
	}
	return res
}
