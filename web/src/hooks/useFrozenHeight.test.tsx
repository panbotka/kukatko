import { render, screen } from '@testing-library/react'
import { useRef } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { useFrozenHeight } from './useFrozenHeight'

/** Props for {@link Probe}. */
interface ProbeProps {
  /** The subject the frozen height belongs to. */
  subject: string
  /** How many rows the measured content currently holds. */
  rows: number
}

/**
 * A block sized by a frozen measurement of its own content, so a shrinking list
 * can be watched from the outside: the box reports the height it is holding.
 */
function Probe({ subject, rows }: ProbeProps) {
  const ref = useRef<HTMLDivElement>(null)
  const height = useFrozenHeight(ref, subject)
  return (
    <div data-testid="box" style={height === null ? undefined : { height: `${String(height)}px` }}>
      <div ref={ref} data-testid="content">
        {Array.from({ length: rows }, (_, index) => (
          <p key={index}>row</p>
        ))}
      </div>
    </div>
  )
}

/** Lays the probe's content out as 20 px per row, which nothing else in jsdom does. */
function measureRowsAt20px(): void {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
    this: HTMLElement,
  ) {
    return {
      width: 0,
      height: this.childElementCount * 20,
      top: 0,
      left: 0,
      bottom: 0,
      right: 0,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    }
  })
}

describe('useFrozenHeight', () => {
  it('freezes nothing where nothing can be measured', () => {
    // jsdom lays nothing out, so the box is sized by its content as before.
    render(<Probe subject="p1" rows={3} />)

    expect(screen.getByTestId('box')).not.toHaveStyle({ height: '0px' })
    expect(screen.getByTestId('box').style.height).toBe('')
  })

  it('holds the height the content had when the subject opened', () => {
    measureRowsAt20px()

    const { rerender } = render(<Probe subject="p1" rows={3} />)
    expect(screen.getByTestId('box')).toHaveStyle({ height: '60px' })

    // Two rows answered away: the content is now a third of what it was, and the
    // block is expected not to notice.
    rerender(<Probe subject="p1" rows={1} />)
    expect(screen.getByTestId('box')).toHaveStyle({ height: '60px' })
  })

  it('measures again when the subject changes, in both directions', () => {
    measureRowsAt20px()

    const { rerender } = render(<Probe subject="p1" rows={3} />)
    expect(screen.getByTestId('box')).toHaveStyle({ height: '60px' })

    rerender(<Probe subject="p2" rows={5} />)
    expect(screen.getByTestId('box')).toHaveStyle({ height: '100px' })

    // Back to a subject already seen: it is measured as it stands now, not
    // remembered from before.
    rerender(<Probe subject="p1" rows={2} />)
    expect(screen.getByTestId('box')).toHaveStyle({ height: '40px' })
  })

  it('rounds a fractional measurement up', () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      width: 0,
      height: 60.25,
      top: 0,
      left: 0,
      bottom: 0,
      right: 0,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    })

    render(<Probe subject="p1" rows={3} />)

    expect(screen.getByTestId('box')).toHaveStyle({ height: '61px' })
  })
})
