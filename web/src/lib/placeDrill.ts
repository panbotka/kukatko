import { type PlaceCountry } from '../services/places'

/**
 * Which level of the place hierarchy is actually on screen, once the levels that
 * would show a single row have been stepped past.
 */
export interface PlaceDrill {
  /** The country being browsed, or `''` while the country list is showing. */
  country: string
  /** The city being browsed, or `''` above the photo grid. */
  city: string
  /** The chosen country's entry, `undefined` when none is (or it is unknown). */
  selected: PlaceCountry | undefined
  /** Whether {@link PlaceDrill.country} was stepped into rather than chosen. */
  countryImplied: boolean
  /** Whether {@link PlaceDrill.city} was stepped into rather than chosen. */
  cityImplied: boolean
  /** Whether returning to the country list would show anything new. */
  canClearCountry: boolean
  /** Whether returning to the city list would show anything new. */
  canClearCity: boolean
}

/**
 * Resolves the URL's `country`/`city` against the fetched hierarchy, skipping any
 * level that holds exactly one entry.
 *
 * A list of one row is not a choice, it is a click charged for nothing: this
 * library has a single country, so `/places` opened on "Česko — 2 351 fotek" and
 * every visit began by dismissing it. Stepping past such a level shows the level
 * below straight away.
 *
 * A city level is only stepped past when its one city accounts for **all** of the
 * country's photos. A country can also hold photos whose town was never resolved,
 * and those have no row of their own; skipping to the single named city would
 * quietly narrow the view to a fraction of the country while the breadcrumb still
 * said the country's name. The country level needs no such test — the list is
 * every country there is, so one entry is the whole library.
 *
 * The `can…` flags say whether stepping back up would show anything new, so the
 * breadcrumb can render a level that was skipped as plain text instead of a link
 * that lands on the same screen.
 */
export function resolvePlaceDrill(
  countries: PlaceCountry[],
  country: string,
  city: string,
): PlaceDrill {
  const sole = countries.length === 1 ? countries[0] : undefined
  const effectiveCountry = country !== '' ? country : (sole?.country ?? '')
  const selected =
    effectiveCountry === '' ? undefined : countries.find((c) => c.country === effectiveCountry)

  const soleCity = selected?.cities.length === 1 ? selected.cities[0] : undefined
  // "Covers the country" is what makes the skip lossless — see the doc comment.
  const skippableCity = soleCity !== undefined && soleCity.count === selected?.count
  const effectiveCity = city !== '' ? city : skippableCity ? soleCity.city : ''

  return {
    country: effectiveCountry,
    city: effectiveCity,
    selected,
    countryImplied: country === '' && effectiveCountry !== '',
    cityImplied: city === '' && effectiveCity !== '',
    canClearCountry: country !== '' && countries.length > 1,
    canClearCity: city !== '' && (selected?.cities.length ?? 0) > 1,
  }
}
