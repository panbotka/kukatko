package privacyguard

import (
	"strings"
	"testing"
)

// The hits below are assembled from pieces (`"ssh someone" + "@somehost"`) so
// this file's own source never matches the rules it tests.

// ruleByName returns the generic rule called name.
func ruleByName(t *testing.T, name string) Rule {
	t.Helper()
	for _, r := range GenericRules() {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no generic rule %q", name)
	return Rule{}
}

func TestGenericRules_haveReasons(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for _, r := range GenericRules() {
		if r.Name == "" || r.Reason == "" || r.Pattern == nil {
			t.Errorf("rule %+v lacks a name, reason or pattern", r)
		}
		if seen[r.Name] {
			t.Errorf("rule name %q repeats", r.Name)
		}
		seen[r.Name] = true
	}
}

func TestGenericRules_hitsAndMisses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rule string
		path string
		line string
		want bool
	}{
		{rule: "ssh-target", line: "run `ssh someone" + "@somehost` first", want: true},
		{rule: "ssh-target", line: "rsync -a ./ deploy" + "@build-1:/srv/x", want: true},
		{rule: "ssh-target", line: "scp x deploy" + "@build-1:/tmp/x", want: true},
		{rule: "ssh-target", line: "ssh someone" + "@host.example.org", want: false},
		{rule: "ssh-target", line: "git clone git" + "@github.com:org/repo.git", want: false},
		{rule: "ssh-target", line: "ssh <user>@<host>", want: false},
		{rule: "home-path", line: "cd /home" + "/someone/projects", want: true},
		{rule: "home-path", line: "cd /home/<user>/projects", want: false},
		{rule: "mount-path", line: "ls /mnt" + "/share", want: true},
		{rule: "mount-path", line: "ls /srv/share", want: false},
		{rule: "secret-reference", line: "op read op:" + "//vault/item/field", want: true},
		{rule: "secret-reference", line: "read it from the password manager", want: false},
		{rule: "private-key", line: "-----BEGIN " + "OPENSSH PRIVATE KEY-----", want: true},
		{rule: "private-key", line: "-----BEGIN PUBLIC KEY-----", want: false},
		{rule: "ipv4-address", path: "docs/x.md", line: "connect to 10.1" + ".2.3:22", want: true},
		{rule: "ipv4-address", path: "docs/x.md", line: "connect to 192.0.2.10:22", want: false},
		{rule: "ipv4-address", path: "docs/x.md", line: "binds 127.0.0.1 or 0.0.0.0", want: false},
		{rule: "ipv4-address", path: "docs/x.md", line: "trust 10.8.0.0/24", want: false},
		{rule: "ipv4-address", path: "docs/x.md", line: "host 10.1" + ".2.3/32", want: true},
		{rule: "ipv4-address", path: "docs/x.md", line: "Chrome/129.0.0.0 Safari", want: false},
		{rule: "ipv4-address", path: "docs/x.md", line: "version 999.1.2.3", want: false},
		{rule: "ipv4-address", path: "internal/x/x_test.go", line: "ip := \"10.1" + ".2.3\"", want: false},
		{rule: "ipv4-address", path: "web/src/x.test.ts", line: "ip: '10.1" + ".2.3'", want: false},
		{rule: "r2-account-endpoint", line: strings.Repeat("ab", 16) + ".r2.cloud" + "flarestorage.com",
			want: true},
		{rule: "r2-account-endpoint", line: "<account>.r2.cloudflarestorage.com", want: false},
		{rule: "email-address", line: "mail someone" + "@mailhost.cz", want: true},
		{rule: "email-address", line: "mail jan@example.org or eva@example.com", want: false},
		{rule: "email-address", line: "seed anna@example.test and x@nowhere.invalid", want: false},
		{rule: "email-address", line: "Co-Authored-By: Bot <noreply@anthropic.com>", want: false},
		{rule: "email-address", line: "1+someone@users.noreply.github.com", want: false},
		{rule: "phone-number", line: "call +420 6" + "01 234 567", want: true},
		{rule: "phone-number", line: "call 6" + "01 234 567", want: true},
		{rule: "phone-number", line: "want [80 200 400 250]", want: false},
		{rule: "phone-number", line: "want [250 400 100 160]", want: false},
		{rule: "phone-number", line: "about 1 200 000 photos", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.rule+"/"+tt.line, func(t *testing.T) {
			t.Parallel()
			r := ruleByName(t, tt.rule)
			path := tt.path
			if path == "" {
				path = "docs/x.md"
			}
			got := r.Applies(path) && r.Matches(tt.line)
			if got != tt.want {
				t.Errorf("%s on %q in %s = %v, want %v", tt.rule, tt.line, path, got, tt.want)
			}
		})
	}
}

// TestGenericRules_sourceIsClean keeps the rules from flagging their own
// definitions: rules.go must pass its own rules without an allow-list entry.
func TestGenericRules_sourceIsClean(t *testing.T) {
	t.Parallel()
	findings, err := Check(".", []string{"rules.go", "rules_test.go"}, GenericRules())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}
