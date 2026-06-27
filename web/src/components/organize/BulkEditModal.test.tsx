import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type BulkResult } from '../../services/bulk'
import { type AlbumCount, type LabelCount } from '../../services/organize'

import { BulkEditModal } from './BulkEditModal'

vi.mock('../../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn() }
})
vi.mock('../../services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/organize')>()
  return { ...actual, fetchAlbums: vi.fn(), fetchLabels: vi.fn() }
})

const { bulkUpdatePhotos } = await import('../../services/bulk')
const { fetchAlbums, fetchLabels } = await import('../../services/organize')
const bulkMock = vi.mocked(bulkUpdatePhotos)
const albumsMock = vi.mocked(fetchAlbums)
const labelsMock = vi.mocked(fetchLabels)

function album(uid: string, title: string): AlbumCount {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    order_by: 'added',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function label(uid: string, name: string): LabelCount {
  return {
    uid,
    slug: name.toLowerCase(),
    name,
    priority: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 0,
  }
}

function result(
  counts: Partial<BulkResult['counts']>,
  results: BulkResult['results'] = [],
): BulkResult {
  return {
    results,
    counts: { total: 0, updated: 0, skipped: 0, errored: 0, ...counts },
  }
}

const onHide = vi.fn()
const onDone = vi.fn()

function renderModal(photoUids = ['ph1', 'ph2']) {
  return render(
    <I18nextProvider i18n={i18n}>
      <BulkEditModal show photoUids={photoUids} onHide={onHide} onDone={onDone} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  bulkMock.mockReset()
  albumsMock.mockReset()
  labelsMock.mockReset()
  onHide.mockReset()
  onDone.mockReset()
  albumsMock.mockResolvedValue([album('al1', 'Trips')])
  labelsMock.mockResolvedValue([label('lb1', 'Sunset')])
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('BulkEditModal', () => {
  it('applies the selected operations with the right payload and renders the result', async () => {
    bulkMock.mockResolvedValue(result({ total: 2, updated: 2 }))
    const user = userEvent.setup()
    renderModal()

    // Wait for the album/label options to load.
    const addAlbum = await screen.findByLabelText('Add to album')
    await user.selectOptions(addAlbum, 'al1')
    await user.selectOptions(screen.getByLabelText('Private'), 'true')
    await user.selectOptions(screen.getByLabelText('Description'), 'set')
    await user.type(await screen.findByLabelText('New description…'), 'Hello')

    await user.click(screen.getByRole('button', { name: 'Apply' }))

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledTimes(1)
    })
    expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], {
      add_to_albums: ['al1'],
      set_private: true,
      set_description: 'Hello',
    })

    // The per-photo result summary replaces the form.
    expect(await screen.findByText(/2 updated/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Done' }))
    expect(onDone).toHaveBeenCalledTimes(1)
  })

  it('blocks applying with no operations chosen', async () => {
    const user = userEvent.setup()
    renderModal()

    await screen.findByLabelText('Add to album')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    expect(await screen.findByText('Choose at least one change to apply.')).toBeInTheDocument()
    expect(bulkMock).not.toHaveBeenCalled()
  })

  it('lists per-photo failures in the result summary', async () => {
    bulkMock.mockResolvedValue(
      result({ total: 2, updated: 1, errored: 1 }, [
        { photo_uid: 'ph1', status: 'updated' },
        { photo_uid: 'ph2', status: 'error', error: 'not found' },
      ]),
    )
    const user = userEvent.setup()
    renderModal()

    await user.selectOptions(await screen.findByLabelText('Archive'), 'archive')
    await user.click(screen.getByRole('button', { name: 'Apply' }))

    expect(await screen.findByText('ph2')).toBeInTheDocument()
    expect(screen.getByText(/not found/)).toBeInTheDocument()
    expect(bulkMock).toHaveBeenCalledWith(['ph1', 'ph2'], { archive: true })
  })
})
