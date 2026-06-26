package worker

import (
	"context"
	"sort"
	"testing"

	"github.com/panbotka/kukatko/internal/jobs"
)

// TestRegistry_registerAndLookup verifies a registered handler is found and an
// unregistered type is reported absent.
func TestRegistry_registerAndLookup(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	called := false
	reg.Register("demo", func(context.Context, jobs.Job) error {
		called = true
		return nil
	})

	handler, ok := reg.Handler("demo")
	if !ok {
		t.Fatal("Handler(demo) ok = false, want true")
	}
	if err := handler(context.Background(), jobs.Job{}); err != nil {
		t.Fatalf("handler returned %v, want nil", err)
	}
	if !called {
		t.Error("handler was not invoked")
	}

	if _, ok := reg.Handler("missing"); ok {
		t.Error("Handler(missing) ok = true, want false")
	}
}

// TestRegistry_types verifies Types returns exactly the registered job types.
func TestRegistry_types(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	reg.Register("a", NoopHandler)
	reg.Register("b", NoopHandler)

	got := reg.Types()
	sort.Strings(got)
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("Types() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Types()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRegistry_registerPanics verifies the programming-error guards: empty type,
// nil handler, and duplicate registration each panic.
func TestRegistry_registerPanics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		register func(*Registry)
	}{
		{"empty type", func(r *Registry) { r.Register("", NoopHandler) }},
		{"nil handler", func(r *Registry) { r.Register("x", nil) }},
		{"duplicate", func(r *Registry) {
			r.Register("x", NoopHandler)
			r.Register("x", NoopHandler)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Error("Register did not panic")
				}
			}()
			tt.register(NewRegistry())
		})
	}
}

// TestRegisterBuiltins verifies the noop handler is wired up by RegisterBuiltins.
func TestRegisterBuiltins(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	RegisterBuiltins(reg)
	handler, ok := reg.Handler(TypeNoop)
	if !ok {
		t.Fatal("noop handler not registered")
	}
	if err := handler(context.Background(), jobs.Job{}); err != nil {
		t.Errorf("NoopHandler returned %v, want nil", err)
	}
}
