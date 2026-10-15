import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'
import { type ProjectMembership } from '../services/auth'

import { ProjectUnavailablePage } from './ProjectUnavailablePage'

const STARY = {
  uid: 'ps',
  slug: 'stary',
  name: 'Starý archiv',
  description: '',
  active: false,
  role: 'admin',
  approved: true,
  subject_uid: null,
} as ProjectMembership

beforeEach(async () => {
  await i18n.changeLanguage('cs')
})

describe('ProjectUnavailablePage', () => {
  it('names the switched-off project and leads back to the chooser', () => {
    const value = {
      project: null,
      projects: [STARY],
      status: 'inactive',
      inAddress: false,
      switchProject: () => undefined,
    } as ProjectContextValue
    render(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter initialEntries={['/p/stary/albums']}>
          <ProjectContext.Provider value={value}>
            <ProjectUnavailablePage />
          </ProjectContext.Provider>
        </MemoryRouter>
      </I18nextProvider>,
    )

    expect(
      screen.getByRole('heading', { level: 1, name: 'Projekt je vypnutý' }),
    ).toBeInTheDocument()
    expect(screen.getByText(/Projekt Starý archiv správce systému vypnul/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Zpět na projekty' })).toHaveAttribute(
      'href',
      '/projects',
    )
  })
})
