import { useEffect, useState } from 'react'

import { fetchSubjects, type SubjectCount } from '../services/people'

/** What {@link useSubjects} exposes: the people to pick from, and whether they arrived. */
export interface UseSubjectsResult {
  /** Every subject in the library, with its photo count. Empty while loading or on error. */
  subjects: SubjectCount[]
  /** True while the list is in flight. */
  loading: boolean
}

/**
 * Loads every subject, for a typeahead that names a face with someone who already
 * exists. Deliberately fetched by the component that needs it rather than at page
 * level: the faces panel mounts only once the user asks for faces, so browsing a
 * photo never pays for the list. A failure resolves to an empty list — the field
 * then only offers to create a new person, which still gets the job done.
 */
export function useSubjects(): UseSubjectsResult {
  const [subjects, setSubjects] = useState<SubjectCount[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    fetchSubjects(controller.signal)
      .then((list) => {
        setSubjects(list)
        setLoading(false)
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setSubjects([])
        setLoading(false)
      })
    return () => {
      controller.abort()
    }
  }, [])

  return { subjects, loading }
}
