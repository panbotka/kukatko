import type { TFunction } from 'i18next'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { useNavigate, useSearchParams } from 'react-router-dom'

import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon } from '../components/Icon'
import { KeyboardShortcutsHelp } from '../components/KeyboardShortcutsHelp'
import { CompareStage } from '../components/duplicates/CompareStage'
import { DiffTable } from '../components/duplicates/DiffTable'
import { MergeConfirmModal } from '../components/duplicates/MergeConfirmModal'
import '../components/duplicates/compare.css'
import { useAuth } from '../auth/AuthContext'
import { useComparePair } from '../hooks/useComparePair'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { useReloadKey } from '../hooks/useReloadKey'
import { useSyncZoom } from '../hooks/useSyncZoom'
import { formatBytes, formatDateTime } from '../lib/format'
import {
  type ComparePair,
  buildDiffRows,
  buildPairQueue,
  countDiffering,
  dropPairsTouching,
  pairIndexInGroup,
  pairsInGroup,
} from '../lib/duplicateCompare'
import { ApiError } from '../services/auth'
import { type MergeResult, fetchDuplicates, mergeDuplicates } from '../services/duplicates'
import { dismissDuplicate } from '../services/feedback'
import { thumbUrl } from '../services/photos'

/** How many groups the queue is built from. One page is a long review session already. */
const PAGE_SIZE = 20

/** The preview size for the stage: large enough that a zoom shows real pixels. */
const COMPARE_PREVIEW_SIZE = 'fit_1920'

/** Top-level load status of the compare view. */
type Status = 'loading' | 'ready' | 'error' | 'unavailable'

/** A merge awaiting confirmation: who wins, who loses, and what the merge would do. */
interface PendingMerge {
  keeperUid: string
  loserUid: string
  preview: MergeResult
}

/** Resolves a failed mutation to a localized message; raw server text is never shown. */
function actionMessage(err: unknown, t: TFunction): string {
  if (err instanceof ApiError && err.status === 503) {
    return t('duplicates.unavailable')
  }
  return t('duplicates.actionError')
}

/**
 * `/duplicates/compare` — the side-by-side decision view for near-duplicate pairs.
 *
 * The duplicates list can tell you two photos are near-identical; it cannot help you
 * decide which to keep, because the things that decide it (one is 12 MP and one is
 * 2 MP, one has the right date, one carries all your albums and people) are not
 * visible until you open both. This view puts them side by side under one
 * synchronised zoom, marks exactly what differs, and offers three answers: keep
 * left, keep right, keep both.
 *
 * It is a queue, not a page: a decision advances to the next pair rather than
 * returning to the list, because duplicate review is only finishable if it feels
 * like one. Groups of more than two are compared **pairwise against the suggested
 * keeper** — see {@link buildPairQueue} — and each decision merges only that pair,
 * so a third member is never resolved by a question the user was not asked.
 *
 * Fullscreen, outside the layout shell, for the same reason the review game is: two
 * photos with a navbar and a sidebar around them are two photos too small to judge.
 */
