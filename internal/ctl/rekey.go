package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// RekeyAction is one transition of a project's storage move ctl can ask for.
type RekeyAction string

// The transitions of a storage move, each the last segment of its route.
const (
	// RekeyPause pauses the running move.
	RekeyPause RekeyAction = "pause"
	// RekeyResume resumes a paused move.
	RekeyResume RekeyAction = "resume"
	// RekeyRetry starts another pass of a failed move.
	RekeyRetry RekeyAction = "retry"
)

// shownRekeyErrors is how many of a run's most recent errors the table lists.
const shownRekeyErrors = 5

// RekeyError is one error a storage move met.
type RekeyError struct {
	Key     string    `json:"key"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// RekeyRun is one storage move of a project as GET /projects/{uid}/rekey
// serves it (rekey.Run).
type RekeyRun struct {
	UID          string       `json:"uid"`
	ProjectUID   string       `json:"project_uid"`
	State        string       `json:"state"`
	TargetPrefix string       `json:"target_prefix"`
	TotalPhotos  int64        `json:"total_photos"`
	TotalBytes   int64        `json:"total_bytes"`
	DonePhotos   int64        `json:"done_photos"`
	DoneBytes    int64        `json:"done_bytes"`
	Errors       []RekeyError `json:"errors"`
	ErrorCount   int          `json:"error_count"`
	StartedAt    time.Time    `json:"started_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	FinishedAt   *time.Time   `json:"finished_at"`
}

// rekeyPath renders the path of a project's storage move.
func rekeyPath(projectUID string) string {
	return projectPath(projectUID) + "/rekey"
}

// RekeyStatus fetches the project's latest storage move as raw JSON. A project
// that was never moved answers 404 (*StatusError).
func (c *Client) RekeyStatus(ctx context.Context, projectUID string) (json.RawMessage, error) {
	if err := requireUID("project", projectUID); err != nil {
		return nil, err
	}
	return c.get(ctx, rekeyPath(projectUID), nil)
}

// StartRekey starts the project's storage move and returns the new run as raw
// JSON. The server answers 409 already_running or already_prefixed for a move
// it refuses.
func (c *Client) StartRekey(ctx context.Context, projectUID string) (json.RawMessage, error) {
	if err := requireUID("project", projectUID); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, rekeyPath(projectUID), nil)
}

// ChangeRekey applies action to the project's storage move and returns the run
// as it now stands, as raw JSON. A run not in the state the action starts from
// answers 409 invalid_state.
func (c *Client) ChangeRekey(ctx context.Context, projectUID string, action RekeyAction) (json.RawMessage, error) {
	if err := requireUID("project", projectUID); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, rekeyPath(projectUID)+"/"+string(action), nil)
}

// DecodeRekeyRun decodes one storage move.
func DecodeRekeyRun(raw json.RawMessage) (RekeyRun, error) {
	var run RekeyRun
	if err := json.Unmarshal(raw, &run); err != nil {
		return RekeyRun{}, fmt.Errorf("decoding the storage move: %w", err)
	}
	return run, nil
}

// WriteRekeyRun renders one storage move as an aligned key/value table: its
// state, its progress and its most recent errors.
func WriteRekeyRun(w io.Writer, run RekeyRun) error {
	finished := "-"
	if run.FinishedAt != nil {
		finished = formatStamp(*run.FinishedAt)
	}
	shown := run.Errors[max(len(run.Errors)-shownRekeyErrors, 0):]
	rows := make([][2]string, 0, 9+len(shown))
	rows = append(rows, [][2]string{
		{"RUN", run.UID},
		{"STATE", run.State},
		{"TARGET PREFIX", run.TargetPrefix},
		{"PHOTOS", strconv.FormatInt(run.DonePhotos, 10) + " of " + strconv.FormatInt(run.TotalPhotos, 10) +
			" (" + rekeyPercent(run.DonePhotos, run.TotalPhotos) + ")"},
		{"ORIGINALS", formatSize(run.DoneBytes) + " of " + formatSize(run.TotalBytes)},
		{"ERRORS", strconv.Itoa(run.ErrorCount)},
		{"STARTED", formatStamp(run.StartedAt)},
		{"UPDATED", formatStamp(run.UpdatedAt)},
		{"FINISHED", finished},
	}...)
	for _, failure := range shown {
		rows = append(rows, [2]string{"ERROR", dash(failure.Key) + ": " + failure.Message})
	}
	return writeKeyValues(w, rows)
}

// rekeyPercent renders done of total as a percentage, 100% for nothing to do.
func rekeyPercent(done, total int64) string {
	if total <= 0 {
		return "100%"
	}
	return fmt.Sprintf("%.1f%%", float64(done)*100/float64(total))
}
