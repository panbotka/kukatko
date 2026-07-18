import { fireEvent, render, screen } from '@testing-library/react'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import type { Photo } from '../../services/photos'

import { PhotoGrid } from './PhotoGrid'

// Minimal stand-in for react-virtuoso's grid (jsdom has no layout), so the
// tiles actually mount and their click handlers can be exercised.
interface MockGridProps {
  data: Photo[]
  itemContent: (index: number, item: Photo) => ReactNode
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({ data, itemContent }: MockGridProps) => (
    <div data-testid="grid">
      {data.map((item, index) => (
        <div key={item.uid}>{itemContent(index, item)}</div>
      ))}
    </div>
  ),
}))

/** Builds a minimal Photo whose tile is findable by its file name. */
function photo(uid: string): Photo {
  return {
    uid,
    file_name: `${uid}.jpg`,
    title: '',
    thumb_url: `/thumb/${uid}`,
  } as unknown as Photo
}

const PHOTOS = ['a', 'b', 'c', 'd'].map(photo)

function renderGrid(selection: {
  onToggle: (uid: string) => void
  onToggleRange?: (uid: string, orderedUids: string[]) => void
}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <PhotoGrid
          photos={PHOTOS}
          loadingMore={false}
          moreError={false}
          onEndReached={vi.fn()}
          onRetry={vi.fn()}
          selection={{ active: true, selected: new Set(), ...selection }}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

describe('PhotoGrid selection', () => {
  it('routes a plain click to onToggle and a Shift+click to onToggleRange', () => {
    const onToggle = vi.fn()
    const onToggleRange = vi.fn()
    renderGrid({ onToggle, onToggleRange })

    fireEvent.click(screen.getByRole('button', { name: 'b.jpg' }))
    expect(onToggle).toHaveBeenCalledWith('b')
    expect(onToggleRange).not.toHaveBeenCalled()

    // Shift+click carries the grid's own photo order, so the selection hook can
    // walk the contiguous range without the page wiring anything extra.
    fireEvent.click(screen.getByRole('button', { name: 'd.jpg' }), { shiftKey: true })
    expect(onToggleRange).toHaveBeenCalledWith('d', ['a', 'b', 'c', 'd'])
    expect(onToggle).toHaveBeenCalledTimes(1)
  })

  it('falls back to onToggle on Shift+click when no range handler is wired', () => {
    const onToggle = vi.fn()
    renderGrid({ onToggle })

    fireEvent.click(screen.getByRole('button', { name: 'c.jpg' }), { shiftKey: true })
    expect(onToggle).toHaveBeenCalledWith('c')
  })
})

/** Renders the grid in hover-select mode with the given selection set. */
function renderHoverGrid(selected: Set<string>, onToggle: (uid: string) => void) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <PhotoGrid
          photos={PHOTOS}
          loadingMore={false}
          moreError={false}
          onEndReached={vi.fn()}
          onRetry={vi.fn()}
          selection={{ active: false, hoverSelect: true, selected, onToggle }}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

describe('PhotoGrid hover-select', () => {
  it('keeps tiles as links but offers a corner checkmark before anything is picked', () => {
    const onToggle = vi.fn()
    renderHoverGrid(new Set(), onToggle)

    // The tile body still navigates to the detail page…
    expect(screen.getByRole('link', { name: 'a.jpg' })).toBeInTheDocument()
    // …while the corner checkmark selects it without opening the photo.
    fireEvent.click(screen.getByRole('button', { name: 'Select a.jpg' }))
    expect(onToggle).toHaveBeenCalledWith('a')
  })

  it('turns tiles into selection targets once anything is selected', () => {
    const onToggle = vi.fn()
    renderHoverGrid(new Set(['a']), onToggle)

    // With a selection active the whole tile toggles instead of navigating.
    expect(screen.queryByRole('link', { name: 'a.jpg' })).toBeNull()
    const tileB = screen.getByRole('button', { name: 'b.jpg' })
    fireEvent.click(tileB)
    expect(onToggle).toHaveBeenCalledWith('b')
    // The selected tile reflects its pressed state on the corner control.
    expect(screen.getByRole('button', { name: 'Select a.jpg' })).toHaveAttribute(
      'aria-pressed',
      'true',
    )
  })
})
