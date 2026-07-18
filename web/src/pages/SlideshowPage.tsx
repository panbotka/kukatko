import { useCallback, useEffect, useMemo } from 'react'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Slideshow, SLIDESHOW_PREVIEW_SIZE } from '../components/slideshow/Slideshow'
import { useImagePreloader } from '../hooks/useImagePreloader'
import { usePaginatedPhotos } from '../hooks/usePaginatedPhotos'
import { preloadWindow, type SlideReadiness, useSlideshow } from '../hooks/useSlideshow'
import { useSlideshowSettings } from '../hooks/useSlideshowSettings'
import { LIBRARY_DEFAULTS, LIBRARY_PATH, type LibraryView, viewToParams } from '../lib/libraryView'
import { searchHref, type SearchView, toMode } from '../lib/searchView'
import { readUrlState } from '../lib/urlState'
import { fetchPhotos, type PhotoListParams, searchPhotos, thumbUrl } from '../services/photos'

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
 */
export function SlideshowPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  const album = searchParams.get('album') ?? ''
  const label = searchParams.get('label') ?? ''
  // A `mode` param means the slideshow was launched from the search page, so the
  // query has to be ranked by `GET /search` — listing the library with the same
  // `q` would only substring-match and play a different set of photos.
  const mode = searchParams.get('mode') ?? ''

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
    }),
    [view, album, label],
  )

  const fetcher = useCallback(
    (p: PhotoListParams, signal: AbortSignal) =>
      mode === '' ? fetchPhotos(p, signal) : searchPhotos(p, toMode(mode), signal),
    [mode],
  )
  const { photos, total, status, loadingMore, hasMore, loadMore, retry } = usePaginatedPhotos(
    params,
    fetcher,
    { key: mode },
  )

  const { settings, setEffect, setIntervalMs } = useSlideshowSettings()

  // The stage's image, at the exact size the stage renders it: a prefetch of
  // any other size would warm a different URL and leave the slide blank anyway.
  const { statusOf, prime } = useImagePreloader()
  const slideSrc = useCallback(
    (i: number): string =>
      i >= 0 && i < photos.length ? thumbUrl(photos[i].uid, SLIDESHOW_PREVIEW_SIZE) : '',
    [photos],
  )
  const readiness = useCallback(
    (i: number): SlideReadiness => {
      const src = slideSrc(i)
      return src === '' ? 'pending' : statusOf(src)
    },
    [slideSrc, statusOf],
  )

  const { index, playing, next, prev, toggle } = useSlideshow({
    length: photos.length,
    hasMore,
    intervalMs: settings.intervalMs,
    onLoadMore: loadMore,
    readiness,
  })

  // Keep a window of decoded slides around the cursor. Everything outside it is
  // released, so a long show does not accumulate every frame it has played.
  useEffect(() => {
    prime(preloadWindow(index, photos.length).map(slideSrc))
  }, [prime, index, photos.length, slideSrc])

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

  if (status === 'loading') {
    return (
      <div className="slideshow d-flex align-items-center justify-content-center">
        <Spinner animation="border" role="status" className="text-light">
          <span className="visually-hidden">{t('slideshow.loading')}</span>
        </Spinner>
      </div>
    )
  }

  if (status === 'error') {
    return (
      <div className="slideshow d-flex align-items-center justify-content-center p-4">
        <ErrorState
          title={t('slideshow.error.load')}
          onRetry={retry}
          action={
            <Button variant="outline-light" size="sm" onClick={exit}>
              {t('slideshow.back')}
            </Button>
          }
        />
      </div>
    )
  }

  if (photos.length === 0) {
    return (
      <div className="slideshow d-flex flex-column align-items-center justify-content-center text-light p-4">
        <EmptyState
          title={t('slideshow.empty.title')}
          hint={t('slideshow.empty.hint')}
          action={
            <Button variant="outline-light" size="sm" onClick={exit}>
              {t('slideshow.back')}
            </Button>
          }
        />
      </div>
    )
  }

  return (
    <Slideshow
      photos={photos}
      index={index}
      total={total}
      playing={playing}
      settings={settings}
      onNext={next}
      onPrev={prev}
      onToggle={toggle}
      onExit={exit}
      onEffectChange={setEffect}
      onIntervalChange={setIntervalMs}
      loadingMore={loadingMore}
    />
  )
}
