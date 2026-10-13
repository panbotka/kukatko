package main

import (
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/panbotka/kukatko/internal/project"
	"github.com/panbotka/kukatko/internal/server"
)

// fakeProjectAPI returns a projectAPI whose every group registers one GET route
// named after its field, plus the field names in declaration order.
func fakeProjectAPI(t *testing.T) (*projectAPI, []string) {
	t.Helper()
	api := &projectAPI{}
	value := reflect.ValueOf(api).Elem()
	names := make([]string, 0, value.NumField())
	for i := range value.NumField() {
		name := value.Type().Field(i).Name
		names = append(names, name)
		register := func(r chi.Router) {
			r.Get("/"+name, func(http.ResponseWriter, *http.Request) {})
		}
		// The fields are unexported, so they are set through an addressable
		// copy of the field's own type rather than reflect.Value.Set.
		reflect.NewAt(value.Field(i).Type(), value.Field(i).Addr().UnsafePointer()).Elem().
			Set(reflect.ValueOf(register))
	}
	return api, names
}

// TestMountProjectAPI_mountsEveryGroup proves mountProjectAPI registers every
// field of projectAPI: a group added to the struct but forgotten in the
// function would never be served.
func TestMountProjectAPI_mountsEveryGroup(t *testing.T) {
	t.Parallel()
	api, names := fakeProjectAPI(t)
	r := chi.NewRouter()
	mountProjectAPI(r, api)

	var got []string
	walk := func(_ string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, strings.TrimPrefix(route, "/"))
		return nil
	}
	if err := chi.Walk(r, walk); err != nil {
		t.Fatalf("walking the routes: %v", err)
	}
	for _, name := range names {
		if !slices.Contains(got, name) {
			t.Errorf("group %q is not mounted by mountProjectAPI", name)
		}
	}
}

// TestProjectRoutes_mountsTheAPIOnlyUnderTheProject proves every project-scoped
// group is served under /api/v1/p/{project} and nowhere else: there is no
// unprefixed copy of a project route, so a client that forgets the project
// gets the API's 404 instead of somebody's library.
func TestProjectRoutes_mountsTheAPIOnlyUnderTheProject(t *testing.T) {
	t.Parallel()
	api, names := fakeProjectAPI(t)
	srv := server.New("", projectRoutes(project.NewStore(nil), api))
	routes, ok := srv.Handler().(chi.Routes)
	if !ok {
		t.Fatalf("server handler %T is not a chi router", srv.Handler())
	}

	got := map[string]bool{}
	walk := func(_ string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[route] = true
		return nil
	}
	if err := chi.Walk(routes, walk); err != nil {
		t.Fatalf("walking the routes: %v", err)
	}
	for _, name := range names {
		if !got["/api/v1/p/{project}/"+name] {
			t.Errorf("/api/v1/p/{project}/%s is not mounted", name)
		}
		if got["/api/v1/"+name] {
			t.Errorf("/api/v1/%s is mounted without the project prefix", name)
		}
	}
}
