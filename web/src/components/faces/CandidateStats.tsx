import { useTranslation } from 'react-i18next'

import { type CandidateResult } from '../../services/faces'

/** Props for {@link CandidateStats}. */
export interface CandidateStatsProps {
  /** The completed search result whose shape the numbers describe. */
  result: CandidateResult
}

/** One labelled figure in the stats row. */
function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="d-flex flex-column">
      <span className="fs-5 fw-semibold">{value}</span>
      <span className="small text-secondary">{label}</span>
    </div>
  )
}

/**
 * CandidateStats surfaces the numbers that shaped the result — how many source
 * photos and faces the search drew from, how many matches came back, how many were
 * already done — and, crucially, the computed `min_match_count`: the vote threshold a
 * photo had to clear. That filter is not hidden, it is explained in a line, so a
 * sparse result reads as "the bar was high", not "there is nothing there".
 */
export function CandidateStats({ result }: CandidateStatsProps) {
  const { t } = useTranslation()

  return (
    <div className="mb-3">
      <div className="d-flex flex-wrap gap-4">
        <Stat label={t('faceSearch.stats.sourcePhotos')} value={result.source_photo_count} />
        <Stat label={t('faceSearch.stats.sourceFaces')} value={result.source_face_count} />
        <Stat label={t('faceSearch.stats.matches')} value={result.candidates.length} />
        <Stat label={t('faceSearch.stats.alreadyDone')} value={result.counts.already_done} />
        <Stat label={t('faceSearch.stats.minMatch')} value={result.min_match_count} />
      </div>
      <p className="small text-secondary mt-2 mb-0">
        {t('faceSearch.stats.minMatchExplain', { count: result.min_match_count })}
      </p>
      {result.faces_without_embedding > 0 && (
        <p className="small text-warning mt-1 mb-0">
          {t('faceSearch.stats.noEmbeddingNote', { count: result.faces_without_embedding })}
        </p>
      )}
    </div>
  )
}
