import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useCallback, useRef } from 'react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type PhotoListParams, type Timeline } from '../../services/photos'
import { declarations, readCss, ruleBody } from '../../test/css'
import { realisticTimeline } from '../../test/timeline'

import { type TimelineJump, TimelineScrubber } from './TimelineScrubber'

// Only the network call is faked; the component's positioning/highlight logic
// runs for real.
vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, fetchTimeline: vi.fn() }
})

const { fetchTimeline } = await import('../../services/photos')
const fetchMock = vi.mocked(fetchTimeline)

const TIMELINE: Timeline = {
  buckets: [
    { year: 2026, month: 2, count: 3, cumulative: 0 },
    { year: 2026, month: 1, count: 5, cumulative: 3 },
  ],
  total: 8,
}

/** The rail height measured on production at a 1280×633 viewport. */
const RAIL_HEIGHT_PX = 549

/**
 * The height of a rendered year label, measured on production. jsdom hands every
 * element a zero-height box, so the overlap assertions below reconstruct each
 * label's box from its inline `top` (a percentage of the rail) plus this height;
 * the real boxes are measured in a browser against the same fixture.
 */
const LABEL_HEIGHT_PX = 16

/**
 * Makes the rail report a real height: without it the component measures 0 and
 * only its fallback height would ever be exercised.
 */
function stubRailGeometry(height = RAIL_HEIGHT_PX): void {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: 0,
    y: 0,
    top: 0,
    left: 0,
    right: 68,
    bottom: height,
    width: 68,
    height,
    toJSON: () => ({}),
  })
}

/** The centre of every rendered element, in pixels down the rail. */
function centres(elements: Element[], height = RAIL_HEIGHT_PX): number[] {
  return elements.map((el) => (parseFloat((el as HTMLElement).style.top) / 100) * height)
}

/**
 * The default filters, as one stable object: a fresh one on every render would be
 * a new filter to `useTimeline`, which refetches and unmounts the rail mid-test.
 */
const DEFAULT_PARAMS: PhotoListParams = { sort: 'newest' }

/** A grid box `gridTop` pixels down the viewport, the size jsdom never computes. */
function gridRectAt(gridTop: number): DOMRect {
  return {
    x: 0,
    y: gridTop,
    top: gridTop,
    left: 0,
    right: 350,
    bottom: gridTop + 400,
    width: 350,
    height: 400,
    toJSON: () => ({}),
  }
}

interface HarnessProps {
  params?: PhotoListParams
  activeIndex?: number
  anchor?: string
  /**
   * Where the grid box starts in viewport coordinates — that is, what everything
   * the page renders above the grid adds up to. Changing it between renders is
   * the page scrolling or its header growing; the rail re-measures on the scroll
   * or resize that follows, as it does in a browser.
   */
  gridTop?: number
  onJump?: (jump: TimelineJump) => void
}

/**
 * Renders the rail the way a page does: a grid box first, the rail after it,
 * with the ref between them. The rail measures that box, so a test can put the
 * page's header height into `gridTop` and read back where the rail decided to
 * start.
 */
function Harness({ params, activeIndex, anchor, gridTop = 0, onJump }: HarnessProps) {
  const gridWrapRef = useRef<HTMLDivElement>(null)
  // The latest geometry, read at measure time rather than captured, so the ref
  // callback stays stable across renders.
  const topRef = useRef(gridTop)
  topRef.current = gridTop
  const attachGrid = useCallback((node: HTMLDivElement | null) => {
    if (node !== null) {
      node.getBoundingClientRect = () => gridRectAt(topRef.current)
    }
    gridWrapRef.current = node
  }, [])
  return (
    <I18nextProvider i18n={i18n}>
      <div ref={attachGrid} data-testid="grid" />
      <TimelineScrubber
        params={params ?? DEFAULT_PARAMS}
        activeIndex={activeIndex ?? 0}
        gridWrapRef={gridWrapRef}
        anchor={anchor}
        onJump={onJump ?? vi.fn()}
      />
    </I18nextProvider>
  )
}

function renderScrubber(props: HarnessProps) {
  return render(<Harness {...props} />)
}

