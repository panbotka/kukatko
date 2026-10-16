// Package privacyguard is the `make check` guard that keeps private data out of
// this public repository: every tracked text file — and every file under
// docs/specs/, tracked or not, since a task's spec is synced there before it is
// committed — is scanned line by line against a short list of generic rules
// (committed, holding no private value) and, when the gitignored
// .secrets/privacy-patterns.txt exists, against the private patterns read from
// it. A hit is reported as `path:line: <rule name>` and never with the matched
// text or the private pattern itself, so the gate's own output cannot leak what
// it guards. Deliberately synthetic hits are allow-listed in allow.txt.
package privacyguard

import (
	"bytes"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// Rule is one thing the guard refuses to find in a file.
type Rule struct {
	// Name identifies the rule in a finding. A private rule's name carries only
	// its line in the pattern file, never the pattern.
	Name string
	// Reason says in one line why a hit is private data.
	Reason string
	// Pattern finds the candidates on one line.
	Pattern *regexp.Regexp
	// Hint, when set, is a literal every candidate contains: a file or line
	// without it is skipped before the (much slower) pattern runs.
	Hint string
	// Benign reports a candidate — line[start:end] — that is not private after
	// all (a documentation address, a synthetic domain); the whole line is
	// passed so the candidate's neighbours can decide. nil means every
	// candidate is a hit.
	Benign func(line string, start, end int) bool
	// Skip reports a path the rule does not apply to; nil means every path.
	Skip func(path string) bool
}

// Matches reports whether line holds at least one candidate of the rule that
// is not benign.
func (r Rule) Matches(line string) bool {
	if r.Hint != "" && !strings.Contains(line, r.Hint) {
		return false
	}
	for _, loc := range r.Pattern.FindAllStringIndex(line, -1) {
		if r.Benign == nil || !r.Benign(line, loc[0], loc[1]) {
			return true
		}
	}
	return false
}

// MayMatch reports whether data could hold a candidate at all: it holds the
// hint (when there is one) and the pattern matches somewhere in it.
func (r Rule) MayMatch(data []byte) bool {
	if r.Hint != "" && !bytes.Contains(data, []byte(r.Hint)) {
		return false
	}
	return r.Pattern.Match(data)
}

// Applies reports whether the rule checks the file at path (slash-separated,
// relative to the repository root).
func (r Rule) Applies(path string) bool {
	return r.Skip == nil || !r.Skip(path)
}

// GenericRules returns the committed rules (a copy: callers may append).
func GenericRules() []Rule { return slices.Clone(genericRules) }

// genericRules are the committed rules. Their patterns are written so that
// their own source never matches them (`-{5}`, `op:/{2}`, an escaped dot), which
// keeps this file clean without an allow-list entry.
var genericRules = []Rule{
	{
		Name:   "ssh-target",
		Reason: "an SSH-style user@host target names a real machine and account",
		Pattern: regexp.MustCompile(
			`\b(?:ssh|scp|sftp|rsync)\s.*?[\w.-]+@[\w.-]+|[\w.-]+@[\w.-]+:[~/]`),
		Hint:   "@",
		Benign: sshTargetBenign,
	},
	{
		Name:    "home-path",
		Reason:  "an absolute home path names a real account on a real machine",
		Pattern: regexp.MustCompile(`/home/[\w.-]+/`),
		Hint:    "/home/",
	},
	{
		Name:    "mount-path",
		Reason:  "an absolute mount path names private storage",
		Pattern: regexp.MustCompile(`/mnt/[\w.-]+`),
		Hint:    "/mnt/",
	},
	{
		Name:    "secret-reference",
		Reason:  "a password-manager secret reference names a real vault and item",
		Pattern: regexp.MustCompile(`\bop:/{2}`),
		Hint:    "op:/",
	},
	{
		Name:    "private-key",
		Reason:  "a private-key header means key material is in the file",
		Pattern: regexp.MustCompile(`-{5}BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-{5}`),
		Hint:    "PRIVATE KEY",
	},
	{
		Name: "ipv4-address",
		Reason: "an IPv4 address outside the documentation ranges points at a real host " +
			"(test tables are synthetic)",
		// A trailing `/len` is captured so a range is recognised.
		Pattern: regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b(?:/\d{1,2}\b)?`),
		Hint:    ".",
		Benign:  ipv4Benign,
		Skip:    isTestFile,
	},
	{
		Name:    "r2-account-endpoint",
		Reason:  "a Cloudflare R2 endpoint carries the real account id",
		Pattern: regexp.MustCompile(`\b[0-9a-f]{16,}\.(?:[a-z]+\.)?r2\.cloudflarestorage\.com`),
		Hint:    "cloudflarestorage",
	},
	{
		Name:    "email-address",
		Reason:  "an e-mail address outside the reserved example domains names a real person",
		Pattern: regexp.MustCompile(`[\w.+-]+@[\w-]+(?:\.[\w-]+)*\.[A-Za-z]{2,}\b`),
		Hint:    "@",
		Benign:  emailBenign,
	},
	{
		Name:   "phone-number",
		Reason: "a phone number reaches a real person",
		Pattern: regexp.MustCompile(
			`\+\d{1,3}[ .-]?\d{3}[ .-]?\d{3}[ .-]?\d{3,4}\b|\b[2-7]\d{2} \d{3} \d{3}\b`),
		Benign: phoneBenign,
	},
}

// docRanges are the IPv4 ranges RFC 5737 reserves for documentation, plus the
// loopback, unspecified and limited-broadcast addresses and the link-local
// cloud metadata address every SSRF discussion names, none of which is a
// particular machine.
var docRanges = []netip.Prefix{
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("0.0.0.0/32"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("169.254.169.254/32"),
}

// isTestFile reports a Go or frontend test file, whose address tables are
// synthetic by construction.
func isTestFile(path string) bool {
	return strings.HasSuffix(path, "_test.go") || strings.Contains(path, ".test.")
}

// isLetter reports an ASCII letter.
func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// isDigit reports an ASCII digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// ipv4Benign reports a dotted quad that is no real host: a version token
// (`Chrome/129.0.0.0`), a network range wider than one address (`/len` below
// 32), one in a documentation range, or no address at all (an octet above 255,
// a leading zero).
func ipv4Benign(line string, start, end int) bool {
	if start >= 2 && line[start-1] == '/' && isLetter(line[start-2]) {
		return true
	}
	quad, length, ranged := strings.Cut(line[start:end], "/")
	if ranged && length != "32" {
		return true
	}
	addr, err := netip.ParseAddr(quad)
	if err != nil {
		return true
	}
	for _, p := range docRanges {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// reservedDomains are the names RFC 2606 reserves for examples and tests; a
// domain equal to one or under one names nobody.
var reservedDomains = []string{
	"example.org", "example.com", "example.net", "example", "test", "invalid", "localhost",
}

// syntheticDomain reports a domain reserved for examples (reservedDomains).
func syntheticDomain(domain string) bool {
	domain = strings.ToLower(domain)
	for _, d := range reservedDomains {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

// phoneBenign reports a national-form candidate that is part of a longer run
// of numbers (`[80 200 400 250]`): a digit group sits right before or after it.
func phoneBenign(line string, start, end int) bool {
	if line[start] == '+' {
		return false
	}
	before := start >= 2 && line[start-1] == ' ' && isDigit(line[start-2])
	after := end+1 < len(line) && line[end] == ' ' && isDigit(line[end+1])
	return before || after
}

// emailBenign reports an address that names nobody: one in a reserved example
// domain, GitHub's noreply addresses, a no-reply mailbox (the commit trailers'
// co-author address is one), or GitHub's public SSH clone user.
func emailBenign(line string, start, end int) bool {
	local, domain, ok := strings.Cut(line[start:end], "@")
	if !ok {
		return true
	}
	domain = strings.ToLower(domain)
	if syntheticDomain(domain) || domain == "users.noreply.github.com" {
		return true
	}
	local = strings.ToLower(local)
	return local == "noreply" || local == "no-reply" || (local == "git" && domain == "github.com")
}

// sshUserHost extracts the user@host pair from an ssh-target candidate.
var sshUserHost = regexp.MustCompile(`[\w.-]+@([\w.-]+)`)

// sshTargetBenign reports a target that is no private machine: an example
// domain, or GitHub's public `git@github.com:` clone address.
func sshTargetBenign(line string, start, end int) bool {
	m := sshUserHost.FindAllStringSubmatch(line[start:end], -1)
	if m == nil {
		return true
	}
	host := strings.TrimSuffix(m[len(m)-1][1], ".")
	return syntheticDomain(host) || strings.EqualFold(host, "github.com")
}