export function DupComparePage() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const { downloadToken } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()

  const [status, setStatus] = useState<Status>('loading')
  // Bumped to re-run the mount-only queue fetch after a failed load.
  const [reloadKey, reloadQueue] = useReloadKey()
  const [pairs, setPairs] = useState<ComparePair[]>([])
  const [index, setIndex] = useState(0)
  const [pending, setPending] = useState<PendingMerge | null>(null)
  const [confirming, setConfirming] = useState(false)
  const [actionError, setActionError] = useState<string | null>(null)
  const [onlyDifferences, setOnlyDifferences] = useState(false)

  const pair = pairs.at(index) ?? null
  const sides = useComparePair(pair)
  const zoom = useSyncZoom({ resetKey: pair?.id ?? '' })

  // The queue is built once from a page of groups. `pair` in the URL only picks the
  // starting position: "back always works" means a reload lands on the pair you were
  // looking at, but the queue itself is derived state and is not worth serialising.
  useEffect(() => {
    const controller = new AbortController()
    const startAt = searchParams.get('pair')
    setStatus('loading')
    fetchDuplicates({ limit: PAGE_SIZE, offset: 0 }, controller.signal)
      .then((res) => {
        const queue = buildPairQueue(res.groups)
        setPairs(queue)
        const found = queue.findIndex((p) => p.id === startAt)
        setIndex(found >= 0 ? found : 0)
        setStatus('ready')
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) {
          return
        }
        setStatus(err instanceof ApiError && err.status === 503 ? 'unavailable' : 'error')
      })
    return () => {
      controller.abort()
    }
    // Runs on mount and on an explicit retry (`reloadKey`). `searchParams` is read
    // for the initial position and then written by this page, so depending on it
    // would refetch on every advance — hence it is intentionally omitted.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reloadKey])

  // Keep the URL pointing at the pair on screen, replacing rather than pushing: the
  // queue is one task, and Back should leave it, not walk back through every answer.
  useEffect(() => {
    if (pair !== null) {
      setSearchParams({ pair: pair.id }, { replace: true })
    }
  }, [pair, setSearchParams])

  const exit = useCallback(() => {
    void navigate('/duplicates')
  }, [navigate])

  // The queue and the cursor as refs, so `advance` can read the live position
  // without depending on it and re-creating itself (and its shortcut bindings) on
  // every answer.
  const pairsRef = useRef(pairs)
  pairsRef.current = pairs
  const indexRef = useRef(index)
  indexRef.current = index

  /** Advances past the current pair, optionally dropping an archived photo's pairs. */
  const advance = useCallback((archivedUid?: string) => {
    setActionError(null)
    if (archivedUid === undefined) {
      setIndex((i) => i + 1)
      return
    }
    // The archived photo is gone, so every pair naming it — not just the one just
    // answered — is a question about a photo that no longer exists. Dropping them
    // shifts the queue under the cursor, so the next position is *the number of
    // pairs that survive before the cursor*, not the cursor plus one: the answered
    // pair always dies, so that index is exactly the next unanswered pair. Counting
    // forward instead would skip one survivor per pair dropped behind the cursor,
    // and those pairs would never be reviewed.
    const prev = pairsRef.current
    const nextIndex = dropPairsTouching(prev.slice(0, indexRef.current), archivedUid).length
    setPairs(dropPairsTouching(prev, archivedUid))
    setIndex(nextIndex)
  }, [])

  /** Previews keeping `keeperUid` over the other side, opening the confirm modal. */
  const beginKeep = useCallback(
    async (keeperUid: string) => {
      if (pair === null) {
        return
      }
      const loserUid = keeperUid === pair.leftUid ? pair.rightUid : pair.leftUid
      setActionError(null)
      try {
        // Only the two photos of this pair are merged, never the whole group: the
        // user answered a question about these two, and acting on a third member
        // they were not shown would archive a photo behind their back.
        const preview = await mergeDuplicates({
          keeper_uid: keeperUid,
          member_uids: [keeperUid, loserUid],
          dry_run: true,
        })
        setPending({ keeperUid, loserUid, preview })
      } catch (err) {
        setActionError(actionMessage(err, t))
      }
    },
    [pair, t],
  )

  /** Performs the confirmed merge and advances. */
  const confirmKeep = useCallback(async () => {
    if (pending === null) {
      return
    }
    setConfirming(true)
    try {
      await mergeDuplicates({
        keeper_uid: pending.keeperUid,
        member_uids: [pending.keeperUid, pending.loserUid],
      })
      setPending(null)
      advance(pending.loserUid)
    } catch (err) {
      setActionError(actionMessage(err, t))
    } finally {
      setConfirming(false)
    }
  }, [advance, pending, t])

  /**
   * Keeps both: records the pair as genuinely different and advances. The dismissal
   * is persisted (POST /feedback/duplicate-dismissals), so the detector drops the
   * edge on every later scan — a decision that evaporated on reload would offer the
   * same pair forever, which is the failure this view exists to end.
   *
   * The advance is optimistic: the request settles behind the user, because a queue
   * that pauses on every answer is a queue nobody finishes. A failure surfaces as an
   * alert and the pair simply comes back on the next scan.
   */
  const keepBoth = useCallback(() => {
    if (pair === null) {
      return
    }
    const { leftUid, rightUid } = pair
    advance()
    dismissDuplicate({ photo_uid: leftUid, other_uid: rightUid }).catch((err: unknown) => {
      setActionError(actionMessage(err, t))
    })
  }, [advance, pair, t])

  const busy = pending !== null || confirming
  useKeyboardShortcuts({
    ArrowLeft: () => {
      if (!busy && pair !== null) {
        void beginKeep(pair.leftUid)
      }
    },
    ArrowRight: () => {
      if (!busy && pair !== null) {
        void beginKeep(pair.rightUid)
      }
    },
    b: () => {
      if (!busy) {
        keepBoth()
      }
    },
    Escape: () => {
      // Leave Escape to an open modal (the confirm dialog, the shortcuts help) —
      // it closes itself.
      if (document.querySelector('.modal.show') === null) {
        exit()
      }
    },
  })

  const rows = useMemo(() => {
    if (sides.data === null) {
      return []
    }
    return buildDiffRows(sides.data.left, sides.data.right, {
      bytes: (n) => formatBytes(n, i18n.language),
      dateTime: (iso) => formatDateTime(iso, i18n.language),
    })
  }, [i18n.language, sides.data])

  return (
    <div className="kk-compare">
      <header className="kk-compare__header d-flex align-items-center justify-content-between gap-2">
        <div className="d-flex align-items-center gap-2">
          <Button variant="outline-secondary" size="sm" onClick={exit}>
            <Icon name="arrow-left" className="me-1" />
            {t('duplicates.compare.back')}
          </Button>
          <h1 className="h6 mb-0">{t('duplicates.compare.title')}</h1>
        </div>
        <div className="d-flex align-items-center gap-2">
          {pair !== null && (
            <span className="small text-secondary" data-testid="compare-progress">
              {t('duplicates.compare.progress', {
                current: index + 1,
                total: pairs.length,
              })}
            </span>
          )}
          <ZoomControls zoom={zoom} />
          <KeyboardShortcutsHelp />
        </div>
      </header>

      {actionError !== null && (
        <Alert
          variant="danger"
          className="mx-3 my-1 py-2"
          dismissible
          onClose={() => {
            setActionError(null)
          }}
        >
          {actionError}
        </Alert>
      )}

      {status === 'loading' && (
        <CompareCentre>
          <Spinner animation="border" />
        </CompareCentre>
      )}

      {status === 'unavailable' && (
        <CompareCentre>
          <Alert variant="warning" className="mb-0">
            {t('duplicates.unavailable')}
          </Alert>
        </CompareCentre>
      )}

      {status === 'error' && (
        <CompareCentre>
          <ErrorState
            title={t('duplicates.error')}
            onRetry={reloadQueue}
            action={
              <Button variant="outline-secondary" onClick={exit}>
                {t('duplicates.compare.back')}
              </Button>
            }
          />
        </CompareCentre>
      )}

      {status === 'ready' && pair === null && (
        <CompareCentre>
          <EmptyState
            title={t('duplicates.compare.done.title')}
            hint={t('duplicates.compare.done.hint')}
            action={
              <Button variant="primary" onClick={exit}>
                {t('duplicates.compare.back')}
              </Button>
            }
          />
        </CompareCentre>
      )}

      {status === 'ready' && pair !== null && (
        <>
          <div className="kk-compare__stage">
            {sides.error ? (
              <CompareCentre>
                <Alert variant="danger" className="mb-0">
                  {t('duplicates.compare.pairError')}
                </Alert>
              </CompareCentre>
            ) : sides.data === null ? (
              <CompareCentre>
                <Spinner animation="border" />
              </CompareCentre>
            ) : (
              <CompareStage
                zoom={zoom}
                left={{
                  uid: pair.leftUid,
                  src: thumbUrl(pair.leftUid, COMPARE_PREVIEW_SIZE, downloadToken ?? undefined),
                  alt: sides.data.left.photo.file_name,
                  caption: t('duplicates.compare.left'),
                  isKeeper: pair.leftUid === pair.group.keeper_uid,
                }}
                right={{
                  uid: pair.rightUid,
                  src: thumbUrl(pair.rightUid, COMPARE_PREVIEW_SIZE, downloadToken ?? undefined),
                  alt: sides.data.right.photo.file_name,
                  caption: t('duplicates.compare.right'),
                  isKeeper: pair.rightUid === pair.group.keeper_uid,
                }}
              />
            )}
          </div>

          <div className="kk-compare__diff">
            <div className="d-flex align-items-center justify-content-between gap-2 mb-2">
              <div className="small text-secondary">
                {pairsInGroup(pair.group) > 1 && (
                  <span className="me-2" data-testid="compare-group-note">
                    {t('duplicates.compare.groupNote', {
                      current: pairIndexInGroup(pair),
                      total: pairsInGroup(pair.group),
                    })}
                  </span>
                )}
                <span data-testid="compare-diff-summary">
                  {t('duplicates.compare.diff.summary', { count: countDiffering(rows) })}
                </span>
              </div>
              <Form.Check
                type="switch"
                id="compare-only-differences"
                label={t('duplicates.compare.diff.onlyDifferences')}
                checked={onlyDifferences}
                onChange={(e) => {
                  setOnlyDifferences(e.target.checked)
                }}
              />
            </div>
            {sides.data !== null && <DiffTable rows={rows} onlyDifferences={onlyDifferences} />}
          </div>

          <footer className="kk-compare__footer d-flex justify-content-center gap-2">
            <Button
              variant="outline-primary"
              disabled={busy || sides.data === null}
              onClick={() => {
                void beginKeep(pair.leftUid)
              }}
            >
              <Icon name="arrow-left" className="me-1" />
              {t('duplicates.compare.keepLeft')}
            </Button>
            <Button variant="outline-secondary" disabled={busy} onClick={keepBoth}>
              <Icon name="files" className="me-1" />
              {t('duplicates.compare.keepBoth')}
            </Button>
            <Button
              variant="outline-primary"
              disabled={busy || sides.data === null}
              onClick={() => {
                void beginKeep(pair.rightUid)
              }}
            >
              {t('duplicates.compare.keepRight')}
              <Icon name="arrow-right" className="ms-1" />
            </Button>
          </footer>
        </>
      )}

      <MergeConfirmModal
        preview={pending?.preview ?? null}
        busy={confirming}
        note={t('duplicates.compare.archiveNote')}
        onConfirm={() => {
          void confirmKeep()
        }}
        onCancel={() => {
          setPending(null)
        }}
      />
    </div>
  )
}

