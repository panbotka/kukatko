import { act, render } from '@testing-library/react'
import { forwardRef, type ReactNode, useImperativeHandle } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { type ListRange, type StateSnapshot } from 'react-virtuoso'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useGridScrollMemory } from '../../hooks/useGridScrollMemory'
import i18n from '../../i18n'
import { readGridScroll, writeGridScroll } from '../../lib/gridScroll'
import { type JustifiedRow } from '../../lib/justifiedLayout'
import { type Photo } from '../../services/photos'

import { PhotoGrid } from './PhotoGrid'

/**
 * What the stand-in virtuoso was handed and what it was asked to do, so a test
 * can drive the list the way a real one drives itself: report a visible range,
 * hand back a position when asked for one, and record the scrolls it is told to
 * make.
 */
const virtuoso: {
  restoreStateFrom: StateSnapshot | undefined
  rangeChanged: ((range: ListRange) => void) | undefined
  state: StateSnapshot
  scrollToIndex: ReturnType<typeof vi.fn>
} = {
  restoreStateFrom: undefined,
  rangeChanged: undefined,
  state: { ranges: [], scrollTop: 0 },
  scrollToIndex: vi.fn(),
}

interface MockListProps {
  data: JustifiedRow[]
  itemContent: (index: number, row: JustifiedRow) => ReactNode
  rangeChanged?: (range: ListRange) => void
  restoreStateFrom?: StateSnapshot
}

// jsdom lays nothing out, so the real virtuoso would mount no rows and report no
// position at all. This stand-in keeps the grid's own wiring — its range
// translation, its `getState` capture, its `scrollToIndex` — and lets the test
// play the part the browser plays.
vi.mock('react-virtuoso', () => ({
  Virtuoso: forwardRef<unknown, MockListProps>(function MockList(
    { data, itemContent, rangeChanged, restoreStateFrom },
    ref,
  ) {
    virtuoso.restoreStateFrom = restoreStateFrom
    virtuoso.rangeChanged = rangeChanged
    useImperativeHandle(ref, () => ({
      scrollToIndex: virtuoso.scrollToIndex,
      getState: (callback: (state: StateSnapshot) => void) => {
        callback(virtuoso.state)
      },
    }))
    return (
      <div data-testid="grid">
        {data.map((row, index) => (
          <div key={index}>{itemContent(index, row)}</div>
        ))}
      </div>
    )
  }),
}))

