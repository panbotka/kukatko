import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { buildAssign } from '../lib/faceAssign'
import { readingOrder } from '../lib/faceGeometry'
import { isNamed } from '../lib/faceState'
import { bulkConfirmations, type FaceConfirmation, topSuggestion } from '../lib/faceSuggestion'
import { ALBUM_DEFAULTS, viewToParams } from '../lib/libraryView'
import {
  assignFace,
  type FaceView,
  fetchFaces,
  type FacesResponse,
  type Suggestion,
} from '../services/people'
import { fetchPhotos, type Photo } from '../services/photos'

/**
 * The search that defines the queue: the photos of one album that still carry a
 * face nobody has named (`face:new`, see `internal/photos` `faceNewCond`), in the
 * album's own order — the same one the album page rests at, so the run walks the
 * event the way it happened.
 */
const QUEUE_QUERY = 'face:new'

/** How many photos one queue page asks for; the backend caps a page at 500. */
const QUEUE_PAGE = 200

/**
 * The album's own resting view, compiled to list params: the order the album page
 * rests at and its archive/filter defaults. Taken from {@link ALBUM_DEFAULTS}
 * rather than written out, so the run and the grid it was started from can never
 * disagree about what "the album's own order" is.
 */
const QUEUE_PARAMS = viewToParams(ALBUM_DEFAULTS)

/**
 * How many photos ahead of the current one have their faces fetched. Two is
 * enough to cover a confirm-and-advance at reading speed without asking the
 * backend for a run the reader may abandon after three photos.
 */
const PREFETCH = 2

/** Fetch lifecycle of the queue itself. */
type QueueState = { status: 'loading' } | { status: 'error' } | { status: 'ready'; photos: Photo[] }

/**
 * Where the run is. `loading` covers both the queue and the faces of the photo
 * being walked to; `done` is the end of the album, including an album that had
 * nothing to ask about in the first place.
 */
type ViewState =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'done' }
  | { status: 'ready'; index: number; photo: Photo; data: FacesResponse }

/**
 * Where the walk is heading. `forced` marks a position the reader asked for by
 * name — going back — which is shown as it is rather than skipped past when it
 * has nothing left to offer.
 */
interface Cursor {
  index: number
  forced: boolean
}

/** One unnamed face of the current photo, as the page asks about it. */
export interface AlbumFaceRow {
  /** The face itself. */
  face: FaceView
  /**
   * The number drawn on the face's box — its position among the photo's faces,
   * 1-based, so the row and the rectangle carry the same mark.
   */
  number: number
  /**
   * The identity confirming this row would apply, or null when nothing clears the
   * suggestion floor. A row without one is listed but cannot be acted on: it is
   * here so the reader can see the photo is not fully named, and it is left to the
   * photo detail, where a name can be typed.
   */
  suggestion: Suggestion | null
}

/** What {@link useAlbumFaces} exposes to the page. */
export interface UseAlbumFacesResult {
  /** Queue/photo lifecycle; see {@link ViewState}. */
  status: 'loading' | 'error' | 'done' | 'ready'
  /** How many photos the album put in the queue. */
  total: number
  /** 1-based position of the current photo in that queue, 0 when there is none. */
  position: number
  /** The photo on screen, or null while loading / when the run is over. */
  photo: Photo | null
  /** Every face of that photo in reading order — what the overlay draws. */
  faces: FaceView[]
  /** The rows to ask about: unnamed, not dismissed this run. */
  rows: AlbumFaceRow[]
  /** What one "confirm all" would name, subject to the one-person-once rule. */
  batch: FaceConfirmation[]
  /** True while confirmations are in flight; the controls stand still. */
  busy: boolean
  /** How many confirmations failed on the current photo. Reset on every move. */
  failed: number
  /** True when going back has somewhere to go. */
  canGoBack: boolean
  /** Names one face with the identity its row offers. */
  confirm: (face: FaceView, subject: Suggestion) => void
  /** Names every face of {@link batch}, one request at a time. */
  confirmAll: () => void
  /** Drops one row for this run only — writes nothing, records nothing. */
  dismiss: (faceIndex: number) => void
  /** Moves on to the next photo with something to ask. */
  next: () => void
  /** Returns to the previous photo this run actually showed. */
  back: () => void
}

