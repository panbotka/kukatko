import { useCallback, useEffect, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../../auth/AuthContext'
import { ApiError } from '../../services/auth'
import {
  clearMyPicture,
  fetchMyPicture,
  MAX_PICTURE_BYTES,
  PICTURE_ACCEPT,
  type PictureOrigin,
  pickMyPicture,
  uploadMyPicture,
} from '../../services/userpic'
import { Icon } from '../Icon'
import { PersonAvatar } from '../PersonAvatar'

import { PhotoPickerModal } from './PhotoPickerModal'

/** The i18n key of the message shown for a failed change. */
type ErrorKey =
  | 'account.picture.errorFormat'
  | 'account.picture.errorTooLarge'
  | 'account.picture.errorPrivate'
  | 'account.picture.errorGeneric'

/** What the card is doing right now. */
type State =
  | { status: 'loading' }
  | { status: 'idle' }
  | { status: 'saving' }
  | { status: 'error'; messageKey: ErrorKey }

/** Maps a failed change to the message that explains it. */
function errorKeyFor(error: unknown): ErrorKey {
  if (error instanceof ApiError) {
    if (error.status === 413) {
      return 'account.picture.errorTooLarge'
    }
    if (error.status === 400) {
      // The two 400s a user can actually provoke are told apart by the server's
      // own message, which names the private/hidden refusal explicitly. Anything
      // else that is 400 here is a picture the decoder could not read.
      return /private|hidden/i.test(error.message)
        ? 'account.picture.errorPrivate'
        : 'account.picture.errorFormat'
    }
  }
  return 'account.picture.errorGeneric'
}

/**
 * "My picture" — the one place a user chooses what stands for their account
 * wherever they are named: beside their comments, on the account control in the
 * bar.
 *
 * It offers the three sources the server resolves, in the order it resolves
 * them. A picture uploaded here wins; a photo of the library picked here comes
 * next; and under both sits the face of the person the account is linked to,
 * which needs no action at all — an account that has said who it is already
 * wears that face, and the card says so rather than presenting an empty control
 * beside a picture that is evidently there.
 *
 * Clearing therefore does not mean "back to the letter": it means "drop what I
 * set", and a linked account falls back to its person's face. The button says
 * which of the two will happen before it is pressed.
 *
 * The preview is the real thing — {@link PersonAvatar} against the same endpoint
 * every reader uses — with a version counter that defeats the response's
 * ten-minute cache, so a picture just changed is the picture shown.
 */
export function MyPictureCard() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [origin, setOrigin] = useState<PictureOrigin>('none')
  const [state, setState] = useState<State>({ status: 'loading' })
  const [version, setVersion] = useState(0)
  const [picking, setPicking] = useState(false)
  const fileInput = useRef<HTMLInputElement>(null)

  const userUid = user?.uid
  // The same name the rest of the app draws this account under, and what the
  // fallback initial is taken from.
  const displayName = user === null ? '' : user.display_name || user.username
  const linked = user?.subject_uid != null && user.subject_uid !== ''

  const reload = useCallback(async (signal?: AbortSignal) => {
    const picture = await fetchMyPicture(signal)
    setOrigin(picture.origin)
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    reload(controller.signal)
      .then(() => {
        setState({ status: 'idle' })
      })
      .catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === 'AbortError')) {
          setState({ status: 'error', messageKey: 'account.picture.errorGeneric' })
        }
      })
    return () => {
      controller.abort()
    }
  }, [reload])

  /** Runs one change, then re-reads which source now answers. */
  async function change(action: () => Promise<void>) {
    setState({ status: 'saving' })
    try {
      await action()
      await reload()
      // The picture is cached for ten minutes; without this the page that just
      // changed it would go on showing the old one.
      setVersion((previous) => previous + 1)
      setState({ status: 'idle' })
    } catch (error: unknown) {
      setState({ status: 'error', messageKey: errorKeyFor(error) })
    }
  }

  const busy = state.status === 'saving' || state.status === 'loading'

  return (
    <Card text="light" className="mb-4">
      <Card.Body>
        <Card.Title as="h2" className="kk-section-title mb-3">
          {t('account.picture.title')}
        </Card.Title>
        <p className="text-secondary">{t('account.picture.hint')}</p>

        {state.status === 'error' && (
          <Alert variant="danger" role="alert">
            {t(state.messageKey)}
          </Alert>
        )}

        <div className="d-flex align-items-center gap-3 mb-3">
          <PersonAvatar
            name={displayName}
            // This one card already knows the answer, so it skips the request
            // that could only 404 — every other caller asks and lets the 404 do
            // the talking.
            userUid={origin === 'none' ? undefined : userUid}
            version={version}
            className="kk-avatar--lg"
          />
          <span className="text-secondary">{t(`account.picture.source.${origin}`)}</span>
        </div>

        <div className="d-flex flex-wrap gap-2">
          <Button
            type="button"
            variant="primary"
            disabled={busy}
            onClick={() => {
              fileInput.current?.click()
            }}
          >
            {state.status === 'saving' && (
              <Spinner
                animation="border"
                size="sm"
                role="status"
                aria-hidden="true"
                className="me-2"
              />
            )}
            <Icon name="cloud-arrow-up" className="me-2" />
            {t('account.picture.upload')}
          </Button>
          <Button
            type="button"
            variant="outline-light"
            disabled={busy}
            onClick={() => {
              setPicking(true)
            }}
          >
            <Icon name="images" className="me-2" />
            {t('account.picture.pick')}
          </Button>
          {(origin === 'upload' || origin === 'photo') && (
            <Button
              type="button"
              variant="outline-light"
              disabled={busy}
              onClick={() => {
                void change(() => clearMyPicture())
              }}
            >
              {linked ? t('account.picture.clearToFace') : t('account.picture.clear')}
            </Button>
          )}
        </div>

        {/* A bare file input renders as a browser-styled control that cannot be
            labelled in the app's voice, so it hides behind the button above —
            the same bargain the upload page's PickFilesButton strikes. */}
        <input
          ref={fileInput}
          type="file"
          className="visually-hidden"
          accept={PICTURE_ACCEPT}
          aria-label={t('account.picture.upload')}
          onChange={(event) => {
            const file = event.target.files?.[0]
            // Reset first, so choosing the same file twice fires again — after a
            // failed upload that is exactly what a user does.
            event.target.value = ''
            if (file !== undefined) {
              void change(() => uploadMyPicture(file))
            }
          }}
        />

        <div className="text-secondary small mt-2">
          {t('account.picture.formats', { megabytes: MAX_PICTURE_BYTES / (1024 * 1024) })}
        </div>

        <PhotoPickerModal
          show={picking}
          busy={state.status === 'saving'}
          onClose={() => {
            setPicking(false)
          }}
          onPick={(photoUid) => {
            setPicking(false)
            void change(() => pickMyPicture(photoUid))
          }}
        />
      </Card.Body>
    </Card>
  )
}
