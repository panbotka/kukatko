import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'

import { PendingApprovalPage } from './PendingApprovalPage'

/** The page at `/p/novy`, where the account's membership waits for approval. */
function renderPage() {
  const ctx: ProjectContextValue = {
    project: null,
    projects: [
      {
        uid: 'pn',
        slug: 'novy',
        name: 'Nový projekt',
        active: true,
        role: 'viewer',
        approved: false,
      },
    ],
    status: 'pending',
    inAddress: false,
    switchProject: vi.fn(),
  }
  return render(
    <I18nextProvider i18n={i18n}>
      <ProjectContext.Provider value={ctx}>
        <MemoryRouter initialEntries={['/p/novy/albums']}>
          <PendingApprovalPage />
        </MemoryRouter>
      </ProjectContext.Provider>
    </I18nextProvider>,
  )
}

afterEach(async () => {
  await i18n.changeLanguage('cs')
})

describe('PendingApprovalPage', () => {
  it('says the membership waits for the project admin, naming the project', async () => {
    await i18n.changeLanguage('cs')
    renderPage()

    expect(screen.getByRole('heading', { level: 1, name: 'Čeká na schválení' })).toBeInTheDocument()
    expect(
      screen.getByText(/v projektu Nový projekt ještě musí schválit správce/),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Zpět na projekty' })).toHaveAttribute(
      'href',
      '/projects',
    )
  })

  it('speaks English too', async () => {
    await i18n.changeLanguage('en')
    renderPage()

    expect(
      screen.getByRole('heading', { level: 1, name: 'Waiting for approval' }),
    ).toBeInTheDocument()
    expect(screen.getByText(/approved by the project admin/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Back to projects' })).toHaveAttribute(
      'href',
      '/projects',
    )
  })
})
