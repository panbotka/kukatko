import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import * as photosService from '../../services/photos'
import { stubFullscreen, stubPlayableMedia } from '../../test/media'

import { VideoPlayer, type VideoPlayerProps } from './VideoPlayer'

vi.mock('../../services/photos', async () => {
  const actual = await vi.importActual<typeof photosService>('../../services/photos')
  return { ...actual, fetchStoryboard: vi.fn() }
})

const fetchStoryboard = vi.mocked(photosService.fetchStoryboard)

/**
 * A stand-in for hls.js. Real playback needs Media Source Extensions and a real
 * decoder — neither of which jsdom has, and neither of which this component owns:
 * what it must get right is *whether* the library is reached for, what it is
 * pointed at, that it is attached to the player's own element, and what happens
 * when it gives up. The fake records exactly that and lets a test raise an error
 * the way the library would.
 */
const hlsjs = vi.hoisted(() => {
  interface FakeInstance {
    source: string | null
    media: HTMLMediaElement | null
    destroyed: boolean
    raise: (fatal: boolean) => void
  }
  const state = { supported: true, throwOnCreate: false, instances: [] as FakeInstance[] }

  class FakeHls implements FakeInstance {
    static readonly Events = { ERROR: 'hlsError' }

    source: string | null = null
    media: HTMLMediaElement | null = null
    destroyed = false
    private listeners: ((event: string, data: { fatal: boolean }) => void)[] = []

    constructor() {
      if (state.throwOnCreate) {
        throw new Error('hls.js failed to initialise')
      }
      state.instances.push(this)
    }

    static isSupported(): boolean {
      return state.supported
    }

    on(event: string, handler: (event: string, data: { fatal: boolean }) => void): void {
      if (event === FakeHls.Events.ERROR) {
        this.listeners.push(handler)
      }
    }

    loadSource(source: string): void {
      this.source = source
    }

    attachMedia(media: HTMLMediaElement): void {
      this.media = media
    }

    destroy(): void {
      this.destroyed = true
    }

    raise(fatal: boolean): void {
      for (const handler of this.listeners) {
        handler(FakeHls.Events.ERROR, { fatal })
      }
    }
  }

  return { state, FakeHls }
})

vi.mock('hls.js/light', () => ({ default: hlsjs.FakeHls }))

/** Makes the browser claim (or deny) that it plays HLS playlists itself. */
function stubNativeHls(supported: boolean): void {
  vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue(supported ? 'maybe' : '')
}

/** Waits for the lazily imported library to have been attached, and returns it. */
async function attachedHls() {
  await waitFor(() => {
    expect(hlsjs.state.instances).toHaveLength(1)
  })
  return hlsjs.state.instances[0]
}

/**
 * Gives every element a laid-out box, which jsdom otherwise reports as zero. The
 * poster overlay is placed from the element's own box, so without this there is
 * no rectangle to place it on. Returns the undo.
 */
function stubLayout(width: number, height: number): () => void {
  const boxes: Record<string, number> = {
    offsetWidth: width,
    offsetHeight: height,
    offsetLeft: 0,
    offsetTop: 0,
  }
  for (const [name, value] of Object.entries(boxes)) {
    Object.defineProperty(HTMLElement.prototype, name, { configurable: true, get: () => value })
  }
  return () => {
    for (const name of Object.keys(boxes)) {
      Reflect.deleteProperty(HTMLElement.prototype, name)
    }
  }
}

/** Puts the keyboard focus inside the player, which is one of the two ways it
 * comes to own the video keys (the other is the clip simply playing). */
function focusPlayer(): void {
  const play = screen.getByRole('button', { name: 'Play' })
  play.focus()
  fireEvent.focus(play)
}

function renderPlayer(uid = 'ph1', streaming = false, extra: Partial<VideoPlayerProps> = {}) {
  const utils = render(
    <I18nextProvider i18n={i18n}>
      <VideoPlayer
        uid={uid}
        title="Clip"
        poster="/poster.jpg"
        downloadHref="/api/v1/photos/ph1/download?original=true"
        streaming={streaming}
        {...extra}
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
  hlsjs.state.supported = true
  hlsjs.state.throwOnCreate = false
  hlsjs.state.instances = []
})

