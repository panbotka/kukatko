import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { gridTemplateColumns } from '../../lib/gridDensity'
import type { Photo } from '../../services/photos'

import { GridDensityControl } from './GridDensityControl'
import { PhotoGrid } from './PhotoGrid'

const STORAGE_KEY = 'kukatko.grid.density'

/** Builds a minimal Photo whose tile is findable by its file name. */
function photo(uid: string): Photo {
  return {
    uid,
    file_hash: `hash-${uid}`,
    file_name: `${uid}.jpg`,
    file_size: 100,
    file_mime: 'image/jpeg',
    file_width: 100,
    file_height: 100,
    taken_at_source: 'exif',
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    thumb_url: `/api/v1/photos/${uid}/thumb/tile_500`,
    download_url: `/api/v1/photos/${uid}/download`,
  }
}

const PHOTOS = ['a', 'b', 'c', 'd'].map(photo)

/** Renders the grid, optionally alongside the density control that drives it. */
function renderGrid(withControl = false) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        {withControl && <GridDensityControl />}
        <PhotoGrid
          photos={PHOTOS}
          loadingMore={false}
          moreError={false}
          onEndReached={vi.fn()}
          onRetry={vi.fn()}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** The grid's own element, the one carrying the column template. */
function gridElement(): HTMLElement {
  const el = document.querySelector<HTMLElement>('.kukatko-photo-grid')
  if (el === null) {
    throw new Error('photo grid not rendered')
  }
  return el
}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

beforeEach(() => {
  window.localStorage.clear()
})

afterEach(() => {
  window.localStorage.clear()
})

describe('PhotoGrid', () => {
  it('stays width-driven when no density is persisted', () => {
    renderGrid()
    const grid = gridElement()
    expect(grid).toHaveAttribute('data-density', 'auto')
    expect(grid.style.gridTemplateColumns).toBe(gridTemplateColumns('auto'))
  })

  it('renders the persisted number of columns', () => {
    window.localStorage.setItem(STORAGE_KEY, '5')
    renderGrid()
    const grid = gridElement()
    expect(grid).toHaveAttribute('data-density', '5')
    // Five equal tracks, each at least a usable tile wide.
    expect(grid.style.gridTemplateColumns).toBe(
      'repeat(auto-fill, minmax(max(140px, calc((100% - 25px) / 5)), 1fr))',
    )
  })

  it('renders one photo per row when pinned to a single column', () => {
    window.localStorage.setItem(STORAGE_KEY, '1')
    renderGrid()
    const grid = gridElement()
    expect(grid).toHaveAttribute('data-density', '1')
    // A single full-width column — the existing tile at full width.
    expect(grid.style.gridTemplateColumns).toBe(gridTemplateColumns(1))
  })

  it('clamps a corrupt persisted value back to the responsive default', () => {
    window.localStorage.setItem(STORAGE_KEY, '{{{')
    renderGrid()
    expect(gridElement()).toHaveAttribute('data-density', 'auto')
  })

  it('follows the density control without remounting the grid', async () => {
    const user = userEvent.setup()
    renderGrid(true)

    const before = gridElement()
    expect(before).toHaveAttribute('data-density', 'auto')

    // The stepper walks auto → 2 → 3; each press restyles the live grid.
    const more = screen.getByRole('button', { name: 'More tiles per row' })
    await user.click(more)
    await user.click(more)

    await waitFor(() => {
      expect(gridElement()).toHaveAttribute('data-density', '3')
    })
    // The very same DOM node is restyled rather than replaced: virtuoso keeps its
    // scroll position and mounted tiles, so a selection the page holds survives.
    expect(gridElement()).toBe(before)
    expect(gridElement().style.gridTemplateColumns).toBe(gridTemplateColumns(3))
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('3')
  })
})
