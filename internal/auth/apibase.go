package auth

import (
	"context"
	"net/url"
)

// APIBasePath is the path the server mounts the JSON API on (internal/server
// routes it as /api/v1). The project-scoped API is mounted under
// APIBasePath + "/p/{project}"; see APIBase.
const APIBasePath = "/api/v1"

// apiBaseKey is the context key of the API base path WithProject records.
type apiBaseKey struct{}

// ContextWithAPIBase returns ctx carrying base as the request's API base path
// (see APIBase). WithProject is what sets it on a served request; it is
// exported for the tests of the packages that build links on it.
func ContextWithAPIBase(ctx context.Context, base string) context.Context {
	return context.WithValue(ctx, apiBaseKey{}, base)
}

// apiBaseFor returns the API base path of a request whose project route
// parameter is slug: APIBasePath + "/p/<slug>" for a project-prefixed route,
// APIBasePath for one without the parameter (an instance-wide route).
func apiBaseFor(slug string) string {
	if slug == "" {
		return APIBasePath
	}
	return APIBasePath + "/p/" + url.PathEscape(slug)
}

// APIBase returns the path prefix the request came through, without a trailing
// slash: "/api/v1/p/<slug>" when it reached a project-prefixed route,
// "/api/v1" when it came through an instance-wide route — and for a request
// that never passed WithProject (a router assembled without it, as most
// package tests do).
//
// Every URL a handler hands back that points at another project-scoped route
// of this API (a thumbnail, a download, a GeoJSON feature's picture) is built
// on it, so a client that addressed one project keeps addressing that project
// through the links it is given. Instance-wide routes (/users/{uid}/avatar)
// are built on APIBasePath instead.
func APIBase(ctx context.Context) string {
	if base, ok := ctx.Value(apiBaseKey{}).(string); ok && base != "" {
		return base
	}
	return APIBasePath
}
