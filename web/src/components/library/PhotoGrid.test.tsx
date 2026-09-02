import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, type ReactNode, useImperativeHandle } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type GridDensityScope, initialColumns, REVIEW_GRID_SCOPE } from '../../lib/gridDensity'
import { type JustifiedRow } from '../../lib/justifiedLayout'
import type { Photo } from '../../services/photos'

import { GridDensityControl } from './GridDensityControl'
import { PhotoGrid } from './PhotoGrid'

const STORAGE_KEY = 'kukatko.grid.density'

// Stand-in for react-virtuoso's list: jsdom lays nothing out, so the real one
// would mount no rows at all. Every row is rendered, through the grid's own
// `itemContent`, so what is asserted below is the real justified layout.
interface MockListProps {
  data: JustifiedRow[]
  itemContent: (index: number, row: JustifiedRow) => ReactNode
  computeItemKey?: (index: number, row: JustifiedRow) => string
}
vi.mock('react-virtuoso', () => ({
  Virtuoso: forwardRef<unknown, MockListProps>(function MockList(
    { data, itemContent, computeItemKey },
    ref,
  ) {
    useImperativeHandle(ref, () => ({ scrollToIndex: vi.fn(), getState: vi.fn() }))
    return (
      <div data-testid="grid">
        {data.map((row, index) => (
          <div key={computeItemKey?.(index, row) ?? index}>{itemContent(index, row)}</div>
        ))}
      </div>
    )
  }),
}))

/** Builds a minimal Photo of the given pixel dimensions. */
function photo(uid: string, width = 100, height = 100): Photo {
  return {
    uid,
    file_hash: `hash-${uid}`,
    file_name: `${uid}.jpg`,
    file_size: 100,
    file_mime: 'image/jpeg',
    file_width: width,
    file_height: height,
    taken_at_source: 'exif',
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    thumb_url: `/api/v1/photos/${uid}/thumb/tile_500`,
    preview_url: `/api/v1/photos/${uid}/thumb/fit_720`,
    download_url: `/api/v1/photos/${uid}/download`,
  }
}

const PHOTOS = ['a', 'b', 'c', 'd'].map((uid) => photo(uid))

/** Renders the grid, optionally alongside the density control that drives it. */
function renderGrid(
  withControl = false,
  scope?: GridDensityScope,
  photos: readonly (Photo | undefined)[] = PHOTOS,
) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        {withControl && <GridDensityControl scope={scope} />}
        <PhotoGrid
          photos={photos}
          loadingMore={false}
          moreError={false}
          onEndReached={vi.fn()}
          onRetry={vi.fn()}
          scope={scope}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** The wall's own element, the one carrying the density and the styling. */
function gridElement(): HTMLElement {
  const el = document.querySelector<HTMLElement>('.kukatko-photo-grid')
  if (el === null) {
    throw new Error('photo grid not rendered')
  }
  return el
}

/** Every laid-out row, in order. */
function rows(): HTMLElement[] {
  return Array.from(document.querySelectorAll<HTMLElement>('.kukatko-photo-row'))
}

