import { useEffect, useMemo, useState } from 'react'

import { type AlbumCount, fetchAlbums, fetchLabels, type LabelCount } from '../services/organize'
import { fetchSubjects, type SubjectCount } from '../services/people'
import { fetchPhotoYears, type PhotoListParams, type YearBucket } from '../services/photos'

/**
 * The option lists behind the library's Year / Album / Label / Person facets.
 * Empty lists are the honest resting state: a fresh catalog has no years, and a
 * request that failed leaves the facet with nothing to offer rather than a stale
 * set.
 */
export interface LibraryFacets {
  /** Years that hold photos, newest first, each with its count. */
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
 * request carries the current filters. `year` itself is stripped: the backend
 * ignores it anyway — a facet must not narrow its own options — and omitting it
 * keeps the request identical while the reader switches years, so no refetch
 * happens. Albums and labels are catalog-wide, so they load once.
 *
 * A failed request leaves that list empty rather than surfacing an error: a
 * filter bar that cannot offer a facet is a degraded bar, not a broken page, and
 * the grid itself reports load failures. In-flight requests are aborted when
 * `params` changes or the caller unmounts, so a slow response cannot overwrite a
 * newer one.
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

  // Drop the year filter so selecting a year does not re-request the same list.
  const yearParams = useMemo<PhotoListParams>(() => ({ ...params, year: '' }), [params])

  useEffect(() => {
    const controller = new AbortController()
    fetchPhotoYears(yearParams, controller.signal)
      .then((res) => {
        setYears(res.years)
      })
      .catch((err: unknown) => {
        if (isAbort(err)) {
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