/** The key a dismissed row is remembered under: one photo's one face. */
function dismissKey(photoUid: string, faceIndex: number): string {
  return `${photoUid}#${String(faceIndex)}`
}

/**
 * The rows one photo raises: its unnamed faces, minus the ones dismissed this
 * run, each carrying the identity its confirm button would apply.
 */
function photoRows(
  photoUid: string,
  faces: FaceView[],
  dismissed: ReadonlySet<string>,
): AlbumFaceRow[] {
  return faces
    .map((face, position) => ({ face, number: position + 1, suggestion: topSuggestion(face) }))
    .filter(
      (row) => !isNamed(row.face) && !dismissed.has(dismissKey(photoUid, row.face.face_index)),
    )
}

/**
 * Whether a photo is worth stopping on: at least one row can actually be
 * confirmed. A photo whose remaining faces carry no offered suggestion asks the
 * reader a question they cannot answer here, so the run walks past it.
 */
function worthShowing(
  photoUid: string,
  faces: FaceView[],
  dismissed: ReadonlySet<string>,
): boolean {
  return photoRows(photoUid, faces, dismissed).some((row) => row.suggestion !== null)
}

/**
 * Loads every photo of the album that still has an unnamed face, page by page.
 * The whole queue is fetched up front: it is what the progress counter counts,
 * and a run of a few hundred photos is a handful of requests made once.
 */
async function loadQueue(albumUid: string, signal: AbortSignal): Promise<Photo[]> {
  const photos: Photo[] = []
  for (;;) {
    const page = await fetchPhotos(
      {
        ...QUEUE_PARAMS,
        album: albumUid,
        q: QUEUE_QUERY,
        limit: QUEUE_PAGE,
        offset: photos.length,
      },
      signal,
    )
    photos.push(...page.photos)
    if (page.photos.length < QUEUE_PAGE || photos.length >= page.total) {
      return photos
    }
  }
}

/**
 * The album face-tagging run: a queue of the album's photos that still have
 * somebody unnamed on them, walked one photo at a time, each face offered with
 * the person the recogniser thinks it is.
 *
 * **The queue is fixed when the page opens.** It is a page of `GET /photos`
 * scoped to the album and filtered to `face:new`, in the album's own order — no
 * new endpoint, and the same question the album header's badge asks. Confirming
 * faces would take photos out of that search, so re-running it mid-session would
 * renumber the progress counter under the reader; the run keeps the list it
 * started with and simply reaches the end of it.
 *
 * **Faces arrive just in time.** `GET /photos/{uid}/faces` is fetched for the
 * photo being walked to and, once it has settled, for the next {@link PREFETCH}
 * behind it, so the common rhythm — confirm, confirm, next — never waits. Every
 * response is cached for the whole run, which is what lets going back show the
 * photo as the reader left it.
 *
 * **Photos nobody can answer for are skipped without being shown.** Walking
 * forward, the hook loads faces until it finds a photo where at least one unnamed
 * face carries an offered suggestion; the rest are stepped over silently (a photo
 * whose faces cannot even be fetched among them). Going *back* is exempt: the
 * reader asked for that photo by name, and bouncing off it would make the control
 * useless right after the confirmation it exists to undo the view of.
 *
 * **Dismissing writes nothing.** It drops the row for this run — deliberately
 * unlike the review game's "no", which stores a rejection. The face is offered
 * again the next time the album is walked, because a face passed over in a hurry
 * is not evidence about who it is.
 *
 * **A failed confirmation leaves the face unnamed.** Names are applied to the
 * cached faces only once the server has taken them, so a refused request leaves
 * the row exactly where it was; the tally is reported and the run goes on.
 */
