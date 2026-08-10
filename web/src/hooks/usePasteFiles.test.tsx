import { render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { usePasteFiles } from './usePasteFiles'

/** A paste event carrying `files`, since jsdom's clipboard cannot hold one. */
function pasteEvent(files: File[]): Event {
  const event = new Event('paste', { cancelable: true })
  Object.defineProperty(event, 'clipboardData', { value: { files } })
  return event
}

/** A paste of plain text: a clipboard with no files at all. */
function textPasteEvent(): Event {
  const event = new Event('paste', { cancelable: true })
  Object.defineProperty(event, 'clipboardData', { value: { files: [] } })
  return event
}

/** Mounts a component that does nothing but listen for pastes. */
function renderListener(onFiles: (files: File[]) => void) {
  function Listener() {
    usePasteFiles(onFiles)
    return null
  }
  return render(<Listener />)
}

describe('usePasteFiles', () => {
  it('hands over every pasted file and claims the event', () => {
    const onFiles = vi.fn()
    const file = new File(['x'], 'pasted.png', { type: 'image/png' })
    renderListener(onFiles)

    const event = pasteEvent([file])
    window.dispatchEvent(event)

    expect(onFiles).toHaveBeenCalledWith([file])
    expect(event.defaultPrevented).toBe(true)
  })

  it('leaves a text paste alone, so the pickers keep working', () => {
    const onFiles = vi.fn()
    renderListener(onFiles)

    const event = textPasteEvent()
    window.dispatchEvent(event)

    expect(onFiles).not.toHaveBeenCalled()
    expect(event.defaultPrevented).toBe(false)
  })

  it('survives an event with no clipboard data at all', () => {
    const onFiles = vi.fn()
    renderListener(onFiles)

    expect(() => {
      window.dispatchEvent(new Event('paste'))
    }).not.toThrow()
    expect(onFiles).not.toHaveBeenCalled()
  })

  it('stops listening once the page is gone', () => {
    const onFiles = vi.fn()
    const file = new File(['x'], 'pasted.png', { type: 'image/png' })
    const { unmount } = renderListener(onFiles)

    unmount()
    window.dispatchEvent(pasteEvent([file]))

    expect(onFiles).not.toHaveBeenCalled()
  })
})
