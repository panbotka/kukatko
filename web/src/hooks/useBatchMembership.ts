import { useEffect, useState } from 'react'

import { type BulkMembershipSummary, fetchBulkMembershipSummary } from '../services/bulk'

/**
 * The most photos one membership question carries — the backend's default
 * `bulk.max_batch_size`. A longer batch is asked about its first this-many
 * photos only, and the answer is marked `partial`.
 */
export const MEMBERSHIP_MAX_UIDS = 1000

/**
 * What is known about where a settled batch already is: nothing asked yet
 * (`idle`), the question in flight (`loading`), the request failed (`error`),
 * or the answer. `partial` is true when the batch was longer than
 * {@link MEMBERSHIP_MAX_UIDS} and only its head was asked about, so a zero
 * `filed` count proves nothing about the rest.
 */
export type BatchMembershipState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; summary: BulkMembershipSummary; partial: boolean }

/**
 * Asks once, in a single `POST /photos/bulk/membership-summary`, whether any of
 * `uids` is already in an album or under a label — so the upload page does not
 * tell its reader a batch of duplicates is "not in any album yet" when the
 * first upload filed them long ago. The upload response itself carries no
 * membership, and a request per photo would be a storm on a large batch.
 *
 * It asks only while `enabled`, and asks afresh every time it is enabled again
 * or the photo set changes: an album picked and then cleared again has filed
 * the batch in between, and a remembered answer from before would be stale.
 * Disabling drops back to `idle`. The caller orders `uids` by what most likely
 * sits in an album already (the duplicates first), since a batch longer than
 * {@link MEMBERSHIP_MAX_UIDS} is asked about its head only.
 */
export function useBatchMembership(uids: string[], enabled: boolean): BatchMembershipState {
  const [state, setState] = useState<BatchMembershipState>({ status: 'idle' })
  // The photo set as one comparable value, so a fresh array holding the same
  // photos does not ask again.
  const key = uids.join(',')

  useEffect(() => {
    if (!enabled || key === '') {
      setState({ status: 'idle' })
      return
    }
    const all = key.split(',')
    const asked = all.slice(0, MEMBERSHIP_MAX_UIDS)
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchBulkMembershipSummary(asked, controller.signal).then(
      (summary) => {
        if (!controller.signal.aborted) {
          setState({ status: 'ready', summary, partial: asked.length < all.length })
        }
      },
      () => {
        if (!controller.signal.aborted) {
          setState({ status: 'error' })
        }
      },
    )
    return () => {
      controller.abort()
    }
  }, [enabled, key])

  return state
}
