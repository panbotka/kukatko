package mailer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestValidateAddress covers the recipient guard: real addresses pass, empty and
// malformed ones are ErrInvalidAddress, and anything in the reserved .invalid
// domain is ErrPlaceholderAddress.
func TestValidateAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		addr    string
		wantErr error
	}{
		{name: "plain address", addr: "jan@example.com", wantErr: nil},
		{name: "address with display name", addr: "Jan Novák <jan@example.com>", wantErr: nil},
		{name: "surrounding whitespace tolerated", addr: "  jan@example.com  ", wantErr: nil},
		{name: "empty", addr: "", wantErr: ErrInvalidAddress},
		{name: "whitespace only", addr: "   ", wantErr: ErrInvalidAddress},
		{name: "no domain", addr: "jan", wantErr: ErrInvalidAddress},
		{name: "two addresses", addr: "jan@example.com, eva@example.com", wantErr: ErrInvalidAddress},
		{name: "invalid tld", addr: "jan@invalid", wantErr: ErrPlaceholderAddress},
		{name: "invalid subdomain", addr: "jan@kukatko.invalid", wantErr: ErrPlaceholderAddress},
		{name: "invalid tld uppercase", addr: "jan@KUKATKO.INVALID", wantErr: ErrPlaceholderAddress},
		{name: "invalid inside display name", addr: "Jan <jan@x.invalid>", wantErr: ErrPlaceholderAddress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAddress(tt.addr)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateAddress(%q) = %v, want %v", tt.addr, err, tt.wantErr)
			}
		})
	}
}

// TestValidateAddress_errorNamesTheAddress verifies the refusal is actionable:
// the offending address appears in the message.
func TestValidateAddress_errorNamesTheAddress(t *testing.T) {
	t.Parallel()

	err := ValidateAddress("jan@kukatko.invalid")
	if err == nil || !strings.Contains(err.Error(), "jan@kukatko.invalid") {
		t.Fatalf("ValidateAddress error = %v, want it to name the address", err)
	}
}

// TestParseRecipient verifies a "Name <addr>" recipient is reduced to the bare
// address that belongs in the SMTP RCPT command.
func TestParseRecipient(t *testing.T) {
	t.Parallel()

	got, err := parseRecipient("Jan Novák <jan@example.com>")
	if err != nil {
		t.Fatalf("parseRecipient returned error: %v", err)
	}
	if got != "jan@example.com" {
		t.Errorf("parseRecipient = %q, want %q", got, "jan@example.com")
	}
}

// TestIsPlaceholder covers the .invalid check on its own, including the shapes
// that must NOT count as placeholders.
func TestIsPlaceholder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		addr string
		want bool
	}{
		{addr: "jan@invalid", want: true},
		{addr: "jan@example.invalid", want: true},
		{addr: "jan@example.INVALID", want: true},
		{addr: "jan@example.invalid.", want: true},
		{addr: "jan@example.com", want: false},
		{addr: "jan@invalid.example.com", want: false},
		{addr: "jan@notinvalid", want: false},
		{addr: "invalid", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			t.Parallel()
			if got := IsPlaceholder(tt.addr); got != tt.want {
				t.Errorf("IsPlaceholder(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

// TestNoop_Send verifies the disabled-mail sender accepts everything, including
// a placeholder address: with mail off there is nothing to refuse.
func TestNoop_Send(t *testing.T) {
	t.Parallel()

	for _, to := range []string{"jan@example.com", "jan@kukatko.invalid", ""} {
		if err := (Noop{}).Send(context.Background(), Message{To: to}); err != nil {
			t.Errorf("Noop.Send(%q) returned error: %v", to, err)
		}
	}
}

// TestFake_records verifies the in-memory sender records what it was handed, in
// order, and that Sent hands back a copy rather than its own slice.
func TestFake_records(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	if _, ok := fake.Last(); ok {
		t.Fatal("Last on an empty Fake reported a message")
	}
	first := Message{To: "jan@example.com", Subject: "První", Body: "tělo\n"}
	second := Message{To: "eva@example.com", Subject: "Druhá", Body: "tělo\n"}
	for _, msg := range []Message{first, second} {
		if err := fake.Send(context.Background(), msg); err != nil {
			t.Fatalf("Fake.Send returned error: %v", err)
		}
	}

	sent := fake.Sent()
	if len(sent) != 2 || sent[0] != first || sent[1] != second {
		t.Fatalf("Sent = %v, want [%v %v]", sent, first, second)
	}
	sent[0] = Message{}
	if again := fake.Sent(); again[0] != first {
		t.Errorf("Sent returned the internal slice: mutating it changed %v", again[0])
	}

	last, ok := fake.Last()
	if !ok || last != second {
		t.Errorf("Last = (%v, %v), want (%v, true)", last, ok, second)
	}

	fake.Reset()
	if got := fake.Sent(); len(got) != 0 {
		t.Errorf("Sent after Reset = %v, want empty", got)
	}
}

// TestFake_guards verifies the fake applies the same recipient guard as the real
// sender and records nothing when a send fails.
func TestFake_guards(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	err := fake.Send(context.Background(), Message{To: "jan@kukatko.invalid", Subject: "Ne"})
	if !errors.Is(err, ErrPlaceholderAddress) {
		t.Fatalf("Send to a placeholder = %v, want ErrPlaceholderAddress", err)
	}

	fake.FailWith(ErrSendFailed)
	if err := fake.Send(context.Background(), Message{To: "jan@example.com"}); !errors.Is(err, ErrSendFailed) {
		t.Fatalf("Send after FailWith = %v, want ErrSendFailed", err)
	}
	if got := fake.Sent(); len(got) != 0 {
		t.Fatalf("Sent = %v, want nothing recorded", got)
	}

	fake.FailWith(nil)
	if err := fake.Send(context.Background(), Message{To: "jan@example.com"}); err != nil {
		t.Fatalf("Send after clearing the failure returned error: %v", err)
	}
	if got := fake.Sent(); len(got) != 1 {
		t.Fatalf("Sent = %v, want one message", got)
	}
}
