import { useEffect, useMemo, useRef, useState } from 'react'

import { type AlbumCount, fetchAlbums, fetchLabels, type LabelCount } from '../services/organize'
import { fetchSubjects, type SubjectCount } from '../services/people'
import { fetchPhotoYears, type PhotoListParams, type YearBucket } from '../services/photos'

/**
 * The option lists behind the library's Period / Album / Label / Person facets.
 * Empty lists are the honest resting state: a fresh catalog has no years, and a
 * request that failed leaves the facet with nothing to offer rather than a stale
 * set.
 */
export interface LibraryFacets {
  /**
   * Years that hold photos, newest first, each with its count — what the period
   * filter groups into decades, so it only ever offers periods the library can
   * actually answer.
   */
  years: YearBucket[]
  /** Every album, ordered by title, each with its photo count. */
  albums: AlbumCount[]
  /** Every label, ordered by priority then name, each with its photo count. */
  labels: LabelCount[]
  /** Every subject (person/pet/other), ordered by name, each with its marker count. */
  subjects: SubjectCount[]
}

/** Reports whether a rejection is just this effect's own abort on cleanup. */
function isAbort(err: unknown): boolean {
  return err instanceof DOMException && err.name === 'AbortError'
}

/**
 * Loads the library's facet option lists.
 *
 * The year counts depend on the rest of the view (a year holds fewer photos once
 * a label is picked), so they are refetched whenever `params` changes and the
 * request carries the current filters. The **period** bounds are stripped: they
 * are what these counts are the options for, and a facet must not narrow its own
 * options — a picked decade would leave every other decade reading zero. Dropping
 * them also keeps the request identical while the reader tries one period after
 * another, so no refetch happens. Albums and labels are catalog-wide, so they
 * load once.
 *
 * A failed request leaves that list empty rather than surfacing an error: a
 * filter bar that cannot offer a facet is a degraded bar, not a broken page, and
 * the grid itself reports load failures. In-flight requests are aborted when
 * `params` changes or the caller unmounts, and every year response is checked
 * against the request that is current when it lands — aborting is a no-op once a
 * response is already on the wire — so a slow one cannot overwrite a newer one.
 *
 * Albums, labels and subjects are catalog-wide, so they load once on mount.
 *
 * `params` should be memoised by the caller (e.g. derived from URL state) so its
 * identity changes only when the query actually changes.
 */
export function useLibraryFacets(params: PhotoListParams): LibraryFacets {
  const [years, setYears] = useState<YearBucket[]>([])
  const [albums, setAlbums] = useState<AlbumCount[]>([])
  const [labels, setLabels] = useState<LabelCount[]>([])
  const [subjects, setSubjects] = useState<SubjectCount[]>([])

  // Drop the period so picking one does not re-request — and does not hollow out
  // — the very list it was picked from.
  const yearParams = useMemo<PhotoListParams>(
    () => ({ ...params, taken_after: '', taken_before: '' }),
    [params],
  )

  // Monotonic id of the newest year request. The abort on cleanup only helps
  // while the response is still in flight; a filter change that lands after the
  // previous counts have already arrived would otherwise flash the old numbers.
  const latestYears = useRef(0)

  useEffect(() => {
    const requestId = latestYears.current + 1
    latestYears.current = requestId
    const controller = new AbortController()
    fetchPhotoYears(yearParams, controller.signal)
      .then((res) => {
        if (latestYears.current !== requestId) {
          return
        }
        setYears(res.years)
      })
      .catch((err: unknown) => {
        if (isAbort(err) || latestYears.current !== requestId) {
          return
        }
        setYears([])
      })
    return () => {
      controller.abort()
    }
  }, [yearParams])

  useEffect(() => {
    const controller = new AbortController()
    fetchAlbums(controller.signal)
      .then((list) => {
        setAlbums(list)
      })
      .catch((err: unknown) => {
        if (isAbort(err)) {
          return
        }
        setAlbums([])
      })
    fetchLabels(controller.signal)
      .then((list) => {
        setLabels(list)
      })
      .catch((err: unknown) => {
        if (isAbort(err)) {
          return
        }
        setLabels([])
      })
    fetchSubjects(controller.signal)
      .then((list) => {
        setSubjects(list)
      })
      .catch((err: unknown) => {
        if (isAbort(err)) {
          return
        }
        setSubjects([])
      })
    return () => {
      controller.abort()
    }
  }, [])

  return useMemo(() => ({ years, albums, labels, subjects }), [years, albums, labels, subjects])
}
