package auth

import (
	"context"
	"testing"
)

// TestAPIBase_defaultsToTheUnprefixedAPI pins the answer for a request that
// never passed WithProject: the bare API base, where the instance-wide routes
// live.
func TestAPIBase_defaultsToTheUnprefixedAPI(t *testing.T) {
	t.Parallel()
	if got := APIBase(context.Background()); got != APIBasePath {
		t.Errorf("APIBase(no project) = %q, want %q", got, APIBasePath)
	}
	if got := APIBase(ContextWithAPIBase(context.Background(), "")); got != APIBasePath {
		t.Errorf("APIBase(empty) = %q, want %q", got, APIBasePath)
	}
}

// TestAPIBaseFor_followsTheRouteParameter pins both forms: the project prefix
// when the route carried a slug, the bare base when it did not.
func TestAPIBaseFor_followsTheRouteParameter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		slug string
		want string
	}{
		{name: "instance routes", slug: "", want: "/api/v1"},
		{name: "bootstrap project", slug: "v", want: "/api/v1/p/v"},
		{name: "another project", slug: "rodina-novaku", want: "/api/v1/p/rodina-novaku"},
		{name: "escaped", slug: "a/b", want: "/api/v1/p/a%2Fb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := APIBase(ContextWithAPIBase(context.Background(), apiBaseFor(tt.slug)))
			if got != tt.want {
				t.Errorf("APIBase(slug %q) = %q, want %q", tt.slug, got, tt.want)
			}
		})
	}
}
