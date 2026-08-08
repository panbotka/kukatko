/**
 * How a photo's cached place reads as text.
 *
 * The `places` job resolves a coordinate into a hierarchy (country → region →
 * city → named place) and leaves the levels it could not resolve empty. Two
 * different sentences are made of it: the shortest name that identifies the photo
 * (for a heading, where a whole address would not fit) and the readable line that
 * answers "where is this?" (under the detail page's mini-map, in place of the raw
 * numbers). Both skip the blanks rather than rendering them, and both live here so
 * the two callers cannot drift apart.
 *
 * Pure, React-free and i18n-free: the level names come from the geocoder already
 * localized, so there is nothing here to translate.
 */

/**
 * The levels of a cached place these rules read. Structurally satisfied by
 * `PhotoPlace` from the photo detail; `region` is optional because no rule here
 * uses it and the title rule's narrower source has never carried it.
 */
export interface PlaceNames {
  /** The country, empty when the geocoder resolved none. */
  country: string
  /** The town or village, empty when unresolved. */
  city: string
  /** The narrowest named place ("Špilberk"), empty when unresolved. */
  place_name: string
  /** The region, unused by these rules. */
  region?: string
}

/** The levels of a place, narrowest first — the order both rules read them in. */
function levels(place: PlaceNames): string[] {
  return [place.place_name, place.city, place.country].map((name) => name.trim())
}

/**
 * The most specific single name the hierarchy offers, narrowest first: the named
 * place ("Špilberk") beats the city, which beats the country. Empty when the
 * photo has no place, or none of its levels was resolved.
 */
export function placeName(place: PlaceNames | undefined): string {
  if (place === undefined) {
    return ''
  }
  return levels(place).find((name) => name !== '') ?? ''
}

/**
 * The place as one readable line, narrowest to widest: "Špilberk, Brno, Česko".
 * Empty when nothing was resolved, so the caller can say so in its own words.
 *
 * A level that repeats the one below it is dropped — a village is its own named
 * place often enough that "Veselice, Veselice, Česko" would be the common case,
 * not the exception. The **region** is deliberately left out: "Špilberk, Brno,
 * Jihomoravský kraj, Česko" is an address, not a caption, and the full hierarchy
 * is a row apiece in the technical details for anyone who wants it.
 */
export function placeLabel(place: PlaceNames | undefined): string {
  if (place === undefined) {
    return ''
  }
  const named: string[] = []
  for (const name of levels(place)) {
    if (name !== '' && !named.includes(name)) {
      named.push(name)
    }
  }
  return named.join(', ')
}