/** Centres a message or spinner in the space the stage would occupy. */
function CompareCentre({ children }: { children: React.ReactNode }) {
  return (
    <div className="kk-compare__stage d-flex align-items-center justify-content-center">
      {children}
    </div>
  )
}

/** The zoom in/out/reset controls, for users not reaching for the wheel. */
function ZoomControls({ zoom }: { zoom: ReturnType<typeof useSyncZoom> }) {
  const { t } = useTranslation()
  return (
    <ButtonGroup size="sm">
      <Button
        variant="outline-secondary"
        onClick={zoom.zoomOut}
        title={t('duplicates.compare.zoomOut')}
      >
        <Icon name="dash-lg" />
        <span className="visually-hidden">{t('duplicates.compare.zoomOut')}</span>
      </Button>
      <Button
        variant="outline-secondary"
        onClick={zoom.reset}
        title={t('duplicates.compare.zoomReset')}
      >
        <Icon name="arrows-angle-contract" />
        <span className="visually-hidden">{t('duplicates.compare.zoomReset')}</span>
      </Button>
      <Button
        variant="outline-secondary"
        onClick={zoom.zoomIn}
        title={t('duplicates.compare.zoomIn')}
      >
        <Icon name="plus-lg" />
        <span className="visually-hidden">{t('duplicates.compare.zoomIn')}</span>
      </Button>
    </ButtonGroup>
  )
}
