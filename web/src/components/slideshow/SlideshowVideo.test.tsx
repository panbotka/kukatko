import { act, fireEvent, render } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { CapabilitiesContext } from '../../capabilities/CapabilitiesContext'
import i18n from '../../i18n'
import { type Photo } from '../../services/photos'

import {
  MAX_VIDEO_SLIDE_MS,
  PLAYBACK_GRACE_MS,
  SlideshowVideo,
  type SlideshowVideoProps,
} from './SlideshowVideo'

/** The ordinary photo interval used throughout, distinct from every other clock. */
const INTERVAL_MS = 4000

/** A video row of the catalogue, as a listing hands it to the stage. */
function clip(overrides: Partial<Photo> = {}): Photo {
  return {
    uid: 'v1',
    file_hash: 'v1',
    file_name: 'clip.mp4',
    file_size: 1,
    file_mime: 'video/mp4',
    file_width: 1920,
    file_height: 1080,
    taken_at_source: 'exif',
    title: 'Holiday',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    thumb_url: '/api/v1/photos/v1/thumb/tile_500',
    download_url: '/api/v1/photos/v1/download?original=true',
    media_type: 'video',
    duration_ms: 12_000,
    ...overrides,
  }
}

/**
 * Gives every `<video>` the playback jsdom does not implement: `play` and
 * `pause` that record the call, and a `load` that does nothing. Returns the
 * spies so a test can assert what the slide asked the element for.
 */
function stubPlayback(rejectPlay = false) {
  const play = vi
    .spyOn(HTMLMediaElement.prototype, 'play')
    .mockImplementation(() =>
      rejectPlay ? Promise.reject(new Error('not allowed')) : Promise.resolve(),
    )
  const pause = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
  const load = vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => undefined)
  return { play, pause, load }
}

function setup(overrides: Partial<SlideshowVideoProps> = {}) {
  const props: SlideshowVideoProps = {
    photo: clip(),
    poster: '/api/v1/photos/v1/thumb/fit_1920',
    playing: true,
    intervalMs: INTERVAL_MS,
    onEnded: vi.fn(),
    className: 'slideshow__image',
    ...overrides,
  }
  const utils = render(
    <I18nextProvider i18n={i18n}>
      <SlideshowVideo {...props} />
    </I18nextProvider>,
  )
  const video = utils.container.querySelector('video')
  if (video === null) {
    throw new Error('the slide rendered no video element')
  }
  const rerender = (patch: Partial<SlideshowVideoProps> = {}) => {
    utils.rerender(
      <I18nextProvider i18n={i18n}>
        <SlideshowVideo {...props} {...patch} />
      </I18nextProvider>,
    )
  }
  return { ...utils, props, video, rerender }
}

/** Advances the mocked clock inside `act`, so the effects settle with it. */
function tick(ms: number): void {
  act(() => {
    vi.advanceTimersByTime(ms)
  })
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  // Real time still has to pass, or the promise `play()` answers with — and any
  // async query — never settles.
  vi.useFakeTimers({ shouldAdvanceTime: true })
})

// The mocks are restored by the shared `restoreMocks` setting, deliberately not
// here: a per-file restore runs before RTL's own cleanup and would unmount the
// slide into a DOM whose playback stubs are already gone.
afterEach(() => {
  vi.useRealTimers()
})