/** Builds a minimal landscape Photo. */
function photo(uid: string): Photo {
  return {
    uid,
    file_hash: `hash-${uid}`,
    file_name: `${uid}.jpg`,
    file_size: 100,
    file_mime: 'image/jpeg',
    file_width: 400,
    file_height: 300,
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

const PHOTOS = Array.from({ length: 60 }, (_, i) => photo(`p${String(i)}`))

/**
 * The shape react-virtuoso really hands back: a run of measured ranges closed by
 * an open-ended one, whose `endIndex` is `Infinity`. Anything that stores a
 * position has to survive this, and a `JSON` round trip of it.
 */
function virtuosoSnapshot(scrollTop: number): StateSnapshot {
  return {
    ranges: [
      { startIndex: 0, endIndex: 4, size: 105 },
      { startIndex: 5, endIndex: Number.POSITIVE_INFINITY, size: 106 },
    ],
    scrollTop,
  }
}

/** A grid page in miniature: the memory hook wired to the grid, nothing else. */
function GridPage({ photos = PHOTOS }: { photos?: readonly Photo[] }) {
  const scroll = useGridScrollMemory({ key: '/', count: photos.length })
  return (
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <PhotoGrid
          photos={photos}
          loadingMore={false}
          moreError={false}
          onRetry={() => undefined}
          scroll={scroll}
        />
      </MemoryRouter>
    </I18nextProvider>
  )
}

/** Reports a visible row range, as the list does whenever it moves. */
function reportRange(startIndex: number, endIndex: number) {
  act(() => {
    virtuoso.rangeChanged?.({ startIndex, endIndex })
  })
}

/** Puts the window at an offset, which jsdom otherwise pins to 0. */
function putWindowAt(y: number) {
  Object.defineProperty(window, 'scrollY', { value: y, configurable: true, writable: true })
}

/** Moves the window, which jsdom otherwise pins to 0 and never scrolls. */
function scrollWindowTo(y: number) {
  putWindowAt(y)
  act(() => {
    window.dispatchEvent(new Event('scroll'))
  })
}

beforeEach(() => {
  window.sessionStorage.clear()
  Object.defineProperty(window, 'scrollY', { value: 0, configurable: true, writable: true })
  virtuoso.restoreStateFrom = undefined
  virtuoso.rangeChanged = undefined
  virtuoso.state = { ranges: [], scrollTop: 0 }
  virtuoso.scrollToIndex = vi.fn()
})

describe('a grid page remembering where it was left', () => {
  it('hands the position it recorded back to the grid on the way in', () => {
    const first = render(<GridPage />)
    virtuoso.state = virtuosoSnapshot(3872)
    scrollWindowTo(4000)
    reportRange(36, 42)
    // Opening a photograph unmounts the page: whatever it was left at has to be
    // written out there and then.
    first.unmount()

    render(<GridPage />)

    expect(virtuoso.restoreStateFrom).toEqual(virtuosoSnapshot(3872))
  })

  it('records the window offset without being told to watch the window', () => {
    const { unmount } = render(<GridPage />)
    // The grid scrolls the window (`useWindowScroll`), so the window's offset is
    // the position — no per-page option decides that.
    scrollWindowTo(4000)
    virtuoso.state = virtuosoSnapshot(3872)
    reportRange(36, 42)
    unmount()

    expect(readGridScroll('/')?.scrollY).toBe(4000)
  })

  it('reveals the photograph the reader was last looking at', () => {
    writeGridScroll('/', {
      count: PHOTOS.length,
      scrollY: 4000,
      snapshot: virtuosoSnapshot(3872),
      uid: 'p40',
    })

    render(<GridPage />)
    // The list settles where it was left — rows 0..2, nowhere near p40, which the
    // reader paged to inside the viewer.
    putWindowAt(4000)
    reportRange(0, 2)

    expect(virtuoso.scrollToIndex).toHaveBeenCalledTimes(1)
    expect(virtuoso.scrollToIndex.mock.calls[0]?.[0]).toMatchObject({ align: 'end' })
  })

  it('waits for the restore to land before revealing anything', () => {
    writeGridScroll('/', {
      count: PHOTOS.length,
      scrollY: 4000,
      snapshot: virtuosoSnapshot(3872),
      uid: 'p40',
    })

    render(<GridPage />)
    // The first range a restored grid reports is the layout it had *before* the
    // restore landed, with the window still at the top. Revealing against that
    // would align the photograph off an offset nobody is at.
    reportRange(0, 2)
    expect(virtuoso.scrollToIndex).not.toHaveBeenCalled()

    putWindowAt(4000)
    reportRange(36, 42)

    expect(virtuoso.scrollToIndex).toHaveBeenCalledTimes(1)
  })

  it('does not move for a photograph that is already on screen', () => {
    writeGridScroll('/', {
      count: PHOTOS.length,
      scrollY: 4000,
      snapshot: virtuosoSnapshot(3872),
      uid: 'p20',
    })

    render(<GridPage />)
    // A range wide enough that p20's row is comfortably inside it.
    putWindowAt(4000)
    reportRange(0, 40)

    expect(virtuoso.scrollToIndex).not.toHaveBeenCalled()
  })

  it('does not chase a photograph that is no longer in the list', () => {
    writeGridScroll('/', {
      count: PHOTOS.length,
      scrollY: 4000,
      snapshot: virtuosoSnapshot(3872),
      uid: 'archived',
    })

    render(<GridPage />)
    putWindowAt(4000)
    reportRange(0, 2)

    expect(virtuoso.scrollToIndex).not.toHaveBeenCalled()
  })

  it('gives up on the reveal once the reader has taken the wall over', () => {
    writeGridScroll('/', {
      count: PHOTOS.length,
      scrollY: 4000,
      snapshot: virtuosoSnapshot(3872),
      uid: 'p40',
    })

    render(<GridPage />)
    // The reader is already scrolling for themselves; the wall is theirs now.
    act(() => {
      window.dispatchEvent(new WheelEvent('wheel'))
    })
    putWindowAt(4000)
    reportRange(0, 2)

    expect(virtuoso.scrollToIndex).not.toHaveBeenCalled()
  })
})
