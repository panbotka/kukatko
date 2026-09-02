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
    for (const label of [
      'Total',
      'Waiting',
      'In progress',
      'Failed',
      'Permanently failed',
      'Waiting for the recognition service',
    ]) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
    // …with an explanation of the tricky states an admin needs — and not a word
    // of the vocabulary they used to be written in ("dead", "the box").
    expect(screen.getByText(/went wrong even after several attempts/)).toBeInTheDocument()
    expect(screen.getByText(/waiting costs it no attempts/)).toBeInTheDocument()
    expect(screen.getByText(/waiting for the service to come back/)).toBeInTheDocument()
  })

  it('omits states that were not requested', () => {
    renderLegend(['total', 'queued', 'running', 'failed', 'dead'])

    expect(screen.getByText('Permanently failed')).toBeInTheDocument()
    // The System-only recognition-service state is not shown on Maintenance.
    expect(screen.queryByText('Waiting for the recognition service')).not.toBeInTheDocument()
  })

  it('renders the Czech wording when the language is Czech', async () => {
    await i18n.changeLanguage('cs')
    renderLegend(['dead'])

    expect(screen.getByText('Trvale se nepovedlo')).toBeInTheDocument()
    expect(screen.getByText(/ani po několika pokusech/)).toBeInTheDocument()
  })
})