export function useAlbumFaces(albumUid: string): UseAlbumFacesResult {
  const [queue, setQueue] = useState<QueueState>({ status: 'loading' })
  const [view, setView] = useState<ViewState>({ status: 'loading' })
  const [cursor, setCursor] = useState<Cursor>({ index: 0, forced: false })
  const [history, setHistory] = useState<number[]>([])
  const [dismissed, setDismissed] = useState<ReadonlySet<string>>(() => new Set())
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(0)

  // Read by the walk and by the actions, which must see the newest set without
  // re-running (an effect that re-walked on every dismissal would advance the
  // reader off the photo they are still working on).
  const dismissedRef = useRef(dismissed)
  dismissedRef.current = dismissed
  // The index on screen and the trail behind it, for the callbacks: `next` and
  // `back` are stable, so they cannot close over the render's values.
  const shownRef = useRef<number | null>(null)
  const historyRef = useRef(history)
  historyRef.current = history

  // Faces per photo for the whole run, and the requests in flight, so the walk
  // and the prefetch never ask for the same photo twice.
  // A run in flight, read between two awaits so a second click cannot start a
  // parallel walk over the same faces.
  const running = useRef(false)
  const cache = useRef(new Map<string, FacesResponse | null>())
  const inflight = useRef(new Map<string, Promise<FacesResponse | null>>())

  const loadFaces = useCallback(async (photoUid: string): Promise<FacesResponse | null> => {
    const cached = cache.current.get(photoUid)
    if (cached !== undefined) {
      return cached
    }
    const pending = inflight.current.get(photoUid)
    if (pending !== undefined) {
      return pending
    }
    const request = fetchFaces(photoUid)
      // Reading order the moment it lands, exactly as `useFaces` does it: the row
      // numbers and the numbers on the boxes are positions in this list, so they
      // have to be the order the eye crosses the photo.
      .then((data): FacesResponse => ({ ...data, faces: readingOrder(data.faces) }))
      .catch((): null => null)
      .then((data) => {
        cache.current.set(photoUid, data)
        inflight.current.delete(photoUid)
        return data
      })
    inflight.current.set(photoUid, request)
    return request
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setQueue({ status: 'loading' })
    setView({ status: 'loading' })
    setCursor({ index: 0, forced: false })
    setHistory([])
    setDismissed(new Set())
    cache.current = new Map()
    inflight.current = new Map()
    loadQueue(albumUid, controller.signal)
      .then((photos) => {
        setQueue({ status: 'ready', photos })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setQueue({ status: 'error' })
        setView({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [albumUid])

  // The walk: settle on the cursor's photo, or on the first one after it that has
  // something to ask. Re-runs only on a queue or a cursor change — the dismissals
  // it consults are read through their ref for exactly that reason.
  useEffect(() => {
    if (queue.status !== 'ready') {
      return undefined
    }
    const photos = queue.photos
    let cancelled = false
    setView({ status: 'loading' })
    setFailed(0)

    // A named function rather than an inline IIFE, so `cancelled` is still a
    // `boolean` to the type checker after the awaits below.
    async function walk() {
      let at = cursor.index
      while (at < photos.length) {
        const photo = photos[at]
        const data = await loadFaces(photo.uid)
        if (cancelled) {
          return
        }
        if (
          data !== null &&
          (cursor.forced || worthShowing(photo.uid, data.faces, dismissedRef.current))
        ) {
          shownRef.current = at
          setView({ status: 'ready', index: at, photo, data })
          return
        }
        at += 1
      }
      if (cancelled) {
        return
      }
      shownRef.current = null
      setView({ status: 'done' })
    }

    void walk()
    return () => {
      cancelled = true
    }
  }, [queue, cursor, loadFaces])

  // Decode the faces of the photos behind the current one, so confirming the last
  // row and moving on is one frame rather than one request.
  const settled = view.status === 'ready' ? view.index : null
  useEffect(() => {
    if (queue.status !== 'ready' || settled === null) {
      return
    }
    for (const photo of queue.photos.slice(settled + 1, settled + 1 + PREFETCH)) {
      void loadFaces(photo.uid)
    }
  }, [queue, settled, loadFaces])

  const next = useCallback(() => {
    const at = shownRef.current
    if (at === null) {
      return
    }
    setHistory([...historyRef.current, at])
    setCursor({ index: at + 1, forced: false })
  }, [])

  const back = useCallback(() => {
    const trail = historyRef.current
    if (trail.length === 0) {
      return
    }
    setHistory(trail.slice(0, -1))
    setCursor({ index: trail[trail.length - 1], forced: true })
  }, [])

  /**
   * Writes a confirmed name into the run's own copy of the photo's faces — the
   * cache and, when it is the photo on screen, the view. Called only after the
   * server has accepted the assignment, so what the reader sees is what is
   * stored.
   */
  const applyName = useCallback((photoUid: string, faceIndex: number, subject: Suggestion) => {
    const cached = cache.current.get(photoUid)
    if (cached === undefined || cached === null) {
      return
    }
    const faces = cached.faces.map((face) =>
      face.face_index === faceIndex
        ? {
            ...face,
            subject_uid: subject.subject_uid,
            subject_name: subject.subject_name,
            action: 'already_done' as const,
            suggestions: [],
          }
        : face,
    )
    const updated: FacesResponse = { ...cached, faces }
    cache.current.set(photoUid, updated)
    setView((current) =>
      current.status === 'ready' && current.photo.uid === photoUid
        ? { ...current, data: updated }
        : current,
    )
  }, [])

  /**
   * Whether the photo still has a question on it. Read from the cache rather than
   * from the view, because it is asked immediately after a batch has patched it.
   */
  const stillAsking = useCallback((photoUid: string): boolean => {
    const cached = cache.current.get(photoUid)
    return cached != null && worthShowing(photoUid, cached.faces, dismissedRef.current)
  }, [])

  const runConfirmations = useCallback(
    (photoUid: string, targets: FaceConfirmation[]) => {
      // The ref, not `busy`: two clicks inside one frame both read the state as
      // false, and confirming the same face twice would draw a second marker.
      if (running.current || targets.length === 0) {
        return
      }
      running.current = true
      setBusy(true)
      void (async () => {
        let failures = 0
        for (const target of targets) {
          try {
            await assignFace(
              photoUid,
              buildAssign(target.face, { subject_uid: target.subject.subject_uid }),
            )
            applyName(photoUid, target.face.face_index, target.subject)
          } catch {
            // One face the server refused is not a reason to abandon the rest; it
            // stays unnamed and is counted below.
            failures += 1
          }
        }
        running.current = false
        setBusy(false)
        if (failures > 0) {
          setFailed((count) => count + failures)
        }
        if (!stillAsking(photoUid)) {
          next()
        }
      })()
    },
    [applyName, next, stillAsking],
  )

  const confirm = useCallback(
    (face: FaceView, subject: Suggestion) => {
      if (view.status !== 'ready') {
        return
      }
      runConfirmations(view.photo.uid, [{ face, subject }])
    },
    [runConfirmations, view],
  )

  const rows = useMemo(
    () => (view.status === 'ready' ? photoRows(view.photo.uid, view.data.faces, dismissed) : []),
    [dismissed, view],
  )
  // The batch is computed from the rows, not from the photo: a face dismissed
  // this run is not offered again by the button that names everything.
  const batch = useMemo(() => bulkConfirmations(rows.map((row) => row.face)), [rows])

  const confirmAll = useCallback(() => {
    if (view.status !== 'ready') {
      return
    }
    runConfirmations(view.photo.uid, batch)
  }, [batch, runConfirmations, view])

  const dismiss = useCallback(
    (faceIndex: number) => {
      if (view.status !== 'ready') {
        return
      }
      const photoUid = view.photo.uid
      const withRow = new Set(dismissedRef.current)
      withRow.add(dismissKey(photoUid, faceIndex))
      dismissedRef.current = withRow
      setDismissed(withRow)
      if (!stillAsking(photoUid)) {
        next()
      }
    },
    [next, stillAsking, view],
  )

  return {
    status: view.status,
    total: queue.status === 'ready' ? queue.photos.length : 0,
    position: view.status === 'ready' ? view.index + 1 : 0,
    photo: view.status === 'ready' ? view.photo : null,
    faces: view.status === 'ready' ? view.data.faces : [],
    rows,
    batch,
    busy,
    failed,
    canGoBack: history.length > 0,
    confirm,
    confirmAll,
    dismiss,
    next,
    back,
  }
}

/**
 * How many photos of one album still have a face nobody has named — the number
 * the album header's "Faces" button carries, and null while it is unknown (still
 * loading, or the request failed).
 *
 * It is the same search the run's queue is built from, asked for its `total`
 * alone (`limit=1`), so the badge and the queue can never disagree about what is
 * left to do.
 */
export function useAlbumFaceCount(albumUid: string): number | null {
  const [count, setCount] = useState<number | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    setCount(null)
    fetchPhotos({ album: albumUid, q: QUEUE_QUERY, limit: 1 }, controller.signal)
      .then((page) => {
        setCount(page.total)
      })
      .catch(() => {
        // A count nobody could fetch simply hides the button; the run is still
        // reachable by its address, and an error banner about a badge would be
        // noise on a page that loaded perfectly well.
      })
    return () => {
      controller.abort()
    }
  }, [albumUid])

  return count
}
