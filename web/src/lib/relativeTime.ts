/**
 * Relative time formatting ("před 2 h", "2h ago") for conversational timestamps.
 *
 * A comment thread is read as a conversation, and a conversation is dated by how
 * long ago it happened, not by a calendar stamp: "před 2 h" places a remark in the
 * afternoon far better than "9. 8. 2026 14:12" does. The absolute stamp is never
 * lost — it belongs in the `title`/tooltip beside this string (see
 * `formatDateTimeMinutes` in `lib/format.ts`).
 *
 * The strings come from `Intl.RelativeTimeFormat`, so Czech and English (and any
 * language added later) are correct without a hand-written plural table. `narrow`
 * is what yields the short "před 2 h" the design asks for, and `numeric: 'auto'`
 * is what turns "před 1 dnem" into "včera".
 */

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR
const WEEK = 7 * DAY
/** The average month/year, so "3 months ago" does not drift across long gaps. */
const MONTH = 30.44 * DAY
const YEAR = 365.25 * DAY

/**
 * Below this the difference is not worth naming — a freshly posted comment reads
 * as "nyní" / "now" rather than counting seconds at the reader.
 */
const JUST_NOW = 45 * SECOND

/** One threshold: everything under `limit` is expressed in `unit` chunks of `step`. */
interface Scale {
  limit: number
  step: number
  unit: Intl.RelativeTimeFormatUnit
}

const SCALES: Scale[] = [
  { limit: MINUTE, step: SECOND, unit: 'second' },
  { limit: HOUR, step: MINUTE, unit: 'minute' },
  { limit: DAY, step: HOUR, unit: 'hour' },
  { limit: WEEK, step: DAY, unit: 'day' },
  { limit: MONTH, step: WEEK, unit: 'week' },
  { limit: YEAR, step: MONTH, unit: 'month' },
]

/**
 * Formats an instant as how long ago it was, in the reader's language.
 *
 * A timestamp slightly in the future — the server's clock a second or two ahead of
 * the browser's, which is exactly the case for a comment the user just posted — is
 * reported as "now" rather than "in 2 s", because the alternative is the app
 * telling the reader that something they just did has not happened yet. A genuinely
 * future instant (beyond that tolerance) is formatted as the future it is.
 *
 * @param value the instant, as an ISO string, epoch milliseconds or a Date.
 * @param locale the BCP-47 language tag (`i18n.language`).
 * @param now the reference instant, overridable so tests are not clock-dependent.
 * @returns the formatted string, or `''` when `value` is not a valid date.
 */
export function formatRelativeTime(
  value: string | number | Date,
  locale: string,
  now: number = Date.now(),
): string {
  const time = new Date(value).getTime()
  if (Number.isNaN(time)) {
    return ''
  }
  const format = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'narrow' })
  const diff = now - time
  if (diff < JUST_NOW && diff > -JUST_NOW) {
    return format.format(0, 'second')
  }
  const magnitude = Math.abs(diff)
  // Past is negative for Intl ("2 minutes ago" is format(-2, 'minute')).
  const sign = diff > 0 ? -1 : 1
  for (const scale of SCALES) {
    if (magnitude < scale.limit) {
      return format.format(sign * Math.round(magnitude / scale.step), scale.unit)
    }
  }
  return format.format(sign * Math.round(magnitude / YEAR), 'year')
}
