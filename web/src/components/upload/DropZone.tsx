import { useId, useState, type DragEvent } from 'react'
import { useTranslation } from 'react-i18next'

import { PICKER_ACCEPT } from '../../lib/mediaFiles'

/** Props for {@link DropZone}. */
export interface DropZoneProps {
  /** Receives files dropped on the zone or chosen through it. */
  onFiles: (files: File[]) => void
}

/**
 * The first stage's one surface: a large target that takes a drag-and-drop on a
 * desktop and a tap on a phone, both opening the same multi-file picker
 * (`accept` groups images/videos and also names RAW/HEIC extensions so they are
 * not hidden — see `lib/mediaFiles`).
 *
 * Whatever it is given starts uploading at once, so the copy promises that
 * rather than a further step, and there is no start button under it to look for.
 * The camera and the labelled "choose photos" button live in the stage's action
 * bar, which is the one place a control is guaranteed to be reachable without
 * scrolling; this zone is the affordance, not the only way in. The footnote
 * names the third way, pasting, which the page listens for (see
 * `hooks/usePasteFiles`) but nothing on screen would otherwise mention.
 */
export function DropZone({ onFiles }: DropZoneProps) {
  const { t } = useTranslation()
  const inputId = useId()
  const [dragActive, setDragActive] = useState(false)

  const emit = (list: FileList | null): void => {
    if (list && list.length > 0) {
      onFiles(Array.from(list))
    }
  }

  const handleDrop = (event: DragEvent<HTMLLabelElement>): void => {
    event.preventDefault()
    setDragActive(false)
    emit(event.dataTransfer.files)
  }

  const handleDragOver = (event: DragEvent<HTMLLabelElement>): void => {
    event.preventDefault()
    setDragActive(true)
  }

  const handleDragLeave = (event: DragEvent<HTMLLabelElement>): void => {
    event.preventDefault()
    setDragActive(false)
  }

  return (
    <div>
      {/* The label is the drop target and opens the picker on click/keyboard. */}
      <label
        htmlFor={inputId}
        className={`kk-upload-drop${dragActive ? ' kk-upload-drop--active' : ''}`}
        onDrop={handleDrop}
        onDragOver={handleDragOver}
        onDragEnter={handleDragOver}
        onDragLeave={handleDragLeave}
      >
        <span className="kk-upload-drop__headline">
          {dragActive ? t('upload.pick.drop') : t('upload.pick.headline')}
        </span>
        <span className="text-secondary">{t('upload.pick.dropHint')}</span>
      </label>
      <input
        id={inputId}
        type="file"
        className="visually-hidden"
        accept={PICKER_ACCEPT}
        multiple
        aria-label={t('upload.pick.ariaInput')}
        onChange={(event) => {
          emit(event.target.files)
          // Reset so picking the same file again re-fires change.
          event.target.value = ''
        }}
      />

      <p className="kk-text-caption text-secondary mt-3 mb-0">{t('upload.pick.paste')}</p>
    </div>
  )
}
