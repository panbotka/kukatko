import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import * as photosService from '../../services/photos'

import { VideoPlayer } from './VideoPlayer'

vi.mock('../../services/photos', async () => {
  const actual = await vi.importActual<typeof photosService>('../../services/photos')
  return { ...actual, fetchStoryboard: vi.fn() }
})

const fetchStoryboard = vi.mocked(photosService.fetchStoryboard)

/**
 * Gives the rendered `<video>` a playable surface: jsdom implements neither
 * playback nor a media clock, so `play`/`pause` are stubbed and `duration` /
 * `currentTime` are turned into ordinary settable properties. Returns the
 * element so a test can drive it the way the browser would.
 */
function stubMedia(video: HTMLVideoElement, duration = 60): HTMLVideoElement {
  let paused = true
  let currentTime = 0
  Object.defineProperty(video, 'paused', { configurable: true, get: () => paused })
  Object.defineProperty(video, 'duration', { configurable: true, get: () => duration })
  Object.defineProperty(video, 'currentTime', {
    configurable: true,
    get: () => currentTime,
    set: (value: number) => {
      currentTime = value
    },
  })
  vi.spyOn(video, 'play').mockImplementation(() => {
    paused = false
    fireEvent.play(video)
    return Promise.resolve()
  })
  vi.spyOn(video, 'pause').mockImplementation(() => {
    paused = true
    fireEvent.pause(video)
  })
  return video
}

function renderPlayer(uid = 'ph1') {
  const utils = render(
    <I18nextProvider i18n={i18n}>
      <VideoPlayer
        uid={uid}
        title="Clip"
        poster="/poster.jpg"
        downloadHref="/api/v1/photos/ph1/download?original=true"
      />
    </I18nextProvider>,
  )
  const video = utils.container.querySelector('video')
  if (video === null) {
    throw new Error('expected a video element')
  }
  return { ...utils, video }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  window.sessionStorage.clear()
  fetchStoryboard.mockResolvedValue({ status: 'unavailable' })
})

