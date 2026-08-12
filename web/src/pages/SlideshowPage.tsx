import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Slideshow, SLIDESHOW_PREVIEW_SIZE } from '../components/slideshow/Slideshow'
import { SlideshowNotice } from '../components/slideshow/SlideshowNotice'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useImagePreloader } from '../hooks/useImagePreloader'
import { usePaginatedPhotos } from '../hooks/usePaginatedPhotos'
import { useSearchMode } from '../hooks/useSearchMode'
import { preloadWindow, type SlideReadiness, useSlideshow } from '../hooks/useSlideshow'
import { useSlideshowSettings } from '../hooks/useSlideshowSettings'
import { LIBRARY_DEFAULTS, LIBRARY_PATH, type LibraryView, viewToParams } from '../lib/libraryView'
import { searchHref, type SearchView, toMode } from '../lib/searchView'
import { extendSeen, newShuffleSeed, playlistOf } from '../lib/slideshowPlaylist'
import { readUrlState } from '../lib/urlState'
import {
  fetchPhotos,
  type Photo,
  type PhotoListParams,
  searchPhotos,
  thumbUrl,
} from '../services/photos'

/**
 * The fullscreen slideshow route (`/slideshow`). It reads the source scope
 * (`?album=` / `?label=` / `?mode=` for a search / none of them) and the library
 * filters/sort from the URL — exactly the state a grid encodes — so the
 * slideshow plays the same photos in the same order as the view it was launched
 * from, and Back returns there. It pages the catalogue through
 * {@link usePaginatedPhotos} (loading more as the cursor advances) and renders
 * loading / empty / error states before handing the loaded photos to the
 * {@link Slideshow} stage. Rendered outside the app layout shell so it can
 * occupy the whole viewport.
 *
 * It also owns the image preloading: a window of upcoming slides is decoded
 * ahead of the cursor through {@link useImagePreloader}, and its readiness feeds
 * back into {@link useSlideshow}, which holds the auto-advance until the next
 * image can actually be painted instead of flashing an empty stage.
 *
 * Shuffle is the one setting that reaches back to the server: it asks for
 * `sort=random` under a seed generated once for this show, because only the
 * server can reach the photos past the first page and only a seeded order can be
 * paged through without repeating or dropping any. Toggling it mid-show therefore
 * reloads the list — so the page keeps a per-pass record of what has already been
 * played and lays it in front of the reloaded photos
 * ({@link import('../lib/slideshowPlaylist').playlistOf}), which is what lets a
 * reorder resume the show on the photo it was showing instead of restarting it.
 */
