import { type ReactNode, useEffect, useMemo, useState } from 'react'
import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { Icon, type IconName } from '../components/Icon'
import { ListSkeleton } from '../components/Skeleton'
import { useReloadKey } from '../hooks/useReloadKey'
import {
  fetchLeaderboard,
  LEADERBOARD_WINDOWS,
  type Leaderboard,
  type LeaderboardWindow,
} from '../services/review'

/**
 * The i18n label key for each window, kept as an explicit map (rather than a
 * template-literal key) so a typo is a compile error and the typed `t` accepts
 * it. Mirrors the pattern in {@link LanguageSwitcher}.
 */
const WINDOW_LABEL_KEYS = {
  all: 'leaderboard.window.all',
  '7d': 'leaderboard.window.week',
  today: 'leaderboard.window.today',
} as const

/**
 * The medal glyph and colour class for the top three ranks (0-indexed). Ranks
 * beyond the podium show a plain number. The glyph is decorative — the rank
 * number is always announced to assistive tech alongside it.
 */
const MEDALS: Partial<Record<number, { icon: IconName; className: string }>> = {
  0: { icon: 'trophy-fill', className: 'kk-medal--gold' },
  1: { icon: 'award-fill', className: 'kk-medal--silver' },
  2: { icon: 'award-fill', className: 'kk-medal--bronze' },
}

/**
 * Narrows an arbitrary query-string value to a supported window, defaulting to
 * all-time. An unknown value (an old bookmark, a hand-edited URL) degrades to
 * the default rather than erroring, matching the backend's tolerance.
 */
function parseWindow(raw: string | null): LeaderboardWindow {
  return LEADERBOARD_WINDOWS.find((candidate) => candidate === raw) ?? 'all'
}

/**
 * The sorting competition standings (`GET /review/leaderboard`): a ranked board
 * of who has recorded the most review decisions, with a top-three podium, the
 * caller's own row highlighted, and an all-time / last-7-days / today window
 * toggle whose state lives in the URL so "Back always works". Visible to every
 * signed-in role — watching the game is not a write action. See docs/FRONTEND.md.
 */
export function LeaderboardPage() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [searchParams, setSearchParams] = useSearchParams()
  const [reloadKey, reload] = useReloadKey()

  const window = parseWindow(searchParams.get('window'))

  const [board, setBoard] = useState<Leaderboard | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(false)
    fetchLeaderboard(window, controller.signal)
      .then((data) => {
        setBoard(data)
        setLoading(false)
      })
      .catch(() => {
        // A cancelled fetch (window switch or unmount) is not a failure; the
        // next effect run — or nothing, on unmount — owns the state from here.
        if (controller.signal.aborted) {
          return
        }
        setBoard(null)
        setError(true)
        setLoading(false)
      })
    return () => {
      controller.abort()
    }
  }, [window, reloadKey])

  /** Writes the chosen window into the URL, replacing history so the toggle
   * does not stack Back entries; the effect above refetches off the change. */
  function selectWindow(next: LeaderboardWindow) {
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev)
        params.set('window', next)
        return params
      },
      { replace: true },
    )
  }

  const onBoard = useMemo(
    () => board?.entries.some((entry) => entry.user_uid === user?.uid) ?? false,
    [board, user],
  )

  let content: ReactNode
  if (loading) {
    content = <ListSkeleton label={t('leaderboard.loading')} count={6} />
  } else if (error || board === null) {
    content = <ErrorState title={t('leaderboard.error.title')} onRetry={reload} />
  } else if (board.entries.length === 0) {
    content = (
      <EmptyState
        icon={<Icon name="trophy" />}
        title={t('leaderboard.empty.title')}
        hint={t('leaderboard.empty.hint')}
        action={
          <Link to="/review" className="btn btn-primary d-inline-flex align-items-center gap-2">
            <Icon name="ui-checks" />
            {t('leaderboard.empty.action')}
          </Link>
        }
      />
    )
  } else {
    content = (
      <>
        <Table responsive hover className="kk-leaderboard align-middle mb-0">
          <thead>
            <tr>
              <th scope="col" className="kk-leaderboard__rank">
                {t('leaderboard.columns.rank')}
              </th>
              <th scope="col">{t('leaderboard.columns.player')}</th>
              <th scope="col" className="text-end">
                {t('leaderboard.columns.yes')}
              </th>
              <th scope="col" className="text-end">
                {t('leaderboard.columns.no')}
              </th>
              <th scope="col" className="text-end">
                {t('leaderboard.columns.total')}
              </th>
            </tr>
          </thead>
          <tbody>
            {board.entries.map((entry, index) => {
              const isMe = entry.user_uid === user?.uid
              const medal = MEDALS[index]
              return (
                <tr
                  key={entry.user_uid}
                  data-testid={`leaderboard-row-${entry.user_uid}`}
                  className={isMe ? 'kk-leaderboard-row--me' : undefined}
                  aria-current={isMe ? 'true' : undefined}
                >
                  <td className="kk-leaderboard__rank">
                    {medal !== undefined ? (
                      <span
                        className={`kk-medal ${medal.className}`}
                        data-testid="leaderboard-medal"
                      >
                        <Icon name={medal.icon} />
                        <span className="visually-hidden">{index + 1}</span>
                      </span>
                    ) : (
                      index + 1
                    )}
                  </td>
                  <td>
                    {entry.display_name}
                    {isMe && (
                      <Badge bg="primary" className="ms-2">
                        {t('leaderboard.you')}
                      </Badge>
                    )}
                  </td>
                  <td className="text-end">{entry.yes_count}</td>
                  <td className="text-end">{entry.no_count}</td>
                  <td className="text-end fw-semibold">{entry.total}</td>
                </tr>
              )
            })}
          </tbody>
        </Table>
        {!onBoard && (
          <p className="text-secondary small mt-3 mb-0" data-testid="leaderboard-not-on-board">
            {t('leaderboard.notOnBoard.hint')}{' '}
            <Link to="/review">{t('leaderboard.notOnBoard.action')}</Link>
          </p>
        )}
      </>
    )
  }

  return (
    <>
      <div className="mb-3">
        <h1 className="kk-page-title mb-1">{t('leaderboard.title')}</h1>
        <p className="text-secondary mb-0">{t('leaderboard.subtitle')}</p>
      </div>

      <ButtonGroup size="sm" aria-label={t('leaderboard.window.label')} className="mb-3">
        {LEADERBOARD_WINDOWS.map((candidate) => {
          const isActive = candidate === window
          return (
            <Button
              key={candidate}
              variant={isActive ? 'light' : 'outline-light'}
              active={isActive}
              aria-pressed={isActive}
              onClick={() => {
                selectWindow(candidate)
              }}
            >
              {t(WINDOW_LABEL_KEYS[candidate])}
            </Button>
          )
        })}
      </ButtonGroup>

      {content}
    </>
  )
}
