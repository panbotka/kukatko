package project

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateSlug verifies the slug format and the reserved words.
func TestValidateSlug(t *testing.T) {
	t.Parallel()

	type slugCase struct {
		name    string
		slug    string
		wantErr bool
	}
	fixed := []slugCase{
		{name: "bootstrap slug", slug: BootstrapSlug},
		{name: "letters digits and hyphens", slug: "rodina-2026"},
		{name: "leading digit", slug: "1st"},
		{name: "forty characters", slug: strings.Repeat("a", 40)},
		{name: "empty", slug: "", wantErr: true},
		{name: "uppercase", slug: "Veselice", wantErr: true},
		{name: "leading hyphen", slug: "-v", wantErr: true},
		{name: "forty-one characters", slug: strings.Repeat("a", 41), wantErr: true},
		{name: "underscore", slug: "a_b", wantErr: true},
		{name: "slash", slug: "a/b", wantErr: true},
		{name: "space", slug: "a b", wantErr: true},
		{name: "diacritics", slug: "kukátko", wantErr: true},
	}
	tests := make([]slugCase, 0, len(fixed)+len(reservedSlugs))
	tests = append(tests, fixed...)
	for _, reserved := range reservedSlugs {
		tests = append(tests, slugCase{name: "reserved " + reserved, slug: reserved, wantErr: true})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSlug(tt.slug)
			if tt.wantErr && !errors.Is(err, ErrInvalidSlug) {
				t.Errorf("ValidateSlug(%q) = %v, want ErrInvalidSlug", tt.slug, err)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("ValidateSlug(%q) = %v, want nil", tt.slug, err)
			}
		})
	}
}

// TestValidateSlug_reservedList pins the reserved words the routing depends on.
func TestValidateSlug_reservedList(t *testing.T) {
	t.Parallel()

	for _, slug := range []string{
		"p", "api", "u", "invite", "system", "login", "register", "projects", "account", "healthz", "metrics",
	} {
		if err := ValidateSlug(slug); !errors.Is(err, ErrInvalidSlug) {
			t.Errorf("ValidateSlug(%q) = %v, want the reserved word refused", slug, err)
		}
	}
}

// TestValidateStoragePrefix verifies the prefix rule mirrors the table's CHECK.
func TestValidateStoragePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		prefix  string
		wantErr bool
	}{
		{prefix: ""},
		{prefix: "projects/pjabc/"},
		{prefix: "x/"},
		{prefix: "projects/pjabc", wantErr: true},
		{prefix: "/projects/pjabc/", wantErr: true},
		{prefix: "/", wantErr: true},
	}
	for _, tt := range tests {
		err := ValidateStoragePrefix(tt.prefix)
		if tt.wantErr != (err != nil) {
			t.Errorf("ValidateStoragePrefix(%q) = %v, wantErr %v", tt.prefix, err, tt.wantErr)
		}
		if err != nil && !errors.Is(err, ErrInvalidStoragePrefix) {
			t.Errorf("ValidateStoragePrefix(%q) = %v, want ErrInvalidStoragePrefix", tt.prefix, err)
		}
	}
}

// TestStoragePrefixFor verifies a created project's prefix is valid and not root.
func TestStoragePrefixFor(t *testing.T) {
	t.Parallel()

	got := StoragePrefixFor("pjabc")
	if got != "projects/pjabc/" {
		t.Errorf("StoragePrefixFor = %q, want projects/pjabc/", got)
	}
	if err := ValidateStoragePrefix(got); err != nil {
		t.Errorf("StoragePrefixFor result %q fails validation: %v", got, err)
	}
}

// TestNewUID verifies the uid shape: "pj", 26 characters, base32 alphabet, and
// two calls never collide.
func TestNewUID(t *testing.T) {
	t.Parallel()

	first, err := newUID()
	if err != nil {
		t.Fatalf("newUID: %v", err)
	}
	second, err := newUID()
	if err != nil {
		t.Fatalf("newUID: %v", err)
	}
	for _, uid := range []string{first, second, BootstrapUID} {
		if len(uid) != 26 || !strings.HasPrefix(uid, uidPrefix) {
			t.Errorf("uid %q: want 26 characters starting with %q", uid, uidPrefix)
		}
		for _, r := range uid[len(uidPrefix):] {
			if !strings.ContainsRune(uidAlphabet, r) {
				t.Errorf("uid %q contains %q outside the base32 alphabet", uid, r)
			}
		}
	}
	if first == second {
		t.Errorf("two uids collided: %q", first)
	}
}

// TestNormalizeName verifies trimming and the empty-name refusal.
func TestNormalizeName(t *testing.T) {
	t.Parallel()

	if got, err := normalizeName("  Veselice "); err != nil || got != "Veselice" {
		t.Errorf("normalizeName = %q, %v; want Veselice, nil", got, err)
	}
	for _, name := range []string{"", "   ", "\t\n"} {
		if _, err := normalizeName(name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("normalizeName(%q) = %v, want ErrInvalidName", name, err)
		}
	}
}

