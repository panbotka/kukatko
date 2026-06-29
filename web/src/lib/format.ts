/** Binary unit suffixes for {@link formatBytes}, ascending by 1024×. */
const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const

/**
 * Formats a byte count as a short human-readable string using binary (1024)
 * units, e.g. `1536` → `"1.5 KB"`. Negative or non-finite inputs render as
 * `"0 B"`. Bytes show no decimals; larger units show one.
 */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B'
  }
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024
    unit += 1
  }
  const digits = unit === 0 ? 0 : 1
  return `${value.toFixed(digits)} ${BYTE_UNITS[unit]}`
}