/** The grid indexes a spy was asked to jump to, in call order. */
function jumpedIndexes(onJump: ReturnType<typeof vi.fn>): number[] {
  return onJump.mock.calls.map((call) => (call[0] as TimelineJump).index)
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
})

describe('TimelineScrubber', () => {
  it('renders a tick per bucket and clicking one jumps to its cumulative index', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    const onJump = vi.fn()
    const user = userEvent.setup()
    renderScrubber({ onJump })

    const jan = await screen.findByRole('button', { name: 'Jump to Jan 2026' })
    expect(screen.getByRole('button', { name: 'Jump to Feb 2026' })).toBeInTheDocument()

    await user.click(jan)
    // A deliberate click pushes its month onto the history, so Back undoes it.
    expect(onJump).toHaveBeenCalledWith({ index: 3, month: '2026-01', replace: false })
  })

  it('reflects the active filters, refetching when the params change', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    const { rerender } = renderScrubber({ params: { sort: 'newest', camera: 'Canon' } })

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(fetchMock.mock.calls[0][0]).toMatchObject({ camera: 'Canon' })

    rerender(<Harness params={{ sort: 'newest', camera: 'Nikon' }} />)

    await waitFor(() => {
      const last = fetchMock.mock.calls[fetchMock.mock.calls.length - 1][0]
      expect(last).toMatchObject({ camera: 'Nikon' })
    })
  })

  it('highlights the month containing the current scroll range', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    const { rerender } = renderScrubber({ activeIndex: 0 })

    // Range starts at index 0 → the newest bucket (Feb) is active.
    const feb = await screen.findByRole('button', { name: 'Jump to Feb 2026' })
    expect(feb).toHaveAttribute('aria-current', 'true')
    expect(screen.getByRole('button', { name: 'Jump to Jan 2026' })).not.toHaveAttribute(
      'aria-current',
      'true',
    )

    // Scrolling to index 5 lands inside the second bucket (cumulative 3..7).
    rerender(<Harness activeIndex={5} />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Jump to Jan 2026' })).toHaveAttribute(
        'aria-current',
        'true',
      )
    })
    expect(screen.getByRole('button', { name: 'Jump to Feb 2026' })).not.toHaveAttribute(
      'aria-current',
      'true',
    )
  })

  it('thins a real long-tailed library down to what the rail can show', async () => {
    stubRailGeometry()
    const timeline = realisticTimeline()
    fetchMock.mockResolvedValue(timeline)
    const { container } = renderScrubber({})

    await screen.findByRole('navigation')
    await waitFor(() => {
      expect(container.querySelectorAll('.kukatko-timeline-tick').length).toBeLessThan(
        timeline.buckets.length / 4,
      )
    })

    const ticks = [...container.querySelectorAll('.kukatko-timeline-tick')]
    const labels = [...container.querySelectorAll('.kukatko-timeline-tick.has-year')]

    // The invariant that matters: no two rendered labels overlap. Asserted on
    // the boxes rather than on a label count, so any future thinning rule has to
    // keep it true.
    const labelTops = centres(labels)
    const overlaps = labelTops.filter(
      (top, index) => index > 0 && top - labelTops[index - 1] < LABEL_HEIGHT_PX,
    )
    expect(overlaps).toEqual([])
    // …and enough of them survive to be worth reading.
    expect(labels.length).toBeGreaterThanOrEqual(8)

    // Month ticks stay individually visible instead of merging into one bar.
    const tickTops = centres(ticks)
    const gaps = tickTops.slice(1).map((top, index) => top - tickTops[index])
    expect(Math.min(...gaps)).toBeGreaterThanOrEqual(4)
    expect(Math.max(...tickTops)).toBeLessThanOrEqual(RAIL_HEIGHT_PX)
  })

  it('keeps the oldest month reachable by click and by drag', async () => {
    stubRailGeometry()
    const timeline = realisticTimeline()
    const oldest = timeline.buckets[timeline.buckets.length - 1]
    fetchMock.mockResolvedValue(timeline)
    const onJump = vi.fn()
    const user = userEvent.setup()
    const { container } = renderScrubber({ onJump })
    const rail = await screen.findByRole('navigation')

    // The bottom of the rail names the first year of the archive and jumps to it.
    await waitFor(() => {
      expect(container.querySelectorAll('.kukatko-timeline-tick.has-year').length).toBeGreaterThan(
        0,
      )
    })
    const labels = [...container.querySelectorAll('.kukatko-timeline-tick.has-year')]
    const last = labels[labels.length - 1]
    expect(last.textContent).toBe(String(oldest.year))
    await user.click(last)
    expect(onJump).toHaveBeenCalledWith({
      index: oldest.cumulative,
      month: `${String(oldest.year)}-06`,
      replace: false,
    })

    // Dragging to the rail's bottom edge lands on the same month: the position a
    // tick is drawn at and the position a drag reads back agree.
    onJump.mockClear()
    fireEvent.pointerDown(rail, { clientY: RAIL_HEIGHT_PX - 1, pointerId: 1 })
    expect(jumpedIndexes(onJump)).toEqual([oldest.cumulative])
    fireEvent.pointerUp(rail, { clientY: RAIL_HEIGHT_PX - 1, pointerId: 1 })
  })

  it('drags through months without a tick swallowing the gesture', async () => {
    stubRailGeometry()
    const timeline = realisticTimeline()
    fetchMock.mockResolvedValue(timeline)
    const onJump = vi.fn()
    const { container } = renderScrubber({ onJump })
    const rail = await screen.findByRole('navigation')
    await waitFor(() => {
      expect(container.querySelectorAll('.kukatko-timeline-tick').length).toBeGreaterThan(0)
    })

    // Press on a tick: that only arms the drag, so the tick's own click still
    // decides the month if the pointer never moves.
    const capture = vi.spyOn(Element.prototype, 'setPointerCapture')
    const tick = screen.getAllByRole('button')[0]
    fireEvent.pointerDown(tick, { clientY: 4, pointerId: 1 })
    expect(onJump).not.toHaveBeenCalled()
    // …and the press must not capture the pointer: capturing retargets the
    // compatibility click to the capturing element, so the tick would never
    // receive its own click and pressing it would do nothing. (jsdom cannot
    // reproduce that retargeting, hence the assertion on the call itself.)
    expect(capture).not.toHaveBeenCalled()

    // Once it moves, the rail takes over and every crossed month fires once, in
    // order, oldest last.
    for (let y = 100; y <= 500; y += 100) {
      fireEvent.pointerMove(rail, { clientY: y, pointerId: 1 })
    }
    fireEvent.pointerUp(rail, { clientY: 500, pointerId: 1 })

    // Once it is a drag, the pointer is captured so it keeps tracking outside
    // the rail's bounds.
    expect(capture).toHaveBeenCalled()

    const jumped = jumpedIndexes(onJump)
    expect(jumped.length).toBe(5)
    expect(jumped).toEqual([...jumped].sort((a, b) => a - b))
    expect(new Set(jumped).size).toBe(jumped.length)
  })

  it('marks the steps of a drag as replacing, so back undoes the whole gesture', async () => {
    stubRailGeometry()
    fetchMock.mockResolvedValue(realisticTimeline())
    const onJump = vi.fn()
    const { container } = renderScrubber({ onJump })
    const rail = await screen.findByRole('navigation')
    await waitFor(() => {
      expect(container.querySelectorAll('.kukatko-timeline-tick').length).toBeGreaterThan(0)
    })

    // The press that starts on the bare rail is still a deliberate pick…
    fireEvent.pointerDown(rail, { clientY: 100, pointerId: 1 })
    expect((onJump.mock.calls[0][0] as TimelineJump).replace).toBe(false)
    // …but every month the drag then sweeps through only replaces it, so the
    // history holds one entry for the gesture rather than one per month.
    for (let y = 200; y <= 400; y += 100) {
      fireEvent.pointerMove(rail, { clientY: y, pointerId: 1 })
    }
    fireEvent.pointerUp(rail, { clientY: 400, pointerId: 1 })
    const replaces = onJump.mock.calls.slice(1).map((call) => (call[0] as TimelineJump).replace)
    expect(replaces.length).toBeGreaterThan(0)
    expect(replaces.every(Boolean)).toBe(true)
  })

  it('restores the month the URL anchors to, once, without pushing it again', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    const onJump = vi.fn()
    const { rerender } = renderScrubber({ anchor: '2026-01', onJump })

    await waitFor(() => {
      expect(onJump).toHaveBeenCalledWith({ index: 3, month: '2026-01', replace: true })
    })
    // Re-rendering with the same anchor must not jump again — otherwise every
    // render would yank a reader who has since scrolled away back to the month.
    rerender(<Harness activeIndex={7} anchor="2026-01" onJump={onJump} />)
    expect(onJump).toHaveBeenCalledTimes(1)
  })

  it('ignores an anchor no bucket matches', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    const onJump = vi.fn()
    renderScrubber({ anchor: '1999-03', onJump })

    await screen.findByRole('navigation')
    expect(onJump).not.toHaveBeenCalled()
  })

  it('renders nothing when the timeline has no buckets', async () => {
    fetchMock.mockResolvedValue({ buckets: [], total: 0 })
    renderScrubber({})

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
  })
})

