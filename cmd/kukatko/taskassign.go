package main

import (
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/taskassignnotify"
)

// buildTaskAssignNotifier assembles the "somebody put you on a task"
// notification: the task store's assign observer that delivers the
// task_assigned kind to the person asked, inside the assignment's transaction,
// through internal/notifydeliver. Every channel is off until the account opts
// in, so wiring it costs nothing for anybody who never chose.
func buildTaskAssignNotifier(cfg *config.Config, db *database.DB) *taskassignnotify.Notifier {
	return taskassignnotify.New(taskassignnotify.Config{Deliver: buildDeliverer(cfg, db)})
}
