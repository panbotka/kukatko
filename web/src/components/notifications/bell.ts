/**
 * The component-free half of `NotificationBell`, kept apart so the bell's
 * module exports only components (Fast Refresh) and tests can import these.
 */

/** How many of the newest notifications the dropdown shows; the rest live on the inbox page. */
export const BELL_LIMIT = 8

/** The inbox page the dropdown's footer opens. */
export const INBOX_PATH = '/notifications'

/** The numeral on the badge: anything past 99 is "99+", so the pill stays a pill. */
export function badgeText(count: number): string {
  return count > 99 ? '99+' : String(count)
}
