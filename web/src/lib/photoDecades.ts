import { type Photo } from '../services/photos'

import { captureYear } from './lifeYears'
import { DECADE_YEARS, decadeOf } from './period'

/**
 * Grouping a loaded list of photos by the decade they were taken in, for the
 * decade navigation on a person's page.
 *
 * This is the *display* side of the time axis and is deliberately separate from
 * `lib/period`, which owns the decade **filter** (what a picked decade means as
 * a pair of date bounds). The two share `decadeOf` so they can never disagree
 * about which decade a year belongs to.
 */

/** One decade's run of photos within a gallery. */
export interface DecadeSection {
  /**
   * First year of the calendar decade (`1950`), or `null` for the photos with no
   * capture date at all — they are a real part of a person's gallery and get
   * their own section rather than being silently dropped from the navigation.
   */
  decade: number | null
  /** The photos in this decade, in the order the gallery received them. */
  photos: Photo[]
}

/**
 * Splits a gallery into decade sections, in order of first appearance.
 *
 * The subject-photos endpoint returns newest first with the undated last, so in
 * practice this yields one contiguous section per decade, newest decade first,
 * and the undated section at the end. Photos are never re-sorted: a decade that
 * reappears out of order is merged into the section it opened, which keeps every
 * decade exactly once in the navigation whatever order the caller supplies.
 *
 * It runs over the photos the gallery has actually loaded, not over the whole
 * person: this navigation is a way around what is on the page, and a decade the
 * reader cannot yet see is not somewhere they can be sent.
 */
export function groupPhotosByDecade(photos: Photo[]): DecadeSection[] {
  const sections = new Map<number | null, DecadeSection>()
  for (const photo of photos) {
    const year = captureYear(photo.taken_at)
    const decade = year === null ? null : decadeOf(year)
    const existing = sections.get(decade)
    if (existing === undefined) {
      sections.set(decade, { decade, photos: [photo] })
      continue
    }
    existing.photos.push(photo)
  }
  return [...sections.values()]
}

/**
 * The decade as the label the navigation and the section heading both show —
 * `"1950–1959"`, the same en-dashed range the period picker offers. The undated
 * section has no range and its label is the caller's translated wording, so this
 * returns `null` for it.
 */
export function formatDecade(decade: number | null): string | null {
  return decade === null ? null : `${decade}–${decade + DECADE_YEARS - 1}`
}

/**
 * The DOM id of a decade's section heading, which is what the navigation scrolls
 * to. Scoped by nothing but the decade because only one gallery is ever on the
 * page.
 */
export function decadeAnchorId(decade: number | null): string {
  return decade === null ? 'kk-decade-undated' : `kk-decade-${decade}`
}
