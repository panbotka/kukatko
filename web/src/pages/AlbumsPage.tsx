import { useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { AlbumEditModal } from '../components/organize/AlbumEditModal'
import { AlbumTile } from '../components/organize/AlbumTile'
import { type AlbumCount, fetchAlbums } from '../services/organize'

/** Fetch lifecycle of the albums list. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; albums: AlbumCount[] }

/**
 * The albums index: a responsive grid of album cards (cover, title, count), each
 * linking to its detail page. Editors and admins get a create button; the modal
 * adds the new album to the grid on success. Mutation controls are hidden from
 * viewers.
 */
export function AlbumsPage() {
  const { t } = useTranslation()
  const { canWrite } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [creating, setCreating] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
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
  }, [])

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
          onSaved={(album) => {
            setState((prev) =>
              prev.status === 'ready'
                ? { status: 'ready', albums: [...prev.albums, { ...album, photo_count: 0 }] }
                : prev,
            )
            setCreating(false)
          }}
        />
      )}
    </>
  )
}
