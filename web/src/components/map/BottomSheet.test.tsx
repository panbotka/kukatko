import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type SheetState } from '../../lib/mapView'

import { BottomSheet } from './BottomSheet'

// jsdom lays nothing out: give every element (the stage included) a height.
beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(700)
})

/** A stage for the sheet to measure, owning the sheet's state as the page does. */
function Stage({ initial = 'peek' as SheetState, onChange = vi.fn() }) {
  const [state, setState] = useState<SheetState>(initial)
  return (
    <div>
      <BottomSheet
        state={state}
        onStateChange={(next) => {
          onChange(next)
          setState(next)
        }}
        label="Photos"
        growLabel="Grow"
        shrinkLabel="Shrink"
        header={<h2>3 photos</h2>}
        peek={<p>strip</p>}
      >
        <p>grid</p>
      </BottomSheet>
    </div>
  )
}

describe('BottomSheet', () => {
  it('peeks with the header and the strip, the grid kept for the raised sheet', () => {
    render(<Stage />)
    const sheet = screen.getByRole('region', { name: 'Photos' })
    expect(sheet).toHaveAttribute('data-state', 'peek')
    expect(screen.getByRole('heading', { name: '3 photos' })).toBeInTheDocument()
    expect(screen.getByText('strip')).toBeInTheDocument()
    expect(screen.queryByText('grid')).not.toBeInTheDocument()
    expect(sheet.style.height).toBe('148px')
  })

  it('steps up through half and full with the handle, then back to the peek', async () => {
    const onChange = vi.fn()
    const user = userEvent.setup()
    render(<Stage onChange={onChange} />)
    const handle = screen.getByRole('button', { name: 'Grow' })
    expect(handle).toHaveAttribute('aria-expanded', 'false')

    await user.click(handle)
    expect(onChange).toHaveBeenLastCalledWith('half')
    expect(screen.getByText('grid')).toBeInTheDocument()
    expect(screen.getByRole('region').style.height).toBe('350px')
    expect(screen.getByRole('button', { name: 'Grow' })).toHaveAttribute('aria-expanded', 'true')

    await user.click(screen.getByRole('button', { name: 'Grow' }))
    expect(onChange).toHaveBeenLastCalledWith('full')

    await user.click(screen.getByRole('button', { name: 'Shrink' }))
    expect(onChange).toHaveBeenLastCalledWith('peek')
  })

  it('follows a drag on the grip and snaps to the nearest stop on release', () => {
    const onChange = vi.fn()
    render(<Stage onChange={onChange} />)
    const grip = screen.getByRole('heading', { name: '3 photos' }).closest('.kk-map-sheet-grip')
    if (grip === null) throw new Error('no grip')
    const sheet = screen.getByRole('region')
    // jsdom has no layout: report the inline height as the rendered one.
    sheet.getBoundingClientRect = () => ({ height: parseFloat(sheet.style.height) }) as DOMRect

    fireEvent.pointerDown(grip, { pointerId: 1, clientY: 600, button: 0 })
    fireEvent.pointerMove(grip, { pointerId: 1, clientY: 590 })
    fireEvent.pointerMove(grip, { pointerId: 1, clientY: 400 })
    expect(sheet.style.height).toBe('348px')
    fireEvent.pointerUp(grip, { pointerId: 1, clientY: 400 })

    expect(onChange).toHaveBeenLastCalledWith('half')
    expect(sheet).toHaveAttribute('data-state', 'half')
  })

  it('treats a press that never moved as a tap, not a drag', () => {
    const onChange = vi.fn()
    render(<Stage onChange={onChange} />)
    const grip = screen.getByRole('heading', { name: '3 photos' }).closest('.kk-map-sheet-grip')
    if (grip === null) throw new Error('no grip')
    fireEvent.pointerDown(grip, { pointerId: 1, clientY: 600, button: 0 })
    fireEvent.pointerMove(grip, { pointerId: 1, clientY: 598 })
    fireEvent.pointerUp(grip, { pointerId: 1, clientY: 598 })
    expect(onChange).not.toHaveBeenCalled()
  })
})
