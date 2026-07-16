import { useEffect, useState } from 'react'

import { type ComparePair, type ComparePhoto } from '../lib/duplicateCompare'
import { fetchFaces } from '../services/people'
import { fetchPhoto } from '../services/photos'

/** The two sides of a comparison, once both have loaded. */
export interface ComparePairData {
  left: ComparePhoto
  right: ComparePhoto
}

/** What {@link useComparePair} reports. */
export interface ComparePairState {
  data: ComparePairData | null
  loading: boolean
  error: boolean
}

/**
 * Loads everything the difference table needs about one pair: each photo's full
 * detail (dimensions, file, camera, capture date, albums, labels, place) and its
 * named people.
 *
 * People come from the faces endpoint rather than the photo record, which is why
 * this is four requests rather than two. It is worth it: "which one carries your
 * curation" is exactly the question the compare view exists to answer, and the
 * worse file is often the one with the people named on it. The four run
 * concurrently, and a failure on any of them fails the pair as a whole — half a
 * difference table is a table that lies by omission.
 */
export function useComparePair(pair: ComparePair | null): ComparePairState {
  const [data, setData] = useState<ComparePairData | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)

  useEffect(() => {
    if (pair === null) {
      setData(null)
      return
    }
    const controller = new AbortController()
    const { signal } = controller
    // Drop the previous pair's data before loading this one. The stage's images
    // come from the pair itself and repaint immediately, so leaving the old data in
    // place would show the new photos above the *old* photos' difference table —
    // and, because the keep buttons are gated on `data`, would let the user merge
    // this pair on the strength of the last pair's numbers.
    setData(null)
    setLoading(true)
    setError(false)

    async function load(left: string, right: string) {
      const [leftPhoto, rightPhoto, leftFaces, rightFaces] = await Promise.all([
        fetchPhoto(left, signal),
        fetchPhoto(right, signal),
        fetchFaces(left, signal),
        fetchFaces(right, signal),
      ])
      return {
        left: { photo: leftPhoto, people: namedPeople(leftFaces.faces) },
        right: { photo: rightPhoto, people: namedPeople(rightFaces.faces) },
      }
    }

    load(pair.leftUid, pair.rightUid)
      .then((next) => {
        setData(next)
        setLoading(false)
      })
      .catch(() => {
        // An abort is this effect being superseded, not a failure the user should
        // see: the next pair's load is already running.
        if (signal.aborted) {
          return
        }
        setError(true)
        setLoading(false)
      })

    return () => {
      controller.abort()
    }
  }, [pair])

  return { data, loading, error }
}

/** The distinct names of the people named on a photo; unnamed faces are ignored. */
function namedPeople(faces: { subject_name?: string }[]): string[] {
  const names = new Set<string>()
  for (const face of faces) {
    if (face.subject_name !== undefined && face.subject_name !== '') {
      names.add(face.subject_name)
    }
  }
  return [...names]
}
