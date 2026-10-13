package obs

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/panbotka/kukatko/internal/clientip"
)

// metricsPath is skipped by AccessLog so scrape traffic does not flood the log.
const metricsPath = "/metrics"

// secretPathSegments are the path fragments after which the next segment is a
// credential rather than an identifier — today only the one-time password-reset
// link, whose whole token travels in the URL because that is what a link is.
// Writing it into the access log would leave a working credential lying in a
// file that outlives it, which is the same reason the database stores only a
// hash of it.
var secretPathSegments = []string{"/auth/password-reset/"}

// redactedSegment stands in for a path segment that was a secret.
const redactedSegment = "REDACTED"

// logPath returns the request path as it may be written to the log: unchanged,
// unless it carries a credential (see secretPathSegments), in which case
// everything from that point on is replaced. The prefix is kept, so a log line
// still says which endpoint was called and a 429 or a 404 on it is still
// diagnosable.
func logPath(path string) string {
	for _, segment := range secretPathSegments {
		if i := strings.Index(path, segment); i >= 0 {
			return path[:i+len(segment)] + redactedSegment
		}
	}
	return path
}

// fields is the mutable, request-scoped bag the AccessLog middleware installs in
// the request context. Downstream middleware (notably auth) stamps the
// authenticated user onto it once known; AccessLog reads it back after the
// handler returns. A pointer is shared through the context so a write deep in
// the chain is visible to the top-level logger, which context values alone
// cannot do.
type fields struct {
	mu      sync.Mutex
	userUID string
	// actorRole is the role the caller acted with when it is worth calling out
	// — "maintainer" for a system administrator entering a project it is not a
	// member of — or "".
	actorRole string
	// projectUID and projectSlug name the project the request was admitted
	// into, once auth has resolved it; both empty for an instance-level
	// request.
	projectUID  string
	projectSlug string
}

// fieldsContextKey is the unexported context key under which the request fields
// pointer is stored.
type fieldsContextKey struct{}

// withFields returns a child context carrying a fresh request fields bag.
func withFields(ctx context.Context) context.Context {
	return context.WithValue(ctx, fieldsContextKey{}, &fields{})
}

// fieldsFrom returns the request fields bag attached to ctx, or nil when none
// was installed (for example outside the AccessLog middleware).
func fieldsFrom(ctx context.Context) *fields {
	f, _ := ctx.Value(fieldsContextKey{}).(*fields)
	return f
}

// SetUser records the authenticated user's UID on the request's fields bag so
// the access-log line can attribute the request. It is safe to call from any
// middleware or handler and is a no-op when uid is empty or no fields bag is
// present (for example in tests that exercise a handler directly).
func SetUser(ctx context.Context, uid string) {
	if uid == "" {
		return
	}
	f := fieldsFrom(ctx)
	if f == nil {
		return
	}
	f.mu.Lock()
	f.userUID = uid
	f.mu.Unlock()
}

// userOf returns the authenticated user UID stamped on ctx, or "" when none.
func userOf(ctx context.Context) string {
	f := fieldsFrom(ctx)
	if f == nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.userUID
}

// SetActorRole records role as the role the request's caller acted with, so the
// access-log line carries it as actor_role and an audit entry built from the
// request (audit.FromRequest) records it too. auth stamps "maintainer" when a
// system administrator enters a project on the strength of that role alone. It
// is a no-op when role is empty or no fields bag is present.
func SetActorRole(ctx context.Context, role string) {
	if role == "" {
		return
	}
	f := fieldsFrom(ctx)
	if f == nil {
		return
	}
	f.mu.Lock()
	f.actorRole = role
	f.mu.Unlock()
}

