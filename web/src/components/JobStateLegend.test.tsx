import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { JobStateLegend, type JobStateKey } from './JobStateLegend'

function renderLegend(states: readonly JobStateKey[]) {
  return render(
    <I18nextProvider i18n={i18n}>
      <JobStateLegend states={states} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(async () => {
  await i18n.changeLanguage('en')
})

describe('JobStateLegend', () => {
  it('renders a labelled, plain-language description for each requested state', () => {
    renderLegend(['total', 'queued', 'running', 'failed', 'dead', 'pending'])

    // Each state term is present as a definition term…
    for (const label of ['Total', 'Queued', 'Running', 'Failed', 'Dead', 'Waiting on box']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
    // …with an explanation of the tricky states an admin needs.
    expect(screen.getByText(/failed even after all attempts were used up/)).toBeInTheDocument()
    expect(screen.getByText(/without using up their retry budget/)).toBeInTheDocument()
    expect(screen.getByText(/waiting in the queue for the box/)).toBeInTheDocument()
  })

  it('omits states that were not requested', () => {
    renderLegend(['total', 'queued', 'running', 'failed', 'dead'])

    expect(screen.getByText('Dead')).toBeInTheDocument()
    // The System-only box-pending state is not shown on the Maintenance page.
    expect(screen.queryByText('Waiting on box')).not.toBeInTheDocument()
  })

  it('renders the Czech wording when the language is Czech', async () => {
    await i18n.changeLanguage('cs')
    renderLegend(['dead'])

    expect(screen.getByText('Mrtvé')).toBeInTheDocument()
    expect(screen.getByText(/po vyčerpání všech pokusů/)).toBeInTheDocument()
  })
})
