import { useEffect, useState } from 'react'

import { fetchLeaderboard } from '../services/review'

/**
 * The player's current day-streak: consecutive days with at least one review
 * decision, ending today or yesterday.
 *
 * The number is computed by the backend and rides on the leaderboard
 * (`streak_days` on the caller's own row), so the game only reads it — there is
 * no second source of truth to drift. A failure is silence, not an error: the
 * streak is flavour, and a game that will not start because a leaderboard is
 * unreachable would be a poor trade. 0 means "no run alive", which the header
 * renders as nothing at all.
 */
export function useReviewStreak(): number {
  const [streak, setStreak] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    async function read() {
      try {
        const board = await fetchLeaderboard('all', controller.signal)
        const mine = board.entries.find((entry) => entry.is_me)
        setStreak(mine?.streak_days ?? 0)
      } catch {
        // Offline, forbidden, or aborted on unmount: the game plays on.
      }
    }
    void read()
    return () => {
      controller.abort()
    }
  }, [])

  return streak
}