/**
 * Where the phone rail begins. It is `position: fixed`, so nothing in the layout
 * keeps it off the page's own header — it used to start a constant 6 rem below
 * the navbar, which is the height of the filter row **and nothing else**.
 *
 * The regression this guards: on the library's arrival screen the „Co je nového"
 * digest renders above the filter row and pushed **Filtry** down to y=194–242
 * (measured on production, 390×844), while the rail still began at y=148 — across
 * 40 px of the button, 38 % of it. A tap at (378, 218), visually inside the
 * button, hit a year tick and scrolled the library to 142 192 px instead of
 * opening the drawer. An instance-wide announcement renders into the same slot,
 * which would make that permanent rather than once per visit.
 */
describe('TimelineScrubber placement', () => {
  const FILTERS_BOTTOM_WITH_DIGEST = 242
  const GRID_TOP_WITH_DIGEST = 250

  /** Where the rendered rail says it starts. */
  function railTop(): string {
    return screen.getByRole('navigation').style.getPropertyValue('--kukatko-timeline-top')
  }

  it('starts at the grid, whatever the page rendered above it', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    // The arrival screen: digest, then the filter row, then the grid.
    const { unmount } = renderScrubber({ gridTop: GRID_TOP_WITH_DIGEST })

    await screen.findByRole('navigation')
    expect(railTop()).toBe(`${GRID_TOP_WITH_DIGEST}px`)
    // The button the tap was aimed at is above the rail's first pixel, which is
    // the whole point: no part of the filter row can be under it.
    expect(parseFloat(railTop())).toBeGreaterThanOrEqual(FILTERS_BOTTOM_WITH_DIGEST)
    unmount()

    // The same page without the digest — the case that used to work — still puts
    // the rail at the grid rather than at the old constant.
    renderScrubber({ gridTop: 125 })
    await waitFor(() => {
      expect(railTop()).toBe('125px')
    })
  })

  it('follows the header as it scrolls away, and stops at zero', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    const { rerender } = renderScrubber({ gridTop: GRID_TOP_WITH_DIGEST })
    await screen.findByRole('navigation')

    // A short scroll: the header is still on screen, so the rail follows it up.
    rerender(<Harness gridTop={90} />)
    fireEvent.scroll(window)
    await waitFor(() => {
      expect(railTop()).toBe('90px')
    })

    // Once the header is off the top the answer clamps to 0 — the stylesheet's
    // own floor takes over from there, and the clamp is what stops every further
    // scroll frame from re-rendering the rail with a more negative number.
    rerender(<Harness gridTop={-4200} />)
    fireEvent.scroll(window)
    await waitFor(() => {
      expect(railTop()).toBe('0px')
    })
  })

  it('is what the phone stylesheet positions the rail by', () => {
    const css = readCss('src/styles/app.css')
    const phone = ruleBody(css, /\.kukatko-timeline\s*(?=\{)/, /--kukatko-timeline-top/)
    expect(phone).toBeDefined()
    // Whitespace-stripped: the declaration is long enough that the formatter
    // wraps it, and where it wraps is not what is being asserted.
    const top = (declarations(phone ?? '').get('top') ?? '').replace(/\s+/g, '')
    // The measurement decides where the rail starts…
    expect(top).toContain('var(--kukatko-timeline-top')
    // …down to a floor just under the sticky navbar, for when the header has
    // scrolled away and the measurement reads 0.
    expect(top).toContain('max(')
    expect(top).toContain('var(--kukatko-navbar-height)')
  })
})

