import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { REKEY_POLL_RUNNING_MS } from '../../lib/rekeyProgress'

import i18n from '../../i18n'
import type { AdminProject } from '../../services/projects'
import { RekeyApiError, type RekeyRun, type RekeyState } from '../../services/rekey'

import { StorageMoveCard } from './StorageMoveCard'

vi.mock('../../services/rekey', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/rekey')>()
  return {
    ...actual,
    fetchRekeyRun: vi.fn(),
    startRekey: vi.fn(),
    pauseRekey: vi.fn(),
    resumeRekey: vi.fn(),
    retryRekey: vi.fn(),
  }
})

const rekey = await import('../../services/rekey')
const fetchMock = vi.mocked(rekey.fetchRekeyRun)
const startMock = vi.mocked(rekey.startRekey)
const pauseMock = vi.mocked(rekey.pauseRekey)
const resumeMock = vi.mocked(rekey.resumeRekey)
const retryMock = vi.mocked(rekey.retryRekey)

const MB = 1024 * 1024

/** The bootstrap project, still in the root of the store unless overridden. */
function project(overrides: Partial<AdminProject> = {}): AdminProject {
  return {
    uid: 'pjv',
    slug: 'v',
    name: 'Veselice',
    description: '',
    active: true,
    storage_prefix: '',
    max_file_bytes: null,
    max_total_bytes: null,
    members_count: 5,
    photos_count: 1000,
    originals_bytes: 1000 * MB,
    created_at: '2026-08-01T08:00:00Z',
    updated_at: '2026-08-01T08:00:00Z',
    ...overrides,
  }
}

/** A run of 1000 photos in `state`, 420 of them moved. */
function run(state: RekeyState, overrides: Partial<RekeyRun> = {}): RekeyRun {
  return {
    uid: 'rk1',
    project_uid: 'pjv',
    state,
    target_prefix: 'projects/pjv/',
    total_photos: 1000,
    total_bytes: 1000 * MB,
    done_photos: 420,
    done_bytes: 420 * MB,
    last_key: '2019/07/IMG_20190714_123456_0042_some_very_long_original_name.jpg',
    errors: [],
    error_count: 0,
    derived_failed: 0,
    started_at: '2026-10-13T08:00:00Z',
    updated_at: '2026-10-13T08:05:00Z',
    ...overrides,
  }
}

function renderCard(p: AdminProject = project()) {
  return render(
    <I18nextProvider i18n={i18n}>
      <StorageMoveCard project={p} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
  for (const mock of [fetchMock, startMock, pauseMock, resumeMock, retryMock]) {
    mock.mockReset()
  }
})

