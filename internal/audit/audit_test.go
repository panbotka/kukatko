package audit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// fakeExec is a stub Execer that records the last call's arguments and returns a
// configurable error, so Write can be tested without a database.
type fakeExec struct {
	sql  string
	args []any
	err  error
}

// Exec records the statement and arguments, returning the stub's configured
// error.
func (f *fakeExec) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.sql = sql
	f.args = args
	return pgconn.CommandTag{}, f.err
}

// TestNullable verifies empty strings become SQL NULL and non-empty strings pass
// through unchanged.
func TestNullable(t *testing.T) {
	t.Parallel()

	if got := nullable(""); got != nil {
		t.Errorf("nullable(\"\") = %v, want nil", got)
	}
	if got := nullable("su123"); got != "su123" {
		t.Errorf("nullable(\"su123\") = %v, want su123", got)
	}
}

// TestDetailsOrEmpty verifies a nil map becomes a non-nil empty map and a
// populated map is returned unchanged.
func TestDetailsOrEmpty(t *testing.T) {
	t.Parallel()

	if got := detailsOrEmpty(nil); got == nil || len(got) != 0 {
		t.Errorf("detailsOrEmpty(nil) = %v, want empty non-nil map", got)
	}
	in := map[string]any{"k": "v"}
	if got := detailsOrEmpty(in); len(got) != 1 || got["k"] != "v" {
		t.Errorf("detailsOrEmpty(%v) = %v, want unchanged", in, got)
	}
}

// TestWrite_argumentsAndDetails verifies Write passes the entry fields in column
// order, nullifies empty actor/target UIDs, and JSON-encodes the details.
func TestWrite_argumentsAndDetails(t *testing.T) {
	t.Parallel()

	exec := &fakeExec{}
	entry := Entry{
		Action:     ActionPhotosBulk,
		TargetType: "photos",
		Details:    map[string]any{"updated": 3},
	}
	if err := Write(context.Background(), exec, entry); err != nil {
		t.Fatalf("Write() error = %v, want nil", err)
	}
	if len(exec.args) != 7 {
		t.Fatalf("Write() passed %d args, want 7", len(exec.args))
	}
	if exec.args[0] != nil {
		t.Errorf("actor_uid arg = %v, want nil", exec.args[0])
	}
	if exec.args[1] != ActionPhotosBulk {
		t.Errorf("action arg = %v, want %s", exec.args[1], ActionPhotosBulk)
	}
	if exec.args[3] != nil {
		t.Errorf("target_uid arg = %v, want nil", exec.args[3])
	}
	if exec.args[5] != nil {
		t.Errorf("ip arg = %v, want nil", exec.args[5])
	}
	if exec.args[6] != nil {
		t.Errorf("user_agent arg = %v, want nil", exec.args[6])
	}
	raw, ok := exec.args[4].([]byte)
	if !ok {
		t.Fatalf("details arg type = %T, want []byte", exec.args[4])
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("details not valid JSON: %v", err)
	}
	if decoded["updated"] != float64(3) {
		t.Errorf("details[updated] = %v, want 3", decoded["updated"])
	}
}

// TestWrite_execError verifies a failing executor surfaces a wrapped error.
func TestWrite_execError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	exec := &fakeExec{err: sentinel}
	err := Write(context.Background(), exec, Entry{Action: ActionPhotosBulk})
	if !errors.Is(err, sentinel) {
		t.Errorf("Write() error = %v, want wrapped %v", err, sentinel)
	}
}