describe('VideoPlayer', () => {
  it('streams the range endpoint and drives playback from its own controls', () => {
    const { video } = renderPlayer()
    expect(video.getAttribute('src')).toContain('/photos/ph1/video')
    expect(video.getAttribute('poster')).toBe('/poster.jpg')
    // The native controls are deliberately gone: the timeline is ours so the
    // scrub preview has somewhere to hang.
    expect(video.hasAttribute('controls')).toBe(false)

    stubPlayableMedia(video)
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
      stubPlayableMedia(video, 60)
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
      stubPlayableMedia(video)

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
      stubPlayableMedia(first.video)
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
      stubPlayableMedia(video, 60)
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
      stubPlayableMedia(video, 60)
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
      stubPlayableMedia(video, 60)
      video.currentTime = 10
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      fireEvent.keyDown(document, { key: 'l' })
      expect(video.currentTime).toBe(20)
    })

    it('steps the speed with < and > and stops at the ends', () => {
      const { video } = renderPlayer()
      stubPlayableMedia(video)
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

    it('seeks five seconds with the arrows while it owns the keyboard', () => {
      const { video } = renderPlayer()
      stubPlayableMedia(video, 60)
      video.currentTime = 30
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      // Prevented, because the press was ours: while the clip runs the arrows
      // are a fine seek, not a step to the next photograph.
      expect(fireEvent.keyDown(document, { key: 'ArrowRight' })).toBe(false)
      expect(video.currentTime).toBe(35)
      fireEvent.keyDown(document, { key: 'ArrowLeft' })
      expect(video.currentTime).toBe(30)
    })

    it('leaves the arrows to the page while it owns nothing', () => {
      const { video } = renderPlayer()
      stubPlayableMedia(video, 60)
      video.currentTime = 30

      // Not focused, not playing: the press passes straight through to the page,
      // which uses it to step between photographs.
      expect(fireEvent.keyDown(document, { key: 'ArrowRight' })).toBe(true)
      expect(video.currentTime).toBe(30)
    })

    it('toggles playback once — never twice — for a space with the play button focused', () => {
      const { video } = renderPlayer()
      const media = stubPlayableMedia(video, 60)
      const play = screen.getByRole('button', { name: 'Play' })
      focusPlayer()

      // The trap: the button under the focus would activate on the same press,
      // so the player takes the key itself and suppresses the default. One
      // press, one toggle — and no scroll of the page behind the viewer.
      expect(fireEvent.keyDown(play, { key: ' ' })).toBe(false)
      expect(media.play).toHaveBeenCalledTimes(1)
      expect(screen.getByRole('button', { name: 'Pause' })).toBeInTheDocument()

      const pause = screen.getByRole('button', { name: 'Pause' })
      pause.focus()
      fireEvent.keyDown(pause, { key: ' ' })
      expect(media.pause).toHaveBeenCalledTimes(1)
      expect(screen.getByRole('button', { name: 'Play' })).toBeInTheDocument()
    })

    it('answers the space bar from the page while the clip is playing', () => {
      const { video } = renderPlayer()
      const media = stubPlayableMedia(video, 60)
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      fireEvent.keyDown(document, { key: ' ' })
      expect(media.pause).toHaveBeenCalledTimes(1)
      expect(screen.getByRole('button', { name: 'Play' })).toBeInTheDocument()
    })

    it('toggles playback with k as well as with the space bar', () => {
      const { video } = renderPlayer()
      const media = stubPlayableMedia(video, 60)
      focusPlayer()

      fireEvent.keyDown(document, { key: 'k' })
      expect(media.play).toHaveBeenCalledTimes(1)
    })

    it('opens the clip at that tenth of it for every digit', () => {
      const { video } = renderPlayer()
      stubPlayableMedia(video, 60)
      focusPlayer()

      fireEvent.keyDown(document, { key: '3' })
      expect(video.currentTime).toBe(18)
      fireEvent.keyDown(document, { key: '9' })
      expect(video.currentTime).toBe(54)
      // `0` is the way back to the start, not a tenth like the others.
      fireEvent.keyDown(document, { key: '0' })
      expect(video.currentTime).toBe(0)
    })

    it('mutes and unmutes with m', () => {
      const { video } = renderPlayer()
      stubPlayableMedia(video, 60)
      focusPlayer()

      fireEvent.keyDown(document, { key: 'm' })
      expect(video.muted).toBe(true)
      expect(screen.getByRole('button', { name: 'Unmute' })).toBeInTheDocument()
      fireEvent.keyDown(document, { key: 'm' })
      expect(video.muted).toBe(false)
      expect(screen.getByRole('button', { name: 'Mute' })).toBeInTheDocument()
    })

    it('fills the screen with f and leaves it again with Escape', () => {
      const undo = stubFullscreen()
      try {
        const scope = vi.fn()
        const { video, container } = renderPlayer('ph1', false, { onKeyboardScope: scope })
        stubPlayableMedia(video, 60)
        focusPlayer()

        fireEvent.keyDown(document, { key: 'f' })
        expect(document.fullscreenElement).toBe(container.querySelector('.kk-video'))
        expect(scope).toHaveBeenLastCalledWith({ active: true, fullscreen: true })

        // Escape belongs to leaving fullscreen here, and the viewer around the
        // player is told so — it must not read the same press as "close".
        expect(fireEvent.keyDown(document, { key: 'Escape' })).toBe(false)
        expect(document.fullscreenElement).toBeNull()
        expect(scope).toHaveBeenLastCalledWith({ active: true, fullscreen: false })

        // A second f puts it back: the key is a toggle, not a one-way door.
        fireEvent.keyDown(document, { key: 'f' })
        expect(document.fullscreenElement).not.toBeNull()
        fireEvent.keyDown(document, { key: 'f' })
        expect(document.fullscreenElement).toBeNull()
      } finally {
        undo()
      }
    })

    it('never takes Escape outside fullscreen, so the viewer keeps its way out', () => {
      const { video } = renderPlayer()
      stubPlayableMedia(video, 60)
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      // Playing, so the player owns the video keys — but not this one.
      expect(fireEvent.keyDown(document, { key: 'Escape' })).toBe(true)
    })

    it('steps a paused clip by about a frame with , and ., and refuses while it runs', () => {
      const { video } = renderPlayer()
      stubPlayableMedia(video, 60)
      video.currentTime = 10
      focusPlayer()

      fireEvent.keyDown(document, { key: ',' })
      expect(video.currentTime).toBeCloseTo(9.96, 5)
      fireEvent.keyDown(document, { key: '.' })
      expect(video.currentTime).toBeCloseTo(10, 5)

      // Running, the step is a no-op: a 1/25 s nudge of a moving picture is
      // nothing anybody can see, and the reader asked to look at one frame.
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))
      const running = video.currentTime
      fireEvent.keyDown(document, { key: '.' })
      expect(video.currentTime).toBe(running)
    })

    it('claims nothing at all while it is neither focused nor playing', () => {
      const { video } = renderPlayer()
      const media = stubPlayableMedia(video, 60)
      video.currentTime = 30

      for (const key of [' ', 'm', 'f', '4', ',', '.']) {
        expect(fireEvent.keyDown(document, { key })).toBe(true)
      }
      expect(video.currentTime).toBe(30)
      expect(video.muted).toBe(false)
      expect(media.play).not.toHaveBeenCalled()
      expect(screen.getByRole('button', { name: 'Play' })).toBeInTheDocument()
    })

    it('reports what it claims of the keyboard, and hands it all back when it goes', () => {
      const scope = vi.fn()
      const { video, unmount } = renderPlayer('ph1', false, { onKeyboardScope: scope })
      stubPlayableMedia(video, 60)

      expect(scope).toHaveBeenLastCalledWith({ active: false, fullscreen: false })

      focusPlayer()
      expect(scope).toHaveBeenLastCalledWith({ active: true, fullscreen: false })

      // Focus leaves, but the clip is running — still the player's keyboard.
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))
      fireEvent.blur(screen.getByRole('button', { name: 'Pause' }), { relatedTarget: null })
      expect(scope).toHaveBeenLastCalledWith({ active: true, fullscreen: false })

      // Paging to a still photograph unmounts the player: the arrows and the
      // digits go straight back to the page.
      unmount()
      expect(scope).toHaveBeenLastCalledWith({ active: false, fullscreen: false })
    })
  })

  describe('HLS', () => {
    it('never loads the library for a photo with no rendition', async () => {
      const { video } = renderPlayer('ph1', false)

      expect(video.getAttribute('src')).toContain('/photos/ph1/video')
      // Give the lazy import a turn: nothing may have asked for it.
      await Promise.resolve()
      expect(hlsjs.state.instances).toHaveLength(0)
    })

    it('attaches the library to the player element and points it at the master playlist', async () => {
      stubNativeHls(false)
      const { video } = renderPlayer('ph1', true)

      const hls = await attachedHls()
      expect(hls.source).toContain('/photos/ph1/hls/master.m3u8')
      expect(hls.media).toBe(video)
      // hls.js feeds the element through MSE, so the element carries no src of
      // its own — one that pointed at the original would download it twice.
      expect(video.hasAttribute('src')).toBe(false)
    })

    it('lets a browser with native support play the playlist, without the library', async () => {
      stubNativeHls(true)
      const { video } = renderPlayer('ph1', true)

      expect(video.getAttribute('src')).toContain('/photos/ph1/hls/master.m3u8')
      await Promise.resolve()
      expect(hlsjs.state.instances).toHaveLength(0)
    })

    it('keeps the controls, the storyboard and the download link while streaming', async () => {
      stubNativeHls(false)
      const { video } = renderPlayer('ph1', true)
      await attachedHls()

      stubPlayableMedia(video)
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))
      expect(screen.getByRole('button', { name: 'Pause' })).toBeInTheDocument()
      expect(screen.getByRole('slider', { name: 'Video timeline' })).toBeInTheDocument()
      await waitFor(() => {
        expect(fetchStoryboard).toHaveBeenCalledWith('ph1', expect.anything())
      })
    })

    it('falls back to the original file when the library cannot run here', async () => {
      stubNativeHls(false)
      hlsjs.state.supported = false
      const { video } = renderPlayer('ph1', true)

      await waitFor(() => {
        expect(video.getAttribute('src')).toContain('/photos/ph1/video')
      })
    })

    it('falls back to the original file when the library fails to initialise', async () => {
      stubNativeHls(false)
      hlsjs.state.throwOnCreate = true
      const { video } = renderPlayer('ph1', true)

      await waitFor(() => {
        expect(video.getAttribute('src')).toContain('/photos/ph1/video')
      })
    })

    it('offers the download after a fatal streaming error', async () => {
      stubNativeHls(false)
      renderPlayer('ph1', true)
      const hls = await attachedHls()

      act(() => {
        hls.raise(true)
      })

      expect(screen.getByText('This video cannot be played in your browser.')).toBeInTheDocument()
      // The original, never a rendition: streaming is for watching, the file is
      // what you keep.
      expect(screen.getByRole('button', { name: 'Download the video' })).toHaveAttribute(
        'href',
        '/api/v1/photos/ph1/download?original=true',
      )
    })

    it('plays on through a recoverable error', async () => {
      stubNativeHls(false)
      renderPlayer('ph1', true)
      const hls = await attachedHls()

      act(() => {
        hls.raise(false)
      })

      // Still the player, not the dead end: the timeline is there and no
      // download link has replaced it. (The unsupported sentence is not a
      // witness here — it is also the `<video>` element's own fallback child.)
      expect(screen.getByRole('slider', { name: 'Video timeline' })).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Download the video' })).not.toBeInTheDocument()
    })

    it('destroys the instance when the player goes away', async () => {
      stubNativeHls(false)
      const { unmount } = renderPlayer('ph1', true)
      const hls = await attachedHls()

      unmount()
      expect(hls.destroyed).toBe(true)
    })
  })

  describe('storyboard', () => {
    it('does not ask for a storyboard before playback starts', () => {
      renderPlayer()
      expect(fetchStoryboard).not.toHaveBeenCalled()
    })

    it('asks once playback starts', async () => {
      const { video } = renderPlayer()
      stubPlayableMedia(video)
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      await waitFor(() => {
        expect(fetchStoryboard).toHaveBeenCalledWith('ph1', expect.anything())
      })
    })

    it('plays normally when the storyboard request fails', async () => {
      fetchStoryboard.mockRejectedValue(new Error('boom'))
      const { video } = renderPlayer()
      stubPlayableMedia(video)
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))

      await waitFor(() => {
        expect(fetchStoryboard).toHaveBeenCalled()
      })
      expect(screen.getByRole('button', { name: 'Pause' })).toBeInTheDocument()
      expect(screen.getByRole('slider', { name: 'Video timeline' })).toBeInTheDocument()
    })
  })

  describe('the poster overlay', () => {
    it('sits on the painted poster, bars excluded', () => {
      // A 4:3 poster in a 1000 x 600 player paints 800 x 600 in the middle: a
      // layer covering the element would put every face box 100px off.
      const undo = stubLayout(1000, 600)
      try {
        renderPlayer('ph1', false, {
          posterRatio: 4 / 3,
          overlay: <div data-testid="boxes" />,
        })

        const layer = screen.getByTestId('boxes').parentElement
        expect(layer).toHaveStyle({ left: '100px', top: '0px', width: '800px', height: '600px' })
      } finally {
        undo()
      }
    })

    it('prefers the shape the element reports over the estimate it was given', () => {
      const undo = stubLayout(1000, 600)
      try {
        const { video } = renderPlayer('ph1', false, {
          posterRatio: 4 / 3,
          overlay: <div data-testid="boxes" />,
        })
        Object.defineProperty(video, 'videoWidth', { configurable: true, get: () => 1000 })
        Object.defineProperty(video, 'videoHeight', { configurable: true, get: () => 1000 })
        fireEvent.loadedMetadata(video)

        // A square clip in the same box paints 600 x 600, centred.
        const layer = screen.getByTestId('boxes').parentElement
        expect(layer).toHaveStyle({ left: '200px', width: '600px', height: '600px' })
      } finally {
        undo()
      }
    })

    it('takes the boxes down once the clip has been played', () => {
      const undo = stubLayout(1000, 600)
      try {
        const { video } = renderPlayer('ph1', false, {
          posterRatio: 4 / 3,
          overlay: <div data-testid="boxes" />,
        })
        expect(screen.getByTestId('boxes')).toBeInTheDocument()

        stubPlayableMedia(video)
        fireEvent.click(screen.getByRole('button', { name: 'Play' }))
        // The element now paints the video, not the poster — boxes measured on
        // the poster would be boxes on the wrong picture. Pausing does not bring
        // the poster back, so neither do they come back.
        expect(screen.queryByTestId('boxes')).not.toBeInTheDocument()
        fireEvent.click(screen.getByRole('button', { name: 'Pause' }))
        expect(screen.queryByTestId('boxes')).not.toBeInTheDocument()
      } finally {
        undo()
      }
    })

    it('draws nothing at all without an overlay to draw', () => {
      const undo = stubLayout(1000, 600)
      try {
        const { container } = renderPlayer()
        expect(container.querySelector('.kk-video__overlay')).toBeNull()
      } finally {
        undo()
      }
    })
  })

  describe('a streaming encode still owed', () => {
    /** The player after the browser has refused to decode the clip. */
    function refused(encode: VideoPlayerProps['encode']) {
      const { video } = renderPlayer('ph1', false, { encode })
      fireEvent.error(video)
    }

    it('says the clip is being prepared rather than that it cannot be played', () => {
      refused({ step: 'hls_transcode', state: 'queued' })

      expect(
        screen.getByText(/still being prepared for playback\. Please come back/i),
      ).toBeInTheDocument()
      expect(
        screen.queryByText('This video cannot be played in your browser.'),
      ).not.toBeInTheDocument()
      // The file itself is still there to be taken away in the meantime.
      expect(screen.getByRole('button', { name: 'Download the video' })).toHaveAttribute(
        'href',
        '/api/v1/photos/ph1/download?original=true',
      )
    })

    it('reads a step that never ran as the same wait', () => {
      refused({ step: 'hls_transcode', state: 'pending' })

      expect(screen.getByText(/still being prepared for playback/i)).toBeInTheDocument()
    })

    it('words a running encode as happening right now', () => {
      refused({ step: 'hls_transcode', state: 'running' })

      expect(screen.getByText(/being prepared for playback right now/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Download the video' })).toBeInTheDocument()
    })

    it('surfaces a failed preparation and points at the Processing panel', () => {
      refused({ step: 'hls_transcode', state: 'failed', error: 'ffmpeg exited with 1' })

      expect(
        screen.getByText('Preparing this video for playback did not finish.'),
      ).toBeInTheDocument()
      expect(screen.getByText('ffmpeg exited with 1')).toBeInTheDocument()
      expect(screen.getByText(/Processing section/i)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: 'Download the video' })).toBeInTheDocument()
    })

    it('keeps the plain download message where nothing more will happen', () => {
      // Streaming switched off for the whole instance, and a clip that WAS
      // encoded and still cannot be decoded: both are dead ends, as before.
      for (const state of ['skipped', 'done'] as const) {
        const { unmount } = render(
          <I18nextProvider i18n={i18n}>
            <VideoPlayer
              uid="ph1"
              title="Clip"
              poster="/poster.jpg"
              downloadHref="/api/v1/photos/ph1/download?original=true"
              encode={{ step: 'hls_transcode', state }}
            />
          </I18nextProvider>,
        )
        const video = document.querySelector('video')
        if (video === null) {
          throw new Error('expected a video element')
        }
        fireEvent.error(video)
        expect(screen.getByText('This video cannot be played in your browser.')).toBeInTheDocument()
        expect(screen.queryByText(/being prepared/i)).not.toBeInTheDocument()
        unmount()
      }
    })

    it('keeps the plain download message when the report says nothing', () => {
      refused(null)

      expect(screen.getByText('This video cannot be played in your browser.')).toBeInTheDocument()
    })

    it('still plays a decodable clip while its encode waits, with a quiet note', () => {
      const { video } = renderPlayer('ph1', false, {
        encode: { step: 'hls_transcode', state: 'queued' },
      })

      // The encode gates nothing: the original file is played exactly as before.
      expect(video.getAttribute('src')).toContain('/photos/ph1/video')
      stubPlayableMedia(video)
      fireEvent.click(screen.getByRole('button', { name: 'Play' }))
      expect(screen.getByRole('button', { name: 'Pause' })).toBeInTheDocument()

      // …and the note is a note: one line under the bar, no dialog, controls intact.
      expect(
        screen.getByText('A smoother version for playing and seeking is being prepared.'),
      ).toBeInTheDocument()
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    it('says nothing under a clip that has its rendition, or whose encode is over', () => {
      const streamed = renderPlayer('ph1', true, {
        encode: { step: 'hls_transcode', state: 'running' },
      })
      expect(screen.queryByText(/smoother version/i)).not.toBeInTheDocument()
      streamed.unmount()

      renderPlayer('ph1', false, { encode: { step: 'hls_transcode', state: 'skipped' } })
      expect(screen.queryByText(/smoother version/i)).not.toBeInTheDocument()
    })
  })
})