describe('StorageMoveCard', () => {
  it('offers the move for a project in the root, with the safety promise', async () => {
    fetchMock.mockResolvedValue(null)
    renderCard()

    expect(await screen.findByText(/Přesun je bezpečný: každý soubor/)).toBeInTheDocument()
    expect(screen.getByText('v kořeni úložiště')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Spustit přesun' })).toBeEnabled()
    expect(screen.queryByTestId('storage-move-state')).not.toBeInTheDocument()
  })

  it('starts nothing until the confirmation says so', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(null)
    startMock.mockResolvedValue(run('running', { done_photos: 0, done_bytes: 0, last_key: '' }))
    renderCard()

    await user.click(await screen.findByRole('button', { name: 'Spustit přesun' }))
    let dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText(/teprve potom se smaže původní/)).toBeInTheDocument()
    expect(within(dialog).getByText(/kdykoli pozastavit/)).toBeInTheDocument()
    expect(within(dialog).getByText(/projects\/pjv\//)).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Zrušit' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
    expect(startMock).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: 'Spustit přesun' }))
    dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Spustit přesun' }))

    await waitFor(() => {
      expect(startMock).toHaveBeenCalledWith('pjv')
    })
    expect(await screen.findByTestId('storage-move-state')).toHaveTextContent('Probíhá')
  })

  it('shows a running move: bar, counts, rate placeholder, last key and pause', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(run('running'))
    pauseMock.mockResolvedValue(run('paused'))
    renderCard()

    const bar = await screen.findByRole('progressbar', { name: 'Průběh přesunu' })
    expect(bar).toHaveAttribute('aria-valuenow', '42')
    expect(screen.getByTestId('storage-move-state')).toHaveTextContent('Probíhá')
    expect(screen.getByTestId('storage-move-counts')).toHaveTextContent(
      /420 z 1\s000 fotek · 420,0 MB z 1\s000,0 MB · 42 %/,
    )
    expect(screen.getByTestId('storage-move-rate')).toHaveTextContent(
      'měřím rychlost… · zbývající čas počítám…',
    )
    const key = screen.getByTitle(run('running').last_key)
    expect(key).toHaveClass('font-monospace')
    expect(key.textContent).toContain('…')
    expect(screen.getByText('projects/pjv/')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Pozastavit' }))
    expect(pauseMock).toHaveBeenCalledWith('pjv')
    expect(await screen.findByRole('button', { name: 'Pokračovat' })).toBeInTheDocument()
    expect(screen.getByTestId('storage-move-state')).toHaveTextContent('Pozastaveno')
  })

  it('shows the rate measured between two polls and the time it implies', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      fetchMock
        .mockResolvedValueOnce(run('running', { done_photos: 400 }))
        .mockResolvedValue(run('running', { done_photos: 430 }))
      renderCard()
      await screen.findByTestId('storage-move-rate')

      await act(async () => {
        await vi.advanceTimersByTimeAsync(REKEY_POLL_RUNNING_MS)
      })

      // 30 photos in 3 s = 600 a minute; 570 left = just under a minute.
      await waitFor(() => {
        expect(screen.getByTestId('storage-move-rate')).toHaveTextContent(
          /^[56]\d\d fotek\/min · zbývá (necelá minuta|asi 1 min)$/,
        )
      })
    } finally {
      vi.useRealTimers()
    }
  })

  it('resumes a paused move', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(run('paused'))
    resumeMock.mockResolvedValue(run('running'))
    renderCard()

    await user.click(await screen.findByRole('button', { name: 'Pokračovat' }))
    expect(resumeMock).toHaveBeenCalledWith('pjv')
    expect(await screen.findByRole('button', { name: 'Pozastavit' })).toBeInTheDocument()
  })

  it('shows a failed move with its errors, newest first, and retries it', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(
      run('failed', {
        error_count: 7,
        errors: [
          { key: '2019/01/old.jpg', message: 'copy timed out', at: '2026-10-13T08:01:00Z' },
          { key: '2019/02/new.jpg', message: 'verify failed', at: '2026-10-13T08:02:00Z' },
        ],
      }),
    )
    retryMock.mockResolvedValue(run('running'))
    renderCard()

    expect(await screen.findByText(/Přesun se zastavil chybou/)).toBeInTheDocument()
    expect(screen.getByTestId('storage-move-state')).toHaveTextContent('Selhalo')
    expect(screen.queryByText('verify failed')).not.toBeInTheDocument()

    const toggle = screen.getByRole('button', { name: '7 chyb' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    const list = document.getElementById(toggle.getAttribute('aria-controls') ?? '')
    if (list === null) {
      throw new Error('the error list is not open')
    }
    const items = within(list).getAllByRole('listitem')
    expect(items[0]).toHaveTextContent('verify failed')
    expect(items[1]).toHaveTextContent('copy timed out')

    await user.click(screen.getByRole('button', { name: 'Zkusit znovu' }))
    expect(retryMock).toHaveBeenCalledWith('pjv')
    expect(await screen.findByRole('button', { name: 'Pozastavit' })).toBeInTheDocument()
  })

  it('warns about a failed move whose derived objects stayed behind and retries it', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(run('failed', { done_photos: 1000, derived_failed: 3 }))
    retryMock.mockResolvedValue(run('running', { done_photos: 1000, derived_failed: 3 }))
    renderCard()

    const warning = await screen.findByTestId('storage-move-derived-failed')
    expect(warning).toHaveTextContent('U 3 fotek zůstaly náhledy nebo segmenty videa')
    expect(warning).toHaveTextContent('„Zkusit znovu“ je přesune.')

    await user.click(screen.getByRole('button', { name: 'Zkusit znovu' }))
    expect(retryMock).toHaveBeenCalledWith('pjv')
    expect(await screen.findByTestId('storage-move-derived-failed')).toHaveTextContent(
      'Přesun je zkouší znovu',
    )
  })

  it('shows no derived warning while nothing stayed behind', async () => {
    fetchMock.mockResolvedValue(run('running'))
    renderCard()

    expect(await screen.findByTestId('storage-move-counts')).toBeInTheDocument()
    expect(screen.queryByTestId('storage-move-derived-failed')).not.toBeInTheDocument()
  })

  it('sums up a done move with the new prefix and the finish time', async () => {
    fetchMock.mockResolvedValue(
      run('done', {
        done_photos: 1000,
        done_bytes: 1000 * MB,
        finished_at: '2026-10-13T09:30:00Z',
      }),
    )
    renderCard()

    const summary = await screen.findByText(/Přesun je hotový/)
    expect(summary).toHaveTextContent('projects/pjv/')
    expect(summary).toHaveTextContent('2026')
    expect(screen.getByTestId('storage-move-state')).toHaveTextContent('Hotovo')
    expect(screen.queryByRole('progressbar')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Spustit přesun' })).not.toBeInTheDocument()
  })

  it('just shows the prefix of a project founded with its own', async () => {
    fetchMock.mockResolvedValue(null)
    renderCard(project({ uid: 'pjr', storage_prefix: 'projects/pjr/' }))

    expect(await screen.findByText('projects/pjr/')).toHaveClass('font-monospace')
    expect(screen.queryByRole('button', { name: 'Spustit přesun' })).not.toBeInTheDocument()
  })

  it('shows the run somebody else started when the start is refused', async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValueOnce(null).mockResolvedValue(run('running'))
    startMock.mockRejectedValue(new RekeyApiError(409, 'already_running'))
    renderCard()

    await user.click(await screen.findByRole('button', { name: 'Spustit přesun' }))
    await user.click(
      within(await screen.findByRole('dialog')).getByRole('button', {
        name: 'Spustit přesun',
      }),
    )

    expect(await screen.findByText('Přesun už běží — ukazuji jeho průběh.')).toBeInTheDocument()
    expect(await screen.findByTestId('storage-move-state')).toHaveTextContent('Probíhá')
  })

  it('offers a retry when the status cannot be read', async () => {
    const user = userEvent.setup()
    fetchMock.mockRejectedValueOnce(new Error('offline')).mockResolvedValue(null)
    renderCard()

    expect(await screen.findByText('Stav přesunu se nepodařilo načíst.')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Zkusit znovu/ }))
    expect(await screen.findByRole('button', { name: 'Spustit přesun' })).toBeInTheDocument()
  })
})
