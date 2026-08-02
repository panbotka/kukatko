import { type Timeline, type TimelineBucket } from '../services/photos'

/**
 * A stand-in for the production library's month histogram: ~460 buckets over
 * 1905–2026 with a violently skewed distribution — a couple of photos a year
 * before 1950, a few dozen a month in the 2000s, thousands a year after 2010,
 * ~10 500 in total.
 *
 * The scrubber only broke once it met a shape like this: a small development
 * library produces a handful of buckets, where every naive layout looks fine.
 * Tests that need to prove the rail stays legible have to start from a library
 * that is genuinely long-tailed, so they all share this one.
 */
export function realisticTimeline(): Timeline {
  const buckets: TimelineBucket[] = []
  let cumulative = 0
  const push = (year: number, month: number, count: number): void => {
    buckets.push({ year, month, count, cumulative })
    cumulative += count
  }
  // Newest-first, exactly as the API returns it.
  push(2026, 2, 45)
  push(2026, 1, 45)
  for (let year = 2025; year >= 2000; year--) {
    const count = year >= 2010 ? 45 : 12
    for (let month = 12; month >= 1; month--) {
      push(year, month, count)
    }
  }
  // The tail: whole years represented by one or two scanned months.
  for (let year = 1999; year >= 1950; year--) {
    push(year, 7, 3)
    push(year, 4, 3)
  }
  for (let year = 1949; year >= 1905; year--) {
    push(year, 6, 2)
  }
  return { buckets, total: cumulative }
}
