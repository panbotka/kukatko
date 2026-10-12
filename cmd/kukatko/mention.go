package main

import (
	"github.com/panbotka/kukatko/internal/comments"
	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/mentionnotify"
)

// buildCommentStore assembles the comments store both threads — a photo's and
// a task's — write through, with the "somebody mentioned you in a comment"
// notification hooked in: the store's mention observer delivers the
// comment_mention kind to every account a comment freshly names, inside the
// comment's transaction, through internal/notifydeliver. Every channel is off
// until the account opts in, so wiring it costs nothing for anybody who never
// chose.
func buildCommentStore(cfg *config.Config, db *database.DB) *comments.Store {
	notifier := mentionnotify.New(mentionnotify.Config{Deliver: buildDeliverer(cfg, db)})
	return comments.NewStore(db.Pool()).WithMentionObserver(notifier)
}
