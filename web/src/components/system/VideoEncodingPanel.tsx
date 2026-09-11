import type { ParseKeys } from 'i18next'
import Badge from 'react-bootstrap/Badge'
import Card from 'react-bootstrap/Card'
import Table from 'react-bootstrap/Table'
import { useTranslation } from 'react-i18next'

import { formatCount, formatDateTime } from '../../lib/format'
import { countToneAlerts, countToneClass, type CountTone } from '../../lib/jobStateTone'
import { formatRelativeTime } from '../../lib/relativeTime'
import type { VideoStatus } from '../../services/system'
import { Icon } from '../Icon'

/** One line of the table: what it counts, how many, and how it is presented. */
interface VideoRow {
  /** Stable key, also the `data-testid` suffix. */
  key: string
  /** i18n key of the row's label. */
  labelKey: ParseKeys
  value: number
  /** True for the four states of an unencoded video, which are indented under it. */
  nested?: boolean
  /**
   * What the number counts, in the page's shared vocabulary — it is what the
   * number is coloured by. Omitted for the three rows that are facts about the
   * library rather than work in some state.
   */
  tone?: CountTone
}

/**
 * The rows, in the order the question is asked: how many videos there are, how
 * many can be streamed, how many cannot — and then, indented under it, what each
 * of those is waiting for. The four sub-rows are disjoint and add up to the row
 * above them, which is what lets a reader check the arithmetic on the screen.
 */
function rowsFor(video: VideoStatus): VideoRow[] {
  return [
    { key: 'videos', labelKey: 'system.video.videos', value: video.videos },
    { key: 'streamable', labelKey: 'system.video.streamable', value: video.streamable },
    // The backlog and the clips nobody has scheduled are both work waiting to
    // happen, so they take the queue's own "waiting" tone rather than a second
    // colour that would mean the same thing.
    { key: 'missing', labelKey: 'system.video.missing', value: video.missing, tone: 'queued' },
    {
      key: 'running',
      labelKey: 'system.video.encodeRunning',
      value: video.encode_running,
      nested: true,
      tone: 'running',
    },
    {
      key: 'queued',
      labelKey: 'system.video.encodeQueued',
      value: video.encode_queued,
      nested: true,
      tone: 'queued',
    },
    {
      key: 'failed',
      labelKey: 'system.video.encodeFailed',
      value: video.encode_failed,
      nested: true,
      tone: 'failed',
    },
    {
      key: 'not-scheduled',
      labelKey: 'system.video.notScheduled',
      value: video.not_scheduled,
      nested: true,
      tone: 'queued',
    },
    { key: 'renditions', labelKey: 'system.video.renditions', value: video.renditions },
  ]
}

/**
 * One count, coloured by what it counts — the page's shared rule, so a failed
 * encode here reads exactly like a failed job in the queue table above. A
 * failure that is actually there also carries a glyph, so the row never leans on
 * colour alone to say something went wrong.
 */
function StateCell({ row, locale }: { row: VideoRow; locale: string }) {
  const tone = row.tone ?? 'plain'
  const tint = countToneClass(tone, row.value)
  return (
    <td className={`text-end${tint === '' ? '' : ` ${tint}`}`} data-testid={`video-${row.key}`}>
      {countToneAlerts(tone, row.value) && <Icon name="exclamation-triangle" className="me-1" />}
      {formatCount(row.value, locale)}
    </td>
  )
}

/**
 * How long the longest-waiting encode has been waiting. A queue that has stopped
 * moving looks exactly like a busy one if only the depth is shown — the number
 * simply fails to fall — so the age of the oldest wait is what says which of the
 * two it is. The exact stamp rides along in the tooltip.
 */
function OldestWait({ at }: { at: string }) {
  const { t, i18n } = useTranslation()
  return (
    <p
      className="text-secondary kk-text-caption mt-3 mb-0"
      title={formatDateTime(at, i18n.language)}
      data-testid="video-oldest-wait"
    >
      {t('system.video.oldestWait', { age: formatRelativeTime(at, i18n.language) })}
    </p>
  )
}

/**
 * How far the streaming encode has got, counted over **videos** rather than over
 * jobs.
 *
 * The job queue above it already has an `hls_transcode` row, but that row counts
 * jobs and the queue keeps finished ones, so its totals are a lifetime tally: it
 * cannot answer the question an operator actually opens this page with — how much
 * of the library still plays as the whole original file, and is the queue moving.
 * The one state neither the queue nor the player can show is `not_scheduled`:
 * nothing is queued and nothing failed, so those clips simply sit there until a
 * backfill schedules them.
 *
 * With streaming switched off nothing is ever encoded, and "no video has a
 * streaming version" is the instance working as configured; the section says so
 * instead of showing the whole library as a backlog.
 */
export function VideoEncodingPanel({ video }: { video: VideoStatus }) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language
  return (
    <section className="mb-4" aria-labelledby="system-video-title">
      <h2 id="system-video-title" className="kk-section-title mb-1">
        {t('system.video.title')}
      </h2>
      <p className="text-secondary small">{t('system.video.intro')}</p>
      <Card>
        <Card.Body>
          {!video.streaming_enabled ? (
            <>
              <Badge bg="secondary">{t('system.video.disabledBadge')}</Badge>
              <p className="text-secondary small mt-2 mb-0" data-testid="video-disabled">
                {video.videos === 0
                  ? t('system.video.disabledEmpty')
                  : t('system.video.disabled', { count: video.videos })}
              </p>
            </>
          ) : video.videos === 0 ? (
            <p className="text-secondary small mb-0" data-testid="video-empty">
              {t('system.video.empty')}
            </p>
          ) : (
            <>
              <div className="table-responsive">
                <Table size="sm" className="align-middle mb-0">
                  <tbody>
                    {rowsFor(video).map((row) => (
                      <tr key={row.key}>
                        <th
                          scope="row"
                          className={`fw-normal${row.nested === true ? ' ps-4 text-secondary' : ''}`}
                        >
                          {t(row.labelKey)}
                        </th>
                        <StateCell row={row} locale={locale} />
                      </tr>
                    ))}
                  </tbody>
                </Table>
              </div>
              {video.oldest_queued_at !== undefined && <OldestWait at={video.oldest_queued_at} />}
              <p className="text-secondary kk-text-caption mt-3 mb-0">
                {t('system.video.renditionsNote')}
              </p>
            </>
          )}
        </Card.Body>
      </Card>
    </section>
  )
}
