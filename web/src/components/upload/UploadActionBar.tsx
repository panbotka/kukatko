import { type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

/** The live batch state drawn into the bar while an upload is running. */
export interface UploadActionBarProgress {
  /** Overall completion in `[0, 100]`, already rounded. */
  percent: number
  /** The headline count, e.g. "7 / 20". */
  count: string
  /** What is left, e.g. "13 remaining". */
  remaining: string
}

/** Props for {@link UploadActionBar}. */
export interface UploadActionBarProps {
  /** Batch progress; omitted on a stage where nothing is moving. */
  progress?: UploadActionBarProgress
  /**
   * The stage's controls. Put the **primary last**: it then reads rightmost on a
   * wide screen and sits lowest — nearest the thumb — on a narrow one.
   */
  children: ReactNode
}

/**
 * The upload flow's one action area: a bar that sticks to the bottom of the
 * viewport and carries both what is happening and what to do next.
 *
 * It exists because the two used to be in different places. The old page put the
 * start button under a picker and the progress in a sticky header far above it,
 * so on a phone the control that mattered was whichever one the queue had just
 * pushed off screen. Here every stage ends in this bar, it never scrolls away,
 * and the progress is drawn *in* it — as the bar's own top edge filling up,
 * rather than as a boxed widget somewhere else on the page.
 *
 * `position: sticky` (not `fixed`) is what makes it behave the same on a phone
 * and on a desktop: it sits at the end of the page's own flow when the content
 * is short, and pins to the bottom edge — clear of the mobile tab bar and the
 * home indicator — as soon as the content is taller than the viewport. Nothing
 * has to reserve clearance for it.
 *
 * The fill is a real `progressbar`, so the count beside it is not the only way
 * to read the batch; a screen reader gets the percentage from the same element.
 */
export function UploadActionBar({ progress, children }: UploadActionBarProps) {
  const { t } = useTranslation()

  return (
    <div className="kk-upload-rail" data-testid="upload-action-bar">
      {progress !== undefined && (
        <div
          className="kk-upload-rail__track"
          role="progressbar"
          aria-label={t('upload.progress.barLabel')}
          aria-valuenow={progress.percent}
          aria-valuemin={0}
          aria-valuemax={100}
        >
          <div className="kk-upload-rail__fill" style={{ width: `${String(progress.percent)}%` }} />
        </div>
      )}
      <div className="kk-upload-rail__body">
        {progress !== undefined && (
          <div className="kk-upload-rail__status" aria-live="polite">
            <span className="kk-upload-rail__count">{progress.count}</span>
            <span className="kk-text-caption text-secondary">{progress.remaining}</span>
          </div>
        )}
        <div className="kk-upload-rail__actions">{children}</div>
      </div>
    </div>
  )
}
