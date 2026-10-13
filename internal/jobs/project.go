package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrProjectRequired indicates an enqueue of a library-wide job type whose
// payload names no project (no non-empty project_uid). A library-wide job works
// on one project's library; one naming none would either be read as some
// default project's — never what a caller without one meant — or, worse, act on
// every project at once. The enqueue is refused and nothing is inserted.
var ErrProjectRequired = errors.New("jobs: a library-wide job must name its project (project_uid)")

// ProjectPayload is the payload field every library-wide job names its project
// under: project_uid. A payload that carries more (the task digest's person,
// the nameless repair's subject) keeps its own struct with the same field; this
// one is the whole payload of the jobs that need nothing else — a
// notification_purge, a family_export. The library-wide dedup indexes key on
// (type, payload ->> 'project_uid'), and ProjectUIDOf reads it back.
type ProjectPayload struct {
	// ProjectUID is the uid of the project the job works on. Required for a
	// library-wide type (see IsLibraryWide).
	ProjectUID string `json:"project_uid"`
}

// libraryWideTypes are the job types that work on one project's library as a
// whole rather than on one photo, one account or the instance: each must carry
// ProjectPayload's project_uid, and Enqueue refuses one that does not.
var libraryWideTypes = map[string]bool{
	TypeFaceCluster:       true,
	TypeFamilyExport:      true,
	TypeNamelessDetach:    true,
	TypeNamelessRestore:   true,
	TypeTaskDigest:        true,
	TypeNotificationPurge: true,
	TypeStorageRekey:      true,
}

// IsLibraryWide reports whether jobType is a library-wide job type — one whose
// payload must name its project (project_uid). The per-photo types read their
// project from the photo row instead, and the instance-wide ones have none; for
// both it returns false. See the package documentation for the three categories.
func IsLibraryWide(jobType string) bool {
	return libraryWideTypes[jobType]
}

// ProjectUIDOf returns the project a job's payload names (its project_uid), or
// "" when the payload names none — a per-photo or instance-wide job, a payload
// that is not a JSON object, or a library-wide job queued before its type
// carried a project. It is the one way a handler learns the project of a
// library-wide job: the project always travels in the payload, never in a
// global, so two projects' jobs never cross.
func ProjectUIDOf(job Job) string {
	return payloadProjectUID(job.Payload)
}

// payloadProjectUID decodes the project_uid of raw, or "" when raw is empty, is
// not a JSON object, or has no string project_uid.
func payloadProjectUID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var p ProjectPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return ""
	}
	return p.ProjectUID
}

// checkProject refuses a library-wide jobType whose payload names no project,
// wrapping ErrProjectRequired with the type. Every other type passes.
func checkProject(jobType string, payload json.RawMessage) error {
	if IsLibraryWide(jobType) && payloadProjectUID(payload) == "" {
		return fmt.Errorf("%w: %s", ErrProjectRequired, jobType)
	}
	return nil
}

// projectJSON builds the canonical {"project_uid": uid} payload of a
// library-wide job that needs nothing but its project.
func projectJSON(projectUID string) (json.RawMessage, error) {
	raw, err := json.Marshal(ProjectPayload{ProjectUID: projectUID})
	if err != nil {
		return nil, fmt.Errorf("jobs: marshaling project payload: %w", err)
	}
	return raw, nil
}
