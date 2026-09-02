import { fireEvent, render, screen } from '@testing-library/react'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { useSelection } from '../../hooks/useSelection'
import type { Photo } from '../../services/photos'
import { SelectionBar } from '../organize/SelectionBar'

import { PhotoGrid } from './PhotoGrid'

/**
 * A deliberately *windowed* stand-in for react-virtuoso: it mounts only the
 * first and the last row of the wall, the way the real list keeps the rows far
 * off screen unmounted. Everything in between exists in the catalogue the grid
 * was handed, but has no DOM node — which is exactly the situation a Shift+click
 * range has to survive.
 */
interface MockListProps {
  data: unknown[]
  itemContent: (index: number, row: never) => ReactNode
}
vi.mock('react-virtuoso', () => ({
  Virtuoso: ({ data, itemContent }: MockListProps) => (
    <div data-testid="grid">
      {data.map((row, index) =>
        index === 0 || index === data.length - 1 ? (
          <div key={index}>{itemContent(index, row as never)}</div>
        ) : null,
      )}
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

/** Enough photos for the wall to lay itself out over several rows. */
const PHOTOS = Array.from({ length: 24 }, (_, index) => photo(`p${String(index).padStart(2, '0')}`))

/**
 * The grid wired to the real selection hook and the real selection bar — the
 * whole chain a page assembles, so the count under test is the one a user sees.
 */
function Harness() {
  const selection = useSelection()
  return (
    <>
      <SelectionBar count={selection.count} onCancel={selection.disable}>
        <span />
      </SelectionBar>
      <PhotoGrid
        photos={PHOTOS}
        loadingMore={false}
        moreError={false}
        onEndReached={vi.fn()}
        onRetry={vi.fn()}
        selection={{
          active: true,
          selected: selection.selected,
          onToggle: selection.toggle,
          onToggleRange: selection.toggleRange,
        }}
      />
    </>
  )
}

function renderHarness() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <Harness />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

describe('PhotoGrid range selection over a virtualized wall', () => {
  it('selects photos whose tiles are not mounted and counts them all', () => {
    renderHarness()
    // The middle of the wall is genuinely absent from the DOM.
    expect(screen.queryByRole('button', { name: 'p12.jpg' })).toBeNull()

    fireEvent.click(screen.getByRole('button', { name: 'p00.jpg' }))
    expect(screen.getByText('1 selected')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: 'p23.jpg' }), { shiftKey: true })
    expect(screen.getByText('24 selected')).toBeInTheDocument()
  })

  it('keeps extending from the same anchor instead of toggling a picked range off', () => {
    renderHarness()
    fireEvent.click(screen.getByRole('button', { name: 'p00.jpg' }))
    fireEvent.click(screen.getByRole('button', { name: 'p23.jpg' }), { shiftKey: true })
    expect(screen.getByText('24 selected')).toBeInTheDocument()

    // A second Shift+click inside the range it just picked adds nothing and
    // takes nothing away — the anchor is still the tile the run started from.
    fireEvent.click(screen.getByRole('button', { name: 'p05.jpg' }), { shiftKey: true })
    expect(screen.getByText('24 selected')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'p05.jpg' })).toHaveAttribute('aria-pressed', 'true')
  })
})
