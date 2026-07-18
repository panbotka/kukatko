import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import type { Photo, PhotoDetail } from '../../services/photos'

import { PhotoTile } from './PhotoTile'

// Only the network call is faked; the stale-URL retry in useThumbSrc runs for real.
vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, fetchPhoto: vi.fn() }
})

const { fetchPhoto } = await import('../../services/photos')
const fetchPhotoMock = vi.mocked(fetchPhoto)

/** Builds a minimal Photo with the given overrides. */
function photo(overrides: Partial<Photo> = {}): Photo {
  return {
    uid: 'ph1',
    file_hash: 'h',
    file_name: 'clip.mp4',
    file_size: 100,
    file_mime: 'video/mp4',
    file_width: 1920,
    file_height: 1080,
    taken_at_source: 'unknown',
    thumb_url: '/api/v1/photos/ph1/thumb/tile_500',
    download_url: '/api/v1/photos/ph1/download?original=true',
    title: 'Clip',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2024-01-01T00:00:00Z',
    updated_at: '2024-01-01T00:00:00Z',
    ...overrides,
  }
}

/** Builds the detail payload a refetch answers with, carrying a fresh thumb URL. */
function detail(overrides: Partial<Photo> = {}): PhotoDetail {
  return { ...photo(overrides), files: [], albums: [], labels: [] }
}

function renderTile(p: Photo, favoritable = false) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <PhotoTile photo={p} favoritable={favoritable} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchPhotoMock.mockReset()
})

describe('PhotoTile thumbnail source', () => {
  // A signed URL at the media Worker: another origin, a path keyed by file hash
  // rather than UID, and a signature the frontend could never compute.
  const signed = 'https://media.example/thumb/ab/cd/ef/abcdef_tile_500.jpg?sig=deadbeef&exp=99'

  it('renders the thumb_url from the payload rather than a path built from the UID', () => {
    renderTile(photo({ media_type: 'image', thumb_url: signed }))

    const img = screen.getByRole('img', { name: 'Clip' })
    expect(img).toHaveAttribute('src', signed)
    // The old behaviour: a same-origin route constructed from the photo's UID.
    expect(img.getAttribute('src')).not.toContain('/api/v1/photos/ph1/thumb/')
  })

  it('retries once with a freshly signed URL when the given one has expired', async () => {
    const fresh = 'https://media.example/thumb/ab/cd/ef/abcdef_tile_500.jpg?sig=freshsig&exp=100'
    fetchPhotoMock.mockResolvedValue(detail({ thumb_url: fresh }))
    renderTile(photo({ media_type: 'image', thumb_url: signed }))

    fireEvent.error(screen.getByRole('img', { name: 'Clip' }))

    await waitFor(() => {
      expect(screen.getByRole('img', { name: 'Clip' })).toHaveAttribute('src', fresh)
    })
    expect(fetchPhotoMock).toHaveBeenCalledWith('ph1')
    expect(screen.queryByText('Preview unavailable')).not.toBeInTheDocument()
  })

  it('gives up after the retried URL also fails', async () => {
    const fresh = 'https://media.example/thumb/ab/cd/ef/abcdef_tile_500.jpg?sig=freshsig&exp=100'
    fetchPhotoMock.mockResolvedValue(detail({ thumb_url: fresh }))
    renderTile(photo({ media_type: 'image', thumb_url: signed }))

    fireEvent.error(screen.getByRole('img', { name: 'Clip' }))
    await waitFor(() => {
      expect(screen.getByRole('img', { name: 'Clip' })).toHaveAttribute('src', fresh)
    })
    fireEvent.error(screen.getByRole('img', { name: 'Clip' }))

    expect(await screen.findByText('Preview unavailable')).toBeInTheDocument()
    // Exactly once: a retry loop would hammer the API behind a missing thumbnail.
    expect(fetchPhotoMock).toHaveBeenCalledTimes(1)
  })

  it('does not retry a route URL, which never goes stale', async () => {
    // The filesystem backend answers with its own route; a failure there means the
    // thumbnail is missing, and refetching would hand back the same address.
    fetchPhotoMock.mockResolvedValue(detail())
    renderTile(photo({ media_type: 'image' }))

    fireEvent.error(screen.getByRole('img', { name: 'Clip' }))

    expect(await screen.findByText('Preview unavailable')).toBeInTheDocument()
  })
})

