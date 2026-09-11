import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import type { LibrarySummary } from '../../services/system'

import { LibraryOverview } from './LibraryOverview'

function library(overrides: Partial<LibrarySummary> = {}): LibrarySummary {
  return {
    photos: 20930,
    videos: 145,
    trashed: 0,
    hidden: 12,
    private: 3,
    uploads: { day: 0, week: 18, month: 402, year: 5561 },
    albums: 132,
    labels: 64,
    people: 87,
    faces: 16585,
    embeddings: 20903,
    library_bytes: 91_000_000_000,
    trash_bytes: 0,
    derived_bytes: 4_100_000_000,
    ...overrides,
  }
}

function renderOverview(summary: LibrarySummary) {
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <LibraryOverview library={summary} />
      </I18nextProvider>
    </MemoryRouter>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('LibraryOverview', () => {
  it('still formats every count for the active language', () => {
    renderOverview(library())

    expect(screen.getByTestId('tile-photos')).toHaveTextContent('20,930')
    expect(screen.getByTestId('tile-faces')).toHaveTextContent('16,585')
    expect(screen.getByTestId('tile-albums')).toHaveTextContent('132')
  })

  it('colours nothing — these are facts about the library, not work in a state', () => {
    renderOverview(library())

    for (const key of ['photos', 'videos', 'hidden', 'private', 'albums', 'faces']) {
      expect(screen.getByTestId(`tile-${key}`).className).toBe('kk-display')
    }
  })

  it('mutes a zero here as everywhere else on the page', () => {
    renderOverview(library())

    const empty = screen.getByTestId('tile-trashed')
    expect(empty).toHaveTextContent('0')
    expect(empty).toHaveClass('text-secondary')
  })
})
