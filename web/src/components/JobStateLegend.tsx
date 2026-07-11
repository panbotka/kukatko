import { useTranslation } from 'react-i18next'

/**
 * A background job-queue state the legend can explain. These are the user-facing
 * buckets shown as badges on the Maintenance and System pages, not the raw
 * backend `jobs.state` values: `total` and `pending` are aggregates the backend
 * derives (every job on record, and work waiting on the AI box), while
 * `queued`/`running`/`failed`/`dead` map to real lifecycle states.
 */
export type JobStateKey = 'total' | 'queued' | 'running' | 'failed' | 'dead' | 'pending'

/** Props for {@link JobStateLegend}. */
interface JobStateLegendProps {
  /**
   * The states to explain, in display order. The Maintenance page omits
   * `pending` (it has no box-pending badge); the System page includes it.
   */
  states: readonly JobStateKey[]
}

/**
 * An always-visible, plain-language legend for the background job-queue states.
 * It renders a compact definition list — one bold term per state followed by a
 * muted one-sentence explanation — so an admin can tell what each badge count
 * means (and, crucially, what "Dead" implies) without reading the code or
 * relying on hover. Both labels and descriptions come from the shared
 * `jobStates.*` i18n block, so the wording stays identical wherever the states
 * appear.
 */
export function JobStateLegend({ states }: JobStateLegendProps) {
  const { t } = useTranslation()
  return (
    <dl className="small text-secondary mb-0">
      {states.map((state) => (
        <div key={state} className="mb-1">
          <dt className="d-inline fw-semibold text-body">{t(`jobStates.labels.${state}`)}</dt>
          {' — '}
          <dd className="d-inline mb-0">{t(`jobStates.descriptions.${state}`)}</dd>
        </div>
      ))}
    </dl>
  )
}