describe('PhotoTile video badge', () => {
  it('shows a video badge with formatted duration for videos', () => {
    renderTile(photo({ media_type: 'video', duration_ms: 154000 }))
    const badge = screen.getByRole('img', { name: 'Video' })
    expect(badge).toBeInTheDocument()
    expect(badge).toHaveTextContent('2:34')
  })

  it('shows a live badge for live photos', () => {
    renderTile(photo({ media_type: 'live', file_name: 'live.heic' }))
    expect(screen.getByRole('img', { name: 'Live' })).toBeInTheDocument()
  })

  it('shows the badge without a duration when the length is unknown', () => {
    renderTile(photo({ media_type: 'video' }))
    const badge = screen.getByRole('img', { name: 'Video' })
    expect(badge).toBeInTheDocument()
    expect(badge.textContent).not.toMatch(/\d:\d\d/)
  })

  it('shows no video badge for still images', () => {
    renderTile(photo({ media_type: 'image', file_name: 'still.jpg' }))
    expect(screen.queryByRole('img', { name: 'Video' })).not.toBeInTheDocument()
    expect(screen.queryByRole('img', { name: 'Live' })).not.toBeInTheDocument()
  })
})

describe('PhotoTile stack badge', () => {
  it('shows the member-count badge for a stacked primary', () => {
    renderTile(photo({ media_type: 'image', file_name: 'raw.jpg', stack_count: 3 }))
    const badge = screen.getByRole('img', { name: '3 in stack' })
    expect(badge).toBeInTheDocument()
    expect(badge).toHaveTextContent('3')
  })

  it('shows no stack badge for a standalone photo', () => {
    renderTile(photo({ media_type: 'image', file_name: 'solo.jpg' }))
    expect(screen.queryByRole('img', { name: /in stack/ })).not.toBeInTheDocument()
  })
})

describe('PhotoTile capture date', () => {
  // The tile shows no date of its own; the alt text is the one place it carries
  // one, so that is where an estimate has to be marked as such.
  it('marks an estimated date in the alt text', () => {
    renderTile(photo({ taken_at: '1950-06-01T12:00:00Z', taken_at_estimated: true }))
    const date = new Date('1950-06-01T12:00:00Z').toLocaleDateString('en')
    expect(screen.getByRole('img', { name: `Clip — c. ${date}` })).toBeInTheDocument()
  })

  it('leaves a known date unmarked in the alt text', () => {
    renderTile(photo({ taken_at: '1950-06-01T12:00:00Z' }))
    const date = new Date('1950-06-01T12:00:00Z').toLocaleDateString('en')
    expect(screen.getByRole('img', { name: `Clip — ${date}` })).toBeInTheDocument()
  })

  // The hero-first tile keeps no always-on date; the scrim reveals one on hover.
  it('reveals the capture date in a hover caption over the tile', () => {
    renderTile(photo({ taken_at: '1950-06-01T12:00:00Z' }))
    const date = new Date('1950-06-01T12:00:00Z').toLocaleDateString('en')
    const caption = document.querySelector('.kk-tile__caption')
    expect(caption).toHaveTextContent(date)
    // The alt text already carries the date for assistive tech, so the visible
    // caption is decorative and must not be announced a second time.
    expect(caption).toHaveAttribute('aria-hidden', 'true')
  })

  it('marks an estimated date in the hover caption too', () => {
    renderTile(photo({ taken_at: '1950-06-01T12:00:00Z', taken_at_estimated: true }))
    const date = new Date('1950-06-01T12:00:00Z').toLocaleDateString('en')
    expect(document.querySelector('.kk-tile__caption')).toHaveTextContent(`c. ${date}`)
  })

  it('renders no caption for a photo with no known date', () => {
    renderTile(photo({ media_type: 'image', file_name: 'undated.jpg' }))
    expect(document.querySelector('.kk-tile__caption')).toBeNull()
  })
})

