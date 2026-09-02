import { forwardRef, type ReactNode, useImperativeHandle } from 'react'

/**
 * The react-virtuoso stand-ins every grid test needs.
 *
 * jsdom lays nothing out, so the real virtualizer measures a viewport of zero
 * and mounts no items at all — a test asserting on tiles would assert on an
 * empty document. These render *everything* instead, through the caller's own
 * `itemContent`, so what a test sees is the component's real markup (for the
 * photo wall, its real justified rows) with only the windowing taken away.
 *
 * Both exports are provided because one mock replaces the whole module: a page
 * may hold the photo wall (`Virtuoso`, one item per row) and a card grid
 * (`VirtuosoGrid`, one item per tile) at the same time.
 */

/** What the stand-ins read off the props; the rest is accepted and ignored. */
export interface MockVirtuosoProps {
  data?: readonly unknown[]
  itemContent?: (index: number, item: never) => ReactNode
  computeItemKey?: (index: number, item: never) => string
}

/** Renders every item of `data`, keyed as the component asked for. */
function renderAll({ data, itemContent, computeItemKey }: MockVirtuosoProps): ReactNode {
  return (data ?? []).map((item, index) => (
    <div key={computeItemKey?.(index, item as never) ?? `item-${String(index)}`}>
      {itemContent?.(index, item as never)}
    </div>
  ))
}

/**
 * The module object to hand `vi.mock('react-virtuoso', …)`. Use it as
 * `vi.mock('react-virtuoso', async () => (await import('../test/virtuoso')).virtuosoMock())`
 * — the dynamic import is what lets a hoisted factory reach this file.
 */
export function virtuosoMock() {
  return {
    Virtuoso: forwardRef<unknown, MockVirtuosoProps>(function MockVirtuoso(props, ref) {
      useImperativeHandle(ref, () => ({
        scrollToIndex: () => undefined,
        getState: () => undefined,
      }))
      return <div data-testid="grid">{renderAll(props)}</div>
    }),
    VirtuosoGrid: forwardRef<unknown, MockVirtuosoProps>(function MockVirtuosoGrid(props, ref) {
      useImperativeHandle(ref, () => ({ scrollToIndex: () => undefined }))
      return <div data-testid="grid">{renderAll(props)}</div>
    }),
  }
}
