import { isAdmin, type Role } from '../../services/auth'
import {
  EMAIL_CHANNEL,
  IN_APP_CHANNEL,
  type NotificationChannel,
  type NotificationKind,
  type NotificationPreference,
  PUSH_CHANNEL,
} from '../../services/notifications'
import { isPlaceholderEmail } from '../users/account'

/**
 * The component-free half of `NotificationsCard`: which kinds and channels the
 * card offers, and to whom. Kept apart from the card so the card's module
 * exports only a component (Fast Refresh) and the tests can ask these questions
 * without mounting anything.
 */

/** The i18n key of a kind's checkbox label. */
export type KindLabelKey =
  | 'account.notifications.kinds.tagged'
  | 'account.notifications.kinds.registration_pending'

/** What the card knows about one kind: its label, and who can ever receive it. */
interface KindCopy {
  label: KindLabelKey
  /** Only an administrator (or a maintainer, who inherits it) is ever sent this kind. */
  adminOnly: boolean
}

/**
 * The kinds the card offers a setting for. A kind the server knows and this
 * client does not is left out rather than shown under its raw name: it will get
 * its words (and its audience) with the release that teaches the client.
 */
const KINDS: Partial<Record<NotificationKind, KindCopy>> = {
  tagged: { label: 'account.notifications.kinds.tagged', adminOnly: false },
  registration_pending: {
    label: 'account.notifications.kinds.registration_pending',
    adminOnly: true,
  },
}

/** One column of the kinds × channels grid. */
export interface ChannelColumn {
  channel: NotificationChannel
  /** The column header, and the switch's own name on a phone. */
  label:
    | 'account.notifications.channels.inApp'
    | 'account.notifications.channels.push'
    | 'account.notifications.channels.email'
}

/**
 * The channels the card offers, in column order. A channel the server knows and
 * this client does not gets no column; its stored choices still ride along on
 * every save (see `withPreference`).
 */
export const CHANNELS: readonly ChannelColumn[] = [
  { channel: IN_APP_CHANNEL, label: 'account.notifications.channels.inApp' },
  { channel: PUSH_CHANNEL, label: 'account.notifications.channels.push' },
  { channel: EMAIL_CHANNEL, label: 'account.notifications.channels.email' },
]

/** Why a channel's switches cannot be used, as an i18n key; null when they can. */
export type ChannelBlock =
  | 'account.notifications.unavailable.push'
  | 'account.notifications.unavailable.emailOff'
  | 'account.notifications.unavailable.emailMissing'
  | null

/** One kind's row: its effective setting per offered channel (absent = the kind lacks it). */
export interface KindRow {
  kind: NotificationKind
  label: KindLabelKey
  byChannel: Map<NotificationChannel, NotificationPreference>
}

/**
 * Groups the effective preferences into one row per kind `role` is offered, in
 * the server's order, each holding the channels the kind is delivered through.
 */
export function kindRows(prefs: NotificationPreference[], role: Role | null): KindRow[] {
  const rows = new Map<NotificationKind, KindRow>()
  for (const pref of prefs) {
    const copy = KINDS[pref.kind]
    if (copy === undefined || !isKindOffered(pref.kind, role)) {
      continue
    }
    let row = rows.get(pref.kind)
    if (row === undefined) {
      row = { kind: pref.kind, label: copy.label, byChannel: new Map() }
      rows.set(pref.kind, row)
    }
    row.byChannel.set(pref.channel, pref)
  }
  return [...rows.values()]
}

/**
 * Reports whether `email` is an address a mail can actually reach: present, and
 * not a generated `.invalid` placeholder.
 */
export function isReachableEmail(email: string | null | undefined): boolean {
  const trimmed = email?.trim() ?? ''
  return trimmed !== '' && !isPlaceholderEmail(trimmed)
}

/**
 * Reports whether `kind` gets a checkbox for an account of `role`: nobody is
 * offered a setting for a message they can never receive.
 */
export function isKindOffered(kind: NotificationKind, role: Role | null): boolean {
  const copy = KINDS[kind]
  if (copy === undefined) {
    return false
  }
  return !copy.adminOnly || (role !== null && isAdmin(role))
}
