import { useId, useRef, useState, type DragEvent } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

/** Props for {@link DropZone}. */
export interface DropZoneProps {
  /** Receives files chosen via the picker, the camera, or drag-and-drop. */
  onFiles: (files: File[]) => void
}

/** Accept filter covering the photo and video formats the backend ingests. */
const ACCEPT = 'image/*,video/*'

/**
 * File selection surface: a large drag-and-drop target plus a hidden file input
 * triggered by a touch-friendly button. On mobile the input opens the gallery
 * (`accept="image/*,video/*"`, `multiple`); a second button opens the camera
 * directly via the `capture` attribute. Fully keyboard- and screen-reader
 * accessible — the label is wired to the input and the drop target is a button.
 */
export function DropZone({ onFiles }: DropZoneProps) {
  const { t } = useTranslation()
  const inputId = useId()
  const cameraInputRef = useRef<HTMLInputElement>(null)
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
    <div className="mb-4">
      {/* The label is the drop target and opens the picker on click/keyboard. */}
      <label
        htmlFor={inputId}
        className={`d-flex flex-column align-items-center justify-content-center text-center rounded border border-2 border-dashed p-4 p-md-5 ${
          dragActive ? 'border-primary bg-primary bg-opacity-10' : 'border-secondary'
        }`}
        style={{ cursor: 'pointer', minHeight: '10rem' }}
        onDrop={handleDrop}
        onDragOver={handleDragOver}
        onDragEnter={handleDragOver}
        onDragLeave={handleDragLeave}
      >
        <span className="kk-section-title mb-1">
          {dragActive ? t('upload.dropzone.active') : t('upload.dropzone.headline')}
        </span>
        <span className="text-secondary mb-3">{t('upload.dropzone.hint')}</span>
        <span className="btn btn-primary btn-lg" aria-hidden="true">
          {t('upload.dropzone.browse')}
        </span>
      </label>
      <input
        id={inputId}
        type="file"
        className="visually-hidden"
        accept={ACCEPT}
        multiple
        aria-label={t('upload.dropzone.ariaInput')}
        onChange={(event) => {
          emit(event.target.files)
          // Reset so picking the same file again re-fires change.
          event.target.value = ''
        }}
      />

      {/* Dedicated camera capture for mobile; harmless on desktop. */}
      <div className="d-grid d-sm-block mt-2">
        <Button
          type="button"
          variant="outline-secondary"
          size="lg"
          onClick={() => cameraInputRef.current?.click()}
        >
          {t('upload.dropzone.camera')}
        </Button>
      </div>
      <input
        ref={cameraInputRef}
        type="file"
        className="visually-hidden"
        accept="image/*"
        capture="environment"
        aria-label={t('upload.dropzone.ariaCamera')}
        onChange={(event) => {
          emit(event.target.files)
          event.target.value = ''
        }}
      />
    </div>
  )
}
