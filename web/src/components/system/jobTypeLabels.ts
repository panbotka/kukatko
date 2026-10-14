import type { ParseKeys } from 'i18next'

/**
 * Every job type the queue knows (`internal/jobs/models.go`'s `Type*`
 * constants) and its name in the family's vocabulary. `jobTypeLabels.test.ts`
 * reads that Go file and fails when a type is missing here or in a locale, so a
 * new handler cannot ship with its raw id on `/system`.
 */
export const JOB_TYPE_LABELS: Record<string, ParseKeys | undefined> = {
  image_embed: 'system.jobs.types.image_embed',
  face_detect: 'system.jobs.types.face_detect',
  thumbnail: 'system.jobs.types.thumbnail',
  places: 'system.jobs.types.places',
  metadata: 'system.jobs.types.metadata',
  ocr: 'system.jobs.types.ocr',
  sidecar: 'system.jobs.types.sidecar',
  family_export: 'system.jobs.types.family_export',
  storyboard: 'system.jobs.types.storyboard',
  hls_transcode: 'system.jobs.types.hls_transcode',
  backup: 'system.jobs.types.backup',
  mail_send: 'system.jobs.types.mail_send',
  push_send: 'system.jobs.types.push_send',
  task_digest: 'system.jobs.types.task_digest',
  tag_notify: 'system.jobs.types.tag_notify',
  album_featured_notify: 'system.jobs.types.album_featured_notify',
  notification_purge: 'system.jobs.types.notification_purge',
  nameless_detach: 'system.jobs.types.nameless_detach',
  nameless_restore: 'system.jobs.types.nameless_restore',
  face_cluster: 'system.jobs.types.face_cluster',
  storage_rekey: 'system.jobs.types.storage_rekey',
}
