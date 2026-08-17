import { useTranslation } from 'react-i18next'

import { DropZone } from './DropZone'
import { PickFilesButton } from './PickFilesButton'
import { UploadActionBar } from './UploadActionBar'

/** Props for {@link UploadStagePick}. */
export interface UploadStagePickProps {
  /** Receives the picked files — which is also what starts the upload. */
  onFiles: (files: File[]) => void
}

/**
 * Stage one: pick the files, and nothing else.
 *
 * An empty upload page used to show all three stages at once — an album picker
 * with no batch to apply it to, and an empty queue explaining what would appear
 * in it. Here the page holds exactly one question, "which photos?", and answering
 * it moves straight to stage two with bytes already going out; there is no album
 * field to fill in first and no start button to find afterwards.
 *
 * The drop zone is the affordance for a mouse; the action bar carries the two
 * controls a phone actually uses — the gallery and the camera — where they
 * cannot scroll out of reach.
 */
export function UploadStagePick({ onFiles }: UploadStagePickProps) {
  const { t } = useTranslation()

  return (
    <section className="kk-upload-stage" aria-labelledby="upload-stage-title">
      <div>
        <h2 id="upload-stage-title" className="kk-section-title mb-1">
          {t('upload.pick.title')}
        </h2>
        <p className="text-secondary mb-0">{t('upload.pick.lead')}</p>
      </div>

      <DropZone onFiles={onFiles} />

      <UploadActionBar>
        <PickFilesButton
          onFiles={onFiles}
          label={t('upload.pick.camera')}
          inputLabel={t('upload.pick.ariaCamera')}
          variant="outline-secondary"
          camera
        />
        <PickFilesButton
          onFiles={onFiles}
          label={t('upload.pick.choose')}
          inputLabel={t('upload.pick.ariaInput')}
        />
      </UploadActionBar>
    </section>
  )
}
