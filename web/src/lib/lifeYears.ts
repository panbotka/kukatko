/**
 * A person's life span and the ages derived from it.
 *
 * A subject may carry a birth and a death **year** (`services/people` —
 * `Subject.birth_year` / `death_year`). Two things are made of them: the header
 * line on the person's page („1923–1998"), and, on every photo of that person
 * whose capture date is known, roughly how old they were when it was taken.
 *
 * The age is deliberately approximate and says so. All that is known is a year,
 * so the difference of two years is off by up to one either way depending on
 * where the birthday fell — which is why the label reads „~23 let" and never
 * pretends to a date.
 */

/** The earliest year a subject may carry, mirroring `people.MinLifeYear` in Go. */
export const MIN_LIFE_YEAR = 1800

/**
 * The oldest age still worth showing. Beyond it the number is not a person's age
 * but the shadow of something wrong — a photo dated 1902 attributed to somebody
 * born in 1750 by a typo, a scan whose EXIF fell back to the epoch. Showing
 * „~276 let" beside a face would present that mistake as a fact, so nothing is
 * shown at all and the picture is left to speak for itself.
 */
export const MAX_PLAUSIBLE_AGE = 120

/**
 * The calendar year of an ISO capture timestamp, or `null` when there is none or
 * it cannot be parsed.
 *
 * The year is read in **UTC**, matching how the rest of the app reads capture
 * times: they are stored as the EXIF wall clock interpreted as UTC (see
 * `lib/period`), so a local-time reading would move a New Year's Eve photograph
 * into the wrong year — and, here, the wrong decade.
 */
export function captureYear(takenAt: string | null | undefined): number | null {
  if (takenAt === null || takenAt === undefined || takenAt === '') {
    return null
  }
  const date = new Date(takenAt)
  return Number.isNaN(date.getTime()) ? null : date.getUTCFullYear()
}

/**
 * Roughly how old the person was on a photo taken at `takenAt`, or `null` when
 * the age cannot be stated: no capture date, no birth year, a photo dated
 * **before** the birth (an impossibility, so nothing is claimed), or an age past
 * {@link MAX_PLAUSIBLE_AGE}.
 *
 * The result is the plain difference of the two years, so a person born in 1923
 * is „~23" on every photo taken during 1946 regardless of month. That is the
 * most the data supports; the „~" in the label carries the rest.
 */
export function approximateAge(
  takenAt: string | null | undefined,
  birthYear: number | null | undefined,
): number | null {
  if (birthYear === null || birthYear === undefined) {
    return null
  }
  const year = captureYear(takenAt)
  if (year === null) {
    return null
  }
  const age = year - birthYear
  if (age < 0 || age > MAX_PLAUSIBLE_AGE) {
    return null
  }
  return age
}

/**
 * The life span as the one line a person's header shows, or `null` when neither
 * year is known (the header then shows nothing rather than an empty ornament):
 *
 * - both years:  `"1923–1998"` (en dash, as the period control writes a span)
 * - birth only:  `"*1923"` — the conventional shorthand for "born in", and the
 *   only honest reading: a missing death year means "not recorded", not "died".
 * - death only:  `"†1998"`, the same convention from the other end. The pair can
 *   be half-known in either direction, and a record that says when somebody died
 *   should not be swallowed just because nobody knew when they were born.
 */
export function formatLifeSpan(
  birthYear: number | null | undefined,
  deathYear: number | null | undefined,
): string | null {
  const birth = birthYear ?? null
  const death = deathYear ?? null
  if (birth !== null && death !== null) {
    return `${birth}–${death}`
  }
  if (birth !== null) {
    return `*${birth}`
  }
  if (death !== null) {
    return `†${death}`
  }
  return null
}
