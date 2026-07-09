import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ListGroup from 'react-bootstrap/ListGroup'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '../components/EmptyState'
import { FilterBar } from '../components/library/FilterBar'
import { GridSkeleton } from '../components/library/GridSkeleton'
import { PhotoGrid } from '../components/library/PhotoGrid'
import { useScopedPhotos } from '../hooks/useScopedPhotos'
import { LIBRARY_DEFAULTS, type LibraryView, viewToParams } from '../lib/libraryView'
import { useUrlState } from '../lib/urlState'
import { fetchPlaces, type PlaceCountry } from '../services/places'

/**
 * URL-encoded view state for the Places page: the library filter/sort fields
 * plus the place drill (`country`, `city`). A type alias (intersection) so it
 * keeps the implicit index signature the urlState `Record<string, string>`
 * constraint requires; the whole view round-trips through the query string, so
 * Back/Forward restore the exact drill and filters.
 */
type PlacesView = LibraryView & {
  country: string
  city: string
}

/** Default Places view: no place selected, library defaults for the rest. */
const PLACES_DEFAULTS: PlacesView = {
  ...LIBRARY_DEFAULTS,
  country: '',
  city: '',
}

/** Fetch lifecycle of the place hierarchy (countries + their cities). */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; countries: PlaceCountry[] }

/**
 * Places page: browse the library by location. Lists countries with photo
 * counts; selecting a country reveals its cities, and selecting a city shows the
 * photo grid scoped to that place (reusing {@link FilterBar} + {@link PhotoGrid},
 * exactly like an album or label gallery). The place drill and the filters live
 * in the URL (`/places?country=…&city=…`), so Back/Forward step through the
 * drill. The country → city hierarchy is fetched once; the grid loads only once a
 * city is chosen.
 */
export function PlacesPage() {
  const { t } = useTranslation()
  const [state, setState] = useState<State>({ status: 'loading' })
  // Bumped to re-run the hierarchy fetch after an error retry.
  const [reloadKey, setReloadKey] = useState(0)

  const [view, setView] = useUrlState<PlacesView>(PLACES_DEFAULTS)
  const { country, city } = view

  const params = useMemo(() => viewToParams(view), [view])
  const scope = useMemo(() => ({ country, city }), [country, city])
  // The grid is only meaningful once a city (within a country) is selected.
  const gridEnabled = country !== '' && city !== ''
  const { photos, total, status, loadingMore, moreError, loadMore, retry } = useScopedPhotos(
    scope,
    params,
    { enabled: gridEnabled },
  )

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchPlaces(undefined, controller.signal)
      .then((countries) => {
        setState({ status: 'ready', countries })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [reloadKey])

  const selectCountry = useCallback(
    (name: string) => {
      setView({ country: name, city: '' })
    },
    [setView],
  )
  const selectCity = useCallback(
    (name: string) => {
      setView({ city: name })
    },
    [setView],
  )
  const clearPlace = useCallback(() => {
    setView({ country: '', city: '' })
  }, [setView])
  const clearCity = useCallback(() => {
    setView({ city: '' })
  }, [setView])

  const selectedCountry = useMemo(
    () =>
      state.status === 'ready' ? state.countries.find((c) => c.country === country) : undefined,
    [state, country],
  )

  return (
    <>
      <div className="d-flex align-items-center gap-2 mb-3 flex-wrap">
        <h1 className="kk-page-title mb-0">{t('places.title')}</h1>
      </div>

      {/* Breadcrumb drill: Places / Country / City, each level clickable. */}
      {(country !== '' || city !== '') && (
        <nav aria-label={t('places.breadcrumb')} className="mb-3">
          <Button variant="link" className="p-0 text-decoration-none" onClick={clearPlace}>
            {t('places.title')}
          </Button>
          {country !== '' && (
            <>
              <span className="text-secondary mx-2">/</span>
              {city === '' ? (
                <span className="fw-semibold">{country}</span>
              ) : (
                <Button variant="link" className="p-0 text-decoration-none" onClick={clearCity}>
                  {country}
                </Button>
              )}
            </>
          )}
          {city !== '' && (
            <>
              <span className="text-secondary mx-2">/</span>
              <span className="fw-semibold">{city}</span>
            </>
          )}
        </nav>
      )}

      {state.status === 'loading' && <GridSkeleton />}

      {state.status === 'error' && (
        <Alert variant="danger" className="d-flex align-items-center justify-content-between">
          <span>{t('places.error')}</span>
          <Button
            variant="outline-light"
            size="sm"
            onClick={() => {
              setReloadKey((k) => k + 1)
            }}
          >
            {t('library.error.retry')}
          </Button>
        </Alert>
      )}

      {state.status === 'ready' && (
        <>
          {/* Level 1: countries. */}
          {country === '' &&
            (state.countries.length === 0 ? (
              <EmptyState title={t('places.empty.title')} hint={t('places.empty.hint')} />
            ) : (
              <ListGroup>
                {state.countries.map((c) => (
                  <ListGroup.Item
                    key={c.country}
                    action
                    onClick={() => {
                      selectCountry(c.country)
                    }}
                    className="d-flex justify-content-between align-items-center"
                  >
                    <span>{c.country}</span>
                    <Badge bg="secondary" pill>
                      {t('places.photoCount', { count: c.count })}
                    </Badge>
                  </ListGroup.Item>
                ))}
              </ListGroup>
            ))}

          {/* Level 2: cities of the selected country. */}
          {country !== '' &&
            city === '' &&
            (selectedCountry === undefined || selectedCountry.cities.length === 0 ? (
              <EmptyState title={t('places.noCities.title')} hint={t('places.noCities.hint')} />
            ) : (
              <ListGroup>
                {selectedCountry.cities.map((c) => (
                  <ListGroup.Item
                    key={c.city}
                    action
                    onClick={() => {
                      selectCity(c.city)
                    }}
                    className="d-flex justify-content-between align-items-center"
                  >
                    <span>{c.city}</span>
                    <Badge bg="secondary" pill>
                      {t('places.photoCount', { count: c.count })}
                    </Badge>
                  </ListGroup.Item>
                ))}
              </ListGroup>
            ))}

          {/* Level 3: the photo grid scoped to the selected place. */}
          {gridEnabled && (
            <>
              <FilterBar view={view} onChange={setView} total={total} />

              {status === 'loading' && <GridSkeleton />}

              {status === 'error' && (
                <Alert
                  variant="danger"
                  className="d-flex align-items-center justify-content-between"
                >
                  <span>{t('library.error.load')}</span>
                  <Button variant="outline-light" size="sm" onClick={retry}>
                    {t('library.error.retry')}
                  </Button>
                </Alert>
              )}

              {status === 'ready' && photos.length === 0 && (
                <EmptyState
                  title={t('places.emptyPhotos.title')}
                  hint={t('places.emptyPhotos.hint')}
                />
              )}

              {status === 'ready' && photos.length > 0 && (
                <PhotoGrid
                  photos={photos}
                  loadingMore={loadingMore}
                  moreError={moreError}
                  onEndReached={loadMore}
                  onRetry={retry}
                />
              )}
            </>
          )}
        </>
      )}
    </>
  )
}
