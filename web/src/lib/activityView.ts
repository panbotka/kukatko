import type { ParseKeys } from 'i18next'

import { type MyActivityParams } from '../services/audit'

/**
 * The own-activity page's route. It hangs off the account — this is "what I did",
 * a fact about the signed-in user — rather than off the admin group, which is
 * about supervising everybody and is visible to admins only.
 */
export const ACTIVITY_PATH = '/account/activity'

/**
 * URL-encoded view state for the own-activity page. Only the page offset: the
 * listing is already narrowed to one user and reads newest-first, so there is
 * nothing else to choose. Keeping it in the URL is the project's "Back always
 * works" convention — page 2 stays page 2 after following a link out and back.
 *
 * A type alias rather than an interface, so it keeps the implicit index
 * signature the urlState `Record<string, string>` constraint requires.
 */
// eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- see above
export type ActivityView = {
  offset: string
}

/** Default view: the first page. Module scope, so the setter identity is stable. */
export const ACTIVITY_DEFAULTS: ActivityView = {
  offset: '0',
}

/**
 * Page size for the own activity. Smaller than the admin log's 100: this page is
 * read to find one recent action, not to sweep a trail, and the recent end is
 * where the answer is.
 */
export const ACTIVITY_PAGE_SIZE = 50

/**
 * How many UIDs one entry's payload contributes as links. A bulk edit names every
 * photo it touched; a handful is enough to get back to the batch, and the rest
 * would bury the row.
 */
export const ACTIVITY_LINK_LIMIT = 5

/** Maps the URL view onto the own-activity request parameters. */
export function viewToParams(view: ActivityView): MyActivityParams {
  return {
    limit: ACTIVITY_PAGE_SIZE,
    offset: Number(view.offset) || 0,
  }
}

/**
 * What each audit action is called in words, keyed by the action label the
 * backend records (`internal/audit`'s Action* constants). The admin log prints
 * the raw `photo.update`; this page is read by whoever made the change, so it
 * says "Úprava fotky" instead.
 *
 * The values are literal i18n keys rather than a computed `activity.actions.${…}`
 * template, so a key that does not exist in the catalogue is a compile error
 * instead of a raw key rendered on the page.
 */