/**
 * The rail as a **touch** control.
 *
 * Measured on production (390×844, with a genuinely coarse pointer active): 31
 * year ticks of 39.8 × 16.2 px at a 20.2 px pitch and 62 month ticks of
 * 16.0 × 5.2 px — 93 buttons in a 40 px strip, against a fingertip of 34–45 px,
 * on the app's primary way across time on a phone. WCAG 2.2 SC 2.5.8 asks for
 * 24 × 24 and its spacing exception cannot rescue a 20 px pitch either: 24 px
 * circles centred on adjacent ticks intersect. A mis-tap here is a jump of tens
 * of thousands of pixels with no undo but scrolling back.
 *
 * The fix has two halves and either alone is a lie: the *positions* (year ticks a
 * fingertip apart, the small ones demoted to decoration) and the *boxes*
 * (2.75 rem, and no hit area at all for the decoration). jsdom computes no
 * layout, so they can only be pinned separately — what is below is the layout
 * half; the boxes are read straight out of `app.css` by
 * `styles/tapTargets.test.ts`, where the app's other coarse-pointer floors are
 * guarded.
 */
describe('TimelineScrubber touch targets', () => {
  /** The app's finger-target floor, and so the minimum pitch of the rail's controls. */
  const TOUCH_TARGET_PX = 44

  const realMatchMedia = window.matchMedia

  /** Points `window.matchMedia` at a device whose primary pointer is a finger. */
  function setCoarsePointer(coarse: boolean): void {
    window.matchMedia = vi.fn().mockImplementation((query: string) => ({
      matches: coarse && query.includes('pointer: coarse'),
      media: query,
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }))
  }

  afterEach(() => {
    // Reassigning `window.matchMedia` outlives mock restoration, so put the
    // shared stub back by hand.
    window.matchMedia = realMatchMedia
  })

  /** Renders the realistic library's rail and waits for its ticks. */
  async function renderRail(onJump = vi.fn()) {
    stubRailGeometry()
    const timeline = realisticTimeline()
    fetchMock.mockResolvedValue(timeline)
    const { container } = renderScrubber({ onJump })
    await screen.findByRole('navigation')
    await waitFor(() => {
      expect(container.querySelectorAll('.kukatko-timeline-tick').length).toBeGreaterThan(0)
    })
    return { container, timeline, onJump }
  }

  it('spaces its controls by a fingertip, and gives the rest of the rail none', async () => {
    setCoarsePointer(true)
    const { container } = await renderRail()

    const buttons = [...container.querySelectorAll('button.kukatko-timeline-tick')]
    // Every control is a year tick: the 5px month ticks are no longer buttons.
    expect(buttons.length).toBeGreaterThanOrEqual(8)
    expect(buttons.every((el) => el.classList.contains('has-year'))).toBe(true)
    expect(container.querySelectorAll('button.kukatko-timeline-tick:not(.has-year)')).toHaveLength(
      0,
    )

    // …and they are far enough apart that a 44px box centred on each touches its
    // neighbours without covering them. This is the assertion the old rail could
    // not pass at any box size: its labels sat 20px apart.
    const tops = centres(buttons)
    const gaps = tops.slice(1).map((top, index) => top - tops[index])
    expect(Math.min(...gaps)).toBeGreaterThan(TOUCH_TARGET_PX - 1e-9)

    // The ticks between them stay drawn — the rail still reads as a ruler — but
    // they are decoration: no role, no name, no focus stop.
    const decorative = [...container.querySelectorAll('.kukatko-timeline-tick.is-decorative')]
    expect(decorative.length).toBeGreaterThan(0)
    expect(decorative.every((el) => el.tagName === 'SPAN')).toBe(true)
    expect(decorative.every((el) => el.getAttribute('aria-hidden') === 'true')).toBe(true)
    expect(screen.getAllByRole('button')).toHaveLength(buttons.length)
  })

  it('keeps both ends of the archive one tap away, and every month in between', async () => {
    setCoarsePointer(true)
    const user = userEvent.setup()
    const { container, timeline, onJump } = await renderRail()
    const oldest = timeline.buckets[timeline.buckets.length - 1]

    const buttons = [...container.querySelectorAll('button.kukatko-timeline-tick')]
    await user.click(buttons[buttons.length - 1])
    expect(onJump).toHaveBeenCalledWith({
      index: oldest.cumulative,
      month: `${String(oldest.year)}-06`,
      replace: false,
    })
    onJump.mockClear()
    await user.click(buttons[0])
    expect(jumpedIndexes(onJump)).toEqual([0])

    // A tap that lands between two controls is not lost: it reaches the rail,
    // which scrubs at the month grain the tap itself gave up.
    onJump.mockClear()
    const [decorative] = container.querySelectorAll('.kukatko-timeline-tick.is-decorative')
    expect(decorative).toBeDefined()
    fireEvent.pointerDown(decorative, { clientY: 300, pointerId: 1 })
    fireEvent.pointerUp(decorative, { clientY: 300, pointerId: 1 })
    expect(onJump).toHaveBeenCalledTimes(1)
  })

  it('names the whole range a control now stands for', async () => {
    setCoarsePointer(true)
    const { container } = await renderRail()

    // A control that swallowed the small ticks after it says so, rather than
    // announcing one month and jumping into a range of them.
    const ranges = [...container.querySelectorAll('button.kukatko-timeline-tick')].filter((el) =>
      (el.getAttribute('aria-label') ?? '').includes('–'),
    )
    expect(ranges.length).toBeGreaterThan(0)
  })

  it('leaves the dense rail to the pointer that can aim at it', async () => {
    setCoarsePointer(false)
    const { container } = await renderRail()

    // A mouse keeps a button per drawn tick, month ticks included: this is the
    // precision the fix must not take away from the desktop.
    const buttons = container.querySelectorAll('button.kukatko-timeline-tick')
    const labelled = container.querySelectorAll('button.kukatko-timeline-tick.has-year')
    expect(buttons.length).toBeGreaterThan(labelled.length)
    expect(container.querySelectorAll('.is-decorative')).toHaveLength(0)
  })
})

