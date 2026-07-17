import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { AlbumEditModal } from '../components/organize/AlbumEditModal'
import { AlbumTile } from '../components/organize/AlbumTile'
import { useReloadKey } from '../hooks/useReloadKey'
import { type AlbumSummary, fetchAlbums } from '../services/organize'

/** Fetch lifecycle of the albums list. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; albums: AlbumSummary[] }

/**
 * The albums index: a responsive grid of album cards (cover, title, count), each
 * linking to its detail page, newest album first as the server ranks them.
 * Editors and admins get a create button; the modal refetches the grid on
 * success. Mutation controls are hidden from viewers.
 */
export function AlbumsPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [creating, setCreating] = useState(false)
  const [reloadKey, reload] = useReloadKey()

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
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('albums.loading')}</span>
          </Spinner>
        </div>
      )}

      {state.status === 'error' && <Alert variant="danger">{t('albums.error')}</Alert>}

      {state.status === 'ready' && state.albums.length === 0 && (
        <EmptyState title={t('albums.empty.title')} hint={t('albums.empty.hint')} />
      )}

      {state.status === 'ready' && state.albums.length > 0 && (
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(auto-fill, minmax(160px, 1fr))',
            gap: '12px',
          }}
        >
          {state.albums.map((album) => (
            <AlbumTile key={album.uid} album={album} />
          ))}
        </div>
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
            setCreating(false)
          }}
        />
      )}
    </>
  )
}
