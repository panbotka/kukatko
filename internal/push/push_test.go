package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestNotificationEncode_validation covers the guards Encode applies before a
// notification is ever encrypted.
func TestNotificationEncode_validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      Notification
		wantErr error
	}{
		{name: "title only", in: Notification{Title: "Ahoj"}},
		{name: "full", in: Notification{Title: "T", Body: "B", URL: "/tasks/x?y=1", Kind: "task", Tag: "t1"}},
		{name: "root path", in: Notification{Title: "T", URL: "/"}},
		{name: "empty title", in: Notification{Body: "B"}, wantErr: ErrInvalidNotification},
		{name: "blank title", in: Notification{Title: "  "}, wantErr: ErrInvalidNotification},
		{name: "absolute url", in: Notification{Title: "T", URL: "https://evil.example/"}, wantErr: ErrInvalidNotification},
		{name: "scheme relative", in: Notification{Title: "T", URL: "//evil.example/"}, wantErr: ErrInvalidNotification},
		{name: "backslash relative", in: Notification{Title: "T", URL: `/\evil.example`}, wantErr: ErrInvalidNotification},
		{name: "relative path", in: Notification{Title: "T", URL: "tasks"}, wantErr: ErrInvalidNotification},
		{name: "javascript", in: Notification{Title: "T", URL: "javascript:alert(1)"}, wantErr: ErrInvalidNotification},
		{
			name:    "oversized body",
			in:      Notification{Title: "T", Body: strings.Repeat("x", MaxPayloadSize)},
			wantErr: ErrPayloadTooLarge,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload, err := tt.in.Encode()
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Encode() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && len(payload) == 0 {
				t.Fatal("Encode() returned an empty payload without an error")
			}
		})
	}
}

// TestNotificationEncode_fieldNames pins the JSON contract with the service
// worker: renaming a field silently breaks every notification on screen.
func TestNotificationEncode_fieldNames(t *testing.T) {
	t.Parallel()

	payload, err := Notification{Title: "T", Body: "B", URL: "/u", Kind: "k", Tag: "g"}.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal %s: %v", payload, err)
	}
	want := map[string]string{"title": "T", "body": "B", "url": "/u", "kind": "k", "tag": "g"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("payload = %v, want %v", got, want)
	}
}

// TestNotificationEncode_sizeBoundary proves the limit is inclusive: a payload
// of exactly MaxPayloadSize bytes encodes, one byte more does not.
func TestNotificationEncode_sizeBoundary(t *testing.T) {
	t.Parallel()

	overhead, err := Notification{Title: "T"}.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	fill := MaxPayloadSize - len(overhead)
	exact := Notification{Title: "T", Body: strings.Repeat("a", fill)}
	payload, err := exact.Encode()
	if err != nil {
		t.Fatalf("Encode at the limit: %v", err)
	}
	if len(payload) != MaxPayloadSize {
		t.Fatalf("payload is %d bytes, want exactly %d", len(payload), MaxPayloadSize)
	}
	over := Notification{Title: "T", Body: strings.Repeat("a", fill+1)}
	if _, err := over.Encode(); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Encode one byte over: error = %v, want ErrPayloadTooLarge", err)
	}
}

// TestNoop_send proves the disabled sender accepts anything, even input the
// real sender would refuse, and never fails.
func TestNoop_send(t *testing.T) {
	t.Parallel()

	if err := (Noop{}).Send(context.Background(), Subscription{}, Notification{}); err != nil {
		t.Fatalf("Noop.Send() = %v, want nil", err)
	}
}

// TestRetryable covers which errors a caller should retry.
func TestRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "retryable", err: ErrRetryable, want: true},
		{name: "wrapped retryable", err: fmt.Errorf("x: %w", ErrRetryable), want: true},
		{name: "status 503", err: &StatusError{StatusCode: 503, kind: ErrRetryable}, want: true},
		{name: "gone", err: ErrGone, want: false},
		{name: "too large", err: ErrPayloadTooLarge, want: false},
		{name: "rejected", err: ErrRejected, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Retryable(tt.err); got != tt.want {
				t.Fatalf("Retryable(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
