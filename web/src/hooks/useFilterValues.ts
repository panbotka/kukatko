import { useEffect, useState } from 'react'

import { type FilterValue, type ValueFacet } from '../lib/queryLanguage'
import { fetchAlbums, fetchLabels } from '../services/organize'
import { fetchSubjects } from '../services/people'

/**
 * How long a `key:` token must sit still before its value list is fetched.
 *
 * The list is catalogue-wide and cached per facet, so at most one request per
 * facet is ever made — but a reader who types `person:` and immediately deletes
 * it should not have caused even that one. The delay is short enough that the
 * dropdown still feels like part of the keystroke.
 */
const LOAD_DEBOUNCE_MS = 200

/** Never-changing empty list, so an unloaded facet returns a stable identity. */
const NO_VALUES: FilterValue[] = []

/** The loaded value lists, keyed by facet; a missing key means "not loaded yet". */
type FacetCache = Partial<Record<ValueFacet, FilterValue[]>>

/**
 * Loads the candidate values of one facet, for the search box's value
 * autocomplete: the album titles, the label names or the people's names, each
 * with the photo tally that ranks it.
 *
 * `facet` is the facet the trailing `key:` token needs values for, or null when
 * the caret is nowhere near one. Nothing is fetched until a facet is actually
 * asked for, and each facet is fetched at most once per mount — the lists are
 * catalogue-wide, so filtering as the reader types is pure client-side work and
 * no keystroke can cost a request. The fetch itself is debounced on top of that,
 * so a `key:` typed and deleted again costs nothing at all.
 *
 * A failed load leaves the facet empty and uncached: the dropdown offers nothing
 * (which reads as "no matches", not as a broken box) and a later visit to the
 * same key tries again.
 */
export function useFilterValues(
  facet: ValueFacet | null,
  debounceMs: number = LOAD_DEBOUNCE_MS,
): FilterValue[] {
  const [cache, setCache] = useState<FacetCache>({})

  useEffect(() => {
    if (facet === null || cache[facet] !== undefined) {
      return
    }
    const controller = new AbortController()
    const timer = setTimeout(() => {
      loadFacetValues(facet, controller.signal)
        .then((values) => {
          setCache((prev) => ({ ...prev, [facet]: values }))
        })
        .catch(() => {
          // Silent, and left uncached so returning to the same key retries.
        })
    }, debounceMs)
    return () => {
      clearTimeout(timer)
      controller.abort()
    }
  }, [facet, cache, debounceMs])

  return (facet === null ? undefined : cache[facet]) ?? NO_VALUES
}

/**
 * Fetches one facet's values and projects them onto the shape the autocomplete
 * ranks and inserts: the name a query must spell, and how many photos carry it.
 *
 * Album titles are taken raw rather than through `albumDisplayTitle`, because the
 * query matches the stored title — a localised name in the dropdown would insert
 * a value that finds nothing. People are counted by photos, not markers: the
 * count is there to rank "who appears most", and a person twice on one photo is
 * not twice as prominent.
 */
async function loadFacetValues(facet: ValueFacet, signal: AbortSignal): Promise<FilterValue[]> {
  switch (facet) {
    case 'album': {
      const albums = await fetchAlbums(signal)
      return albums.map((album) => ({ name: album.title, count: album.photo_count }))
    }
    case 'label': {
      const labels = await fetchLabels(signal)
      return labels.map((label) => ({ name: label.name, count: label.photo_count }))
    }
    case 'person': {
      const subjects = await fetchSubjects(signal)
      return subjects.map((subject) => ({ name: subject.name, count: subject.photo_count }))
    }
  }
}
