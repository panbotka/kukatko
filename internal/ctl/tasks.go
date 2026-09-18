package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrEmptyTaskTitle is returned when a task is opened without a question.
	ErrEmptyTaskTitle = errors.New("task title is required")
	// ErrNoTaskEdits is returned when an update names no field to change.
	ErrNoTaskEdits = errors.New("nothing to update: give at least one field")
	// ErrNoTaskPhotos is returned when a membership change names no photographs.
	ErrNoTaskPhotos = errors.New("at least one photo uid is required")
)

// titleWidthTask bounds the question in a table row. A question is a sentence;
// the full text is one `tasks show` away.
const titleWidthTask = 44

// Task mirrors the JSON a task is served as (phototask.Task): the question, whose
// move it is, and the two counts that say whether anything has happened.
type Task struct {
	UID           string     `json:"uid"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	State         string     `json:"state"`
	Resolution    string     `json:"resolution"`
	Query         string     `json:"query"`
	CreatedBy     string     `json:"created_by"`
	CreatedByName string     `json:"created_by_name"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	StateAt       time.Time  `json:"state_at"`
	ClosedAt      *time.Time `json:"closed_at"`
	ClosedBy      string     `json:"closed_by"`
	ClosedByName  string     `json:"closed_by_name"`
	PhotoCount    int        `json:"photo_count"`
	CoverPhotoUID string     `json:"cover_photo_uid"`
	CommentCount  int        `json:"comment_count"`
	LastCommentAt *time.Time `json:"last_comment_at"`
	// HasNewAnswer is the signal an agent polls for: somebody has written in the
	// thread since the state last moved.
	HasNewAnswer bool `json:"has_new_answer"`
}