describe('SlideshowVideo', () => {
  it('plays the clip muted, from the beginning, over its own poster', () => {
    const { play } = stubPlayback()
    const { video } = setup()

    expect(play).toHaveBeenCalled()
    expect(video.muted).toBe(true)
    expect(video.getAttribute('poster')).toBe('/api/v1/photos/v1/thumb/fit_1920')
    expect(video.getAttribute('src')).toContain('/photos/v1/video')
    // No rendition is reported by a listing, so the original is played as it is.
    expect(video.getAttribute('src')).not.toContain('m3u8')
  })

  it('says the slide is a clip, and how long, until it starts moving', () => {
    stubPlayback()
    const { getByRole, queryByRole, video } = setup()

    expect(getByRole('img', { name: 'Video' })).toHaveTextContent('0:12')

    act(() => {
      fireEvent.playing(video)
    })
    expect(queryByRole('img', { name: 'Video' })).toBeNull()
  })

  it('ends the slide when the clip ends', () => {
    stubPlayback()
    const { props, video } = setup()

    act(() => {
      fireEvent.playing(video)
    })
    tick(INTERVAL_MS * 3)
    // The photo interval is not this slide's clock: only the clip is.
    expect(props.onEnded).not.toHaveBeenCalled()

    act(() => {
      fireEvent.ended(video)
    })
    expect(props.onEnded).toHaveBeenCalledTimes(1)
  })

  it('takes the show back from a clip that runs too long', () => {
    stubPlayback()
    const { props, video } = setup({ photo: clip({ duration_ms: 20 * 60 * 1000 }) })

    act(() => {
      fireEvent.playing(video)
    })
    tick(MAX_VIDEO_SLIDE_MS - 1000)
    expect(props.onEnded).not.toHaveBeenCalled()

    tick(1000)
    expect(props.onEnded).toHaveBeenCalledTimes(1)
  })

  it('falls back to the poster for one photo interval when the clip errors', () => {
    stubPlayback()
    const { props, video } = setup()

    act(() => {
      fireEvent.error(video)
    })
    // The poster is still what is on screen, and the badge with it.
    expect(video.getAttribute('poster')).toBe('/api/v1/photos/v1/thumb/fit_1920')
    tick(INTERVAL_MS - 100)
    expect(props.onEnded).not.toHaveBeenCalled()

    tick(100)
    expect(props.onEnded).toHaveBeenCalledTimes(1)
  })

  it('gives up on a clip that never starts, and moves on', () => {
    stubPlayback()
    const { props } = setup()

    tick(PLAYBACK_GRACE_MS)
    // Given up on, but the poster is now an ordinary slide: it gets its interval.
    expect(props.onEnded).not.toHaveBeenCalled()

    tick(INTERVAL_MS)
    expect(props.onEnded).toHaveBeenCalledTimes(1)
  })

  it('treats a refused play the same as a broken clip', async () => {
    stubPlayback(true)
    const { props } = setup()

    // The rejection lands in a microtask, not on a timer.
    await act(async () => {
      await Promise.resolve()
    })
    tick(INTERVAL_MS)
    expect(props.onEnded).toHaveBeenCalledTimes(1)
  })

  it('pauses the clip with the show, and resumes it where it stopped', () => {
    const spies = stubPlayback()
    const { props, video, rerender } = setup()

    act(() => {
      fireEvent.playing(video)
    })
    tick(MAX_VIDEO_SLIDE_MS / 2)

    spies.play.mockClear()
    rerender({ playing: false })
    expect(spies.pause).toHaveBeenCalled()

    // A paused show is paused: the cap does not run out behind it.
    tick(MAX_VIDEO_SLIDE_MS * 2)
    expect(props.onEnded).not.toHaveBeenCalled()

    rerender({ playing: true })
    expect(spies.play).toHaveBeenCalled()
    // Half the cap was spent before the pause; the other half is still there.
    tick(MAX_VIDEO_SLIDE_MS / 2 - 2000)
    expect(props.onEnded).not.toHaveBeenCalled()
    tick(2000)
    expect(props.onEnded).toHaveBeenCalledTimes(1)
  })

  it('advances on resume when the clip finished while the show was paused', () => {
    stubPlayback()
    const { props, video, rerender } = setup()

    act(() => {
      fireEvent.playing(video)
    })
    rerender({ playing: false })
    act(() => {
      fireEvent.ended(video)
    })
    expect(props.onEnded).not.toHaveBeenCalled()

    rerender({ playing: true })
    expect(props.onEnded).toHaveBeenCalledTimes(1)
  })

  it('stops playback and releases the element when the slide goes away', () => {
    const { pause, load } = stubPlayback()
    const { video, unmount } = setup()

    act(() => {
      fireEvent.playing(video)
    })
    pause.mockClear()

    unmount()
    expect(pause).toHaveBeenCalled()
    expect(video.hasAttribute('src')).toBe(false)
    expect(load).toHaveBeenCalled()
  })
})

describe('SlideshowVideo while the streaming rendition is still being made', () => {
  /**
   * Renders a slide on an instance that DOES encode streaming renditions —
   * the premise of the whole state, and deliberately not the default `setup`,
   * which is about a library that plays every clip from the original.
   */
  function setupPending(overrides: Partial<SlideshowVideoProps> = {}) {
    const props: SlideshowVideoProps = {
      photo: clip({ hls: false }),
      poster: '/api/v1/photos/v1/thumb/fit_1920',
      playing: true,
      intervalMs: INTERVAL_MS,
      onEnded: vi.fn(),
      className: 'slideshow__image',
      ...overrides,
    }
    const utils = render(
      <I18nextProvider i18n={i18n}>
        <CapabilitiesContext.Provider
          value={{ semantic_search: false, passkeys: false, video_streaming: true, known: true }}
        >
          <SlideshowVideo {...props} />
        </CapabilitiesContext.Provider>
      </I18nextProvider>,
    )
    return { ...utils, props }
  }

  it('shows the poster and says the clip is being processed, playing nothing', () => {
    const { play } = stubPlayback()
    const { container, getByText } = setupPending()

    expect(container.querySelector('video')).toBeNull()
    expect(container.querySelector('img')).toHaveAttribute(
      'src',
      '/api/v1/photos/v1/thumb/fit_1920',
    )
    expect(getByText('Video is being processed')).toBeInTheDocument()
    expect(play).not.toHaveBeenCalled()
  })

  it('holds that poster for one ordinary photo interval, then advances', () => {
    stubPlayback()
    const { props } = setupPending()

    tick(INTERVAL_MS - 1)
    expect(props.onEnded).not.toHaveBeenCalled()
    tick(1)
    expect(props.onEnded).toHaveBeenCalledTimes(1)
  })

  it('plays a clip that already has its rendition as usual', () => {
    const { play } = stubPlayback()
    const { container } = setupPending({ photo: clip({ hls: true }) })

    expect(container.querySelector('video')).not.toBeNull()
    expect(play).toHaveBeenCalled()
  })
})

