import { useRef } from 'react'
import Button from 'react-bootstrap/Button'

import { PICKER_ACCEPT } from '../../lib/mediaFiles'

/** Props for {@link PickFilesButton}. */
export interface PickFilesButtonProps {
  /** Receives the chosen files; the input is reset so the same pick re-fires. */
  onFiles: (files: File[]) => void
  /** The button's label — the action itself ("Choose photos", "Add more files"). */
  label: string
  /** Accessible name of the hidden input the button stands in for. */
  inputLabel: string
  /** Bootstrap variant; the stage decides which of its two buttons is primary. */
  variant?: string
  /**
   * Opens the camera instead of the gallery (`capture`), narrowing `accept` to
   * images because that is all a camera can hand back.
   */
  camera?: boolean
  /** Extra classes for the button, e.g. sizing inside the action bar. */
  className?: string
}

/**
 * One way into the upload: a real button that opens the file picker, with its
 * `<input type="file">` hidden behind it.
 *
 * A bare input renders as a browser-styled control that cannot be sized for a
 * thumb or labelled in the app's voice, so every entry point in the flow — the
 * gallery, the camera, and "add more files" while a batch is already running —
 * is this button instead. Choosing files is also the start: the queue uploads
 * whatever it is given (see `useUploadQueue`), so there is no second control to
 * press afterwards and none of these buttons has a "then start" twin.
 */
export function PickFilesButton({
  onFiles,
  label,
  inputLabel,
  variant = 'primary',
  camera = false,
  className,
}: PickFilesButtonProps) {
  const inputRef = useRef<HTMLInputElement>(null)

  return (
    <>
      <Button
        type="button"
        variant={variant}
        size="lg"
        className={className}
        onClick={() => {
          inputRef.current?.click()
        }}
      >
        {label}
      </Button>
      <input
        ref={inputRef}
        type="file"
        className="visually-hidden"
        accept={camera ? 'image/*' : PICKER_ACCEPT}
        capture={camera ? 'environment' : undefined}
        multiple={!camera}
        aria-label={inputLabel}
        onChange={(event) => {
          const list = event.target.files
          if (list !== null && list.length > 0) {
            onFiles(Array.from(list))
          }
          // Reset so picking the same file again re-fires change.
          event.target.value = ''
        }}
      />
    </>
  )
}
