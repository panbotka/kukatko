import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { CHROME_IDLE_MS } from '../../hooks/useIdleChrome'
import i18n from '../../i18n'
import { kenBurnsMotion } from '../../lib/kenBurns'
import {
  SLIDESHOW_DEFAULTS,
  SLIDESHOW_INTERVALS_MS,
  type SlideshowSettings,
} from '../../lib/slideshowSettings'
import { type Photo } from '../../services/photos'

import { Slideshow, type SlideshowProps } from './Slideshow'

/** Forces `usePrefersReducedMotion` to report the given preference. */
function stubReducedMotion(matches: boolean): void {
  vi.stubGlobal(
    'matchMedia',
    vi.fn().mockImplementation((query: string) => ({
      matches: query.includes('prefers-reduced-motion') ? matches : false,
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  )
}

/**
 * Installs the Fullscreen API jsdom does not implement. The player leaves its
 * fullscreen control out where the browser cannot honour it (iOS Safari), so
 * without this the button under test would not be rendered at all.
 */
function stubFullscreenApi(): void {
  Object.defineProperty(Element.prototype, 'requestFullscreen', {
    configurable: true,
    writable: true,
    value: vi.fn().mockResolvedValue(undefined),
  })
  Object.defineProperty(document, 'exitFullscreen', {
    configurable: true,
    writable: true,
    value: vi.fn().mockResolvedValue(undefined),
  })
  Object.defineProperty(document, 'fullscreenEnabled', { configurable: true, value: true })
}

/** Puts the environment back to a browser without the Fullscreen API. */
function removeFullscreenApi(): void {
  Reflect.deleteProperty(Element.prototype, 'requestFullscreen')
  Reflect.deleteProperty(document, 'exitFullscreen')
  Reflect.deleteProperty(document, 'fullscreenEnabled')
}

/** The chrome layer (controls, close, progress) of the rendered player. */
function chrome(): HTMLElement {
  const element = document.querySelector<HTMLElement>('.slideshow__chrome')
  if (element === null) {
    throw new Error('the player rendered no chrome layer')
  }
  return element
}

/** Whether the chrome is currently on screen. */
function chromeVisible(): boolean {
  return !chrome().hasAttribute('inert')
}

function photo(uid: string, name: string, title = '', mime = 'image/jpeg'): Photo {
  return {
    uid,
    file_hash: uid,
    file_name: name,
    file_size: 1,
    file_mime: mime,
    file_width: 1,
    file_height: 1,
    taken_at_source: 'exif',
    thumb_url: `/api/v1/photos/${uid}/thumb/tile_500`,
    download_url: `/api/v1/photos/${uid}/download?original=true`,
    title,
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

const PHOTOS = [photo('a', 'a.jpg', 'Beach'), photo('b', 'b.jpg'), photo('c', 'c.jpg')]
const SETTINGS: SlideshowSettings = { ...SLIDESHOW_DEFAULTS }

/** The default settings with a patch, for the many single-field variations. */
function settings(patch: Partial<SlideshowSettings> = {}): SlideshowSettings {
  return { ...SLIDESHOW_DEFAULTS, ...patch }
}

function makeProps(overrides: Partial<SlideshowProps> = {}): SlideshowProps {
  return {
    photos: PHOTOS,
    index: 0,
    playing: true,
    settings: SETTINGS,
    onNext: vi.fn(),
    onPrev: vi.fn(),
    onToggle: vi.fn(),
    onExit: vi.fn(),
    onSettingsChange: vi.fn(),
    ...overrides,
  }
}

function setup(overrides: Partial<SlideshowProps> = {}) {
  const props = makeProps(overrides)
  render(
    <I18nextProvider i18n={i18n}>
      <Slideshow {...props} />
    </I18nextProvider>,
  )
  return props
}

/** Opens the settings panel (where the speed control and estimate live). */
async function openSettings(user: ReturnType<typeof userEvent.setup>): Promise<void> {
  await user.click(screen.getByRole('button', { name: 'Settings' }))
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  stubFullscreenApi()
})

afterEach(() => {
  vi.unstubAllGlobals()
  removeFullscreenApi()
})

describe('Slideshow', () => {
  it('shows the current photo and its position, with no time in the header', () => {
    setup({ index: 0 })
    const img = screen.getByRole('img')
    expect(img).toHaveAttribute('alt', 'Beach')
    expect(img).toHaveAttribute('src', expect.stringContaining('/photos/a/thumb/'))
    // The header bar carries only the position: the remaining time moved to the
    // settings panel, so nothing reads "… left" until that panel is open, and
    // what the photo *is* moved onto the photo itself.
    expect(screen.getByText('slide 1 of 3')).toBeInTheDocument()
    expect(screen.queryByText(/left/)).not.toBeInTheDocument()
  })

  it('shows the estimated remaining time beside the speed control during the show', async () => {
    const user = userEvent.setup()
    setup({ index: 0 }) // three photos at 5 s → two still to come → 10 s

    // Hidden until the settings panel (with the speed control) is open.
    expect(screen.queryByText('10 s left')).not.toBeInTheDocument()

    await openSettings(user)

    const remaining = screen.getByText('10 s left')
    // It sits on the speed control's own row, right next to the "Speed" label.
    expect(remaining.parentElement).toBe(screen.getByText('Speed').parentElement)
  })

  it('recomputes the remaining time at once when the interval changes', async () => {
    const user = userEvent.setup()
    const { rerender } = render(
      <I18nextProvider i18n={i18n}>
        <Slideshow
          {...makeProps({ index: 0, settings: settings({ effect: 'fade', intervalMs: 5000 }) })}
        />
      </I18nextProvider>,
    )
    await openSettings(user)
    expect(screen.getByText('10 s left')).toBeInTheDocument() // two to come × 5 s

    rerender(
      <I18nextProvider i18n={i18n}>
        <Slideshow
          {...makeProps({ index: 0, settings: settings({ effect: 'fade', intervalMs: 10000 }) })}
        />
      </I18nextProvider>,
    )
    // The panel stays open and the estimate follows the new speed immediately.
    expect(screen.getByText('20 s left')).toBeInTheDocument() // two to come × 10 s
    expect(screen.queryByText('10 s left')).not.toBeInTheDocument()
  })

  it('measures the estimate against the whole show, not just the loaded pages', async () => {
    const user = userEvent.setup()
    // Three photos loaded of forty; slide 7 of 40 leaves 33 × 5 s = 2 min 45 s.
    setup({ index: 6, total: 40 })
    expect(screen.getByText('slide 7 of 40')).toBeInTheDocument()

    await openSettings(user)
    expect(screen.getByText('2 min 45 s left')).toBeInTheDocument()
  })

  it('keeps the estimate visible, frozen at its value, while paused', async () => {
    const user = userEvent.setup()
    setup({ index: 0, playing: false })

    await openSettings(user)
    // A paused show still shows the estimate; with the cursor held it stays at 10 s.
    expect(screen.getByText('10 s left')).toBeInTheDocument()
  })

  it('applies the active transition effect to the image', () => {
    setup({ settings: settings({ effect: 'slide', intervalMs: 5000 }) })
    const img = screen.getByRole('img')
    expect(img).toHaveClass('slideshow__image--slide')
    expect(img).toHaveAttribute('data-effect', 'slide')
  })

  it('wires the control buttons to their handlers', async () => {
    const user = userEvent.setup()
    const props = setup()

    // Every control here is a bare glyph — ✕ ‹ ❚❚ › ⛶ ⚙ — so each one hands the
    // mouse the same sentence its accessible name carries.
    for (const name of ['Next', 'Previous', 'Pause', 'Close', 'Fullscreen', 'Settings']) {
      expect(screen.getByRole('button', { name })).toHaveAttribute('title', name)
    }

    await user.click(screen.getByRole('button', { name: 'Next' }))
    expect(props.onNext).toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Previous' }))
    expect(props.onPrev).toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Pause' }))
    expect(props.onToggle).toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Close' }))
    expect(props.onExit).toHaveBeenCalled()
  })

  it('shows a play label when paused', () => {
    setup({ playing: false })
    // The tooltip switches with the state exactly as the accessible name does.
    expect(screen.getByRole('button', { name: 'Play' })).toHaveAttribute('title', 'Play')
  })

  it('handles arrow / space / escape keyboard controls', () => {
    const props = setup()

    fireEvent.keyDown(window, { key: 'ArrowRight' })
    expect(props.onNext).toHaveBeenCalledTimes(1)

    fireEvent.keyDown(window, { key: 'ArrowLeft' })
    expect(props.onPrev).toHaveBeenCalledTimes(1)

    fireEvent.keyDown(window, { key: ' ' })
    expect(props.onToggle).toHaveBeenCalledTimes(1)

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(props.onExit).toHaveBeenCalledTimes(1)
  })

  it('lets the user change the effect and speed from the settings panel', async () => {
    const user = userEvent.setup()
    const props = setup()

    await user.click(screen.getByRole('button', { name: 'Settings' }))

    await user.selectOptions(screen.getByLabelText('Transition'), 'slide')
    expect(props.onSettingsChange).toHaveBeenCalledWith({ effect: 'slide' })

    await user.selectOptions(screen.getByLabelText('Speed'), '3000')
    expect(props.onSettingsChange).toHaveBeenCalledWith({ intervalMs: 3000 })
  })

  it('offers repeat, shuffle and the caption toggles mid-show, like the start dialog', async () => {
    const user = userEvent.setup()
    const props = setup({ settings: settings({ shuffle: false, showDate: true }) })

    await openSettings(user)

    // Everything the dialog offers is offered here too — the running player must
    // not be the poor relation of the one that starts the show.
    for (const name of ['Repeat', 'Shuffle', 'Title', 'Description', 'Date taken']) {
      expect(screen.getByRole('checkbox', { name })).toBeInTheDocument()
    }

    await user.click(screen.getByRole('checkbox', { name: 'Shuffle' }))
    expect(props.onSettingsChange).toHaveBeenCalledWith({ shuffle: true })

    await user.click(screen.getByRole('checkbox', { name: 'Date taken' }))
    expect(props.onSettingsChange).toHaveBeenCalledWith({ showDate: false })
  })

  it('labels every speed option with its own number of seconds', async () => {
    const user = userEvent.setup()
    setup()

    await user.click(screen.getByRole('button', { name: 'Settings' }))

    const options = within(screen.getByLabelText('Speed')).getAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual(
      SLIDESHOW_INTERVALS_MS.map((ms) => `${ms / 1000} s`),
    )
    // Regression guard: the interpolated seconds must never come out blank.
    for (const option of options) {
      expect(option).toHaveTextContent(/^\d+ s$/)
    }
  })

  it('labels every speed option in Czech too', async () => {
    await i18n.changeLanguage('cs')
    const user = userEvent.setup()
    setup()

    await user.click(screen.getByRole('button', { name: 'Nastavení' }))

    const options = within(screen.getByLabelText('Rychlost')).getAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual(
      SLIDESHOW_INTERVALS_MS.map((ms) => `${ms / 1000} s`),
    )
  })

  it('preselects the active interval so the stored speed is visible', async () => {
    const user = userEvent.setup()
    setup({ settings: settings({ effect: 'fade', intervalMs: 15000 }) })

    await user.click(screen.getByRole('button', { name: 'Settings' }))

    expect(screen.getByLabelText('Speed')).toHaveValue('15000')
  })

  it('offers Ken Burns among the transition effects', async () => {
    const user = userEvent.setup()
    const props = setup()

    await user.click(screen.getByRole('button', { name: 'Settings' }))

    await user.selectOptions(screen.getByLabelText('Transition'), 'kenburns')
    expect(props.onSettingsChange).toHaveBeenCalledWith({ effect: 'kenburns' })
  })

  it('drives the Ken Burns animation from the photo uid and the interval', () => {
    setup({ settings: settings({ effect: 'kenburns', intervalMs: 10000 }) })
    const img = screen.getByRole('img')
    const motion = kenBurnsMotion('a', 10000)

    expect(img).toHaveClass('slideshow__image--kenburns')
    expect(img.style.getPropertyValue('--kb-duration')).toBe('10000ms')
    expect(img.style.getPropertyValue('--kb-from-scale')).toBe(String(motion.fromScale))
    expect(img.style.getPropertyValue('--kb-to-scale')).toBe(String(motion.toScale))
    expect(img.style.getPropertyValue('--kb-to-x')).toBe(`${motion.toX}%`)
  })

  it('follows the interval setting with the animation duration', () => {
    setup({ settings: settings({ effect: 'kenburns', intervalMs: 3000 }) })
    expect(screen.getByRole('img').style.getPropertyValue('--kb-duration')).toBe('3000ms')

    cleanup()
    setup({ settings: settings({ effect: 'kenburns', intervalMs: 30000 }) })
    expect(screen.getByRole('img').style.getPropertyValue('--kb-duration')).toBe('30000ms')
  })

  it('gives the same photo the same motion on every replay', () => {
    setup({ settings: settings({ effect: 'kenburns', intervalMs: 5000 }) })
    const first = screen.getByRole('img').getAttribute('style')

    cleanup()
    setup({ settings: settings({ effect: 'kenburns', intervalMs: 5000 }) })

    expect(screen.getByRole('img').getAttribute('style')).toBe(first)
  })

  it('gives different photos different motion', () => {
    setup({ index: 0, settings: settings({ effect: 'kenburns', intervalMs: 5000 }) })
    const first = screen.getByRole('img').getAttribute('style')

    cleanup()
    setup({ index: 1, settings: settings({ effect: 'kenburns', intervalMs: 5000 }) })

    expect(screen.getByRole('img').getAttribute('style')).not.toBe(first)
  })

  it('disables Ken Burns under prefers-reduced-motion, leaving a static slide', () => {
    stubReducedMotion(true)
    setup({ settings: settings({ effect: 'kenburns', intervalMs: 5000 }) })
    const img = screen.getByRole('img')

    expect(img).not.toHaveClass('slideshow__image--kenburns')
    expect(img.style.getPropertyValue('--kb-duration')).toBe('')
    expect(img.getAttribute('style')).toBeNull()
  })

  it('leaves videos motionless: Ken Burns applies to images only', () => {
    setup({
      photos: [photo('v', 'clip.mp4', 'Clip', 'video/mp4')],
      index: 0,
      settings: settings({ effect: 'kenburns', intervalMs: 5000 }),
    })
    const img = screen.getByRole('img')

    expect(img).not.toHaveClass('slideshow__image--kenburns')
    expect(img.getAttribute('style')).toBeNull()
  })

  it('triggers a swipe to the next photo on a left drag', () => {
    const props = setup()
    const region = screen.getByRole('region')

    fireEvent.touchStart(region, { changedTouches: [{ clientX: 200, clientY: 100 }] })
    fireEvent.touchEnd(region, { changedTouches: [{ clientX: 100, clientY: 105 }] })
    expect(props.onNext).toHaveBeenCalled()
  })

  it('leaves the fullscreen control out where the browser cannot honour it', () => {
    // iOS Safari fullscreens a <video> and nothing else: the button would be
    // there, be pressed, and do nothing.
    removeFullscreenApi()
    setup()

    expect(screen.queryByRole('button', { name: 'Fullscreen' })).not.toBeInTheDocument()
    // The controls that do work are all still there.
    expect(screen.getByRole('button', { name: 'Next' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Settings' })).toBeInTheDocument()
  })

  it('does not toggle playback when Space activates a focused control', () => {
    const props = setup()
    const next = screen.getByRole('button', { name: 'Next' })
    next.focus()

    fireEvent.keyDown(next, { key: ' ' })

    // The button gets its own press; the player must not also pause the show,
    // which would leave the reader a slide on and stopped after one keystroke.
    expect(props.onToggle).not.toHaveBeenCalled()
  })

  it('navigates with the page keys a presentation remote sends', () => {
    const props = setup()

    fireEvent.keyDown(window, { key: 'PageDown' })
    expect(props.onNext).toHaveBeenCalledTimes(1)

    fireEvent.keyDown(window, { key: 'PageUp' })
    expect(props.onPrev).toHaveBeenCalledTimes(1)
  })

  it('closes the settings panel with Escape before it closes the show', async () => {
    const user = userEvent.setup()
    const props = setup()

    await openSettings(user)
    expect(screen.getByLabelText('Speed')).toBeInTheDocument()

    fireEvent.keyDown(window, { key: 'Escape' })
    // Escape peels one layer: the panel goes, the show stays.
    expect(screen.queryByLabelText('Speed')).not.toBeInTheDocument()
    expect(props.onExit).not.toHaveBeenCalled()

    fireEvent.keyDown(window, { key: 'Escape' })
    expect(props.onExit).toHaveBeenCalledTimes(1)
  })

  it('caps the transition against the speed, so a 1 s show is not all fade', () => {
    const { container, rerender } = render(
      <I18nextProvider i18n={i18n}>
        <Slideshow {...makeProps({ settings: settings({ effect: 'fade', intervalMs: 5000 }) })} />
      </I18nextProvider>,
    )
    const stage = container.querySelector<HTMLElement>('.slideshow')

    expect(stage?.style.getPropertyValue('--slideshow-transition')).toBe('600ms')

    rerender(
      <I18nextProvider i18n={i18n}>
        <Slideshow {...makeProps({ settings: settings({ effect: 'fade', intervalMs: 1000 }) })} />
      </I18nextProvider>,
    )
    expect(stage?.style.getPropertyValue('--slideshow-transition')).toBe('250ms')
  })

  describe('the chrome over the photograph', () => {
    beforeEach(() => {
      vi.useFakeTimers()
    })

    afterEach(() => {
      vi.useRealTimers()
    })

    /** Lets the idle countdown run out. */
    function idle(): void {
      act(() => {
        vi.advanceTimersByTime(CHROME_IDLE_MS)
      })
    }

    it('takes the controls off the picture once nothing has happened', () => {
      setup()
      expect(chromeVisible()).toBe(true)

      idle()

      // Gone from sight *and* from the keyboard: an invisible bar that still
      // caught Tab stops would be worse than one that stayed.
      expect(chromeVisible()).toBe(false)
      expect(document.querySelector('.slideshow')).toHaveClass('slideshow--idle')
    })

    it('brings them back when the mouse moves, and not when a finger swipes', () => {
      setup()
      idle()
      expect(chromeVisible()).toBe(false)

      fireEvent.pointerMove(screen.getByRole('region'), { pointerType: 'touch' })
      expect(chromeVisible()).toBe(false)

      fireEvent.pointerMove(screen.getByRole('region'), { pointerType: 'mouse' })
      expect(chromeVisible()).toBe(true)
    })

    it('brings them back on any key press', () => {
      setup()
      idle()

      fireEvent.keyDown(window, { key: 'ArrowRight' })
      expect(chromeVisible()).toBe(true)
    })

    it('spends the first Tab bringing the controls back, so they can be tabbed to', () => {
      setup()
      idle()

      const prevented = !fireEvent.keyDown(window, { key: 'Tab' })
      expect(chromeVisible()).toBe(true)
      expect(prevented).toBe(true)

      // Once they are on screen Tab is the browser's again.
      const second = !fireEvent.keyDown(window, { key: 'Tab' })
      expect(second).toBe(false)
    })

    it('lets a tap ask for the controls and a second tap dismiss them', () => {
      const props = setup()
      const region = screen.getByRole('region')
      idle()
      expect(chromeVisible()).toBe(false)

      // A finger that stays put: the only way a touch screen can ask.
      fireEvent.touchStart(region, { changedTouches: [{ clientX: 100, clientY: 300 }] })
      fireEvent.touchEnd(region, { changedTouches: [{ clientX: 103, clientY: 302 }] })
      expect(chromeVisible()).toBe(true)
      expect(props.onNext).not.toHaveBeenCalled()
      expect(props.onPrev).not.toHaveBeenCalled()

      fireEvent.touchStart(region, { changedTouches: [{ clientX: 100, clientY: 300 }] })
      fireEvent.touchEnd(region, { changedTouches: [{ clientX: 100, clientY: 300 }] })
      expect(chromeVisible()).toBe(false)
    })

    it('does not mistake a swipe for a tap', () => {
      const props = setup()
      const region = screen.getByRole('region')
      idle()

      fireEvent.touchStart(region, { changedTouches: [{ clientX: 200, clientY: 100 }] })
      fireEvent.touchEnd(region, { changedTouches: [{ clientX: 100, clientY: 105 }] })

      expect(props.onNext).toHaveBeenCalledTimes(1)
      // Steering the show is not asking for the buttons.
      expect(chromeVisible()).toBe(false)
    })

    it('ignores a second finger, which is a pinch and not a swipe', () => {
      const props = setup()
      const region = screen.getByRole('region')

      fireEvent.touchStart(region, {
        touches: [
          { clientX: 200, clientY: 100 },
          { clientX: 260, clientY: 140 },
        ],
        changedTouches: [{ clientX: 260, clientY: 140 }],
      })
      fireEvent.touchEnd(region, { changedTouches: [{ clientX: 100, clientY: 105 }] })

      expect(props.onNext).not.toHaveBeenCalled()
    })

    it('keeps the controls while the settings panel is open', () => {
      setup()
      fireEvent.click(screen.getByRole('button', { name: 'Settings' }))

      idle()
      idle()

      // A panel that timed out from under the hand editing it is a trap.
      expect(chromeVisible()).toBe(true)
      expect(screen.getByLabelText('Speed')).toBeInTheDocument()
    })

    it('keeps the controls while the mouse rests on them', () => {
      setup()
      const bar = document.querySelector('.slideshow__controls')
      if (bar === null) {
        throw new Error('the player rendered no control bar')
      }

      fireEvent.pointerOver(bar, { pointerType: 'mouse' })
      idle()
      expect(chromeVisible()).toBe(true)

      fireEvent.pointerOut(bar, { pointerType: 'mouse', relatedTarget: document.body })
      idle()
      expect(chromeVisible()).toBe(false)
    })

    it('does not let the mouse leaving cancel a hold the keyboard still needs', () => {
      setup()
      const next = screen.getByRole('button', { name: 'Next' })

      // Clicking a control leaves it focused; the mouse then wanders off the
      // bar. If that cancelled the hold, the chrome would go inert under the
      // focused button and drop the focus with it.
      fireEvent.pointerOver(next, { pointerType: 'mouse' })
      fireEvent.focus(next)
      fireEvent.pointerOut(next, { pointerType: 'mouse', relatedTarget: document.body })

      idle()
      expect(chromeVisible()).toBe(true)
    })

    it('keeps the controls while the keyboard is inside them', () => {
      setup()
      const next = screen.getByRole('button', { name: 'Next' })
      fireEvent.focus(next)

      idle()
      expect(chromeVisible()).toBe(true)

      // Focus leaves: the countdown starts again from there.
      fireEvent.blur(next)
      idle()
      expect(chromeVisible()).toBe(false)
    })
  })

  describe('the caption over the photo', () => {
    const described = [
      {
        ...photo('d', 'd.jpg', 'Wedding'),
        description: 'The whole family on the church steps.',
        taken_at: '1974-06-15T10:00:00Z',
      },
    ]

    it('says what the photo is when every caption is on', () => {
      setup({ photos: described, index: 0 })

      expect(screen.getByText('Wedding')).toBeInTheDocument()
      expect(screen.getByText('The whole family on the church steps.')).toBeInTheDocument()
      expect(screen.getByText('6/15/1974')).toBeInTheDocument()
    })

    it('shows only what is switched on', () => {
      setup({
        photos: described,
        index: 0,
        settings: settings({ showDescription: false, showDate: false }),
      })

      expect(screen.getByText('Wedding')).toBeInTheDocument()
      expect(screen.queryByText('The whole family on the church steps.')).not.toBeInTheDocument()
      expect(screen.queryByText('6/15/1974')).not.toBeInTheDocument()
    })

    it('shows nothing at all for a photo that has nothing to say', () => {
      // b.jpg is untitled, undescribed and undated: an empty field must show
      // nothing — not a blank line, not a placeholder, not the file name.
      const { container } = render(
        <I18nextProvider i18n={i18n}>
          <Slideshow {...makeProps({ index: 1 })} />
        </I18nextProvider>,
      )

      expect(container.querySelector('.slideshow__meta')).toBeNull()
      expect(screen.queryByText('b.jpg')).not.toBeInTheDocument()
    })

    it('stays out of the chrome, so fading the chrome does not take it', () => {
      vi.useFakeTimers()
      try {
        const { container } = render(
          <I18nextProvider i18n={i18n}>
            <Slideshow {...makeProps({ photos: described, index: 0 })} />
          </I18nextProvider>,
        )

        const meta = container.querySelector('.slideshow__meta')
        expect(meta).not.toBeNull()
        expect(meta?.closest('.slideshow__chrome')).toBeNull()

        // And it is still there once the controls have gone: what the photo is
        // must not depend on the mouse having moved recently.
        act(() => {
          vi.advanceTimersByTime(CHROME_IDLE_MS)
        })
        expect(chromeVisible()).toBe(false)
        expect(screen.getByText('Wedding')).toBeInTheDocument()
      } finally {
        vi.useRealTimers()
      }
    })

    it('states a coarse date as the period it was stated as', () => {
      setup({
        photos: [
          {
            ...photo('y', 'y.jpg'),
            taken_at: '1974-01-01T00:00:00Z',
            taken_at_precision: 'year',
          },
        ],
        index: 0,
      })

      // Never "1 January 1974": a day nobody ever claimed.
      expect(screen.getByText('1974')).toBeInTheDocument()
    })
  })
})
