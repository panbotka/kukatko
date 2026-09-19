package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"
)

// ErrNoUser is returned when a participant change names no account.
var ErrNoUser = errors.New("a user uid is required")

// Participant mirrors the JSON one person on a task is served as
// (phototask.Participant).
type Participant struct {
	UserUID  string    `json:"user_uid"`
	Name     string    `json:"name"`
	JoinedAt time.Time `json:"joined_at"`
	// AddedBy is empty for somebody who joined by acting on the task, and names
	// whoever asked them otherwise.
	AddedBy     string `json:"added_by"`
	AddedByName string `json:"added_by_name"`
}

// ParticipantList is the envelope the participant endpoints answer with.
type ParticipantList struct {
	Participants []Participant `json:"participants"`
}

// DirectoryUser mirrors one entry of the library's people (auth.DirectoryEntry):
// the name a person is known by, and the uid to address them with.
type DirectoryUser struct {
	UID  string `json:"uid"`
	Name string `json:"name"`
}

// DirectoryList is the envelope the people endpoint answers with.
type DirectoryList struct {
	Users []DirectoryUser `json:"users"`
}

// TaskParticipants returns the people on a task.
func (c *Client) TaskParticipants(ctx context.Context, uid string) (json.RawMessage, error) {
	if err := requireUID("task", uid); err != nil {
		return nil, err
	}
	return c.send(ctx, "GET", taskPath(uid)+"/participants", nil)
}

// AssignTask puts somebody on a task — asking a particular person — and returns
// the participants as they now stand.
func (c *Client) AssignTask(ctx context.Context, uid, userUID string) (json.RawMessage, error) {
	if err := requireUID("task", uid); err != nil {
		return nil, err
	}
	if userUID == "" {
		return nil, ErrNoUser
	}
	body := struct {
		UserUID string `json:"user_uid"`
	}{UserUID: userUID}
	return c.send(ctx, "POST", taskPath(uid)+"/participants", body)
}

// UnassignTask takes somebody off a task and returns the participants as they
// now stand. Somebody who was not on it is not an error.
func (c *Client) UnassignTask(ctx context.Context, uid, userUID string) (json.RawMessage, error) {
	if err := requireUID("task", uid); err != nil {
		return nil, err
	}
	if userUID == "" {
		return nil, ErrNoUser
	}
	return c.send(ctx, "DELETE", taskPath(uid)+"/participants/"+url.PathEscape(userUID), nil)
}

// People returns the library's accounts — uid and name — which is how a uid is
// found for the assign commands.
func (c *Client) People(ctx context.Context) (json.RawMessage, error) {
	return c.send(ctx, "GET", "/users", nil)
}

// DecodeParticipants decodes the participant envelope.
func DecodeParticipants(raw json.RawMessage) (ParticipantList, error) {
	var list ParticipantList
	if err := json.Unmarshal(raw, &list); err != nil {
		return ParticipantList{}, fmt.Errorf("decoding participants: %w", err)
	}
	return list, nil
}

// WriteParticipants renders the people on a task. HOW is the column that carries
// the meaning: "acted" is somebody who did something to the task, a name is
// somebody who was asked by that person.
func WriteParticipants(w io.Writer, list ParticipantList) error {
	if len(list.Participants) == 0 {
		return writeLine(w, "nobody is on this task")
	}
	rows := make([][]string, 0, len(list.Participants))
	for _, p := range list.Participants {
		rows = append(rows, []string{p.UserUID, dash(p.Name), joinedHow(p), formatStamp(p.JoinedAt)})
	}
	return writeTable(w, []string{"UID", "NAME", "HOW", "SINCE"}, rows)
}

// joinedHow renders the one distinction the table exists to show.
func joinedHow(p Participant) string {
	if p.AddedBy == "" {
		return "acted"
	}
	return "asked by " + NamedUID(p.AddedByName, p.AddedBy)
}

// DecodeDirectory decodes the people envelope.
func DecodeDirectory(raw json.RawMessage) (DirectoryList, error) {
	var list DirectoryList
	if err := json.Unmarshal(raw, &list); err != nil {
		return DirectoryList{}, fmt.Errorf("decoding people: %w", err)
	}
	return list, nil
}

// WriteDirectory renders the library's accounts — the uids the assign commands
// take.
func WriteDirectory(w io.Writer, list DirectoryList) error {
	if len(list.Users) == 0 {
		return writeLine(w, "no accounts found")
	}
	rows := make([][]string, 0, len(list.Users))
	for _, u := range list.Users {
		rows = append(rows, []string{u.UID, dash(u.Name)})
	}
	return writeTable(w, []string{"UID", "NAME"}, rows)
}