describe('VideoPlayer', () => {
  it('streams the range endpoint and drives playback from its own controls', () => {
    const { video } = renderPlayer()
    expect(video.getAttribute('src')).toContain('/photos/ph1/video')
    expect(video.getAttribute('poster')).toBe('/poster.jpg')
    // The native controls are deliberately gone: the timeline is ours so the
    // scrub preview has somewhere to hang.
    expect(video.hasAttribute('controls')).toBe(false)

    stubMedia(video)
    fireEvent.click(screen.getByRole('button', { name: 'Play' }))
    expect(screen.getByRole('button', { name: 'Pause' })).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Pause' }))
    expect(screen.getByRole('button', { name: 'Play' })).toBeInTheDocument()
  })

  it('falls back to a download link when the codec cannot be played', () => {
    const { video } = renderPlayer()
    fireEvent.error(video)

    expect(screen.getByText('This video cannot be played in your browser.')).toBeInTheDocument()
    // react-bootstrap renders the styled anchor with role="button".
    const link = screen.getByRole('button', { name: 'Download the video' })
    expect(link).toHaveAttribute('href', '/api/v1/photos/ph1/download?original=true')
  })

  describe('skip controls', () => {
    it('jumps ten seconds back and forward, clamped to the clip', () => {
      const { video } = renderPlayer()
      stubMedia(video, 60)
      video.currentTime = 30

      fireEvent.click(screen.getByRole('button', { name: 'Forward 10 s' }))
      expect(video.currentTime).toBe(40)
      fireEvent.click(screen.getByRole('button', { name: 'Back 10 s' }))
      expect(video.currentTime).toBe(30)

      // Near the start a back-skip saturates at zero rather than going negative.
      video.currentTime = 4
      fireEvent.click(screen.getByRole('button', { name: 'Back 10 s' }))
      expect(video.currentTime).toBe(0)

      // Near the end a forward-skip saturates at the duration.
      video.currentTime = 55
      fireEvent.click(screen.getByRole('button', { name: 'Forward 10 s' }))
      expect(video.currentTime).toBe(60)
    })
  })

  describe('playback speed', () => {
    it('applies a chosen rate to the element and shows it on the control', async () => {
      const { video } = renderPlayer()
      stubMedia(video)

      fireEvent.click(screen.getByRole('button', { name: 'Playback speed' }))
      fireEvent.click(await screen.findByRole('button', { name: '1.5×' }))

      expect(video.playbackRate).toBe(1.5)
      expect(screen.getByRole('button', { name: 'Playback speed' })).toHaveTextContent('1.5×')
    })

    it('offers exactly the five documented speeds', async () => {
      const { container } = renderPlayer()
      fireEvent.click(screen.getByRole('button', { name: 'Playback speed' }))
      const menu = await waitFor(() => {
        const found = container.querySelector('.dropdown-menu.show')
        if (found === null) {
          throw new Error('the speed menu did not open')
        }
        return found as HTMLElement
      })
      const labels = within(menu)
        .getAllByRole('button')
        .map((item) => item.textContent)
      expect(labels).toEqual(['0.5×', '1×', '1.25×', '1.5×', '2×'])
    })

    it('remembers the rate for the session and re-applies it to the next clip', async () => {
      const first = renderPlayer()
      stubMedia(first.video)
      fireEvent.click(screen.getByRole('button', { name: 'Playback speed' }))
      fireEvent.click(await screen.findByRole('button', { name: '2×' }))
      first.unmount()

      const second = renderPlayer('ph2')
      await waitFor(() => {
        expect(second.video.playbackRate).toBe(2)
      })
      expect(screen.getByRole('button', { name: 'Playback speed' })).toHaveTextContent('2×')
    })
  })

  describe('keyboard shortcuts', () => {
    it('ignores J/K/L until the player is focused or playing', () => {
      const { video } = renderPlayer()
      stubMedia(video, 60)
      video.currentTime = 30

      // Nothing focused, nothing playing: the keys belong to the page, which uses
      // them to move between photos in the grid.
      fireEvent.keyDown(document, { key: 'l' })
      expect(video.currentTime).toBe(30)
      fireEvent.keyDown(document, { key: 'j' })
      expect(video.currentTime).toBe(30)
    })

    it('seeks and toggles playback once the player holds focus', () => {
      const { video } = renderPlayer()
      stubMedia(video, 60)
      video.currentTime = 30

      screen.getByRole('button', { name: 'Play' }).focus()
      fireEvent.focus(screen.getByRole('button', { name: 'Play' }))

      fireEvent.keyDown(document, { key: 'l' })
      expect(video.currentTime).toBe(40)
      fireEvent.keyDown(document, { key: 'j' })
      expect(video.currentTime).toBe(30)
      fireEvent.keyDown(document, { key: 'k' })
      expect(screen.getByRole('button', { name: 'Pause' })).toBeInTheDocument()
    })

    it('keeps answering while the clip plays, even without focus', () => {
      const { video } = renderPlayer()
      stubMedia(video, 60)
      video.currentTime = 10
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      fireEvent.keyDown(document, { key: 'l' })
      expect(video.currentTime).toBe(20)
    })

    it('steps the speed with < and > and stops at the ends', () => {
      const { video } = renderPlayer()
      stubMedia(video)
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      fireEvent.keyDown(document, { key: '>' })
      expect(video.playbackRate).toBe(1.25)
      fireEvent.keyDown(document, { key: '<' })
      expect(video.playbackRate).toBe(1)
      fireEvent.keyDown(document, { key: '<' })
      expect(video.playbackRate).toBe(0.5)
      // Already the slowest: another step is a no-op, not a wrap to 2×.
      fireEvent.keyDown(document, { key: '<' })
      expect(video.playbackRate).toBe(0.5)
    })

    it('never claims the arrow keys, so the page keeps paging between photos', () => {
      const { video } = renderPlayer()
      stubMedia(video, 60)
      video.currentTime = 30
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      const right = new KeyboardEvent('keydown', {
        key: 'ArrowRight',
        bubbles: true,
        cancelable: true,
      })
      document.dispatchEvent(right)

      expect(video.currentTime).toBe(30)
      expect(right.defaultPrevented).toBe(false)
    })
  })

  describe('storyboard', () => {
    it('does not ask for a storyboard before playback starts', () => {
      renderPlayer()
      expect(fetchStoryboard).not.toHaveBeenCalled()
    })

    it('asks once playback starts', async () => {
      const { video } = renderPlayer()
      stubMedia(video)
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      await waitFor(() => {
        expect(fetchStoryboard).toHaveBeenCalledWith('ph1', expect.anything())
      })
    })

    it('plays normally when the storyboard request fails', async () => {
      fetchStoryboard.mockRejectedValue(new Error('boom'))
      const { video } = renderPlayer()
      stubMedia(video)
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      await waitFor(() => {
        expect(fetchStoryboard).toHaveBeenCalled()
      })
      expect(screen.getByRole('button', { name: 'Pause' })).toBeInTheDocument()
      expect(screen.getByRole('slider', { name: 'Video timeline' })).toBeInTheDocument()
    })
  })
})
