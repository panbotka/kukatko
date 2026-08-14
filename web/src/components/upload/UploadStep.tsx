import { useId, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

/** Props for {@link UploadStep}. */
export interface UploadStepProps {
  /** 1-based position, drawn in the step marker and spoken before the title. */
  index: number
  /** What this step is for, as a heading. */
  title: string
  /** One line saying what the step decides; omitted when the title says it all. */
  hint?: string
  /** Right-aligned state of the step, e.g. how many files are picked. */
  status?: ReactNode
  /** The step's controls. */
  children: ReactNode
}

/**
 * One stage of the upload flow, as a numbered section. The upload page is three
 * of these — pick files, organise the batch, start — and the marker plus the
 * plain top-to-bottom order is what makes that sequence readable instead of
 * something the user has to infer from a wall of controls.
 *
 * The numeral is decorative (`aria-hidden`): a screen reader hears it in the
 * heading instead, as a visually hidden "Step 2:" prefix, so the same ordering
 * information reaches both without the digit being read twice.
 */
export function UploadStep({ index, title, hint, status, children }: UploadStepProps) {
  const { t } = useTranslation()
  const headingId = useId()

  return (
    <section className="kk-upload-step mb-4" aria-labelledby={headingId} data-testid="upload-step">
      <div className="d-flex align-items-start gap-3 mb-2">
        <span className="kk-upload-step__marker" aria-hidden="true">
          {index}
        </span>
        <div className="flex-grow-1 kk-min-w-0">
          <h2 id={headingId} className="kk-section-title mb-0">
            <span className="visually-hidden">{t('upload.step.aria', { index })} </span>
            {title}
          </h2>
          {hint !== undefined && <p className="kk-text-caption text-secondary mb-0">{hint}</p>}
        </div>
        {status !== undefined && (
          <div className="flex-shrink-0 kk-text-caption text-secondary">{status}</div>
        )}
      </div>
      {children}
    </section>
  )
}
