import { cleanup, fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { kenBurnsMotion } from '../../lib/kenBurns'
import { SLIDESHOW_INTERVALS_MS, type SlideshowSettings } from '../../lib/slideshowSettings'
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
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

const PHOTOS = [photo('a', 'a.jpg', 'Beach'), photo('b', 'b.jpg'), photo('c', 'c.jpg')]
const SETTINGS: SlideshowSettings = { effect: 'fade', intervalMs: 5000 }

function setup(overrides: Partial<SlideshowProps> = {}) {
  const props: SlideshowProps = {
    photos: PHOTOS,
    index: 0,
    playing: true,
    settings: SETTINGS,
    onNext: vi.fn(),
    onPrev: vi.fn(),
    onToggle: vi.fn(),
    onExit: vi.fn(),
    onEffectChange: vi.fn(),
    onIntervalChange: vi.fn(),
    ...overrides,
  }
  render(
    <I18nextProvider i18n={i18n}>
      <Slideshow {...props} />
    </I18nextProvider>,
  )
  return props
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('Slideshow', () => {
  it('shows the current photo, its caption and position', () => {
    setup({ index: 0 })
    const img = screen.getByRole('img')
    expect(img).toHaveAttribute('alt', 'Beach')
    expect(img).toHaveAttribute('src', expect.stringContaining('/photos/a/thumb/'))
    expect(screen.getByText('1 / 3')).toBeInTheDocument()
  })

  it('applies the active transition effect to the image', () => {
    setup({ settings: { effect: 'slide', intervalMs: 5000 } })
    const img = screen.getByRole('img')
    expect(img).toHaveClass('slideshow__image--slide')
    expect(img).toHaveAttribute('data-effect', 'slide')
  })

  it('wires the control buttons to their handlers', async () => {
    const user = userEvent.setup()
    const props = setup()

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
    expect(screen.getByRole('button', { name: 'Play' })).toBeInTheDocument()
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
    expect(props.onEffectChange).toHaveBeenCalledWith('slide')

    await user.selectOptions(screen.getByLabelText('Speed'), '3000')
    expect(props.onIntervalChange).toHaveBeenCalledWith(3000)
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
    setup({ settings: { effect: 'fade', intervalMs: 15000 } })

    await user.click(screen.getByRole('button', { name: 'Settings' }))

    expect(screen.getByLabelText('Speed')).toHaveValue('15000')
  })

  it('offers Ken Burns among the transition effects', async () => {
    const user = userEvent.setup()
    const props = setup()

    await user.click(screen.getByRole('button', { name: 'Settings' }))

    await user.selectOptions(screen.getByLabelText('Transition'), 'kenburns')
    expect(props.onEffectChange).toHaveBeenCalledWith('kenburns')
  })

  it('drives the Ken Burns animation from the photo uid and the interval', () => {
    setup({ settings: { effect: 'kenburns', intervalMs: 10000 } })
    const img = screen.getByRole('img')
    const motion = kenBurnsMotion('a', 10000)

    expect(img).toHaveClass('slideshow__image--kenburns')
    expect(img.style.getPropertyValue('--kb-duration')).toBe('10000ms')
    expect(img.style.getPropertyValue('--kb-from-scale')).toBe(String(motion.fromScale))
    expect(img.style.getPropertyValue('--kb-to-scale')).toBe(String(motion.toScale))
    expect(img.style.getPropertyValue('--kb-to-x')).toBe(`${motion.toX}%`)
  })

  it('follows the interval setting with the animation duration', () => {
    setup({ settings: { effect: 'kenburns', intervalMs: 3000 } })
    expect(screen.getByRole('img').style.getPropertyValue('--kb-duration')).toBe('3000ms')

    cleanup()
    setup({ settings: { effect: 'kenburns', intervalMs: 30000 } })
    expect(screen.getByRole('img').style.getPropertyValue('--kb-duration')).toBe('30000ms')
  })

  it('gives the same photo the same motion on every replay', () => {
    setup({ settings: { effect: 'kenburns', intervalMs: 5000 } })
    const first = screen.getByRole('img').getAttribute('style')

    cleanup()
    setup({ settings: { effect: 'kenburns', intervalMs: 5000 } })

    expect(screen.getByRole('img').getAttribute('style')).toBe(first)
  })

  it('gives different photos different motion', () => {
    setup({ index: 0, settings: { effect: 'kenburns', intervalMs: 5000 } })
    const first = screen.getByRole('img').getAttribute('style')

    cleanup()
    setup({ index: 1, settings: { effect: 'kenburns', intervalMs: 5000 } })

    expect(screen.getByRole('img').getAttribute('style')).not.toBe(first)
  })

  it('disables Ken Burns under prefers-reduced-motion, leaving a static slide', () => {
    stubReducedMotion(true)
    setup({ settings: { effect: 'kenburns', intervalMs: 5000 } })
    const img = screen.getByRole('img')

    expect(img).not.toHaveClass('slideshow__image--kenburns')
    expect(img.style.getPropertyValue('--kb-duration')).toBe('')
    expect(img.getAttribute('style')).toBeNull()
  })

  it('leaves videos motionless: Ken Burns applies to images only', () => {
    setup({
      photos: [photo('v', 'clip.mp4', 'Clip', 'video/mp4')],
      index: 0,
      settings: { effect: 'kenburns', intervalMs: 5000 },
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
})