// TaskPage is one page of a task listing with the total before paging.
type TaskPage struct {
	Tasks  []Task `json:"tasks"`
	Total  int    `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// TaskMembership is the answer to a membership change: how many rows actually
// changed, and the task as it now stands.
type TaskMembership struct {
	Changed int  `json:"changed"`
	Task    Task `json:"task"`
}

// TaskListOptions narrows a listing. The zero value lists everything, open tasks
// first.
type TaskListOptions struct {
	// States restricts the listing to the named states.
	States []string
	// Open is the shorthand for the three states a live task can be in.
	Open bool
	// Answered restricts the listing to tasks somebody has replied to since the
	// state last moved — the work waiting to be written into the library.
	Answered bool
	// Search matches a substring of the question or its context.
	Search string
	// PhotoUID restricts the listing to tasks that photograph is part of.
	PhotoUID string
	Limit    int
	Offset   int
}

// query renders the options as the listing's query parameters, omitting every
// zero value so the server applies its own defaults.
func (o TaskListOptions) query() (url.Values, error) {
	if o.Limit < 0 || o.Offset < 0 {
		return nil, ErrInvalidPaging
	}
	q := url.Values{}
	if len(o.States) > 0 {
		q.Set("state", strings.Join(o.States, ","))
	}
	if o.Open {
		q.Set("open", "true")
	}
	if o.Answered {
		q.Set("answered", "true")
	}
	setNonEmpty(q, "q", o.Search)
	setNonEmpty(q, "photo", o.PhotoUID)
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Offset > 0 {
		q.Set("offset", strconv.Itoa(o.Offset))
	}
	return q, nil
}

// TaskInput is what a task is opened with.
type TaskInput struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Query string `json:"query,omitempty"`
	State string `json:"state,omitempty"`
	// PhotoUIDs is the frozen group the question is about.
	PhotoUIDs []string `json:"photo_uids,omitempty"`
}

// TaskUpdate is a partial change. A nil field is left alone; a pointer to an
// empty string clears the value, which is how a resolution or a remembered query
// is removed.
type TaskUpdate struct {
	Title      *string `json:"title,omitempty"`
	Body       *string `json:"body,omitempty"`
	Query      *string `json:"query,omitempty"`
	State      *string `json:"state,omitempty"`
	Resolution *string `json:"resolution,omitempty"`
}

// empty reports whether the update names no field at all.
func (u TaskUpdate) empty() bool {
	return u.Title == nil && u.Body == nil && u.Query == nil && u.State == nil && u.Resolution == nil
}

// tasksPath is the collection endpoint.
const tasksPath = "/tasks"

// taskPath returns the endpoint of one task, escaping the uid.
func taskPath(uid string) string {
	return tasksPath + "/" + url.PathEscape(uid)
}

// ListTasks returns one page of tasks matching opts, as the raw JSON envelope.
func (c *Client) ListTasks(ctx context.Context, opts TaskListOptions) (json.RawMessage, error) {
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	return c.get(ctx, tasksPath, q)
}

// GetTask returns one task by UID.
func (c *Client) GetTask(ctx context.Context, uid string) (json.RawMessage, error) {
	if err := requireUID("task", uid); err != nil {
		return nil, err
	}
	return c.get(ctx, taskPath(uid), nil)
}

// CreateTask opens a task and returns it as stored. The title is required here as
// well as server-side, so a typo costs no round trip.
func (c *Client) CreateTask(ctx context.Context, in TaskInput) (json.RawMessage, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, ErrEmptyTaskTitle
	}
	return c.send(ctx, "POST", tasksPath, in)
}

// UpdateTask folds a partial change onto a task and returns it as it now stands.
// An update naming no field is refused without contacting the server.
func (c *Client) UpdateTask(ctx context.Context, uid string, upd TaskUpdate) (json.RawMessage, error) {
	if err := requireUID("task", uid); err != nil {
		return nil, err
	}
	if upd.empty() {
		return nil, ErrNoTaskEdits
	}
	return c.send(ctx, "PATCH", taskPath(uid), upd)
}

// DeleteTask removes a task, its membership and its thread.
func (c *Client) DeleteTask(ctx context.Context, uid string) error {
	if err := requireUID("task", uid); err != nil {
		return err
	}
	_, err := c.send(ctx, "DELETE", taskPath(uid), nil)
	return err
}

// AddTaskPhotos adds photographs to a task's frozen group.
func (c *Client) AddTaskPhotos(
	ctx context.Context, uid string, photoUIDs []string,
) (json.RawMessage, error) {
	return c.changeTaskPhotos(ctx, "POST", uid, photoUIDs)
}

// RemoveTaskPhotos drops photographs from a task's frozen group.
func (c *Client) RemoveTaskPhotos(
	ctx context.Context, uid string, photoUIDs []string,
) (json.RawMessage, error) {
	return c.changeTaskPhotos(ctx, "DELETE", uid, photoUIDs)
}

// changeTaskPhotos is the shared half of the two membership calls: the same body,
// the same path, a different verb.
func (c *Client) changeTaskPhotos(
	ctx context.Context, method, uid string, photoUIDs []string,
) (json.RawMessage, error) {
	if err := requireUID("task", uid); err != nil {
		return nil, err
	}
	if len(photoUIDs) == 0 {
		return nil, ErrNoTaskPhotos
	}
	body := struct {
		PhotoUIDs []string `json:"photo_uids"`
	}{PhotoUIDs: photoUIDs}
	return c.send(ctx, method, taskPath(uid)+"/photos", body)
}

// DecodeTaskPage decodes a task listing envelope.
func DecodeTaskPage(raw json.RawMessage) (TaskPage, error) {
	var page TaskPage
	if err := json.Unmarshal(raw, &page); err != nil {
		return TaskPage{}, fmt.Errorf("decoding task list: %w", err)
	}
	return page, nil
}

// DecodeTask decodes one task.
func DecodeTask(raw json.RawMessage) (Task, error) {
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return Task{}, fmt.Errorf("decoding task: %w", err)
	}
	return task, nil
}

// DecodeTaskMembership decodes the answer to a membership change.
func DecodeTaskMembership(raw json.RawMessage) (TaskMembership, error) {
	var result TaskMembership
	if err := json.Unmarshal(raw, &result); err != nil {
		return TaskMembership{}, fmt.Errorf("decoding task membership: %w", err)
	}
	return result, nil
}

// WriteTaskPage renders a listing as a table with the paging summary beneath it.
// The NEW column is the reason an agent reads this at all: it marks the tasks
// somebody has answered since the state last moved.
func WriteTaskPage(w io.Writer, page TaskPage) error {
	if len(page.Tasks) == 0 {
		return writeLine(w, "no tasks found")
	}
	rows := make([][]string, 0, len(page.Tasks))
	for _, task := range page.Tasks {
		rows = append(rows, []string{
			task.UID,
			elide(dash(task.Title), titleWidthTask),
			task.State,
			strconv.Itoa(task.PhotoCount),
			strconv.Itoa(task.CommentCount),
			newAnswerMark(task.HasNewAnswer),
		})
	}
	if err := writeTable(w, []string{"UID", "QUESTION", "STATE", "PHOTOS", "COMMENTS", "NEW"}, rows); err != nil {
		return err
	}
	return writeLine(w, "\n"+taskPageSummary(page))
}

// newAnswerMark renders the "somebody has replied" flag as a mark a person reads
// at a glance and a script greps for.
func newAnswerMark(has bool) string {
	if has {
		return "yes"
	}
	return "-"
}

// taskPageSummary is the line under a listing: how much of the total this page
// is, and where the next one starts.
func taskPageSummary(page TaskPage) string {
	summary := fmt.Sprintf("%d of %d %s · offset %d",
		len(page.Tasks), page.Total, plural(page.Total, "task", "tasks"), page.Offset)
	if next := page.Offset + len(page.Tasks); next < page.Total {
		summary += fmt.Sprintf(" · next offset %d", next)
	}
	return summary
}

// WriteTask renders one task in full: the question, the state of play, and the
// text a person needs to act on it.
func WriteTask(w io.Writer, task Task) error {
	rows := [][2]string{
		{"UID", task.UID},
		{"Question", dash(task.Title)},
		{"State", task.State},
		{"Photos", strconv.Itoa(task.PhotoCount)},
		{"Comments", strconv.Itoa(task.CommentCount)},
		{"New answer", newAnswerMark(task.HasNewAnswer)},
		{"Opened by", NamedUID(task.CreatedByName, task.CreatedBy)},
		{"Opened", formatStamp(task.CreatedAt)},
		{"State since", formatStamp(task.StateAt)},
		{"Last comment", formatTime(task.LastCommentAt)},
		{"Query", dash(task.Query)},
	}
	if task.ClosedAt != nil {
		rows = append(rows,
			[2]string{"Closed", formatTime(task.ClosedAt)},
			[2]string{"Closed by", NamedUID(task.ClosedByName, task.ClosedBy)},
			[2]string{"Resolution", dash(task.Resolution)})
	}
	if err := writeKeyValues(w, rows); err != nil {
		return err
	}
	if task.Body == "" {
		return nil
	}
	return writeLine(w, "\n"+task.Body)
}

// WriteTaskMembership renders the answer to a membership change: what moved, and
// where the task now stands.
func WriteTaskMembership(w io.Writer, result TaskMembership) error {
	line := fmt.Sprintf("%d %s changed · %d in the task",
		result.Changed, plural(result.Changed, "photo", "photos"), result.Task.PhotoCount)
	return writeLine(w, line)
}
