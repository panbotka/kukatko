/** Milliseconds in a day, the unit the retention window is expressed in. */
const MS_PER_DAY = 24 * 60 * 60 * 1000

/**
 * How long an archived photo has before it is automatically purged. `daysLeft`
 * is the whole days remaining (rounded up, never negative); `due` is true once
 * the auto-purge time has passed (the next scheduled run will remove it).
 */
export interface PurgeCountdown {
  daysLeft: number
  due: boolean
}

/**
 * Computes the auto-purge countdown for an archived photo from its `archived_at`
 * timestamp and the configured retention window.
 *
 * Returns `null` when no countdown applies: the photo is not archived
 * (`archivedAt` absent), the retention is disabled (`retentionDays <= 0`, no
 * scheduled purge), or the timestamp is unparseable. Otherwise it returns the
 * days remaining, flagging `due` once the purge time has elapsed.
 *
 * `now` is injectable for deterministic tests; it defaults to the current time.
 */
export function purgeCountdown(
  archivedAt: string | undefined,
  retentionDays: number,
  now: number = Date.now(),
): PurgeCountdown | null {
  if (archivedAt === undefined || archivedAt === '' || retentionDays <= 0) {
    return null
  }
  const archived = Date.parse(archivedAt)
  if (Number.isNaN(archived)) {
    return null
  }
  const remaining = archived + retentionDays * MS_PER_DAY - now
  if (remaining <= 0) {
    return { daysLeft: 0, due: true }
  }
  return { daysLeft: Math.ceil(remaining / MS_PER_DAY), due: false }
}
