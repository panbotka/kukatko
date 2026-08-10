import { useEffect } from 'react'

/**
 * Hands the page every file pasted into it, for as long as the calling
 * component is mounted.
 *
 * This is the upload route that works where drag-and-drop does not. On an
 * iPhone — which has no Web Share Target, so the share sheet cannot reach
 * Kukátko at all (see `pwa/shareContract.ts`) — copying a photo in the Photos
 * app and pasting it here is the shortest path from the camera roll into the
 * library; on a desktop it is Ctrl-V after a screenshot.
 *
 * The listener is on `window` rather than on a drop zone so the gesture works
 * anywhere on the page, and it only acts when the clipboard actually carries
 * files: pasting text into the album or label pickers stays ordinary pasting.
 *
 * @param onFiles receives the pasted files; must be stable (a `useCallback`),
 *   or the listener is torn down and rebuilt on every render.
 */
export function usePasteFiles(onFiles: (files: File[]) => void): void {
  useEffect(() => {
    function handlePaste(event: ClipboardEvent): void {
      const files = Array.from(event.clipboardData?.files ?? [])
      if (files.length === 0) {
        return
      }
      // Only now: an image paste has no other meaning on this page, but a text
      // paste must keep working in the inputs.
      event.preventDefault()
      onFiles(files)
    }

    window.addEventListener('paste', handlePaste)
    return () => {
      window.removeEventListener('paste', handlePaste)
    }
  }, [onFiles])
}
