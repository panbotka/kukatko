import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { Virtuoso } from 'react-virtuoso'

import { formatDate, formatDateTimeMinutes } from '../../lib/format'
import { photoLabel } from '../../lib/photoTitle'
import { formatRelativeTime } from '../../lib/relativeTime'
import { formatTakenPeriod } from '../../lib/takenDate'
import { commentExcerpt, sourceLabel } from '../../lib/taskLedger'
import { type Photo, thumbUrl } from '../../services/photos'
import { Icon } from '../Icon'
import { type GridContext, GridFooter, type PhotoGridSelection } from '../library/PhotoGrid'

/** The square crop a row's thumbnail is drawn from — the avatar size, cache-warm. */
const ROW_THUMB_SIZE = 'tile_100'

/** Props for {@link TaskLedger}. */
export interface TaskLedgerProps {
  /** The task's group, in the listing's order — the same array the wall draws. */
  photos: readonly Photo[]
  loadingMore: boolean
  moreError: boolean
  /** Called when the reader reaches the end of the loaded rows; the next page follows. */
  onEndReached?: () => void
  onRetry: () => void
  /**
   * The same selection wiring the wall takes, so the batch bar's actions apply
   * from the ledger too. Omitted, the rows are plain links.
   */
  selection?: PhotoGridSelection
  /**
   * Query string appended to each row's detail link so the viewer inherits the
   * task scope (prev/next and Back stay inside the task) and this list's order.
   */
  detailQuery?: string
}

/** Props for {@link LedgerRow}. */
interface LedgerRowProps {
  photo: Photo
  href: string
  selection: PhotoGridSelection | undefined
  /** The whole list's order, for a Shift+click range. */
  orderedUids: readonly string[]
}

/**
 * One photograph as the line a reviewer reads: thumbnail, title, the date as
 * stated with its source, and the last word in its thread.
 *
 * The date is rendered at the precision the catalogue actually claims
 * (`lib/takenDate`): a photograph dated "June 1974" reads as that, never as
 * "1 June 1974". The source rides beside it as a badge and an estimated date
 * is flagged, because the two together are what a reviewer checks an agent's
 * work against — "manual, estimate" next to a comment saying why is the change
 * as the agent meant it; "exif" next to no comment is a photograph the batch
 * never touched.
 *
 * The title is the row's link and stretches over the row (`stretched-link`),
 * so clicking anywhere opens the photograph. The selection button sits above
 * that stretched surface. In selection-first mode — anything already picked —
 * the row toggles instead of opening, the same rule as the wall's tiles.
 */
function LedgerRow({ photo, href, selection, orderedUids }: LedgerRowProps) {
  const { t, i18n } = useTranslation()
  const locale = i18n.language
  const label = photoLabel(photo, locale)
  const selected = selection?.selected.has(photo.uid) ?? false
  const selectFirst = selection !== undefined && (selection.active || selection.selected.size > 0)
  const toggle = (shiftKey: boolean) => {
    if (selection === undefined) {
      return
    }
    if (shiftKey && selection.onToggleRange !== undefined) {
      selection.onToggleRange(photo.uid, [...orderedUids])
      return
    }
    selection.onToggle(photo.uid)
  }

  const period = formatTakenPeriod(photo.taken_at, photo.taken_at_precision, t, locale)
  const date =
    period !== ''
      ? period
      : photo.taken_at !== undefined && photo.taken_at !== ''
        ? formatDate(photo.taken_at, locale)
        : ''
  const source = sourceLabel(photo.taken_at_source, t)
  const comment = photo.last_comment ?? null
  const excerpt = commentExcerpt(comment)

  return (
    <div
      className={`kk-ledger-row${selected ? ' kk-ledger-row--selected' : ''}`}
      data-photo-uid={photo.uid}
      data-selected={selected ? 'true' : undefined}
    >
      {selection !== undefined && (
        <button
          type="button"
          className={`kk-ledger-row__check${selected ? ' kk-ledger-row__check--on' : ''}`}
          aria-pressed={selected}
          aria-label={t('selection.toggle', { name: label })}
          title={t('selection.toggle', { name: label })}
          onClick={(event) => {
            toggle(event.shiftKey)
          }}
        >
          {selected && <Icon name="check-lg" />}
        </button>
      )}
      <img
        className="kk-ledger-row__thumb"
        src={thumbUrl(photo.uid, ROW_THUMB_SIZE)}
        alt=""
        width={56}
        height={56}
        loading="lazy"
        decoding="async"
      />
      <Link
        to={href}
        className="kk-ledger-row__title stretched-link"
        title={label}
        onClick={
          selectFirst
            ? (event) => {
                event.preventDefault()
                toggle(event.shiftKey)
              }
            : undefined
        }
      >
        {label}
      </Link>
      <div className="kk-ledger-row__date">
        {date === '' ? (
          <span className="text-body-secondary">{t('taskDetail.ledger.noDate')}</span>
        ) : (
          <span>{date}</span>
        )}
        {source !== '' && (
          <Badge bg="secondary" className="fw-normal" title={t('photo.technical.takenAtSource')}>
            {source}
          </Badge>
        )}
        {photo.taken_at_estimated === true && (
          <Badge
            bg="warning"
            text="dark"
            className="fw-normal"
            title={t('photo.metadata.estimatedTitle')}
          >
            {t('taskDetail.ledger.estimated')}
          </Badge>
        )}
      </div>
      <div className="kk-ledger-row__comment">
        {comment === null || excerpt === '' ? (
          <span className="text-body-secondary">{t('taskDetail.ledger.noComment')}</span>
        ) : (
          <>
            <span className="kk-ledger-row__excerpt" title={comment.body}>
              {excerpt}
            </span>
            <span className="text-body-secondary small text-nowrap">
              {comment.author_name}
              {' · '}
              <time
                dateTime={comment.created_at}
                title={formatDateTimeMinutes(comment.created_at, locale)}
              >
                {formatRelativeTime(comment.created_at, locale)}
              </time>
            </span>
          </>
        )}
      </div>
    </div>
  )
}

/**
 * A task's group as a **review ledger**: one line per photograph, virtualized
 * with `react-virtuoso` and scrolled with the window like the wall it stands in
 * for. It draws the same array the wall draws, in the same order the filter
 * bar sorted it, and pages the same way (`onEndReached` + the shared footer),
 * so switching layouts changes how the group is read and nothing about what it
 * is.
 *
 * Built for the batch in review: an agent has changed sixty dates and left a
 * comment on each saying why, and the human approving it needs to read those
 * sixty lines rather than open sixty photographs. See `LedgerRow` for what a
 * line holds.
 */
export function TaskLedger({
  photos,
  loadingMore,
  moreError,
  onEndReached,
  onRetry,
  selection,
  detailQuery,
}: TaskLedgerProps) {
  const orderedUids = photos.map((photo) => photo.uid)
  const context: GridContext = { loadingMore, moreError, onRetry }
  const query = detailQuery !== undefined && detailQuery !== '' ? `?${detailQuery}` : ''
  return (
    <div className="kk-ledger">
      <Virtuoso
        useWindowScroll
        data={photos}
        context={context}
        endReached={onEndReached}
        components={{ Footer: GridFooter }}
        itemContent={(_index, photo) => (
          <LedgerRow
            photo={photo}
            href={`/photos/${photo.uid}${query}`}
            selection={selection}
            orderedUids={orderedUids}
          />
        )}
        computeItemKey={(_index, photo) => photo.uid}
      />
    </div>
  )
}
