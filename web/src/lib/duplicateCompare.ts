/**
 * The pure logic behind the duplicate compare view: turning duplicate groups into a
 * queue of pairs, and turning two photos into the difference table that decides
 * which one to keep. Effects (fetching, advancing, merging) live in the page and
 * the hook; everything here is a total function of its inputs, so the two rules
 * that actually matter — which pairs get offered, and which rows count as different
 * — are testable without a DOM.
 */

import type { DuplicateGroup } from '../services/duplicates'
import type { PhotoDetail } from '../services/photos'

/**
 * One comparison: two photos of the same group, plus the group they came from
 * (needed to merge, which acts on the whole group) and the keeper the detector
 * suggested.
 */
export interface ComparePair {
  /** Stable identity of the pair, so a queue position survives a refetch. */
  id: string
  group: DuplicateGroup
  leftUid: string
  rightUid: string
}

/**
 * Everything the diff table needs about one side. `people` is passed separately
 * because named people live on the faces endpoint, not on the photo record.
 */
export interface ComparePhoto {
  photo: PhotoDetail
  /** Names of the people named on this photo, in the order the API returned them. */
  people: string[]
}

/** One row of the difference table. */
export interface DiffRow {
  /** Suffix of the row's i18n label key (`duplicates.compare.diff.<key>`). */
  key: DiffRowKey
  /** The left photo's value, already formatted for display. */
  left: string
  /** The right photo's value, already formatted for display. */
  right: string
  /** Whether the two differ — the rows the user is actually here to see. */
  differs: boolean
}

/** The rows the difference table compares, in display order. */
export type DiffRowKey =
  | 'dimensions'
  | 'fileSize'
  | 'format'
  | 'takenAt'
  | 'camera'
  | 'lens'
  | 'fileName'
  | 'location'
  | 'albums'
  | 'labels'
  | 'people'

/**
 * Builds the queue of pairs to review from a page of duplicate groups.
 *
 * A two-member group yields its one pair. A larger group is compared **pairwise
 * against the suggested keeper**: [K, A, B] yields (K,A) and (K,B), never (A,B).
 * That is a real choice with a real cost — comparing every combination would be
 * n·(n-1)/2 questions for a group the user probably wants to resolve in one action
 * — but the keeper is the copy the merge would keep, so "keeper vs this copy" is
 * the question each decision actually answers. No member is silently dropped: every
 * non-keeper appears in exactly one pair, and the UI says so ({@link pairsInGroup}
 * drives the "pair i of n in this group" caption).
 *
 * A group whose keeper is not among its members (an inconsistent payload) is
 * skipped rather than guessed at.
 */
export function buildPairQueue(groups: DuplicateGroup[]): ComparePair[] {
  const pairs: ComparePair[] = []
  for (const group of groups) {
    const keeper = group.members.find((m) => m.uid === group.keeper_uid)
    if (keeper === undefined) {
      continue
    }
    for (const member of group.members) {
      if (member.uid === group.keeper_uid) {
        continue
      }
      pairs.push({
        id: pairId(keeper.uid, member.uid),
        group,
        leftUid: keeper.uid,
        rightUid: member.uid,
      })
    }
  }
  return pairs
}

/**
 * A pair's stable id. The pair is unordered, so the id is order-independent and
 * matches the backend's canonical (smaller uid first) form.
 */
export function pairId(a: string, b: string): string {
  return a <= b ? `${a}|${b}` : `${b}|${a}`
}

/** How many pairs a group contributes to the queue: every member but the keeper. */
export function pairsInGroup(group: DuplicateGroup): number {
  return Math.max(group.members.length - 1, 0)
}

/** This pair's 1-based position among its group's pairs, for the "pair i of n" caption. */
export function pairIndexInGroup(pair: ComparePair): number {
  const others = pair.group.members.filter((m) => m.uid !== pair.group.keeper_uid)
  return others.findIndex((m) => m.uid === pair.rightUid) + 1
}

/**
 * Drops every pair that touches `uid` from the queue. Used after a merge: the loser
 * has been archived, so any other pair naming it is a question about a photo that is
 * no longer there.
 */
export function dropPairsTouching(pairs: ComparePair[], uid: string): ComparePair[] {
  return pairs.filter((p) => p.leftUid !== uid && p.rightUid !== uid)
}

/** Formats a pixel count as megapixels, e.g. `12.2 Mpx`. Zero-size yields ''. */
function megapixels(width: number, height: number): string {
  const px = width * height
  if (px <= 0) {
    return ''
  }
  return `${(px / 1_000_000).toFixed(1)} Mpx`
}

/** Joins a camera make and model, tolerating either being empty. */
function cameraName(photo: PhotoDetail): string {
  return [photo.camera_make, photo.camera_model]
    .map((part) => part.trim())
    .filter((part) => part !== '')
    .join(' ')
}

