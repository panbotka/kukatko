import { type SyntheticEvent } from 'react'

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

import { ReasonedButton } from './ReasonedButton'

describe('ReasonedButton', () => {
  it('is a plain live button when no reason is given', async () => {
    const onClick = vi.fn()
    render(<ReasonedButton onClick={onClick}>Uložit pohled</ReasonedButton>)

    const button = screen.getByRole('button', { name: 'Uložit pohled' })
    expect(button).not.toHaveAttribute('aria-disabled')
    expect(button).not.toHaveAttribute('aria-describedby')
    expect(button).not.toHaveClass('kk-btn-inert')

    await userEvent.click(button)
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('goes off and swallows the click when a reason is given', async () => {
    const onClick = vi.fn()
    render(
      <ReasonedButton disabledReason="Nejdřív vyberte fotky." onClick={onClick}>
        Hromadná úprava
      </ReasonedButton>,
    )

    const button = screen.getByRole('button', { name: 'Hromadná úprava' })
    expect(button).toHaveAttribute('aria-disabled', 'true')
    expect(button).toHaveClass('kk-btn-inert')

    await userEvent.click(button)
    expect(onClick).not.toHaveBeenCalled()
  })

  it('stays focusable so a keyboard reader can reach the reason', async () => {
    render(<ReasonedButton disabledReason="Nejdřív vyberte fotky.">Hromadná úprava</ReasonedButton>)

    // The whole point of `aria-disabled` over the native attribute: a natively
    // disabled button is skipped by Tab, and its explanation with it.
    await userEvent.tab()
    expect(screen.getByRole('button', { name: 'Hromadná úprava' })).toHaveFocus()
  })

  it('states the reason for the mouse and for assistive technology alike', () => {
    render(<ReasonedButton disabledReason="Nejdřív vyberte fotky.">Hromadná úprava</ReasonedButton>)

    const button = screen.getByRole('button', { name: 'Hromadná úprava' })
    expect(button).toHaveAttribute('title', 'Nejdřív vyberte fotky.')

    const describedBy = button.getAttribute('aria-describedby')
    expect(describedBy).not.toBeNull()
    const note = document.getElementById(describedBy ?? '')
    expect(note).toHaveTextContent('Nejdřív vyberte fotky.')
    // Out of the visual flow, so a toolbar gains no stray box from it.
    expect(note).toHaveClass('visually-hidden')
  })

  it('drops the reason again when the button comes back to life', () => {
    const { rerender } = render(
      <ReasonedButton disabledReason="Nejdřív vyberte fotky.">Hromadná úprava</ReasonedButton>,
    )
    rerender(<ReasonedButton title="Uloží aktuální pohled">Hromadná úprava</ReasonedButton>)

    const button = screen.getByRole('button', { name: 'Hromadná úprava' })
    expect(button).not.toHaveAttribute('aria-disabled')
    expect(button).toHaveAttribute('title', 'Uloží aktuální pohled')
    expect(document.querySelector('.visually-hidden')).toBeNull()
  })

  it('points at a reason already on screen instead of hiding a second copy', () => {
    render(
      <>
        <ReasonedButton disabledReason="Nejdřív vyberte fotky." reasonId="why">
          Hromadná úprava
        </ReasonedButton>
        <p id="why">Nejdřív vyberte fotky.</p>
      </>,
    )

    expect(screen.getByRole('button', { name: 'Hromadná úprava' })).toHaveAttribute(
      'aria-describedby',
      'why',
    )
    // One copy of the sentence, so a screen reader does not read it twice.
    expect(screen.getAllByText('Nejdřív vyberte fotky.')).toHaveLength(1)
  })

  it('treats an empty reason as no reason', () => {
    render(<ReasonedButton disabledReason="">Hromadná úprava</ReasonedButton>)

    expect(screen.getByRole('button', { name: 'Hromadná úprava' })).not.toHaveAttribute(
      'aria-disabled',
    )
  })

  it('never submits the form it sits in while it is off', async () => {
    const onSubmit = vi.fn((event: SyntheticEvent) => {
      event.preventDefault()
    })
    render(
      <form onSubmit={onSubmit}>
        <ReasonedButton type="submit" disabledReason="Zadejte jméno.">
          Uložit
        </ReasonedButton>
      </form>,
    )

    await userEvent.click(screen.getByRole('button', { name: 'Uložit' }))
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('keeps the caller variant and merges extra classes', () => {
    render(
      <ReasonedButton variant="outline-danger" className="mt-2" disabledReason="Právě probíhá.">
        Vymazat polohu
      </ReasonedButton>,
    )

    const button = screen.getByRole('button', { name: 'Vymazat polohu' })
    expect(button).toHaveClass('btn-outline-danger', 'kk-btn-inert', 'mt-2')
  })

  it('the shipped stylesheet greys the button without hiding the tooltip', () => {
    // `pointer-events: none` — what Bootstrap's own `.btn:disabled` sets — would
    // stop the browser ever showing the `title`, so the rule must not restore it.
    const body = ruleBody(readCss('src/styles/app.css'), /\.kk-btn-inert\s*(?=\{)/)
    expect(body).toBeDefined()
    const decls = declarations(body ?? '')
    expect(decls.get('opacity')).toBe('var(--bs-btn-disabled-opacity)')
    expect(decls.get('cursor')).toBe('not-allowed')
    expect(decls.has('pointer-events')).toBe(false)
  })
})
