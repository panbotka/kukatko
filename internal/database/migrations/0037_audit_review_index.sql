-- 0037_audit_review_index: back the review leaderboard aggregation. The
-- leaderboard (internal/review LeaderboardStore, GET /review/leaderboard) groups
-- the review game's decisive answers — audit_log rows tagged details.via =
-- 'review' — per acting user and time window. A partial index on that marker,
-- keyed by actor and creation time, keeps the aggregation's scan cheap while
-- staying tiny (only review rows are indexed) and leaving the audit_log's other
-- access paths untouched.
--
-- The 'review' literal here must match viaReview in internal/review/answer.go
-- and the predicate in internal/review/leaderboard.go so the planner can use it.
-- details ->> 'via' is IMMUTABLE, so it is a valid partial-index predicate.

CREATE INDEX idx_audit_log_review_actor
    ON audit_log (actor_uid, created_at)
    WHERE details ->> 'via' = 'review';
