import { useCallback, useMemo } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { Slideshow } from '../components/slideshow/Slideshow'
import { usePaginatedPhotos } from '../hooks/usePaginatedPhotos'
import { useSlideshow } from '../hooks/useSlideshow'
import { useSlideshowSettings } from '../hooks/useSlideshowSettings'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { readUrlState } from '../lib/urlState'
import { fetchPhotos, type PhotoListParams } from '../services/photos'

/**
 * The fullscreen slideshow route (`/slideshow`). It reads the source scope
 * (`?album=` / `?label=` / none) and the library filters/sort from the URL —
 * exactly the state a grid encodes — so the slideshow plays the same photos in
 * the same order as the view it was launched from, and Back returns there. It
 * pages the catalogue through {@link usePaginatedPhotos} (loading more as the
 * cursor advances) and renders loading / empty / error states before handing the
 * loaded photos to the {@link Slideshow} stage. Rendered outside the app layout
 * shell so it can occupy the whole viewport.
 */
export function SlideshowPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()

  const album = searchParams.get('album') ?? ''
  const label = searchParams.get('label') ?? ''

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
    (p: PhotoListParams, signal: AbortSignal) => fetchPhotos(p, signal),
    [],
  )
  const { photos, status, loadingMore, hasMore, loadMore, retry } = usePaginatedPhotos(
    params,
    fetcher,
  )

  const { settings, setEffect, setIntervalMs } = useSlideshowSettings()
  const { index, playing, next, prev, toggle } = useSlideshow({
    length: photos.length,
    hasMore,
    intervalMs: settings.intervalMs,
    onLoadMore: loadMore,
  })

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
    } else {
      void navigate('/library')
    }
  }, [navigate, album, label])

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
        <Alert variant="danger" className="d-flex align-items-center gap-3 mb-0">
          <span>{t('slideshow.error.load')}</span>
          <Button variant="outline-light" size="sm" onClick={retry}>
            {t('slideshow.error.retry')}
          </Button>
          <Button variant="outline-light" size="sm" onClick={exit}>
            {t('slideshow.back')}
          </Button>
        </Alert>
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