// TestPatchLimit verifies set, clear, keep and the two refusals.
func TestPatchLimit(t *testing.T) {
	t.Parallel()

	current, set, zero := int64(10), int64(20), int64(0)
	tests := []struct {
		name    string
		set     *int64
		remove  bool
		want    *int64
		wantErr bool
	}{
		{name: "keep", want: &current},
		{name: "set", set: &set, want: &set},
		{name: "remove", remove: true},
		{name: "set and remove", set: &set, remove: true, wantErr: true},
		{name: "zero", set: &zero, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := patchLimit(&current, tt.set, tt.remove)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidLimit) {
					t.Fatalf("patchLimit = %v, want ErrInvalidLimit", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("patchLimit: %v", err)
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Errorf("patchLimit = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestApplyPatch verifies a patch touches only the fields it sets and refuses
// an empty name.
func TestApplyPatch(t *testing.T) {
	t.Parallel()

	limit := int64(5)
	current := Project{UID: "pjx", Slug: "x", Name: "X", Description: "d", Active: true, MaxFileBytes: &limit}
	name, inactive := " Y ", false
	next, err := applyPatch(current, UpdatePatch{Name: &name, Active: &inactive, ClearMaxFileBytes: true})
	if err != nil {
		t.Fatalf("applyPatch: %v", err)
	}
	if next.Name != "Y" || next.Active || next.MaxFileBytes != nil || next.Description != "d" || next.Slug != "x" {
		t.Errorf("applyPatch = %+v", next)
	}
	if changes := diff(current, next); changes.Len() != 3 {
		t.Errorf("diff recorded %d changes, want 3 (name, active, max_file_bytes)", changes.Len())
	}
	if diff(current, current).Len() != 0 {
		t.Error("diff of an unchanged project is not empty")
	}

	blank := " "
	if _, err := applyPatch(current, UpdatePatch{Name: &blank}); !errors.Is(err, ErrInvalidName) {
		t.Errorf("applyPatch with a blank name = %v, want ErrInvalidName", err)
	}
}

// TestAppPath verifies a project page's address is the page's own path under
// the project's root.
func TestAppPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		slug, path, want string
	}{
		{slug: "v", path: "/n/nt1", want: "/p/v/n/nt1"},
		{slug: "rodina", path: "/tasks/tk1#comment-cm1", want: "/p/rodina/tasks/tk1#comment-cm1"},
		{slug: "v", path: "/photos/ph1?panel=info", want: "/p/v/photos/ph1?panel=info"},
	}
	for _, tt := range tests {
		if got := AppPath(tt.slug, tt.path); got != tt.want {
			t.Errorf("AppPath(%q, %q) = %q, want %q", tt.slug, tt.path, got, tt.want)
		}
	}
}

// TestActive verifies only active projects survive, in their order.
func TestActive(t *testing.T) {
	t.Parallel()

	list := []Project{{UID: "a", Active: true}, {UID: "b"}, {UID: "c", Active: true}}
	got := Active(list)
	if len(got) != 2 || got[0].UID != "a" || got[1].UID != "c" {
		t.Fatalf("Active = %+v, want a and c", got)
	}
	if got := Active(nil); len(got) != 0 {
		t.Fatalf("Active(nil) = %+v, want none", got)
	}
}

// TestForKey verifies a key resolves to the project whose prefix it is under,
// the longest prefix winning, and a bare key to the root-layout project.
func TestForKey(t *testing.T) {
	t.Parallel()

	root := Project{UID: "root"}
	a := Project{UID: "a", StoragePrefix: "projects/a/"}
	ab := Project{UID: "ab", StoragePrefix: "projects/a/b/"}
	tests := []struct {
		name   string
		list   []Project
		key    string
		want   string
		wantOK bool
	}{
		{name: "bare key is the root layout", list: []Project{root, a}, key: "2024/05/x.jpg", want: "root", wantOK: true},
		{name: "prefixed key", list: []Project{root, a}, key: "projects/a/2024/05/x.jpg", want: "a", wantOK: true},
		{name: "longest prefix wins", list: []Project{a, ab, root}, key: "projects/a/b/2024/x.jpg", want: "ab", wantOK: true},
		{name: "no root and no prefix match", list: []Project{a}, key: "2024/05/x.jpg"},
		{name: "empty list", key: "2024/05/x.jpg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ForKey(tt.list, tt.key)
			if ok != tt.wantOK {
				t.Fatalf("ForKey(%q) ok = %v, want %v", tt.key, ok, tt.wantOK)
			}
			if ok && got.UID != tt.want {
				t.Fatalf("ForKey(%q) = %s, want %s", tt.key, got.UID, tt.want)
			}
		})
	}
}
