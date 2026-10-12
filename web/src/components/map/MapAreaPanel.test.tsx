import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ComponentType, type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type MapFeature } from '../../services/map'

import { MapAreaPanel, type MapAreaPanelProps } from './MapAreaPanel'

// jsdom mounts no virtualized items: render every item through the panel's own
// List/Item components, so the grid's markup is the real one.
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({
    data,
    itemContent,
    computeItemKey,
    components,
  }: {
    data: MapFeature[]
    itemContent: (index: number, item: MapFeature) => ReactNode
    computeItemKey: (index: number, item: MapFeature) => string
    components: {
      List: ComponentType<{ children?: ReactNode }>
      Item: ComponentType<{ children?: ReactNode }>
    }
  }) => {
    const { List, Item } = components
    return (
      <List>
        {data.map((item, index) => (
          <Item key={computeItemKey(index, item)}>{itemContent(index, item)}</Item>
        ))}
      </List>
    )
  },
}))

function photo(uid: string, extra: Partial<MapFeature['properties']> = {}): MapFeature {
  return {
    type: 'Feature',
    geometry: { type: 'Point', coordinates: [16.6, 49.2] },
    properties: { uid, title: '', media_type: 'image', thumb: '', ...extra },
  }
}

function renderPanel(overrides: Partial<MapAreaPanelProps> = {}) {
  const props: MapAreaPanelProps = {
    layout: 'side',
    photos: [
      photo('ph1', { taken_at: '2021-03-05T12:00:00Z' }),
      photo('ph2', { media_type: 'video' }),
    ],
    mode: 'area',
    detailQuery: 'bbox=1%2C2%2C3%2C4',
    onBackToArea: vi.fn(),
    onHoverPhoto: vi.fn(),
    highlightUid: null,
    open: true,
    onOpenChange: vi.fn(),
    sheetState: 'peek',
    onSheetStateChange: vi.fn(),
    ...overrides,
  }
  render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <MapAreaPanel {...props} />
      </MemoryRouter>
    </I18nextProvider>,
  )
  return props
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('MapAreaPanel (desktop column)', () => {
  it('heads the grid with the count and links each photo into the viewer with the area', () => {
    renderPanel()
    const panel = screen.getByRole('complementary', { name: 'Photos in the map area' })
    expect(
      within(panel).getByRole('heading', { name: '2 photos in this area' }),
    ).toBeInTheDocument()
    const first = within(panel).getByRole('link', { name: 'Photo from 3/5/2021' })
    expect(first).toHaveAttribute('href', '/p/v/photos/ph1?bbox=1%2C2%2C3%2C4')
    // A video says so in words as well as with its glyph.
    expect(within(panel).getByText('Video')).toBeInTheDocument()
  })

  it('reports the photo under the pointer, so the map can mark its tile', async () => {
    const user = userEvent.setup()
    const props = renderPanel()
    const link = screen.getByRole('link', { name: 'Photo from 3/5/2021' })
    await user.hover(link)
    expect(props.onHoverPhoto).toHaveBeenLastCalledWith('ph1')
    await user.unhover(link)
    expect(props.onHoverPhoto).toHaveBeenLastCalledWith(null)
  })

  it('marks the highlighted photo', () => {
    renderPanel({ highlightUid: 'ph1' })
    expect(screen.getByRole('link', { name: 'Photo from 3/5/2021' })).toHaveClass('is-highlighted')
  })

  it('names an opened cluster as a spot and offers the way back to the area', async () => {
    const user = userEvent.setup()
    const props = renderPanel({ mode: 'spot', photos: [photo('ph1')] })
    expect(screen.getByRole('heading', { name: '1 photo at this spot' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Whole area/ }))
    expect(props.onBackToArea).toHaveBeenCalled()
  })

  it('says what to do with an empty area', () => {
    renderPanel({ photos: [] })
    expect(screen.getByRole('heading', { name: '0 photos in this area' })).toBeInTheDocument()
    expect(screen.getByText('Pan or zoom out the map.')).toBeInTheDocument()
  })

  it('collapses into a small button that brings it back with the count', async () => {
    const user = userEvent.setup()
    const props = renderPanel({ open: false })
    expect(screen.queryByRole('complementary')).not.toBeInTheDocument()
    const reopen = screen.getByRole('button', { name: /Photos in this area/ })
    expect(reopen).toHaveTextContent('2')
    await user.click(reopen)
    expect(props.onOpenChange).toHaveBeenCalledWith(true)
  })
})

describe('MapAreaPanel (phone sheet)', () => {
  it('peeks with the count and a strip of the newest photos', () => {
    renderPanel({ layout: 'sheet' })
    const sheet = screen.getByRole('region', { name: 'Photos in the map area' })
    expect(sheet).toHaveAttribute('data-state', 'peek')
    expect(within(sheet).getAllByRole('link')).toHaveLength(2)
  })
})
