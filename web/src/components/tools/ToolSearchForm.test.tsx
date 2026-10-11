import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { ToolLimitField, ToolRangeField, ToolSearchForm, ToolSubmit } from './ToolSearchForm'

/** renderForm draws the full tool row: slider, limit, submit. */
function renderForm(onSubmit = vi.fn(), onLimit = vi.fn(), onRange = vi.fn()) {
  render(
    <ToolSearchForm onSubmit={onSubmit}>
      <ToolRangeField
        md={6}
        id="t-range"
        label="Threshold"
        valueText="50 %"
        min={20}
        max={80}
        step={5}
        value={50}
        onChange={onRange}
        lowHint="More results"
        highHint="Better matches"
      />
      <ToolLimitField
        md
        id="t-limit"
        label="Limit"
        hint="0 = no limit"
        min={0}
        value={0}
        onChange={onLimit}
      />
      <ToolSubmit>Search</ToolSubmit>
    </ToolSearchForm>,
  )
}

describe('ToolSearchForm', () => {
  it('stacks every column full-width on phones, the button last and full-width', () => {
    renderForm()
    const limit = screen.getByLabelText('Limit')
    const slider = screen.getByLabelText(/Threshold/)
    const button = screen.getByRole('button', { name: 'Search' })

    for (const control of [slider, limit, button]) {
      expect(control.closest('.col-12')).not.toBeNull()
    }
    expect(slider.compareDocumentPosition(limit) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(limit.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    // The helper text stays directly under its control, before the button.
    const hint = screen.getByText('0 = no limit')
    expect(limit.compareDocumentPosition(hint) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(hint.compareDocumentPosition(button) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(button).toHaveClass('w-100')
    expect(button.closest('.col-md-auto')).not.toBeNull()
  })

  it('gives every row a label line, so the controls sit level on desktop', () => {
    renderForm()
    const row = screen.getByRole('button', { name: 'Search' }).closest('.kk-tool-form')
    expect(row).not.toBeNull()
    // One label line per column: two real labels plus the submit's spacer.
    const labels = row?.querySelectorAll(':scope > [class*="col"] > .form-label') ?? []
    expect(labels).toHaveLength(3)
    // The slider sits in a control-tall slot, and the tap targets match it.
    expect(screen.getByLabelText(/Threshold/).parentElement).toHaveClass('kk-tool-range')
    expect(screen.getByLabelText('Limit')).toHaveClass('kukatko-tap-target')
    expect(screen.getByRole('button', { name: 'Search' })).toHaveClass('kukatko-tap-target')
  })

  it('submits without reloading and reports numeric values', () => {
    const onSubmit = vi.fn()
    const onLimit = vi.fn()
    const onRange = vi.fn()
    renderForm(onSubmit, onLimit, onRange)

    fireEvent.change(screen.getByLabelText('Limit'), { target: { value: '12' } })
    expect(onLimit).toHaveBeenCalledWith(12)
    fireEvent.change(screen.getByLabelText(/Threshold/), { target: { value: '65' } })
    expect(onRange).toHaveBeenCalledWith(65)
    fireEvent.click(screen.getByRole('button', { name: 'Search' }))
    expect(onSubmit).toHaveBeenCalledTimes(1)
  })

  it('renders a plain button that does not submit for a cancel action', () => {
    const onSubmit = vi.fn()
    const onCancel = vi.fn()
    render(
      <ToolSearchForm onSubmit={onSubmit}>
        <ToolSubmit type="button" variant="outline-secondary" onClick={onCancel}>
          Cancel
        </ToolSubmit>
      </ToolSearchForm>,
    )
    const button = screen.getByRole('button', { name: 'Cancel' })
    expect(button).toHaveAttribute('type', 'button')
    expect(button).toHaveClass('btn-outline-secondary', 'w-100')
    fireEvent.click(button)
    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onSubmit).not.toHaveBeenCalled()
  })
})
