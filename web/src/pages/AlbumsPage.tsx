import { useEffect, useMemo, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { AlbumEditModal } from '../components/organize/AlbumEditModal'
import { AlbumFilterBar } from '../components/organize/AlbumFilterBar'
import { AlbumTile } from '../components/organize/AlbumTile'
import { TileGridSkeleton } from '../components/Skeleton'
import { TileGrid } from '../components/TileGrid'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useReloadKey } from '../hooks/useReloadKey'
import {
  type AlbumsView,
  albumBrowseOptions,
  ALBUMS_DEFAULTS,
  ALBUMS_SHOW_EMPTY,
  browseAlbums,
} from '../lib/albumBrowse'
import { planAlbumCovers } from '../lib/albumCovers'
import { useUrlState } from '../lib/urlState'
import { type AlbumSummary, fetchAlbums } from '../services/organize'

/** Fetch lifecycle of the albums list. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; albums: AlbumSummary[] }

/** No albums at all, so the tab counts have nothing to report. */
const NO_ALBUMS: AlbumSummary[] = []

/**
 * The albums index: a responsive, virtualized grid of album cards (cover, title,
 * count), each linking to its detail page.
 *
 * The API returns every album in one list, but more than half of them are
 * machine-made — month folders, moments, place albums — and in one pile they
 * bury the albums somebody actually created. So the page splits them by `type`
 * into four sections (Moje alba · Podle měsíce · Momenty · Místa), opens on the
 * hand-made ones, hides the albums holding no photos, and offers a name search
 * and an ordering. All of it lives in the URL, so Back steps through the
 * sections and a link carries the exact view. Nothing here is stored: the
 * machine-made English titles are only *rendered* in Czech (see
 * `i18n/albumNames`).
 *
 * The covers are planned for the whole section at once (`lib/albumCovers`)
 * rather than tile by tile, because overlapping albums share their newest photo
 * and a per-tile choice drew it on every one of them.
 *
 * Editors and admins get a create button; the modal refetches the grid on
 * success. Mutation controls are hidden from viewers.
 */
export function AlbumsPage() {
  const { t, i18n } = useTranslation()
  useDocumentTitle(t('albums.title'))
  const { canWrite } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [creating, setCreating] = useState(false)
  const [reloadKey, reload] = useReloadKey()
  const [view, setView] = useUrlState<AlbumsView>(ALBUMS_DEFAULTS)

  useEffect(() => {
    const controller = new AbortController()
    // No reset to 'loading' here: the initial state already is, and on a reload
    // the grid stays up until the fresh list arrives instead of flashing a spinner.
    fetchAlbums(controller.signal)
      .then((albums) => {
        setState({ status: 'ready', albums })
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

  const albums = state.status === 'ready' ? state.albums : NO_ALBUMS
  const language = i18n.language
  const { visible, counts, filteredOut } = useMemo(
    () => browseAlbums(albums, albumBrowseOptions(view, language)),
    [albums, view, language],
  )
  // Planned once for the whole section, not per tile: overlapping albums share
  // their newest photo, and a cover picked tile by tile would draw it on every
  // one of them. Memoized against `visible` so the grid — which is virtualized,
  // and so mounts a tile afresh every time it scrolls back — keeps handing that
  // tile the same cover.
  const covers = useMemo(() => planAlbumCovers(visible), [visible])

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('albums.title')}</h1>
        {canWrite && (
          <Button
            variant="primary"
            onClick={() => {
              setCreating(true)
            }}
          >
            {t('albums.create')}
          </Button>
        )}
      </div>

      {state.status === 'loading' && (
        <TileGridSkeleton label={t('albums.loading')} minTile={160} captionLines={2} />
      )}

      {state.status === 'error' && <ErrorState title={t('albums.error')} onRetry={reload} />}

      {state.status === 'ready' && albums.length === 0 && (
        <EmptyState title={t('albums.empty.title')} hint={t('albums.empty.hint')} />
      )}

      {state.status === 'ready' && albums.length > 0 && (
        <>
          <AlbumFilterBar view={view} onChange={setView} counts={counts} />

          {visible.length === 0 && (
            <EmptyState
              title={t('albums.noMatches.title')}
              hint={
                filteredOut > 0 ? t('albums.noMatches.hintFiltered') : t('albums.noMatches.hint')
              }
            />
          )}

          {visible.length > 0 && (
            // Virtualized (see TileGrid) so a large collection keeps the DOM — and the
            // cover loads it starts — bounded by the viewport, not by the album count.
            // The geometry matches the skeleton above, so the grid doesn't shift when
            // the data lands.
            <TileGrid
              items={visible}
              itemKey={(album) => album.uid}
              renderItem={(album) => <AlbumTile album={album} cover={covers.get(album.uid)} />}
              minTile={160}
              gap={12}
            />
          )}
        </>
      )}

      {canWrite && (
        <AlbumEditModal
          show={creating}
          onHide={() => {
            setCreating(false)
          }}
          onSaved={() => {
            // Refetch rather than appending the new album: the server ranks albums
            // by their newest photo, and a fresh (empty) one ranks with the undated
            // ones at the end, where random uids decide the order among them. Only
            // the server knows where it lands, so ask it instead of guessing.
            reload()
            // A brand-new album holds no photos and carries no search term, so
            // the default view would swallow it whole. Show the section it landed
            // in, with empty albums visible, rather than let it vanish on save.
            setView({ ...ALBUMS_DEFAULTS, empty: ALBUMS_SHOW_EMPTY })
            setCreating(false)
          }}
        />
      )}
    </>
  )
}