/** The rendered width of each tile in a row, in pixels. */
function widths(row: HTMLElement): number[] {
  return Array.from(row.children).map((child) =>
    Number.parseFloat(child instanceof HTMLElement ? child.style.width : ''),
  )
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
  it('lays every photo out exactly once, in order', () => {
    renderGrid()
    const laid = rows().flatMap((row) =>
      Array.from(row.querySelectorAll('img')).map((img) => img.getAttribute('src')),
    )
    expect(laid).toEqual(PHOTOS.map((p) => p.preview_url))
  })

  it('gives a photo the width its own shape asks for', () => {
    // A panorama, a portrait and a square in one row: same height, and the
    // widths in the order their proportions imply.
    window.localStorage.setItem(STORAGE_KEY, '3')
    renderGrid(false, undefined, [
      photo('wide', 3000, 1000),
      photo('tall', 1000, 2000),
      photo('sq'),
    ])

    const [wide, tall, square] = widths(rows()[0])
    expect(wide).toBeGreaterThan(square)
    expect(square).toBeGreaterThan(tall)
  })

  it('lays a rotated photo out as it is shown, not as it is stored', () => {
    // Orientation 6 turns a 3000×1000 file into a tall one; the thumbnail has
    // that rotation baked in, so the tile has to be tall too.
    window.localStorage.setItem(STORAGE_KEY, '3')
    renderGrid(false, undefined, [
      { ...photo('turned', 3000, 1000), file_orientation: 6 },
      photo('sq'),
    ])

    const [turned, square] = widths(rows()[0])
    expect(turned).toBeLessThan(square)
  })

  it('fills a full row edge to edge', () => {
    window.localStorage.setItem(STORAGE_KEY, '3')
    renderGrid(
      false,
      undefined,
      Array.from({ length: 12 }, (_, i) => photo(`p${String(i)}`, 3000, 2000)),
    )

    const all = rows()
    expect(all.length).toBeGreaterThan(1)
    for (const row of all.slice(0, -1)) {
      const tiles = widths(row)
      const gaps = 3 * (tiles.length - 1)
      expect(tiles.reduce((sum, w) => sum + w, 0) + gaps).toBe(1024)
    }
  })

  it('keeps a not-yet-loaded slot in the row it belongs to', () => {
    window.localStorage.setItem(STORAGE_KEY, '3')
    renderGrid(false, undefined, [photo('a'), undefined, photo('c')])

    const row = rows()[0]
    expect(row.children).toHaveLength(3)
    expect(row.querySelectorAll('img')).toHaveLength(2)
  })

  it('seeds a concrete column count when no density is persisted', () => {
    renderGrid()
    // First use resolves the width-based seed to a concrete number — never 'auto'.
    const seeded = initialColumns()
    expect(gridElement()).toHaveAttribute('data-density', String(seeded))
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe(String(seeded))
  })

  it('follows the density control without remounting the grid', async () => {
    window.localStorage.setItem(STORAGE_KEY, '2')
    const user = userEvent.setup()
    renderGrid(true)

    const before = gridElement()
    expect(before).toHaveAttribute('data-density', '2')
    const twoAcross = widths(rows()[0])[0]

    // The stepper walks 2 → 3 → 4; each press re-lays the live wall out.
    const more = screen.getByRole('button', { name: 'More tiles per row' })
    await user.click(more)
    await user.click(more)

    await waitFor(() => {
      expect(gridElement()).toHaveAttribute('data-density', '4')
    })
    // The very same DOM node is re-laid-out rather than replaced: virtuoso keeps
    // its scroll position and mounted tiles, so a selection the page holds
    // survives — and the tiles are smaller than they were at two across.
    expect(gridElement()).toBe(before)
    expect(widths(rows()[0])[0]).toBeLessThan(twoAcross)
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('4')
  })

  it('follows the review tools’ own count when scoped to them', () => {
    // /expand is a judging grid, not the browsing wall: it must not move when the
    // library's density does.
    window.localStorage.setItem(STORAGE_KEY, '2')
    window.localStorage.setItem(REVIEW_GRID_SCOPE.storageKey, '7')

    renderGrid(false, REVIEW_GRID_SCOPE)

    expect(gridElement()).toHaveAttribute('data-density', '7')
    // The library's own number is untouched by the review stepper.
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('2')
  })

  it('moves a review-scoped grid with the review stepper, not the library one', async () => {
    window.localStorage.setItem(STORAGE_KEY, '2')
    window.localStorage.setItem(REVIEW_GRID_SCOPE.storageKey, '3')
    const user = userEvent.setup()
    renderGrid(true, REVIEW_GRID_SCOPE)

    await user.click(screen.getByRole('button', { name: 'More tiles per row' }))

    await waitFor(() => {
      expect(gridElement()).toHaveAttribute('data-density', '4')
    })
    expect(window.localStorage.getItem(REVIEW_GRID_SCOPE.storageKey)).toBe('4')
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('2')
  })
})
