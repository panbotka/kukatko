import { useEffect, useState } from 'react'

import { fetchSubject, type SubjectCount } from '../services/people'

/**
 * Resolves a subject uid to the person's name, for a control whose value comes
 * from the URL rather than from a click.
 *
 * The face-search page keeps its selection in `?subject=<uid>`, so a reload or a
 * Back lands with an identifier and nothing else — and the picker cannot show a
 * uid at somebody. The already-loaded list of subjects answers almost every case
 * for free; this hook is the fallback for the rest: the moment that list has
 * arrived without the uid in it (a subject held back from the listing, or one
 * created since the page loaded), it asks the subjects API for that single
 * person. A failed lookup resolves to null — the caller then shows its
 * placeholder, exactly as it does while the answer is still in flight.
 *
 * @param uid the subject to name, or null when nothing is selected
 * @param subjects the subject list already loaded by the page
 * @param subjectsLoading true while that list is still in flight (nothing is
 *   missing until it has arrived)
 * @returns the person's name, or null while it is unknown
 */
export function useSubjectName(
  uid: string | null,
  subjects: SubjectCount[],
  subjectsLoading: boolean,
): string | null {
  const listed = subjects.find((subject) => subject.uid === uid)?.name ?? null
  // Keyed by uid so a stale answer never labels the person picked since.
  const [fetched, setFetched] = useState<{ uid: string; name: string } | null>(null)

  useEffect(() => {
    if (uid === null || uid === '' || subjectsLoading || listed !== null) {
      return undefined
    }
    const controller = new AbortController()
    fetchSubject(uid, controller.signal)
      .then((subject) => {
        setFetched({ uid, name: subject.name })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setFetched(null)
      })
    return () => {
      controller.abort()
    }
  }, [uid, subjectsLoading, listed])

  return listed ?? (fetched?.uid === uid ? fetched.name : null)
}