describe('SlideshowVideo when the capability flags arrive late', () => {
  /** The flags as they read before and after the capabilities request lands. */
  const unknownFlags = {
    semantic_search: false,
    passkeys: false,
    video_streaming: false,
    known: false,
  }
  const streamingFlags = { ...unknownFlags, video_streaming: true, known: true }
  const noStreamingFlags = { ...unknownFlags, known: true }

  /**
   * Mounts a slide the way a cold load does: the flags are all-off and not yet
   * known, because `GET /capabilities` has not answered. `land` re-renders the
   * same slide with the answer, exactly as the provider does.
   */
  function setupColdLoad(overrides: Partial<SlideshowVideoProps> = {}) {
    const props: SlideshowVideoProps = {
      photo: clip({ hls: false }),
      poster: '/api/v1/photos/v1/thumb/fit_1920',
      playing: true,
      intervalMs: INTERVAL_MS,
      onEnded: vi.fn(),
      className: 'slideshow__image',
      ...overrides,
    }
    const tree = (flags: typeof unknownFlags) => (
      <I18nextProvider i18n={i18n}>
        <CapabilitiesContext.Provider value={flags}>
          <SlideshowVideo {...props} />
        </CapabilitiesContext.Provider>
      </I18nextProvider>
    )
    const utils = render(tree(unknownFlags))
    const land = (flags: typeof unknownFlags) => {
      act(() => {
        utils.rerender(tree(flags))
      })
    }
    return { ...utils, props, land }
  }

  it('hands the original to nobody while the flags are still on their way', () => {
    const { play } = stubPlayback()
    const { container, getByRole } = setupColdLoad()

    // Not even for one paint: the first slide of a cold load is the very place
    // this used to fetch ~250 MB of a clip it was about to stop playing.
    expect(container.querySelector('video')).toBeNull()
    expect(container.querySelector('img')).toHaveAttribute(
      'src',
      '/api/v1/photos/v1/thumb/fit_1920',
    )
    expect(play).not.toHaveBeenCalled()
    // Nothing is claimed about the clip yet — it is a video, and that is all.
    expect(getByRole('img', { name: 'Video' })).toHaveTextContent('0:12')
  })

  it('holds the first slide once the flags say the clip is being encoded', () => {
    const { play } = stubPlayback()
    const { container, getByText, land } = setupColdLoad()

    land(streamingFlags)

    expect(getByText('Video is being processed')).toBeInTheDocument()
    expect(container.querySelector('video')).toBeNull()
    expect(play).not.toHaveBeenCalled()
  })

  it('plays the clip once the flags say the instance does not encode at all', () => {
    const { play } = stubPlayback()
    const { container, land } = setupColdLoad()

    land(noStreamingFlags)

    const video = container.querySelector('video')
    expect(video).not.toBeNull()
    expect(video?.getAttribute('src')).toContain('/photos/v1/video')
    expect(play).toHaveBeenCalled()
  })

  it('advances rather than waiting forever on flags that never come', () => {
    stubPlayback()
    const { props } = setupColdLoad()

    tick(INTERVAL_MS - 1)
    expect(props.onEnded).not.toHaveBeenCalled()
    tick(1)
    expect(props.onEnded).toHaveBeenCalledTimes(1)
  })

  it('releases the element when a later answer turns the player into a picture', () => {
    const { pause, load } = stubPlayback()
    const { container, land } = setupColdLoad()

    land(noStreamingFlags)
    const video = container.querySelector('video')
    if (video === null) {
      throw new Error('the slide rendered no video element')
    }
    pause.mockClear()

    // The flags are read again — an instance that has since been given an
    // encoder — and the slide becomes the held poster. The element it just
    // dropped must not keep downloading the original behind it.
    land(streamingFlags)
    expect(container.querySelector('video')).toBeNull()
    expect(pause).toHaveBeenCalled()
    expect(video.hasAttribute('src')).toBe(false)
    expect(load).toHaveBeenCalled()
  })
})
