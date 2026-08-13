import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import type { LibraryStats } from '../../services/system'

import { CoverageMeters } from './CoverageMeters'

/** Counts with a known share behind every meter: 60 % / 90 % / 75 %. */
function stats(overrides: Partial<LibraryStats> = {}): LibraryStats {
  return {
    photos: 1000,
    videos: 10,
    photos_live: 1000,
    photos_archived: 0,
    photos_hidden: 0,
    photos_stacked: 0,
    photos_listed: 1000,
    photos_with_embedding: 900,
    photos_with_faces: 500,
    photos_without_embedding: 100,
    photos_without_faces: 500,
    photos_with_gps: 600,
    embeddings: 900,
    faces: 400,
    faces_assigned: 300,
    faces_unassigned: 100,
    subjects: 5,
    subjects_person: 4,
    subjects_pet: 1,
    subjects_other: 0,
    markers: 400,
    markers_assigned: 300,
    markers_unassigned: 100,
    albums: 3,
    labels: 9,
    ...overrides,
  }
}

function renderMeters(overrides: Partial<LibraryStats> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <CoverageMeters stats={stats(overrides)} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('CoverageMeters', () => {
  it('reports each share as a percentage of its own whole', () => {
    renderMeters()

    expect(screen.getByTestId('coverage-gps')).toHaveTextContent('60%')
    expect(screen.getByTestId('coverage-content')).toHaveTextContent('90%')
    // Faces are counted against the detected faces, not against the photos.
    expect(screen.getByTestId('coverage-faces')).toHaveTextContent('75%')
  })

  it('states both numbers behind the share, so the bar is never the only answer', () => {
    renderMeters()

    expect(
      screen.getByRole('meter', { name: 'Photos with a known place: 600 of 1,000 (60%)' }),
    ).toHaveAttribute('aria-valuenow', '60')
  })

  it('divides the faces meter by the two halves the faces card adds up', () => {
    // The card shows 300 named and 100 nameless; the meter must divide by their
    // sum, not by a marker count, or the two disagree on the same screen.
    renderMeters({ faces: 4671 + 16586, faces_assigned: 4671, faces_unassigned: 16586 })

    expect(screen.getByTestId('coverage-faces')).toHaveTextContent('22%')
    expect(
      screen.getByRole('meter', { name: 'Faces with a name: 4,671 of 21,257 (22%)' }),
    ).toHaveAttribute('aria-valuenow', '22')
  })

  it('reads an untouched whole as 0 %, not as a division by zero', () => {
    renderMeters({ faces: 0, faces_assigned: 0, faces_unassigned: 0 })

    expect(screen.getByTestId('coverage-faces')).toHaveTextContent('0%')
    expect(screen.getByRole('meter', { name: /Faces with a name/ })).toHaveAttribute(
      'aria-valuenow',
      '0',
    )
  })

  it('groups the numbers in the active language', async () => {
    await i18n.changeLanguage('cs')
    renderMeters()

    const meter = screen.getByRole('meter', { name: /známým místem/ })
    expect(meter.getAttribute('aria-label')?.replace(/\s/gu, ' ')).toContain('600 z 1 000')
  })
})
