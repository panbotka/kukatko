import { type DuplicateMarkerGroup } from '../services/dupmarkers'

/**
 * How many valid face markers of one person on one photo still make a finding.
 * It mirrors `dupmarkers.minGroupSize` on the server: one marker is the normal,
 * correct case, so a group that falls to one has been fixed and leaves the queue.
 */
export const MIN_GROUP_SIZE = 2

/**
 * The identity of a finding: the (photo, person) pair, which is what every
 * decision about it is keyed by — server-side too, right down to the dismissal
 * table. Marker uids deliberately play no part: they change when a photo is
 * re-detected, and the group would then look like a different one.
 */
export function groupKey(group: Pick<DuplicateMarkerGroup, 'photo_uid' | 'subject_uid'>): string {
  return `${group.photo_uid}:${group.subject_uid}`
}

/**
 * Drops the whole finding from the list — the outcome of "keep this one" and of
 * "leave it be", both of which settle the group in one go.
 */
export function removeGroup(groups: DuplicateMarkerGroup[], key: string): DuplicateMarkerGroup[] {
  return groups.filter((group) => groupKey(group) !== key)
}

/**
 * Drops one marker from its finding, and the finding itself once fewer than
 * {@link MIN_GROUP_SIZE} markers are left.
 *
 * This is what makes "there is no face in this box" converge on a three-marker
 * group: the first click leaves a two-marker finding still worth deciding, the
 * second empties it of the problem and the card leaves the queue — without a
 * refetch, so the list never jumps under the pointer mid-review.
 */
export function dropMarker(
  groups: DuplicateMarkerGroup[],
  key: string,
  markerUid: string,
): DuplicateMarkerGroup[] {
  const out: DuplicateMarkerGroup[] = []
  for (const group of groups) {
    if (groupKey(group) !== key) {
      out.push(group)
      continue
    }
    const markers = group.markers.filter((marker) => marker.uid !== markerUid)
    if (markers.length >= MIN_GROUP_SIZE) {
      out.push({ ...group, markers })
    }
  }
  return out
}
