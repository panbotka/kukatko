import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../../auth/AuthContext'
import { hasAnsweredPushPrompt, recordPushPromptAnswer } from '../../lib/pushPromptAnswer'
import {
  getPushPermission,
  getPushState,
  isIosOutsideInstalledApp,
  isPushEnabled,
  isPushSupported,
  requestPushPermission,
  subscribeToPush,
} from '../../pwa/push'
import { Icon } from '../Icon'
import Modal from '../Modal'
import { useToast } from '../toast/ToastContext'

/**
 * The prompt's lifecycle. `checking` is the look at the browser and the
 * instance it takes before it dares appear; `done` is final — nothing reopens
 * it for the rest of the page's life.
 */
type Phase = 'idle' | 'checking' | 'open' | 'done'

/**
 * What the open dialog shows: the question, the question while the browser's
 * own prompt and the subscription run, the browser's refusal, or a failure.
 */
type View = 'ask' | 'busy' | 'blocked' | 'failed'

/**
 * Decides whether `userUid` should be asked about notifications in this
 * browser. Every "no" is a case where the question has no honest answer or has
 * already been given one: the account answered here before, the browser cannot
 * push (iOS outside an installed home-screen app among them), the browser
 * already blocked the site, this browser is already subscribed, there is no
 * service worker for a push to land in (the dev server), or the instance has
 * push switched off. Cheapest checks first — the instance is asked last.
 */
async function shouldOfferPush(userUid: string): Promise<boolean> {
  if (hasAnsweredPushPrompt(userUid)) {
    return false
  }
  if (!isPushSupported() || isIosOutsideInstalledApp()) {
    return false
  }
  if (getPushPermission() === 'denied') {
    return false
  }
  const { status } = await getPushState()
  if (status !== 'prompt' && status !== 'unsubscribed') {
    return false
  }
  return isPushEnabled()
}

/**
 * Asks, once, whether the reader wants notifications — "Kukátko will tell you
 * when somebody tags you in a photo" — with two answers: not now, and turn on.
 *
 * # Exactly once
 *
 * Every way the question ends is remembered for this account in this browser
 * ({@link recordPushPromptAnswer}): turned on, "not now", the ✕, the browser's
 * own prompt dismissed, the browser's block, a failure. None of them is asked
 * again; the settings page is the way back. A prompt that returns after a "no"
 * teaches people to block the site outright, and a blocked site can never be
 * asked again at all.
 *
 * # The permission belongs to the click
 *
 * Browsers grant `Notification.requestPermission()` only from a user gesture,
 * so nothing is requested on mount: "turn on" is the only path to the browser
 * prompt, and it subscribes in that same click (Firefox wants the gesture for
 * `pushManager.subscribe` too).
 *
 * # When it does not appear
 *
 * See {@link shouldOfferPush}. On iOS in a browser tab a push can never arrive —
 * only an installed home-screen app (Safari 16.4+) receives one — so the prompt
 * stays away rather than offer a button that silently fails; the settings page
 * is where that platform limit is explained.
 *
 * # Outcomes
 *
 * Success closes the dialog and confirms with a toast. A browser-level denial
 * keeps the dialog open to say plainly that the browser blocked it and that it
 * has to be re-allowed in the browser's own site settings — the page cannot ask
 * again. A failure (the server refused, the push service failed) says so too.
 *
 * Mounted by the app shell ({@link Layout}) once the first-run welcome has
 * settled, so the two never stack; like every dialog it paints in the shared
 * dialog band above the full-screen sheets.
 */
export function PushPrompt() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const toast = useToast()
  const [phase, setPhase] = useState<Phase>('idle')
  const [view, setView] = useState<View>('ask')
  const userUid = user?.uid ?? null

  // Decide once per page load: the phase latches, so a later session refresh
  // cannot reopen a question already answered or already declined to ask.
  useEffect(() => {
    if (phase === 'idle' && userUid !== null) {
      setPhase('checking')
    }
  }, [phase, userUid])

  useEffect(() => {
    if (phase !== 'checking' || userUid === null) {
      return
    }
    let cancelled = false
    void shouldOfferPush(userUid)
      .catch(() => false)
      .then((offer) => {
        if (!cancelled) {
          setPhase(offer ? 'open' : 'done')
        }
      })
    return () => {
      cancelled = true
    }
  }, [phase, userUid])

  const record = useCallback(() => {
    if (userUid !== null) {
      recordPushPromptAnswer(userUid)
    }
  }, [userUid])

  /** Closes the prompt for good; whatever closed it counts as the answer. */
  const finish = useCallback(() => {
    record()
    setPhase('done')
  }, [record])

  async function turnOn() {
    setView('busy')
    // The first await of the click, so the browser still sees the gesture.
    const permission = await requestPushPermission()
    if (permission === 'denied') {
      record()
      setView('blocked')
      return
    }
    if (permission !== 'granted') {
      // The browser's own prompt was dismissed without an answer: that is a
      // "not now" too, and asking again would only wear the prompt down.
      finish()
      return
    }
    const result = await subscribeToPush()
    if (result.status === 'subscribed') {
      finish()
      toast.show({ message: t('pushPrompt.enabled'), variant: 'success' })
      return
    }
    record()
    setView(result.status === 'denied' ? 'blocked' : 'failed')
  }

  if (phase !== 'open') {
    return null
  }

  const busy = view === 'busy'
  const answered = view === 'blocked' || view === 'failed'

  return (
    <Modal show onHide={finish} centered aria-labelledby="push-prompt-title">
      <Modal.Header closeButton closeLabel={t('pushPrompt.close')}>
        <Modal.Title id="push-prompt-title">
          <Icon name="bell" className="me-2" />
          {t('pushPrompt.title')}
        </Modal.Title>
      </Modal.Header>
      <Modal.Body>
        {view === 'blocked' ? (
          <Alert variant="warning" role="alert" className="mb-0">
            {t('pushPrompt.blocked')}
          </Alert>
        ) : view === 'failed' ? (
          <Alert variant="danger" role="alert" className="mb-0">
            {t('pushPrompt.failed')}
          </Alert>
        ) : (
          <>
            <p>{t('pushPrompt.body')}</p>
            <p className="text-secondary mb-0">{t('pushPrompt.later')}</p>
          </>
        )}
      </Modal.Body>
      <Modal.Footer>
        {answered ? (
          <Button variant="primary" onClick={finish}>
            {t('pushPrompt.understood')}
          </Button>
        ) : (
          <>
            <Button variant="link" onClick={finish} disabled={busy}>
              {t('pushPrompt.notNow')}
            </Button>
            <Button
              variant="primary"
              disabled={busy}
              onClick={() => {
                void turnOn()
              }}
            >
              {busy && (
                <Spinner
                  animation="border"
                  size="sm"
                  role="status"
                  aria-hidden="true"
                  className="me-2"
                />
              )}
              {t('pushPrompt.turnOn')}
            </Button>
          </>
        )}
      </Modal.Footer>
    </Modal>
  )
}
