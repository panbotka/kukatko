import { useCallback, useEffect, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Form from 'react-bootstrap/Form'
import ListGroup from 'react-bootstrap/ListGroup'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../../auth/AuthContext'
import { formatDateTimeMinutes } from '../../lib/format'
import { recordPushPromptAnswer } from '../../lib/pushPromptAnswer'
import { type DeviceOs, describeUserAgent } from '../../lib/userAgent'
import {
  getPushPermission,
  getPushState,
  isIosOutsideInstalledApp,
  isPushEnabled,
  type PushState,
  requestPushPermission,
  subscribeToPush,
  unsubscribeFromPush,
} from '../../pwa/push'
import { isAdmin, type Role } from '../../services/auth'
import {
  deletePushSubscription,
  fetchNotificationPreferences,
  fetchPushSubscriptions,
  type NotificationKind,
  type NotificationPreference,
  type PushSubscriptionRecord,
  updateNotificationPreferences,
  withPreference,
} from '../../services/notifications'
import { ConfirmModal } from '../ConfirmModal'
import { ErrorState } from '../ErrorState'
import { Icon } from '../Icon'

/** A list the card fetches from the server. */
type Loaded<T> = { status: 'loading' } | { status: 'error' } | { status: 'ready'; items: T[] }

/** The i18n key of a kind's checkbox label. */
type KindLabelKey =
  | 'account.notifications.kinds.tagged'
  | 'account.notifications.kinds.registration_pending'

/** What the card knows about one kind: its label, and who can ever receive it. */
interface KindCopy {
  label: KindLabelKey
  /** Only an administrator (or a maintainer, who inherits it) is ever sent this kind. */
  adminOnly: boolean
}

/**
 * The kinds the card offers a setting for. A kind the server knows and this
 * client does not is left out rather than shown under its raw name: it will get
 * its words (and its audience) with the release that teaches the client.
 */
const KINDS: Partial<Record<NotificationKind, KindCopy>> = {
  tagged: { label: 'account.notifications.kinds.tagged', adminOnly: false },
  registration_pending: {
    label: 'account.notifications.kinds.registration_pending',
    adminOnly: true,
  },
}

/** The i18n key of the "on <system>" phrase for each system the summary names. */
const OS_KEYS = {
  android: 'account.notifications.os.android',
  ios: 'account.notifications.os.ios',
  ipados: 'account.notifications.os.ipados',
  windows: 'account.notifications.os.windows',
  macos: 'account.notifications.os.macos',
  chromeos: 'account.notifications.os.chromeos',
  linux: 'account.notifications.os.linux',
} as const satisfies Record<DeviceOs, string>

/**
 * Reports whether `kind` gets a checkbox for an account of `role`: nobody is
 * offered a setting for a message they can never receive.
 */
export function isKindOffered(kind: NotificationKind, role: Role | null): boolean {
  const copy = KINDS[kind]
  if (copy === undefined) {
    return false
  }
  return !copy.adminOnly || (role !== null && isAdmin(role))
}

/** The readable name of a registered browser — "Chrome on Android". */
function useDeviceName(): (userAgent: string) => string {
  const { t } = useTranslation()
  return useCallback(
    (userAgent: string) => {
      const { browser, os } = describeUserAgent(userAgent)
      if (browser !== null && os !== null) {
        return t('account.notifications.deviceName', { browser, os: t(OS_KEYS[os]) })
      }
      if (browser !== null) {
        return browser
      }
      if (os !== null) {
        return t('account.notifications.deviceNameOsOnly', { os: t(OS_KEYS[os]) })
      }
      return t('account.notifications.deviceUnknown')
    },
    [t],
  )
}

/** One registered browser: its readable name, whether it is this one, when it was used. */
function DeviceRow({
  device,
  current,
  busy,
  onRemove,
}: {
  device: PushSubscriptionRecord
  current: boolean
  busy: boolean
  onRemove: (device: PushSubscriptionRecord) => void
}) {
  const { t, i18n } = useTranslation()
  const name = useDeviceName()(device.user_agent)

  return (
    <ListGroup.Item as="li" className="d-flex align-items-start justify-content-between gap-2">
      <div className="kk-min-w-0">
        <div className="fw-semibold text-break d-flex align-items-center gap-2 flex-wrap">
          {name}
          {current && <Badge bg="info">{t('account.notifications.thisBrowserBadge')}</Badge>}
        </div>
        <div className="text-secondary small">
          {device.last_used_at === null
            ? t('account.notifications.neverUsed', {
                date: formatDateTimeMinutes(device.created_at, i18n.language),
              })
            : t('account.notifications.lastUsed', {
                date: formatDateTimeMinutes(device.last_used_at, i18n.language),
              })}
        </div>
      </div>
      <Button
        variant="outline-danger"
        size="sm"
        className="d-inline-flex align-items-center gap-2 flex-shrink-0 kukatko-tap-target-touch"
        aria-label={t('account.notifications.removeNamed', { name })}
        title={t('account.notifications.remove')}
        disabled={busy}
        onClick={() => {
          onRemove(device)
        }}
      >
        <Icon name="trash" />
        <span className="d-none d-sm-inline">{t('account.notifications.remove')}</span>
      </Button>
    </ListGroup.Item>
  )
}

/**
 * The "Notifications" section of the account page: whether **this browser**
 * receives pushes, **which messages** the account wants, and **which devices**
 * are registered.
 *
 * # Two scopes, said out loud
 *
 * The master control belongs to the browser — a push subscription lives in one
 * browser on one device — while the per-kind checkboxes belong to the account
 * and apply to every device it is signed in on. That split is the thing people
 * reliably get wrong about web push, so each part of the card says which of
 * the two it is, in its own heading and hint, rather than leaving it to guess.
 *
 * # This browser
 *
 * Off: a button that asks for the permission (only from that click — a browser
 * grants the prompt to a gesture alone) and subscribes. On: a sentence saying
 * so, and a button that unsubscribes in both the browser and the server. A
 * browser holding a subscription the server no longer lists — it was removed
 * from another device — counts as off, so "on" is never claimed for a browser
 * nothing delivers to; turning it on again re-registers the same subscription.
 *
 * Where no button could work there is none: a browser that blocked the site is
 * told to re-allow it in its own site settings, iOS outside the installed
 * home-screen app is told to install it, a browser without the Push API is told
 * so. The account-wide choices and the device list stay, since they still
 * matter for the person's other devices.
 *
 * # Off instance-wide
 *
 * With push switched off on the server (or its configuration unreadable) the
 * whole section is one sentence saying so: no control that would only fail.
 *
 * # Which messages
 *
 * One checkbox per kind the account can receive; the "registration waiting"
 * kind only for an administrator or a maintainer. A change saves at once.
 */
export function NotificationsCard() {
  const { t } = useTranslation()
  const { user, role } = useAuth()
  const deviceName = useDeviceName()
  const userUid = user?.uid ?? null

  const [instanceOn, setInstanceOn] = useState<boolean | null>(null)
  const [browser, setBrowser] = useState<PushState | null>(null)
  const [prefs, setPrefs] = useState<Loaded<NotificationPreference>>({ status: 'loading' })
  const [devices, setDevices] = useState<Loaded<PushSubscriptionRecord>>({ status: 'loading' })
  const [busy, setBusy] = useState(false)
  const [browserError, setBrowserError] = useState(false)
  const [savingKind, setSavingKind] = useState<NotificationKind | null>(null)
  const [prefError, setPrefError] = useState(false)
  const [pendingRemove, setPendingRemove] = useState<PushSubscriptionRecord | null>(null)
  const [removeError, setRemoveError] = useState(false)

  const loadPrefs = useCallback((signal?: AbortSignal) => {
    setPrefs({ status: 'loading' })
    fetchNotificationPreferences(signal)
      .then((items) => {
        setPrefs({ status: 'ready', items })
      })
      .catch(() => {
        if (signal?.aborted !== true) {
          setPrefs({ status: 'error' })
        }
      })
  }, [])

  const loadDevices = useCallback((signal?: AbortSignal) => {
    fetchPushSubscriptions(signal)
      .then((items) => {
        setDevices({ status: 'ready', items })
      })
      .catch(() => {
        if (signal?.aborted !== true) {
          setDevices({ status: 'error' })
        }
      })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void isPushEnabled().then((on) => {
      if (controller.signal.aborted) {
        return
      }
      setInstanceOn(on)
      if (!on) {
        return
      }
      void getPushState().then((state) => {
        if (!controller.signal.aborted) {
          setBrowser(state)
        }
      })
      loadPrefs(controller.signal)
      loadDevices(controller.signal)
    })
    return () => {
      controller.abort()
    }
  }, [loadPrefs, loadDevices])

  /** Records that this account has answered the question, so the one-time prompt stays away. */
  function answered() {
    if (userUid !== null) {
      recordPushPromptAnswer(userUid)
    }
  }

  /** Applies the outcome of a subscribe/unsubscribe to the card. */
  function settle(result: PushState) {
    if (result.status === 'disabled') {
      setInstanceOn(false)
      return
    }
    if (result.status === 'failed') {
      setBrowserError(true)
      return
    }
    setBrowser(result)
  }

  async function turnOn() {
    setBusy(true)
    setBrowserError(false)
    answered()
    // Nothing may be awaited before the prompt: the browser grants it to the
    // click's own gesture only.
    const permission =
      getPushPermission() === 'default' ? await requestPushPermission() : getPushPermission()
    if (permission === 'denied') {
      setBrowser({ status: 'denied', endpoint: null })
      setBusy(false)
      return
    }
    if (permission !== 'granted') {
      // The browser's own prompt was dismissed: nothing changed, nothing to report.
      setBusy(false)
      return
    }
    settle(await subscribeToPush())
    loadDevices()
    setBusy(false)
  }

  async function turnOff() {
    setBusy(true)
    setBrowserError(false)
    answered()
    const result = await unsubscribeFromPush()
    if (result.status === 'subscribed') {
      setBrowserError(true)
    } else {
      settle(result)
    }
    loadDevices()
    setBusy(false)
  }

  async function removeDevice(device: PushSubscriptionRecord) {
    setRemoveError(false)
    if (device.endpoint === browser?.endpoint) {
      // This browser: forgetting it only on the server would leave the browser
      // believing it is on, so it is the master control's "turn off".
      await turnOff()
      return
    }
    let previous: PushSubscriptionRecord[] = []
    setDevices((prev) => {
      if (prev.status !== 'ready') {
        return prev
      }
      previous = prev.items
      return { status: 'ready', items: prev.items.filter((item) => item.id !== device.id) }
    })
    try {
      await deletePushSubscription(device.endpoint)
    } catch {
      setRemoveError(true)
      setDevices({ status: 'ready', items: previous })
    }
  }

  async function toggleKind(kind: NotificationKind, enabled: boolean) {
    if (prefs.status !== 'ready') {
      return
    }
    const previous = prefs.items
    setPrefError(false)
    setSavingKind(kind)
    setPrefs({
      status: 'ready',
      items: previous.map((pref) =>
        pref.kind === kind ? { ...pref, enabled, is_default: false } : pref,
      ),
    })
    try {
      const items = await updateNotificationPreferences(withPreference(previous, kind, enabled))
      setPrefs({ status: 'ready', items })
    } catch {
      setPrefError(true)
      setPrefs({ status: 'ready', items: previous })
    } finally {
      setSavingKind(null)
    }
  }

  const title = (
    <Card.Title as="h2" className="kk-section-title mb-3">
      <Icon name="bell" className="me-2" />
      {t('account.notifications.title')}
    </Card.Title>
  )

  if (instanceOn === null) {
    return (
      <Card text="light" className="mb-4">
        <Card.Body>
          {title}
          <div className="d-flex justify-content-center py-3">
            <Spinner animation="border" size="sm" role="status">
              <span className="visually-hidden">{t('account.notifications.loading')}</span>
            </Spinner>
          </div>
        </Card.Body>
      </Card>
    )
  }

  if (!instanceOn) {
    return (
      <Card text="light" className="mb-4">
        <Card.Body>
          {title}
          <Alert variant="secondary" role="status" className="mb-0">
            <Icon name="slash-circle" className="me-2" />
            {t('account.notifications.instanceOff')}
          </Alert>
        </Card.Body>
      </Card>
    )
  }

  const serverKnowsThisBrowser =
    devices.status !== 'ready' ||
    devices.items.some((device) => device.endpoint === browser?.endpoint)
  const on = browser?.status === 'subscribed' && serverKnowsThisBrowser
  const offeredKinds =
    prefs.status === 'ready' ? prefs.items.filter((pref) => isKindOffered(pref.kind, role)) : []

  return (
    <Card text="light" className="mb-4">
      <Card.Body>
        {title}
        <p className="text-secondary">{t('account.notifications.intro')}</p>

        <section className="mb-4" aria-labelledby="notifications-browser">
          <h3 id="notifications-browser" className="h6 mb-1">
            {t('account.notifications.browser.title')}
          </h3>
          <p className="text-secondary small">{t('account.notifications.browser.scope')}</p>

          {browserError && (
            <Alert variant="danger" role="alert">
              {t('account.notifications.browser.failed')}
            </Alert>
          )}

          <BrowserControl
            state={browser}
            on={on}
            busy={busy}
            onTurnOn={() => {
              void turnOn()
            }}
            onTurnOff={() => {
              void turnOff()
            }}
          />
        </section>

        <section className="mb-4" aria-labelledby="notifications-kinds">
          <h3 id="notifications-kinds" className="h6 mb-1">
            {t('account.notifications.kindsTitle')}
          </h3>
          <p className="text-secondary small">{t('account.notifications.kindsScope')}</p>

          {prefError && (
            <Alert variant="danger" role="alert">
              {t('account.notifications.saveError')}
            </Alert>
          )}
          {prefs.status === 'loading' && (
            <Spinner animation="border" size="sm" role="status">
              <span className="visually-hidden">{t('account.notifications.loading')}</span>
            </Spinner>
          )}
          {prefs.status === 'error' && (
            <ErrorState
              title={t('account.notifications.prefsLoadError')}
              onRetry={() => {
                loadPrefs()
              }}
              size="sm"
            />
          )}
          {offeredKinds.map((pref) => {
            const copy = KINDS[pref.kind]
            return copy === undefined ? null : (
              <Form.Check
                key={pref.kind}
                type="checkbox"
                id={`notification-kind-${pref.kind}`}
                label={t(copy.label)}
                checked={pref.enabled}
                disabled={savingKind !== null}
                onChange={(event) => {
                  void toggleKind(pref.kind, event.target.checked)
                }}
              />
            )
          })}
        </section>

        <section aria-labelledby="notifications-devices">
          <h3 id="notifications-devices" className="h6 mb-1">
            {t('account.notifications.devices.title')}
          </h3>
          <p className="text-secondary small">{t('account.notifications.devices.hint')}</p>

          {removeError && (
            <Alert variant="danger" role="alert">
              {t('account.notifications.devices.removeError')}
            </Alert>
          )}
          {devices.status === 'loading' && (
            <Spinner animation="border" size="sm" role="status">
              <span className="visually-hidden">{t('account.notifications.loading')}</span>
            </Spinner>
          )}
          {devices.status === 'error' && (
            <ErrorState
              title={t('account.notifications.devices.loadError')}
              onRetry={() => {
                loadDevices()
              }}
              size="sm"
            />
          )}
          {devices.status === 'ready' && devices.items.length === 0 && (
            <p className="text-secondary mb-0">{t('account.notifications.devices.empty')}</p>
          )}
          {devices.status === 'ready' && devices.items.length > 0 && (
            <ListGroup as="ul">
              {devices.items.map((device) => (
                <DeviceRow
                  key={device.id}
                  device={device}
                  current={browser?.status === 'subscribed' && device.endpoint === browser.endpoint}
                  busy={busy}
                  onRemove={setPendingRemove}
                />
              ))}
            </ListGroup>
          )}
        </section>
      </Card.Body>

      <ConfirmModal
        show={pendingRemove !== null}
        title={t('account.notifications.devices.confirmTitle')}
        confirmLabel={t('account.notifications.devices.confirmAction')}
        onCancel={() => {
          setPendingRemove(null)
        }}
        onConfirm={() => {
          const device = pendingRemove
          setPendingRemove(null)
          if (device !== null) {
            void removeDevice(device)
          }
        }}
      >
        {pendingRemove !== null &&
          t('account.notifications.devices.confirmBody', {
            name: deviceName(pendingRemove.user_agent),
          })}
      </ConfirmModal>
    </Card>
  )
}

/**
 * The master control for this browser: a button where one can work, a sentence
 * saying why where none can.
 */
function BrowserControl({
  state,
  on,
  busy,
  onTurnOn,
  onTurnOff,
}: {
  state: PushState | null
  on: boolean
  busy: boolean
  onTurnOn: () => void
  onTurnOff: () => void
}) {
  const { t } = useTranslation()

  if (state === null) {
    return (
      <Spinner animation="border" size="sm" role="status">
        <span className="visually-hidden">{t('account.notifications.loading')}</span>
      </Spinner>
    )
  }

  switch (state.status) {
    case 'denied':
      return (
        <Alert variant="warning" role="alert" className="mb-0">
          <Icon name="lock-fill" className="me-2" />
          {t('account.notifications.browser.blocked')}
        </Alert>
      )
    case 'unsupported':
      return (
        <Alert variant="secondary" role="status" className="mb-0">
          <Icon name="info-circle" className="me-2" />
          {isIosOutsideInstalledApp()
            ? t('account.notifications.browser.iosInstall')
            : t('account.notifications.browser.unsupported')}
        </Alert>
      )
    case 'no-worker':
      return (
        <Alert variant="secondary" role="status" className="mb-0">
          <Icon name="info-circle" className="me-2" />
          {t('account.notifications.browser.noWorker')}
        </Alert>
      )
    default:
      break
  }

  const spinner = busy && (
    <Spinner animation="border" size="sm" role="status" aria-hidden="true" className="me-2" />
  )

  if (on) {
    return (
      <div className="d-flex align-items-center justify-content-between gap-2 flex-wrap">
        <span className="text-success d-inline-flex align-items-center gap-2">
          <Icon name="check-lg" />
          {t('account.notifications.browser.on')}
        </span>
        <Button variant="outline-light" size="sm" disabled={busy} onClick={onTurnOff}>
          {spinner}
          {t('account.notifications.browser.turnOff')}
        </Button>
      </div>
    )
  }

  return (
    <div className="d-flex align-items-center justify-content-between gap-2 flex-wrap">
      <span className="text-secondary">{t('account.notifications.browser.off')}</span>
      <Button variant="primary" size="sm" disabled={busy} onClick={onTurnOn}>
        {spinner}
        {t('account.notifications.browser.turnOn')}
      </Button>
    </div>
  )
}
