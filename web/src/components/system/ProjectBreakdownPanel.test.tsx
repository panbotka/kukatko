import { render, screen, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import type { ProjectSummary } from '../../services/system'

import { ProjectBreakdownPanel } from './ProjectBreakdownPanel'

function project(overrides: Partial<ProjectSummary> = {}): ProjectSummary {
  return {
    uid: 'pjv',
    slug: 'v',
    name: 'Veselice',
    active: true,
    photos: 20930,
    videos: 145,
    trashed: 3,
    originals_bytes: 2048,
    faces: 16585,
    subjects: 87,
    albums: 132,
    queue: { queued: 4, running: 1, failed: 2 },
    duplicates: 12,
    last_import: null,
    ...overrides,
  }
}

function renderPanel(projects: ProjectSummary[]) {
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <ProjectBreakdownPanel projects={projects} />
      </I18nextProvider>
    </MemoryRouter>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('ProjectBreakdownPanel', () => {
  it('shows one row per project, each with its own numbers', () => {
    renderPanel([
      project(),
      project({ uid: 'pjb', slug: 'b', name: 'Bravo', photos: 7, duplicates: 0 }),
    ])

    const rows = screen.getAllByRole('row').slice(1)
    expect(rows).toHaveLength(2)
    expect(within(rows[0]).getByRole('link', { name: 'Veselice' })).toHaveAttribute('href', '/p/v')
    expect(rows[0]).toHaveTextContent('20,930')
    expect(rows[0]).toHaveTextContent('4 queued · 1 running · 2 failed')
    expect(within(rows[1]).getByRole('link', { name: 'Bravo' })).toHaveAttribute('href', '/p/b')
    expect(rows[1]).toHaveTextContent('7')
  })

  it('marks a deactivated project and leaves its unscanned duplicates blank', () => {
    renderPanel([project({ active: false, duplicates: null })])

    const row = screen.getAllByRole('row')[1]
    expect(row).toHaveTextContent('deactivated')
    expect(row).toHaveTextContent('—')
    expect(row).toHaveTextContent('never')
  })
})
