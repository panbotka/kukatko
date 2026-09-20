import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type Photo } from '../../services/photos'

import { TaskLedger } from './TaskLedger'

vi.mock('react-virtuoso', async () => (await import('../../test/virtuoso')).virtuosoMock())

/** A catalogued photograph with the capture-time fields under test. */
function photo(uid: string, taken: Partial<Photo>): Photo {
  return {
    uid,
    file_hash: uid,
    file_name: `${uid}.jpg`,
    file_size: 1,
    file_mime: 'image/jpeg',
    file_width: 100,
    file_height: 100,
    taken_at_source: 'exif',
    thumb_url: `/api/v1/photos/${uid}/thumb/tile_500`,
    download_url: `/api/v1/photos/${uid}/download?original=true`,
    title: '',
    description: '',
    camera_make: '',
    camera_model: '',
    lens_model: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    last_comment: null,
    ...taken,
  }
}

function renderLedger(photos: Photo[]) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <TaskLedger photos={photos} loadingMore={false} moreError={false} onRetry={vi.fn()} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** The row a file name identifies. */
function row(fileName: string): HTMLElement {
  const el = screen.getByRole('link', { name: fileName }).closest('.kk-ledger-row')
  if (el === null) {
    throw new Error(`no row for ${fileName}`)
  }
  return el as HTMLElement
}

describe('TaskLedger dates', () => {
  beforeEach(async () => {
    // The locale is read at render, so it is set before it.
    await i18n.changeLanguage('cs')
  })

  it('renders each date at the precision the catalogue claims, in Czech', () => {
    renderLedger([
      photo('day', { taken_at: '1974-06-14T10:00:00Z', taken_at_precision: 'day' }),
      photo('month', { taken_at: '1974-06-01T00:00:00Z', taken_at_precision: 'month' }),
      photo('year', { taken_at: '1974-01-01T00:00:00Z', taken_at_precision: 'year' }),
      photo('decade', { taken_at: '1970-01-01T00:00:00Z', taken_at_precision: 'decade' }),
      photo('none', { taken_at: undefined }),
    ])

    expect(row('day.jpg')).toHaveTextContent('14. 6. 1974')
    expect(row('month.jpg')).toHaveTextContent('červen 1974')
    expect(row('year.jpg')).toHaveTextContent('1974')
    expect(row('year.jpg')).not.toHaveTextContent('1. 1. 1974')
    // The one label a decade has everywhere in the app (see lib/takenDate).
    expect(row('decade.jpg')).toHaveTextContent('1970–1979')
    expect(row('none.jpg')).toHaveTextContent('bez data')
  })

  it('badges the source and the estimate, and names a source it does not know verbatim', () => {
    renderLedger([
      photo('a', {
        taken_at: '1974-06-14T10:00:00Z',
        taken_at_source: 'manual',
        taken_at_estimated: true,
      }),
      photo('b', { taken_at: '1974-06-14T10:00:00Z', taken_at_source: 'estimate' }),
    ])
    expect(row('a.jpg')).toHaveTextContent('Ručně')
    expect(row('a.jpg')).toHaveTextContent('odhad')
    expect(row('b.jpg')).toHaveTextContent('estimate')
    expect(row('b.jpg')).not.toHaveTextContent('odhad')
  })

  it('links every row into the viewer with the query it was handed', async () => {
    await i18n.changeLanguage('en')
    render(
      <I18nextProvider i18n={i18n}>
        <MemoryRouter>
          <TaskLedger
            photos={[photo('a', {})]}
            loadingMore={false}
            moreError={false}
            onRetry={vi.fn()}
            detailQuery="task=tk1&view=list"
          />
        </MemoryRouter>
      </I18nextProvider>,
    )
    expect(screen.getByRole('link', { name: 'a.jpg' })).toHaveAttribute(
      'href',
      '/photos/a?task=tk1&view=list',
    )
    expect(screen.getByText('no comment')).toBeInTheDocument()
  })
})