export function SlideshowPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('slideshow.title'))
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  const album = searchParams.get('album') ?? ''
  const label = searchParams.get('label') ?? ''
  // A `mode` param means the slideshow was launched from the search page, so the
  // query has to be ranked by `GET /search` — listing the library with the same
  // `q` would only substring-match and play a different set of photos.
  const mode = searchParams.get('mode') ?? ''

  const { settings, update } = useSlideshowSettings()

  // The seed of this show's shuffle. Generated once and held for the life of the
  // show: the random order is a function of the photo uid and this string, so a
  // seed that changed between pages would hand out a different permutation each
  // time — pages overlapping, photos falling between them. Turning shuffle off
  // and on again therefore replays the same shuffle, which is the honest reading
  // of "this show's order".
  const [seed] = useState(newShuffleSeed)

  // Derive the same API params a grid would, from the URL view state plus scope.
  const view = useMemo<LibraryView>(
    () => readUrlState(searchParams, LIBRARY_DEFAULTS),
    [searchParams],
  )
  const params = useMemo<PhotoListParams>(
    () => ({
      ...viewToParams(view),
      album: album === '' ? undefined : album,
      label: label === '' ? undefined : label,
      // Shuffle is ordered by the server, not by reordering what happens to be
      // loaded: only the server can reach the photos beyond the first page, and
      // only a seeded order can be paged through without repeats or gaps.
      ...(settings.shuffle ? { sort: 'random' as const, seed } : {}),
    }),
    [view, album, label, settings.shuffle, seed],
  )

  // The same downgrade the search grid applies: with the embeddings sidecar
  // unreachable a semantic/hybrid replay would only wait for the sidecar timeout
  // before the backend answered it with full-text anyway — and a slideshow that
  // starts half a minute late reads as broken.
  const { mode: searchMode } = useSearchMode(toMode(mode))
  const fetcher = useCallback(
    (p: PhotoListParams, signal: AbortSignal) =>
      mode === '' ? fetchPhotos(p, signal) : searchPhotos(p, searchMode, signal),
    [mode, searchMode],
  )
  const { photos, total, status, loadingMore, hasMore, loadMore, retry } = usePaginatedPhotos(
    params,
    fetcher,
    { key: mode === '' ? '' : searchMode },
  )

  // The photos already played in this pass, and the ones carried across a change
  // of order. Turning shuffle on or off reloads the list from its first page in
  // the new order, which on its own would replay photos the reader has already
  // seen and could even drop the one on screen; carrying the seen ones in front
  // of the reloaded list — and filtering them out of it — reorders only what is
  // still to come. See `lib/slideshowPlaylist`.
  const [seen, setSeen] = useState<Photo[]>([])
  const [carried, setCarried] = useState<Photo[]>([])
  const playlist = useMemo(() => playlistOf(carried, photos), [carried, photos])

  // The stage's image, at the exact size the stage renders it: a prefetch of
  // any other size would warm a different URL and leave the slide blank anyway.
  const { statusOf, prime } = useImagePreloader()
  const slideSrc = useCallback(
    (i: number): string =>
      i >= 0 && i < playlist.length ? thumbUrl(playlist[i].uid, SLIDESHOW_PREVIEW_SIZE) : '',
    [playlist],
  )
  const readiness = useCallback(
    (i: number): SlideReadiness => {
      const src = slideSrc(i)
      return src === '' ? 'pending' : statusOf(src)
    },
    [slideSrc, statusOf],
  )

  const { index, playing, pass, next, prev, toggle } = useSlideshow({
    length: playlist.length,
    hasMore,
    intervalMs: settings.intervalMs,
    repeat: settings.repeat,
    onLoadMore: loadMore,
    readiness,
  })

  // Record what the show has played, so a change of order knows what not to
  // replay. It only ever grows within a pass: stepping back with ← revisits a
  // photo, it does not un-see it.
  useEffect(() => {
    setSeen((prev) => extendSeen(prev, playlist, index))
  }, [playlist, index])

  // The seen list as the two effects below read it — they must not re-run every
  // time it grows, only when the pass or the order actually changes.
  const seenRef = useRef(seen)
  seenRef.current = seen

  // A wrap starts a new pass, and with it a clean sheet: the photos of the round
  // just finished are fair game again, so a shuffle toggled during the second
  // pass reshuffles the whole list rather than finding everything already seen.
  // Guarded on the value, not the mount, so neither the first render nor
  // StrictMode's double-invoked effect wipes the pass that is running.
  const lastPass = useRef(pass)
  useEffect(() => {
    if (lastPass.current === pass) {
      return
    }
    lastPass.current = pass
    setSeen([])
    setCarried([])
  }, [pass])

  // Changing the order (shuffle on or off) hands the seen photos to the playlist
  // before the reloaded first page arrives, so the show keeps running on the
  // photo it was showing instead of blanking and starting over. The order is the
  // only dependency: no other setting reloads anything.
  const lastShuffle = useRef(settings.shuffle)
  useEffect(() => {
    if (lastShuffle.current === settings.shuffle) {
      return
    }
    lastShuffle.current = settings.shuffle
    setCarried(seenRef.current)
  }, [settings.shuffle])

  // How many photos the show plays. Reordering it does not change that, but the
  // reload reports a total of zero until its first page lands, so the last known
  // one stands in — otherwise the position readout would blink "5 of 5" the
  // moment shuffle is toggled, as if the show had shrunk to what is on screen.
  const lastTotal = useRef(0)
  if (total > 0) {
    lastTotal.current = total
  }
  const showLength = Math.max(total, lastTotal.current)

  // Keep a window of decoded slides around the cursor. Everything outside it is
  // released, so a long show does not accumulate every frame it has played.
  useEffect(() => {
    prime(preloadWindow(index, playlist.length).map(slideSrc))
  }, [prime, index, playlist.length, slideSrc])

  // Leave to the prior view (Back) when there is history; otherwise fall back to
  // the source view so a directly opened slideshow still has somewhere to go.
  const exit = useCallback(() => {
    if (window.history.length > 1) {
      void navigate(-1)
      return
    }
    if (album !== '') {
      void navigate(`/albums/${album}`)
    } else if (label !== '') {
      void navigate(`/labels/${label}`)
    } else if (mode !== '') {
      const searchView: SearchView = { ...view, mode: toMode(mode) }
      void navigate(searchHref(searchView))
    } else {
      void navigate(LIBRARY_PATH)
    }
  }, [navigate, album, label, mode, view])

  // Only a show with nothing to show waits behind the spinner. Toggling shuffle
  // reloads the list from its first page, and blanking the running show for that
  // would be a restart in all but name — the carried photos keep it on screen.
  //
  // The wait says what it is waiting for, out loud rather than only to a screen
  // reader: a black screen with a spinner and no words on it is how a slow first
  // page reads as a broken slideshow.
  if (status === 'loading' && playlist.length === 0) {
    return (
      <SlideshowNotice onClose={exit}>
        <Spinner animation="border" role="status" className="text-light" />
        <p className="mb-0">{t('slideshow.loading')}</p>
      </SlideshowNotice>
    )
  }

  if (status === 'error') {
    return (
      <SlideshowNotice onClose={exit}>
        <ErrorState
          title={t('slideshow.error.load')}
          onRetry={retry}
          action={
            <Button variant="outline-light" size="sm" onClick={exit}>
              {t('slideshow.back')}
            </Button>
          }
        />
      </SlideshowNotice>
    )
  }

  if (playlist.length === 0) {
    return (
      <SlideshowNotice onClose={exit}>
        <EmptyState
          title={t('slideshow.empty.title')}
          hint={t('slideshow.empty.hint')}
          action={
            <Button variant="outline-light" size="sm" onClick={exit}>
              {t('slideshow.back')}
            </Button>
          }
        />
      </SlideshowNotice>
    )
  }

  return (
    <Slideshow
      photos={playlist}
      index={index}
      total={showLength}
      playing={playing}
      settings={settings}
      onNext={next}
      onPrev={prev}
      onToggle={toggle}
      onExit={exit}
      onSettingsChange={update}
      loadingMore={loadingMore}
    />
  )
}
