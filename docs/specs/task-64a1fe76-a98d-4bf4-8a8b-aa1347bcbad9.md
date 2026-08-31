# `kukatko ctl`: faces, people and clusters

Give `ctl` the whole recognition surface, so naming who is on a photo — the most
frequent curation work in the library — no longer needs the web UI.

## Requirements

- `ctl photos faces <uid>` lists a photo's detections: named subjects with their
  marker, and the unassigned ones with their detection score and box.
- `ctl faces assign <photo-uid> <face> <subject-uid>` attaches a detection to a
  person, and the reverse detaches it. It reuses `POST /photos/{uid}/faces/assign`;
  the assignment state machine stays on the server.
- `ctl subjects` gains the write half: create, rename, merge two people into one,
  delete. Merging is the one that matters — duplicate people are the usual mess.
- `ctl clusters` lists the auto-clustered groups of unassigned faces, assigns a whole
  cluster to a person, and removes a face that does not belong in it.
- `ctl faces reject` / `confirm` record the persisted opinions behind
  `/face-rejections` and `/face-confirmations`, so a wrong suggestion stays refused
  instead of coming back on every sweep.
- Every command supports `-o llm` and reports names as well as uids: a uid alone is
  unreadable, and the agent must be able to say who it just named.
- **Merging and deleting a person are irreversible** and need the same explicit
  confirmation flag and `--dry-run` as the destructive photo commands.

## Implementation Notes

- Follow the existing shape: Cobra command in `cmd/kukatko/ctl_*.go`, client and
  renderer in `internal/ctl/*.go`, a `_test.go` next to each.
- The endpoints exist and are exercised by the web UI — this is a CLI layer over
  `internal/peopleapi`, `internal/clusterapi` and `internal/feedbackapi`, not new
  recognition logic. Do not reimplement matching, voting or thresholds.
- Builds on the `-o llm` format introduced by the earlier `ctl` task. If that has not
  landed yet, do not reinvent it — depend on it.