const ACTION_LABEL_KEYS: Record<string, ParseKeys> = {
  'album.add_photos': 'activity.actions.album.add_photos',
  'album.create': 'activity.actions.album.create',
  'album.delete': 'activity.actions.album.delete',
  'album.feature': 'activity.actions.album.feature',
  'album.remove_photos': 'activity.actions.album.remove_photos',
  'album.unfeature': 'activity.actions.album.unfeature',
  'album.update': 'activity.actions.album.update',
  'announcement.clear': 'activity.actions.announcement.clear',
  'announcement.set': 'activity.actions.announcement.set',
  'api_token.create': 'activity.actions.api_token.create',
  'api_token.revoke': 'activity.actions.api_token.revoke',
  'api_token.update': 'activity.actions.api_token.update',
  'audit.purge': 'activity.actions.audit.purge',
  'comment.create': 'activity.actions.comment.create',
  'comment.delete': 'activity.actions.comment.delete',
  'comment.update': 'activity.actions.comment.update',
  'duplicate.confirm': 'activity.actions.duplicate.confirm',
  'duplicate.dismiss': 'activity.actions.duplicate.dismiss',
  'duplicate.unconfirm': 'activity.actions.duplicate.unconfirm',
  'duplicate.undismiss': 'activity.actions.duplicate.undismiss',
  'duplicate_marker.dismiss': 'activity.actions.duplicate_marker.dismiss',
  'duplicate_marker.undismiss': 'activity.actions.duplicate_marker.undismiss',
  'face.assign': 'activity.actions.face.assign',
  'face.confirm': 'activity.actions.face.confirm',
  'face.reject': 'activity.actions.face.reject',
  'face.unassign': 'activity.actions.face.unassign',
  'face.unconfirm': 'activity.actions.face.unconfirm',
  'face.unreject': 'activity.actions.face.unreject',
  'family.update': 'activity.actions.family.update',
  'label.attach': 'activity.actions.label.attach',
  'label.create': 'activity.actions.label.create',
  'label.delete': 'activity.actions.label.delete',
  'label.detach': 'activity.actions.label.detach',
  'label.reject': 'activity.actions.label.reject',
  'label.unreject': 'activity.actions.label.unreject',
  'label.update': 'activity.actions.label.update',
  'library.reset': 'activity.actions.library.reset',
  'location.confirm': 'activity.actions.location.confirm',
  'location.reject': 'activity.actions.location.reject',
  'marker.invalidate': 'activity.actions.marker.invalidate',
  'notification_prefs.update': 'activity.actions.notification_prefs.update',
  'passkey.delete': 'activity.actions.passkey.delete',
  'passkey.login': 'activity.actions.passkey.login',
  'passkey.register': 'activity.actions.passkey.register',
  'person.attach': 'activity.actions.person.attach',
  'person.detach': 'activity.actions.person.detach',
  'photo.archive': 'activity.actions.photo.archive',
  'photo.date_clear': 'activity.actions.photo.date_clear',
  'photo.edit': 'activity.actions.photo.edit',
  'photo.embedding': 'activity.actions.photo.embedding',
  'photo.faces': 'activity.actions.photo.faces',
  'photo.hide': 'activity.actions.photo.hide',
  'photo.place': 'activity.actions.photo.place',
  'photo.purge': 'activity.actions.photo.purge',
  'photo.storyboard': 'activity.actions.photo.storyboard',
  'photo.thumbnail': 'activity.actions.photo.thumbnail',
  'photo.unarchive': 'activity.actions.photo.unarchive',
  'photo.unhide': 'activity.actions.photo.unhide',
  'photo.update': 'activity.actions.photo.update',
  'photos.bulk': 'activity.actions.photos.bulk',
  'photos.merge': 'activity.actions.photos.merge',
  'photos.stack': 'activity.actions.photos.stack',
  'project.create': 'activity.actions.project.create',
  'project.invite.accept': 'activity.actions.project.invite.accept',
  'project.invite.create': 'activity.actions.project.invite.create',
  'project.invite.revoke': 'activity.actions.project.invite.revoke',
  'project.member.add': 'activity.actions.project.member.add',
  'project.member.add_request': 'activity.actions.project.member.add_request',
  'project.member.add_resolve': 'activity.actions.project.member.add_resolve',
  'project.member.approve': 'activity.actions.project.member.approve',
  'project.member.remove': 'activity.actions.project.member.remove',
  'project.member.role': 'activity.actions.project.member.role',
  'project.member.subject': 'activity.actions.project.member.subject',
  'project.member.withdraw_request': 'activity.actions.project.member.withdraw_request',
  'project.member.withdraw_resolve': 'activity.actions.project.member.withdraw_resolve',
  'project.purge': 'activity.actions.project.purge',
  'project.rekey.pause': 'activity.actions.project.rekey.pause',
  'project.rekey.resume': 'activity.actions.project.rekey.resume',
  'project.rekey.retry': 'activity.actions.project.rekey.retry',
  'project.rekey.start': 'activity.actions.project.rekey.start',
  'project.storage_prefix': 'activity.actions.project.storage_prefix',
  'project.update': 'activity.actions.project.update',
  'settings.update': 'activity.actions.settings.update',
  'stack.set_primary': 'activity.actions.stack.set_primary',
  'stack.ungroup': 'activity.actions.stack.ungroup',
  'stack.ungroup_all': 'activity.actions.stack.ungroup_all',
  'stacks.detect': 'activity.actions.stacks.detect',
  'subject.create': 'activity.actions.subject.create',
  'subject.delete': 'activity.actions.subject.delete',
  'subject.merge': 'activity.actions.subject.merge',
  'subject.relation.add': 'activity.actions.subject.relation.add',
  'subject.relation.remove': 'activity.actions.subject.relation.remove',
  'subject.update': 'activity.actions.subject.update',
  'task.add_photos': 'activity.actions.task.add_photos',
  'task.assign': 'activity.actions.task.assign',
  'task.create': 'activity.actions.task.create',
  'task.delete': 'activity.actions.task.delete',
  'task.remove_photos': 'activity.actions.task.remove_photos',
  'task.unassign': 'activity.actions.task.unassign',
  'task.update': 'activity.actions.task.update',
  'upload_link.create': 'activity.actions.upload_link.create',
  'upload_link.extend': 'activity.actions.upload_link.extend',
  'upload_link.restore_code': 'activity.actions.upload_link.restore_code',
  'upload_link.revoke': 'activity.actions.upload_link.revoke',
  'upload_link.rotate_code': 'activity.actions.upload_link.rotate_code',
  'upload_link.upload': 'activity.actions.upload_link.upload',
  'user.approve': 'activity.actions.user.approve',
  'user.create': 'activity.actions.user.create',
  'user.disable': 'activity.actions.user.disable',
  'user.instance_role': 'activity.actions.user.instance_role',
  'user.password': 'activity.actions.user.password',
  'user.password_reset': 'activity.actions.user.password_reset',
  'user.password_reset_use': 'activity.actions.user.password_reset_use',
  'user.register': 'activity.actions.user.register',
  'user.rename': 'activity.actions.user.rename',
  'user.update': 'activity.actions.user.update',
}

/**
 * The i18n key naming an action in words, or `undefined` for an action this
 * catalogue does not know — a new one shipped by the backend, or an old row. The
 * caller then falls back to the raw label, which is still readable enough to act
 * on; a missing translation must never blank the row.
 */
export function activityActionKey(action: string): ParseKeys | undefined {
  return ACTION_LABEL_KEYS[action]
}

/**
 * What each audit target type is called in words, by the same rule as the
 * actions. `markers` is deliberately the photo's label: a marker's link leads to
 * the photo the face sits on (see `auditTargetHref`), so calling it anything else
 * would promise a page that does not exist.
 */
const TARGET_LABEL_KEYS: Record<string, ParseKeys> = {
  albums: 'activity.targets.albums',
  announcement: 'activity.targets.announcement',
  api_tokens: 'activity.targets.api_tokens',
  audit_log: 'activity.targets.audit_log',
  families: 'activity.targets.families',
  labels: 'activity.targets.labels',
  library: 'activity.targets.library',
  markers: 'activity.targets.photos',
  passkey_credentials: 'activity.targets.passkey_credentials',
  photo_tasks: 'activity.targets.photo_tasks',
  photos: 'activity.targets.photos',
  project: 'activity.targets.project',
  project_invites: 'activity.targets.project_invites',
  project_member: 'activity.targets.project_member',
  subjects: 'activity.targets.subjects',
  upload_links: 'activity.targets.upload_links',
  users: 'activity.targets.users',
}

/**
 * The i18n key naming a target type in words, or `undefined` for a type this
 * catalogue does not know (the caller falls back to the raw type).
 */
export function activityTargetKey(targetType: string): ParseKeys | undefined {
  return TARGET_LABEL_KEYS[targetType]
}