/**
 * The awake/asleep state (`is-active`), which is what makes the rail usable on a
 * phone: there it lies over the photographs rather than beside them, so it is a
 * faint scale at rest and only comes to full strength — plated, with the month
 * bubble showing — while it is being used. Everything that state *means* is CSS;
 * what is asserted here is that the class tracks activity, since a rail stuck
 * asleep is unreadable and one stuck awake permanently covers photographs.
 */
describe('TimelineScrubber wakefulness', () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('wakes on arrival and falls asleep once nothing has happened', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    renderScrubber({})

    // Arriving is itself activity: the rail announces that it is there.
    const rail = await screen.findByRole('navigation')
    expect(rail).toHaveClass('is-active')

    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(rail).not.toHaveClass('is-active')
  })

  it('wakes again when the grid scrolls under it', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    // One params object across both renders: a fresh one would be a new filter
    // to `useTimeline`, which would refetch and unmount the rail mid-test.
    const params: PhotoListParams = { sort: 'newest' }
    const { rerender } = renderScrubber({ params, activeIndex: 0 })

    const rail = await screen.findByRole('navigation')
    act(() => {
      vi.advanceTimersByTime(2000)
    })
    expect(rail).not.toHaveClass('is-active')

    // The visible range moving *is* the scroll signal — the rail needs no
    // listener of its own to notice.
    rerender(<Harness params={params} activeIndex={5} />)
    expect(screen.getByRole('navigation')).toHaveClass('is-active')
  })

  it('stays awake for as long as a finger is on it', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    stubRailGeometry()
    renderScrubber({})

    const rail = await screen.findByRole('navigation')
    act(() => {
      vi.advanceTimersByTime(2000)
    })

    fireEvent.pointerDown(rail, { clientY: 10 })
    expect(rail).toHaveClass('is-active')
    // Half the idle window later a drag step keeps it up rather than letting the
    // countdown that started with the press run out mid-gesture.
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    fireEvent.pointerMove(rail, { clientY: 200 })
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect(rail).toHaveClass('is-active')
  })
})
