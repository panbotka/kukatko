import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type SlideshowSettings } from '../../lib/slideshowSettings'
import { type Photo } from '../../services/photos'

import { Slideshow, type SlideshowProps } from './Slideshow'

function photo(uid: string, name: string, title = ''): Photo {
  return {
    uid,
    file_hash: uid,
    file_name: name,
    file_size: 1,
    file_mime: 'image/jpeg',
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

  it('triggers a swipe to the next photo on a left drag', () => {
    const props = setup()
    const region = screen.getByRole('region')

    fireEvent.touchStart(region, { changedTouches: [{ clientX: 200, clientY: 100 }] })
    fireEvent.touchEnd(region, { changedTouches: [{ clientX: 100, clientY: 105 }] })
    expect(props.onNext).toHaveBeenCalled()
  })
})
