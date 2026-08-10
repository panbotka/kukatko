import { fireEvent, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import type { Storyboard } from '../../services/photos'

import { VideoScrubber } from './VideoScrubber'

/** A ready storyboard: two rows of ten 160×90 frames, one every two seconds. */
const readyStoryboard: Storyboard = {
  status: 'ready',
  columns: 10,
  rows: 2,
  count: 20,
  tile_width: 160,
  tile_height: 90,
  interval_ms: 2000,
}

/**
 * Renders the scrubber over a 40-second clip and pins the track's geometry:
 * jsdom lays nothing out, so without a stubbed rect every pointer maps to
 * position zero and no seek could be asserted.
 */
function renderScrubber(storyboard: Storyboard | null, position = 0) {
  const onSeek = vi.fn()
  const utils = render(
    <I18nextProvider i18n={i18n}>
      <VideoScrubber
        uid="ph1"
        position={position}
        duration={40}
        storyboard={storyboard}
        onSeek={onSeek}
      />
    </I18nextProvider>,
  )
  const track = screen.getByRole('slider', { name: 'Video timeline' })
  vi.spyOn(track, 'getBoundingClientRect').mockReturnValue({
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 400,
    bottom: 4,
    width: 400,
    height: 4,
    toJSON: () => ({}),
  })
  return { ...utils, track, onSeek }
}

/** The preview bubble, or null when none is shown. */
function preview(container: HTMLElement): HTMLElement | null {
  return container.querySelector('.kk-video__preview')
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('VideoScrubber', () => {
  it('reports the playback position to assistive technology', () => {
    const { track } = renderScrubber(readyStoryboard, 10)
    expect(track).toHaveAttribute('aria-valuenow', '10')
    expect(track).toHaveAttribute('aria-valuemax', '40')
    expect(track).toHaveAttribute('aria-valuetext', '0:10 / 0:40')
  })

  it('seeks to where the timeline was clicked', () => {
    const { track, onSeek } = renderScrubber(readyStoryboard)
    fireEvent.pointerDown(track, { clientX: 100, pointerId: 1, pointerType: 'mouse' })
    // A quarter along a 40-second clip.
    expect(onSeek).toHaveBeenCalledWith(10)
  })

  it('keeps seeking while the pointer is dragged, and stops on release', () => {
    const { track, onSeek } = renderScrubber(readyStoryboard)
    fireEvent.pointerDown(track, { clientX: 100, pointerId: 1, pointerType: 'mouse' })
    fireEvent.pointerMove(track, { clientX: 300, pointerId: 1, pointerType: 'mouse' })
    expect(onSeek).toHaveBeenLastCalledWith(30)

    fireEvent.pointerUp(track, { clientX: 300, pointerId: 1, pointerType: 'mouse' })
    onSeek.mockClear()
    fireEvent.pointerMove(track, { clientX: 40, pointerId: 1, pointerType: 'mouse' })
    expect(onSeek).not.toHaveBeenCalled()
  })

  it('shows the storyboard frame for the hovered moment', () => {
    const { container, track } = renderScrubber(readyStoryboard)
    fireEvent.pointerMove(track, { clientX: 300, pointerId: 1, pointerType: 'mouse' })

    const bubble = preview(container)
    expect(bubble).not.toBeNull()
    expect(bubble).toHaveTextContent('0:30')
    const frame = bubble?.querySelector('.kk-video__preview-frame')
    expect(frame).not.toBeNull()
    // 30 s at one tile per 2 s is tile 15: row 1, column 5 of a ten-wide grid.
    expect(frame).toHaveStyle({ backgroundPosition: '-800px -90px' })
    expect(frame?.getAttribute('style')).toContain('/photos/ph1/storyboard/sprite')
  })

  it('hides the preview when the pointer leaves the timeline', () => {
    const { container, track } = renderScrubber(readyStoryboard)
    fireEvent.pointerMove(track, { clientX: 300, pointerId: 1, pointerType: 'mouse' })
    expect(preview(container)).not.toBeNull()
    fireEvent.pointerLeave(track)
    expect(preview(container)).toBeNull()
  })

  it('still shows the time bubble when the clip has no storyboard', () => {
    const { container, track } = renderScrubber(null)
    fireEvent.pointerMove(track, { clientX: 200, pointerId: 1, pointerType: 'mouse' })

    const bubble = preview(container)
    expect(bubble).toHaveTextContent('0:20')
    expect(bubble?.querySelector('.kk-video__preview-frame')).toBeNull()
  })

  it('shows no frame while the storyboard is still being generated', () => {
    const { container, track } = renderScrubber({ status: 'pending' })
    fireEvent.pointerMove(track, { clientX: 200, pointerId: 1, pointerType: 'mouse' })
    expect(preview(container)?.querySelector('.kk-video__preview-frame')).toBeNull()
  })

  it('shows no preview under a finger — the touch it would hide is the scrub', () => {
    const { container, track, onSeek } = renderScrubber(readyStoryboard)
    fireEvent.pointerDown(track, { clientX: 100, pointerId: 1, pointerType: 'touch' })
    fireEvent.pointerMove(track, { clientX: 300, pointerId: 1, pointerType: 'touch' })

    expect(preview(container)).toBeNull()
    // Dragging still seeks: only the preview is desktop-only.
    expect(onSeek).toHaveBeenLastCalledWith(30)
  })

  it('seeks with the arrow keys once the timeline has focus', () => {
    const { track, onSeek } = renderScrubber(readyStoryboard, 20)
    fireEvent.keyDown(track, { key: 'ArrowRight' })
    expect(onSeek).toHaveBeenLastCalledWith(25)
    fireEvent.keyDown(track, { key: 'ArrowLeft' })
    expect(onSeek).toHaveBeenLastCalledWith(15)
  })

  it('leaves keys it does not handle to the page', () => {
    const { track, onSeek } = renderScrubber(readyStoryboard, 20)
    const event = new KeyboardEvent('keydown', { key: 'f', bubbles: true, cancelable: true })
    track.dispatchEvent(event)
    expect(onSeek).not.toHaveBeenCalled()
    expect(event.defaultPrevented).toBe(false)
  })
})
