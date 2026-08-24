import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { PendingFilter } from './PendingFilter'

function renderFilter(props: Partial<Parameters<typeof PendingFilter>[0]> = {}) {
  const onChange = props.onChange ?? vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <PendingFilter
        pendingOnly={props.pendingOnly ?? false}
        pendingCount={props.pendingCount ?? 0}
        onChange={onChange}
      />
    </I18nextProvider>,
  )
  return onChange
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('PendingFilter', () => {
  it('prints how many accounts are waiting, so the errand is noticed', () => {
    renderFilter({ pendingCount: 2 })

    expect(screen.getByText('Waiting for approval: 2')).toBeInTheDocument()
  })

  it('still prints the count of the whole roster while the filter is on', () => {
    renderFilter({ pendingOnly: true, pendingCount: 3 })

    // The number is of everybody, not of what is on screen — a filtered list
    // that said "1" while hiding two others would be a trap.
    expect(screen.getByText('Waiting for approval: 3')).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Only waiting for approval' })).toBeChecked()
  })

  it('reports the switch being turned on', async () => {
    const actor = userEvent.setup()
    const onChange = renderFilter({ pendingCount: 1 })

    await actor.click(screen.getByRole('checkbox', { name: 'Only waiting for approval' }))

    expect(onChange).toHaveBeenCalledWith(true)
  })
})