/**
 * A photo's location as text: the reverse-geocoded place when known, else the raw
 * coordinates, else ''. Coordinates are rounded to ~11 m, enough to tell two
 * genuinely different locations apart without implying false precision.
 */
function locationText(photo: PhotoDetail): string {
  const place = photo.place
  if (place !== undefined) {
    const parts = [place.city, place.country].filter((part) => part !== '')
    if (parts.length > 0) {
      return parts.join(', ')
    }
  }
  if (photo.lat !== undefined && photo.lng !== undefined) {
    return `${photo.lat.toFixed(4)}, ${photo.lng.toFixed(4)}`
  }
  return ''
}

/** A stable, comparable rendering of a set of names: sorted and joined. */
function nameSet(names: string[]): string {
  return [...names].sort((a, b) => a.localeCompare(b)).join(', ')
}

/**
 * Builds the difference table for two photos.
 *
 * `differs` is computed from a comparison key rather than from the formatted text,
 * so a difference the formatting rounds away (two capture times in the same minute,
 * two file sizes that both render as "2.1 MB") is still marked. The reverse — two
 * values that format identically but compare differently — would mark a row the
 * user cannot see, so the comparison keys are chosen to be what a human would call
 * the same value: names are compared as a sorted set, not in API order.
 *
 * `formatBytes`/`formatDateTime` are injected rather than imported so this stays a
 * pure function of its inputs and the tests do not depend on the runtime locale.
 */
export function buildDiffRows(
  left: ComparePhoto,
  right: ComparePhoto,
  fmt: {
    bytes: (n: number) => string
    dateTime: (iso: string) => string
  },
): DiffRow[] {
  const l = left.photo
  const r = right.photo
  const rows: (DiffRow & { leftKey: string; rightKey: string })[] = [
    {
      key: 'dimensions',
      left: dimensionsText(l.file_width, l.file_height),
      right: dimensionsText(r.file_width, r.file_height),
      leftKey: `${String(l.file_width)}x${String(l.file_height)}`,
      rightKey: `${String(r.file_width)}x${String(r.file_height)}`,
      differs: false,
    },
    {
      key: 'fileSize',
      left: fmt.bytes(l.file_size),
      right: fmt.bytes(r.file_size),
      leftKey: String(l.file_size),
      rightKey: String(r.file_size),
      differs: false,
    },
    {
      key: 'format',
      left: l.file_mime,
      right: r.file_mime,
      leftKey: l.file_mime,
      rightKey: r.file_mime,
      differs: false,
    },
    {
      key: 'takenAt',
      left: l.taken_at === undefined ? '' : fmt.dateTime(l.taken_at),
      right: r.taken_at === undefined ? '' : fmt.dateTime(r.taken_at),
      leftKey: l.taken_at ?? '',
      rightKey: r.taken_at ?? '',
      differs: false,
    },
    {
      key: 'camera',
      left: cameraName(l),
      right: cameraName(r),
      leftKey: cameraName(l),
      rightKey: cameraName(r),
      differs: false,
    },
    {
      key: 'lens',
      left: l.lens_model,
      right: r.lens_model,
      leftKey: l.lens_model,
      rightKey: r.lens_model,
      differs: false,
    },
    {
      key: 'fileName',
      left: l.file_name,
      right: r.file_name,
      leftKey: l.file_name,
      rightKey: r.file_name,
      differs: false,
    },
    {
      key: 'location',
      left: locationText(l),
      right: locationText(r),
      leftKey: locationText(l),
      rightKey: locationText(r),
      differs: false,
    },
    {
      key: 'albums',
      left: nameSet(l.albums.map((a) => a.title)),
      right: nameSet(r.albums.map((a) => a.title)),
      leftKey: nameSet(l.albums.map((a) => a.title)),
      rightKey: nameSet(r.albums.map((a) => a.title)),
      differs: false,
    },
    {
      key: 'labels',
      left: nameSet(l.labels.map((x) => x.name)),
      right: nameSet(r.labels.map((x) => x.name)),
      leftKey: nameSet(l.labels.map((x) => x.name)),
      rightKey: nameSet(r.labels.map((x) => x.name)),
      differs: false,
    },
    {
      key: 'people',
      left: nameSet(left.people),
      right: nameSet(right.people),
      leftKey: nameSet(left.people),
      rightKey: nameSet(right.people),
      differs: false,
    },
  ]
  return rows.map(({ leftKey, rightKey, ...row }) => ({ ...row, differs: leftKey !== rightKey }))
}

/** Formats dimensions with the megapixel count, e.g. `4032 × 3024 (12.2 Mpx)`. */
function dimensionsText(width: number, height: number): string {
  if (width <= 0 || height <= 0) {
    return ''
  }
  return `${String(width)} × ${String(height)} (${megapixels(width, height)})`
}

/** How many rows of a built table differ — the count the summary line reports. */
export function countDiffering(rows: DiffRow[]): number {
  return rows.filter((row) => row.differs).length
}
