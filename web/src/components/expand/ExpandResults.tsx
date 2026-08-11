import { useCallback, useMemo } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { type VirtuosoGridHandle } from 'react-virtuoso'

import { similarityPercent } from '../../lib/expandSearch'
import { REVIEW_GRID_SCOPE } from '../../lib/gridDensity'
import { type ExpandCandidate, type ExpandResult } from '../../services/expand'
import { type Photo } from '../../services/photos'
import { GridDensityControl } from '../library/GridDensityControl'
import { PhotoGrid, type PhotoGridSelection } from '../library/PhotoGrid'
import { Icon } from '../Icon'

/** Props for {@link ExpandResults}. */
export interface ExpandResultsProps {
  /** The search response the summary numbers come from. */
  result: ExpandResult
  /**
   * The candidates still on offer — the response minus everything already added
   * or rejected — so tiles leave the grid without a refetch or a scroll jump.
   */
  candidates: ExpandCandidate[]
  /** Selection wiring for the grid; `undefined` outside selection mode. */
  selection: PhotoGridSelection | undefined
  /**
   * Persists a "not this collection" rejection for the photo. Only labels have
   * a rejection model, so the caller passes it for labels and omits it for
   * albums — no ✗ is shown then.
   */
  onReject?: (photoUid: string) => void
  /** Imperative handle to the grid, for keyboard-navigation scrolling. */
  gridRef?: React.Ref<VirtuosoGridHandle>
  /** Index of the tile carrying the keyboard focus highlight, or -1. */
  focusedIndex?: number
  /** Opens the candidate at this index of `candidates` in the review lightbox. */
  onEnlarge: (index: number) => void
}

/** A callback that changes nothing: the result grid has no further pages. */
function noop() {
  // The expand search returns one bounded page; there is nothing to load.
}

/**
 * The per-tile overlay of one candidate: the similarity percentage, a vote
 * badge when more than one source photo matched it, the ⤢ that enlarges the
 * photo without leaving the results, and — when the collection models
 * rejections — the ✗ that persists "never offer this again". The badges are
 * `pe-none` so they never swallow a click meant for the tile.
 *
 * The enlarge control is a corner button rather than the tile itself, unlike the
 * card-based review tools: these tiles are the library's, and a tile-click means
 * "open this photo" everywhere else in the app. Two meanings on one gesture is
 * exactly what the shared review UX exists to avoid, so „look closer" gets its
 * own control and „leave for the photo" keeps the one it has.
 */
function CandidateExtras({
  candidate,
  onReject,
  onEnlarge,
}: {
  candidate: ExpandCandidate
  onReject?: (photoUid: string) => void
  onEnlarge: () => void
}) {
  const { t } = useTranslation()
  const percent = similarityPercent(candidate.similarity)
  return (
    <>
      <Button
        variant="dark"
        size="sm"
        className="position-absolute top-0 start-0 m-1 d-flex align-items-center p-1 lh-1 opacity-75"
        title={t('review.card.enlarge')}
        aria-label={t('review.card.enlarge')}
        onClick={onEnlarge}
        data-testid="review-enlarge"
      >
        <Icon name="arrows-angle-expand" />
      </Button>
      <span className="position-absolute bottom-0 end-0 m-1 d-flex gap-1 pe-none">
        {candidate.match_count > 1 && (
          <span
            className="badge text-bg-primary"
            role="img"
            aria-label={t('expand.tile.matches', { count: candidate.match_count })}
          >
            {t('expand.tile.matchesValue', { count: candidate.match_count })}
          </span>
        )}
        <span
          className="badge text-bg-dark opacity-75"
          role="img"
          aria-label={t('expand.tile.similarity', { percent })}
        >
          {t('expand.tile.similarityValue', { percent })}
        </span>
      </span>
      {onReject !== undefined && (
        <Button
          variant="danger"
          size="sm"
          className="position-absolute top-0 end-0 m-1 d-flex align-items-center p-1 lh-1"
          title={t('expand.tile.reject')}
          aria-label={t('expand.tile.reject')}
          onClick={() => {
            onReject(candidate.photo.uid)
          }}
        >
          <Icon name="x-lg" />
        </Button>
      )}
    </>
  )
}

/**
 * ExpandResults renders a finished expansion search: a summary row that makes
 * the ranking legible — how many source photos there are, how many actually
 * carry an embedding, the vote floor a candidate had to clear and the ordering
 * rule — above the standard virtualized photo grid. Each tile carries its
 * similarity percentage and, past one vote, a match-count badge; clicking a
 * tile opens the photo detail exactly like the library, and in selection mode
 * the tiles are the usual selection targets.
 */
export function ExpandResults({
  result,
  candidates,
  selection,
  onReject,
  gridRef,
  focusedIndex = -1,
  onEnlarge,
}: ExpandResultsProps) {
  const { t } = useTranslation()

  const byUid = useMemo(
    () =>
      new Map(candidates.map((candidate, index) => [candidate.photo.uid, { candidate, index }])),
    [candidates],
  )
  const photos = useMemo(() => candidates.map((candidate) => candidate.photo), [candidates])
  const tileExtras = useCallback(
    (photo: Photo) => {
      const found = byUid.get(photo.uid)
      if (found === undefined) {
        return null
      }
      return (
        <CandidateExtras
          candidate={found.candidate}
          onReject={onReject}
          onEnlarge={() => {
            onEnlarge(found.index)
          }}
        />
      )
    },
    [byUid, onReject, onEnlarge],
  )

  return (
    <>
      <div className="kk-surface p-3 mb-3">
        <div className="d-flex flex-wrap column-gap-3 row-gap-1">
          <span>{t('expand.summary.sourcePhotos', { count: result.source_photo_count })}</span>
          <span aria-hidden="true">·</span>
          <span>
            {t('expand.summary.withEmbedding', { count: result.source_photos_with_embedding })}
          </span>
          <span aria-hidden="true">·</span>
          <span>{t('expand.summary.minMatch', { count: result.min_match_count })}</span>
          <span aria-hidden="true">·</span>
          <span className="fw-semibold">
            {t('expand.summary.results', { count: candidates.length })}
          </span>
          {/* The stepper on the review tools' shared count: this is a judging
              grid, not the browsing wall, and it moves with the other tools. */}
          {candidates.length > 0 && (
            <span className="ms-auto">
              <GridDensityControl scope={REVIEW_GRID_SCOPE} />
            </span>
          )}
        </div>
        <div className="kk-text-caption text-secondary mt-2">
          <p className="mb-0">{t('expand.summary.voteRule', { count: result.min_match_count })}</p>
          <p className="mb-0">{t('expand.summary.ordering')}</p>
          {result.source_capped && (
            <p className="mb-0">
              {t('expand.summary.sampled', {
                sampled: result.source_photos_sampled,
                count: result.source_photo_count,
              })}
            </p>
          )}
        </div>
      </div>

      {candidates.length === 0 ? (
        <p className="text-secondary py-4 text-center mb-0">{t('expand.summary.allHandled')}</p>
      ) : (
        <PhotoGrid
          photos={photos}
          loadingMore={false}
          moreError={false}
          onEndReached={noop}
          onRetry={noop}
          selection={selection}
          gridRef={gridRef}
          focusedIndex={focusedIndex}
          tileExtras={tileExtras}
          scope={REVIEW_GRID_SCOPE}
        />
      )}
    </>
  )
}
