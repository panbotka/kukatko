import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { SearchModeControl } from './SearchModeControl'

function renderControl(mode = 'hybrid', semanticAvailable = true) {
  const onChange = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <SearchModeControl mode={mode} onChange={onChange} semanticAvailable={semanticAvailable} />
    </I18nextProvider>,
  )
  return onChange
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('SearchModeControl', () => {
  it('keeps the switch out of the way while the search runs the default mode', () => {
    renderControl()

    expect(screen.queryByLabelText('How to search')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: /advanced/i })).toHaveAttribute(
      'aria-expanded',
      'false',
    )
  })

  it('reveals the switch on request, in words rather than in retrieval jargon', async () => {
    const user = userEvent.setup()
    renderControl()

    await user.click(screen.getByRole('button', { name: /advanced/i }))

    const select = screen.getByLabelText('How to search')
    expect(within(select).getByRole('option', { name: 'Smart (recommended)' })).toBeInTheDocument()
    expect(within(select).getByRole('option', { name: 'By text' })).toBeInTheDocument()
    expect(
      within(select).getByRole('option', { name: 'By what is in the photo' }),
    ).toBeInTheDocument()
    // And one sentence saying what the mode in force actually does.
    expect(screen.getByText(/searches the captions and what can be seen/i)).toBeInTheDocument()
  })

  it('reports the picked mode', async () => {
    const user = userEvent.setup()
    const onChange = renderControl()

    await user.click(screen.getByRole('button', { name: /advanced/i }))
    await user.selectOptions(screen.getByLabelText('How to search'), 'fulltext')

    expect(onChange).toHaveBeenCalledWith('fulltext')
  })

  it('unfolds itself for a mode nobody would meet by accident', () => {
    // A shared `?mode=semantic` link ranks differently from what the same query
    // gives everyone else; the switch that did it has to be on screen.
    renderControl('semantic')

    expect(screen.getByLabelText('How to search')).toHaveValue('semantic')
    // Not twice, though: the select below the toggle already says which one.
    expect(screen.getByRole('button', { name: 'Advanced' })).toBeInTheDocument()
  })

  it('keeps saying which mode is in force once folded back up', async () => {
    const user = userEvent.setup()
    renderControl('semantic')

    await user.click(screen.getByRole('button', { name: /advanced/i }))

    expect(screen.queryByLabelText('How to search')).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /advanced — by what is in the photo/i }),
    ).toBeInTheDocument()
  })

  it('takes the unservable mode off the menu and explains why', async () => {
    const user = userEvent.setup()
    renderControl('hybrid', false)

    await user.click(screen.getByRole('button', { name: /advanced/i }))

    const semantic = within(screen.getByLabelText('How to search')).getByRole('option', {
      name: 'By what is in the photo',
    })
    expect(semantic).toBeDisabled()
    expect(semantic).toHaveAttribute('title', expect.stringMatching(/unavailable/i))
  })
})
