package notifystanding

import "sync/atomic"

// Stage is where a notification was dropped: when it was produced, or when
// its queued push or e-mail ran.
type Stage string

// The stages a drop is counted under.
const (
	// StageDeliver is internal/notifydeliver refusing to record and schedule a
	// notification for an account out of standing.
	StageDeliver Stage = "deliver"
	// StagePush is the `push_send` handler refusing a queued push.
	StagePush Stage = "push"
	// StageMail is the `mail_send` handler refusing a queued notification mail.
	StageMail Stage = "mail"
)

// Reason is why a notification was dropped.
type Reason string

// The reasons a drop is counted under.
const (
	// ReasonStanding: the account is no longer in standing in the project — not
	// an approved member, disabled, or the project was deactivated.
	ReasonStanding Reason = "standing"
	// ReasonDeviceMoved: the browser subscription a push was queued for now
	// belongs to another account (a shared browser subscribed again by
	// somebody else).
	ReasonDeviceMoved Reason = "device_moved"
	// ReasonUnaddressed: a queued push names no account — queued by a release
	// before pushes carried one — so whom it is for cannot be checked.
	ReasonUnaddressed Reason = "unaddressed"
)

// Drop is the count of one stage and reason.
type Drop struct {
	// Stage is where the notifications were dropped.
	Stage Stage
	// Reason is why.
	Reason Reason
	// Count is how many since the process started.
	Count uint64
}

// dropCells are every (stage, reason) pair the package counts, in a fixed
// order, so Drops reports each of them — at zero too — and a dashboard can tell
// "nothing was dropped" from "nothing is counted".
var dropCells = []struct {
	stage  Stage
	reason Reason
}{
	{StageDeliver, ReasonStanding},
	{StagePush, ReasonStanding},
	{StagePush, ReasonDeviceMoved},
	{StagePush, ReasonUnaddressed},
	{StageMail, ReasonStanding},
}

// dropCounts holds one counter per dropCells entry, at the same index.
var dropCounts = make([]atomic.Uint64, len(dropCells))

// CountDrop records one notification dropped at stage for reason. The counters
// are process-wide, like the runtime statistics the metrics registry reads at
// scrape time: the drops happen in producers and queue handlers built in many
// places, and one counter per process is what the metric means. A pair the
// package does not define is ignored.
func CountDrop(stage Stage, reason Reason) {
	for i, cell := range dropCells {
		if cell.stage == stage && cell.reason == reason {
			dropCounts[i].Add(1)
			return
		}
	}
}

// Drops returns the count of every (stage, reason) pair, zeros included, in a
// fixed order.
func Drops() []Drop {
	out := make([]Drop, len(dropCells))
	for i, cell := range dropCells {
		out[i] = Drop{Stage: cell.stage, Reason: cell.reason, Count: dropCounts[i].Load()}
	}
	return out
}
