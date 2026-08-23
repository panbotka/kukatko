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

// AccessLog returns a middleware that installs the request fields bag and logs
// one structured line per request through logger after the handler returns. The
// line carries the request id, method, path, matched route pattern, status,
// duration, response bytes, remote IP, and the authenticated user when known.
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