// ActorRole returns the role SetActorRole stamped on ctx, or "" when none.
func ActorRole(ctx context.Context) string {
	f := fieldsFrom(ctx)
	if f == nil {
		return ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.actorRole
}

// SetProject records the project the request was admitted into — its uid and
// its slug — so the access-log line carries the slug as project and an audit
// entry built from the request (audit.FromRequest) records the uid. auth stamps
// it once a caller has entered the project; a later call (the inner
// /p/{project} resolution) replaces an earlier one. It is a no-op when uid is
// empty or no fields bag is present.
func SetProject(ctx context.Context, uid, slug string) {
	if uid == "" {
		return
	}
	f := fieldsFrom(ctx)
	if f == nil {
		return
	}
	f.mu.Lock()
	f.projectUID, f.projectSlug = uid, slug
	f.mu.Unlock()
}

// projectContextKey is the context key under which WithProject records the
// request's project uid.
type projectContextKey struct{}

// WithProject records the project a request was admitted into on both halves:
// the fields bag, for the access-log line (SetProject), and a child context it
// returns, so ProjectUID answers for a handler even where no AccessLog
// installed a bag (a test server, a handler driven directly). auth calls it
// when it admits a caller into a project.
func WithProject(ctx context.Context, uid, slug string) context.Context {
	if uid == "" {
		return ctx
	}
	SetProject(ctx, uid, slug)
	return context.WithValue(ctx, projectContextKey{}, uid)
}

// ProjectUID returns the uid of the project the request was admitted into —
// WithProject's, else SetProject's — or "" when it was admitted into none.
func ProjectUID(ctx context.Context) string {
	if uid, ok := ctx.Value(projectContextKey{}).(string); ok {
		return uid
	}
	uid, _ := projectOf(ctx)
	return uid
}

// projectOf returns the uid and slug SetProject stamped on ctx, both "" when
// none.
func projectOf(ctx context.Context) (string, string) {
	f := fieldsFrom(ctx)
	if f == nil {
		return "", ""
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.projectUID, f.projectSlug
}

// AccessLog returns a middleware that installs the request fields bag and logs
// one structured line per request through logger after the handler returns. The
// line carries the request id, method, path, matched route pattern, status,
// duration, response bytes, remote IP, the authenticated user when known and
// the project's slug (never its name) when the request was admitted into one.
// Requests to /metrics are served without logging. A nil logger falls back to
// the slog default.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == metricsPath {
				next.ServeHTTP(w, r)
				return
			}
			ctx := withFields(r.Context())
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r.WithContext(ctx))
			logRequest(ctx, logger, r, ww, time.Since(start))
		})
	}
}

// logRequest emits the structured access-log line for a finished request,
// selecting the level by status class (5xx → error, 4xx → warn, else info).
func logRequest(
	ctx context.Context, logger *slog.Logger, r *http.Request,
	ww middleware.WrapResponseWriter, elapsed time.Duration,
) {
	attrs := []slog.Attr{
		slog.String("request_id", middleware.GetReqID(ctx)),
		slog.String("method", r.Method),
		slog.String("path", logPath(r.URL.Path)),
		slog.String("route", routePattern(ctx)),
		slog.Int("status", ww.Status()),
		slog.Int("bytes", ww.BytesWritten()),
		slog.Float64("duration_ms", float64(elapsed.Microseconds())/1000.0),
		// The resolved client address (internal/clientip), not the raw RemoteAddr:
		// the same value the rate limiters and the audit trail key on, so a log
		// line and an audit row about one request name the same machine.
		slog.String("remote_ip", clientip.FromRequest(r)),
	}
	if uid := userOf(ctx); uid != "" {
		attrs = append(attrs, slog.String("user", uid))
	}
	if role := ActorRole(ctx); role != "" {
		attrs = append(attrs, slog.String("actor_role", role))
	}
	if _, slug := projectOf(ctx); slug != "" {
		attrs = append(attrs, slog.String("project", slug))
	}
	logger.LogAttrs(ctx, levelForStatus(ww.Status()), "http request", attrs...)
}

// routePattern returns the chi route pattern matched for the request, or
// "unmatched" when routing did not match a registered route (for example SPA
// fallback paths). It keeps the route field bounded, mirroring the metric label.
func routePattern(ctx context.Context) string {
	if rc := chi.RouteContext(ctx); rc != nil {
		if pattern := rc.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	return "unmatched"
}

// levelForStatus maps an HTTP status code to the slog level its access-log line
// is emitted at: 5xx is an error, 4xx a warning, everything else informational.
func levelForStatus(status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case status >= http.StatusBadRequest:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}
