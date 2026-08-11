import { type RefObject } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { type CandidateReview } from '../../hooks/useCandidateReview'
import { candidateKey, type FilterTab, type ReviewItem } from '../../lib/candidateReview'
import { gridTemplateColumns, REVIEW_GRID_SCOPE } from '../../lib/gridDensity'
import { type CandidateResult } from '../../services/faces'
import { EmptyState } from '../EmptyState'
import { Icon } from '../Icon'
import { GridDensityControl } from '../library/GridDensityControl'

import { CandidateCard } from './CandidateCard'
import { CandidateFilterTabs } from './CandidateFilterTabs'
import { CandidateLegend } from './CandidateLegend'
import { CandidateStats } from './CandidateStats'

/** Props for {@link CandidateResults}. */
export interface CandidateResultsProps {
  /** The completed search result being reviewed. */
  result: CandidateResult
  /** The review controller (working list, actions, confirm-all progress). */
  review: CandidateReview
  /** The selected filter tab. */
  activeTab: FilterTab
  /** Selects a filter tab. */
  onSelectTab: (tab: FilterTab) => void
  /** The items visible under the active tab, in display order. */
  visible: ReviewItem[]
  /** Index of the keyboard-focused card within `visible`, or -1. */
  focusedIndex: number
  /** How many visible cards "confirm all" would act on. */
  actionableCount: number
  /** Grid element ref, used by the page for column count and scroll-into-view. */
  gridRef: RefObject<HTMLDivElement | null>
  /** How many cards sit side by side — the review tools' shared column count. */
  density: number
  /** Opens the card at this index of `visible` in the lightbox. */
  onEnlarge: (index: number) => void
}

/**
 * CandidateResults renders the outcome of a completed candidate search. For the
 * structural empty cases it explains why there is nothing to do (the subject has no
 * faces, its faces have no embeddings yet, or nothing matched). Otherwise it shows
 * the stats, the filter tabs, the batch "confirm all" control, the density stepper,
 * the colour legend and the grid of large candidate cards.
 *
 * How many cards fit across is the user's, on the count every review tool shares
 * (`REVIEW_GRID_SCOPE`) — the grid used to be a fixed `minmax(16rem, 1fr)`, which
 * survives only as the width that count is seeded from on a device's first visit.
 */
export function CandidateResults({
  result,
  review,
  activeTab,
  onSelectTab,
  visible,
  focusedIndex,
  actionableCount,
  gridRef,
  density,
  onEnlarge,
}: CandidateResultsProps) {
  const { t } = useTranslation()

  if (result.reason === 'no_faces') {
    return (
      <EmptyState
        icon={<Icon name="person-circle" />}
        title={t('faceSearch.noFaces.title')}
        hint={t('faceSearch.noFaces.hint')}
      />
    )
  }
  if (result.reason === 'no_embeddings') {
    return (
      <EmptyState
        icon={<Icon name="person-circle" />}
        title={t('faceSearch.noEmbeddings.title')}
        hint={t('faceSearch.noEmbeddings.hint')}
      />
    )
  }
  if (result.candidates.length === 0) {
    return (
      <EmptyState
        icon={<Icon name="people" />}
        title={t('faceSearch.zero.title')}
        hint={t('faceSearch.zero.hint')}
      />
    )
  }

  const { running, current, total } = review.confirmAllState

  return (
    <>
      <CandidateStats result={result} />

      <div className="d-flex flex-wrap gap-3 align-items-center mb-3">
        <CandidateFilterTabs
          active={activeTab}
          counts={review.counts}
          onSelect={onSelectTab}
          disabled={running}
        />
        <div className="ms-auto d-flex flex-wrap align-items-center gap-3">
          {/* The library's stepper, on the review tools' own stored count — see
              REVIEW_GRID_SCOPE for why it is not the library's number. */}
          <GridDensityControl scope={REVIEW_GRID_SCOPE} />
          {running ? (
            <Button variant="outline-secondary" onClick={review.cancelConfirmAll}>
              <Icon name="x-lg" /> {t('faceSearch.confirmAll.progress', { current, total })}
            </Button>
          ) : (
            <Button
              variant="primary"
              onClick={() => {
                review.confirmAll(activeTab)
              }}
              disabled={actionableCount === 0}
            >
              <Icon name="check-lg" /> {t('faceSearch.confirmAll.idle', { n: actionableCount })}
            </Button>
          )}
        </div>
      </div>

      <CandidateLegend />

      {visible.length === 0 ? (
        <p className="text-secondary mt-3">{t('faceSearch.tabEmpty')}</p>
      ) : (
        <div
          ref={gridRef}
          className="d-grid mt-3"
          data-density={density}
          style={{
            gridTemplateColumns: gridTemplateColumns(density),
            gap: `${String(REVIEW_GRID_SCOPE.gapPx)}px`,
          }}
        >
          {visible.map((item, index) => (
            <CandidateCard
              key={candidateKey(item.candidate)}
              item={item}
              focused={index === focusedIndex}
              onConfirm={() => {
                review.confirm(item.candidate)
              }}
              onReject={() => {
                review.reject(item.candidate)
              }}
              onEnlarge={() => {
                onEnlarge(index)
              }}
            />
          ))}
        </div>
      )}
    </>
  )
}