describe('PhotoTile curation controls', () => {
  // Star rating and pick/reject flagging were moved off the tile into the photo
  // detail view; the tile keeps only the favourite heart. Each case renders with
  // `favoritable` so the heart is present — proving its removal, not the tile's
  // failure to render an overlay, is what leaves the curation controls absent.
  it('renders no star rating control on the tile', () => {
    renderTile(photo({ media_type: 'image', rating: 3 }), true)
    expect(screen.queryByRole('button', { name: /Rate \d of 5/ })).not.toBeInTheDocument()
  })

  it('renders no personal-marking flag control on the tile', () => {
    renderTile(photo({ media_type: 'image' }), true)
    expect(screen.queryByRole('button', { name: 'Eye' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Thumbs up' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Thumbs down' })).not.toBeInTheDocument()
  })

  it('neither dims nor badges a reject-flagged photo on the tile', () => {
    renderTile(photo({ media_type: 'image', flag: 'reject' }), true)
    expect(screen.queryByText('Rejected')).not.toBeInTheDocument()
  })

  it('keeps the favourite heart on the tile', () => {
    renderTile(photo({ media_type: 'image' }), true)
    expect(screen.getByRole('button', { name: 'Add to favorites' })).toBeInTheDocument()
  })
})

describe('PhotoTile selection-mode rendering stability', () => {
  const p = photo({ media_type: 'image', thumb_url: '/thumb/ph1' })

  /** A selectable tile at the given selection-first state (label resolves to 'Clip'). */
  function tile(selectFirst: boolean, selected: boolean, onToggleSelect = vi.fn()) {
    return (
      <I18nextProvider i18n={i18n}>
        <MemoryRouter>
          <PhotoTile
            photo={p}
            selectable
            selectFirst={selectFirst}
            selected={selected}
            anySelected={selectFirst}
            onToggleSelect={onToggleSelect}
          />
        </MemoryRouter>
      </I18nextProvider>
    )
  }

  // The reported bug: selecting the FIRST photo flips every tile into
  // selection-first mode at once, and swapping each tile's root element between
  // <a> and <button> remounted its <img>, re-running the load-in fade across the
  // whole grid. The tile now keeps a single <Link> root, so the transition must
  // not replace the root element nor the image, nor reset an already-loaded image.
  it('keeps the same root element and <img> when selection goes empty→non-empty', () => {
    const { rerender } = render(tile(false, false))

    const rootBefore = screen.getByRole('link', { name: 'Clip' })
    const imgBefore = screen.getByRole('img', { name: 'Clip' })
    expect(rootBefore.tagName).toBe('A')

    // Mark the image as decoded — the FadeInImage 'is-loaded' state must survive
    // the transition; a remount would drop it back to the unloaded (faded) start.
    fireEvent.load(imgBefore)
    expect(imgBefore).toHaveClass('is-loaded')
    const srcBefore = imgBefore.getAttribute('src')

    // The grid turns selection-first the instant the first photo is picked.
    rerender(tile(true, false))

    // The root is now exposed as a toggle button, but it is the SAME anchor node —
    // only its role/handlers changed, so nothing under it unmounted or re-faded.
    const rootAfter = screen.getByRole('button', { name: 'Clip' })
    const imgAfter = screen.getByRole('img', { name: 'Clip' })
    expect(rootAfter).toBe(rootBefore)
    expect(imgAfter).toBe(imgBefore)
    expect(rootAfter.tagName).toBe('A')
    // The already-loaded image keeps its src and loaded state — no reset.
    expect(imgAfter.getAttribute('src')).toBe(srcBefore)
    expect(imgAfter).toHaveClass('is-loaded')
  })

  it('toggles selection on a selection-first tile click instead of navigating', () => {
    const onToggleSelect = vi.fn()
    render(tile(true, false, onToggleSelect))
    fireEvent.click(screen.getByRole('button', { name: 'Clip' }))
    expect(onToggleSelect).toHaveBeenCalledWith('ph1', false)
  })

  it('carries the Shift state so a range selection still works from the tile body', () => {
    const onToggleSelect = vi.fn()
    render(tile(true, false, onToggleSelect))
    fireEvent.click(screen.getByRole('button', { name: 'Clip' }), { shiftKey: true })
    expect(onToggleSelect).toHaveBeenCalledWith('ph1', true)
  })

  it('stays a navigable link to the detail page when not selecting', () => {
    render(tile(false, false))
    expect(screen.getByRole('link', { name: 'Clip' })).toHaveAttribute('href', '/photos/ph1')
  })
})
