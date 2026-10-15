-- 0120_audit_log_project_backfill: file the pre-tenancy library actions under
-- the bootstrap project.
--
-- Migration 0102 added audit_log.project_id without a backfill, so every entry
-- written before the upgrade kept a NULL project. A project's own trail, the
-- review leaderboard, the day streaks and a project admin's person filter all
-- read `project_id = <project>`, and the instance-only listing reads
-- `project_id IS NULL`: the bootstrap project `v` lost its whole history and the
-- maintainer saw every library action of it labelled as an instance one.
--
-- Before the upgrade one library existed, so every library action of that time
-- belongs to `v`. The rows touched are exactly those that:
--   * have no project yet;
--   * predate the bootstrap project's row (migration 0100 creates it during the
--     upgrade, so a fresh install has no such row and a later run touches none of
--     the entries written since — an entry the release writes names its project
--     itself);
--   * carry a project-scoped action. The list below is audit.ProjectScopedActions
--     (internal/audit, the authority behind audit.ProjectScoped), written out
--     because SQL cannot call Go; TestAuditProjectBackfill_listIsProjectScoped
--     keeps the two equal. An instance action (a sign-in, an account change, a
--     project's lifecycle, the retention purge, the wipe) and an unknown label
--     stay NULL, as ProjectScoped is false for both.
--
-- Idempotent: a second run finds no row with a NULL project among those.
-- Rollback: docs/migrations-rollback/0120_down.sql.
UPDATE audit_log
SET project_id = 'pj0000000000000000veselice'
WHERE project_id IS NULL
  AND created_at < (SELECT created_at FROM projects WHERE uid = 'pj0000000000000000veselice')
  AND action IN (
    'album.add_photos',
    'album.create',
    'album.delete',
    'album.feature',
    'album.remove_photos',
    'album.unfeature',
    'album.update',
    'announcement.clear',
    'announcement.set',
    'comment.create',
    'comment.delete',
    'comment.update',
    'duplicate.confirm',
    'duplicate.dismiss',
    'duplicate.unconfirm',
    'duplicate.undismiss',
    'duplicate_marker.dismiss',
    'duplicate_marker.undismiss',
    'face.assign',
    'face.confirm',
    'face.reject',
    'face.unassign',
    'face.unconfirm',
    'face.unreject',
    'family.update',
    'label.attach',
    'label.create',
    'label.delete',
    'label.detach',
    'label.reject',
    'label.unreject',
    'label.update',
    'location.confirm',
    'location.reject',
    'marker.invalidate',
    'person.attach',
    'person.detach',
    'photo.archive',
    'photo.date_clear',
    'photo.edit',
    'photo.embedding',
    'photo.faces',
    'photo.hide',
    'photo.place',
    'photo.purge',
    'photo.storyboard',
    'photo.thumbnail',
    'photo.unarchive',
    'photo.unhide',
    'photo.update',
    'photos.bulk',
    'photos.merge',
    'photos.stack',
    'project.invite.accept',
    'project.invite.create',
    'project.invite.revoke',
    'project.member.add',
    'project.member.add_request',
    'project.member.approve',
    'project.member.remove',
    'project.member.role',
    'project.member.subject',
    'project.member.withdraw_request',
    'settings.update',
    'stack.set_primary',
    'stack.ungroup',
    'stack.ungroup_all',
    'stacks.detect',
    'subject.create',
    'subject.delete',
    'subject.merge',
    'subject.relation.add',
    'subject.relation.remove',
    'subject.update',
    'task.add_photos',
    'task.assign',
    'task.create',
    'task.delete',
    'task.remove_photos',
    'task.unassign',
    'task.update',
    'upload_link.create',
    'upload_link.extend',
    'upload_link.restore_code',
    'upload_link.revoke',
    'upload_link.rotate_code',
    'upload_link.upload'
  );
