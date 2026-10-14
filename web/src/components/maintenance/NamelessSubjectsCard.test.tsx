import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { ApiError } from '../../services/auth'
import type { NamelessReport, NamelessUndoFile } from '../../services/maintenance'
import type { AdminProject } from '../../services/projects'

import { NamelessSubjectsCard } from './NamelessSubjectsCard'

vi.mock('../../services/maintenance', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../services/maintenance')>()),
  fetchNamelessSubjects: vi.fn(),
  detachNamelessSubjects: vi.fn(),
  restoreNamelessSubjects: vi.fn(),
}))

const {
  fetchNamelessSubjects,
  detachNamelessSubjects,
  restoreNamelessSubjects,
  NamelessRestoreError,
} = await import('../../services/maintenance')
const reportMock = vi.mocked(fetchNamelessSubjects)
const detachMock = vi.mocked(detachNamelessSubjects)
const restoreMock = vi.mocked(restoreNamelessSubjects)

vi.mock('../../services/projects', () => ({
  fetchAdminProjects: vi.fn(),
}))

const { fetchAdminProjects } = await import('../../services/projects')
const projectsMock = vi.mocked(fetchAdminProjects)

/** One project of the instance as `GET /projects` lists it. */
function adminProject(slug: string, name: string, active = true): AdminProject {
  return {
    uid: `pr-${slug}`,
    slug,
    name,
    description: '',
    active,
    storage_prefix: '',
    max_file_bytes: null,
    max_total_bytes: null,
    members_count: 1,
    photos_count: 10,
    originals_bytes: 1024,
    created_at: '2026-10-01T10:00:00Z',
    updated_at: '2026-10-01T10:00:00Z',
  }
}

beforeEach(() => {
  // Most tests run on a single-project instance, where the one project is
  // picked for the maintainer; the picker tests below list several.
  projectsMock.mockResolvedValue([adminProject('v', 'Veselice')])
})

/** The production shape in miniature: one catch-all owning most of the library. */
function foundReport(): NamelessReport {
  return {
    subjects: [
      {
        uid: 'sunuikf1e9jdpjog5qgomsvgrb',
        slug: 'subject',
        name: '',
        type: 'person',
        created_at: '2026-08-02T10:00:00Z',
        marker_count: 16531,
        face_count: 111155,
      },
    ],
    marker_total: 16531,
    face_total: 111155,
  }
}

/** An empty report — the library has no nameless subject. */
function cleanReport(): NamelessReport {
  return { subjects: [], marker_total: 0, face_total: 0 }
}

/** The undo file a successful detach hands over. */
function undoFile(): NamelessUndoFile {
  return {
    filename: 'kukatko-nameless-undo-20260803T101500Z.json',
    subjects: 1,
    markers: 16531,
    faces: 111155,
  }
}

function renderCard() {
  return render(
    <I18nextProvider i18n={i18n}>
      <NamelessSubjectsCard />
    </I18nextProvider>,
  )
}

/**
 * Runs the read-only report and waits for its table. The row is identified by
 * what it says in words — the uid it used to lead with now waits behind the
 * row's technical-details disclosure.
 */
async function check(user: ReturnType<typeof userEvent.setup>) {
  await clickCheck(user)
  await screen.findByText('Osoba bez jména')
}

/** Clicks the check once the project list has loaded and enabled it. */
async function clickCheck(user: ReturnType<typeof userEvent.setup>) {
  await waitFor(() => {
    expect(screen.getByRole('button', { name: /zkontrolovat/i })).toBeEnabled()
  })
  await user.click(screen.getByRole('button', { name: /zkontrolovat/i }))
}

describe('NamelessSubjectsCard', () => {
  it('reports the nameless subject with what points at it', async () => {
    reportMock.mockResolvedValue(foundReport())
    const user = userEvent.setup()
    renderCard()

    expect(await screen.findByText(/zatím neproběhla žádná kontrola/i)).toBeInTheDocument()
    await check(user)

    expect(screen.getByText(/16531 značek a 111155 obličejů/)).toBeInTheDocument()
    // The uid and the slug are identifiers, so they are one click away.
    expect(screen.queryByText('sunuikf1e9jdpjog5qgomsvgrb')).toBeNull()
    await user.click(screen.getByRole('button', { name: /technické podrobnosti/i }))
    expect(screen.getByText('sunuikf1e9jdpjog5qgomsvgrb')).toBeInTheDocument()
    // Reporting is read-only: nothing may be scheduled by looking.
    expect(detachMock).not.toHaveBeenCalled()
  })

  it('says the library is clean when nothing is found', async () => {
    reportMock.mockResolvedValue(cleanReport())
    const user = userEvent.setup()
    renderCard()

    await clickCheck(user)

    expect(await screen.findByText(/žádná osoba bez jména/i)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /odpojit a smazat/i })).not.toBeInTheDocument()
  })

  it('detaches only behind a confirmation and reports the downloaded undo file', async () => {
    reportMock.mockResolvedValueOnce(foundReport()).mockResolvedValue(cleanReport())
    detachMock.mockResolvedValue(undoFile())
    const user = userEvent.setup()
    renderCard()
    await check(user)

    await user.click(screen.getByRole('button', { name: /odpojit a smazat/i }))
    // The confirmation has to be explicit about both the loss and the undo file.
    expect(screen.getByText(/vrátit to jde jedině souborem níže/i)).toBeInTheDocument()
    expect(screen.getByText(/uschovejte ho/i)).toBeInTheDocument()
    expect(detachMock).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: /^ano, odpojit$/i }))

    await waitFor(() => {
      expect(detachMock).toHaveBeenCalledWith('v')
    })
    expect(
      await screen.findByText(/kukatko-nameless-undo-20260803T101500Z\.json/),
    ).toBeInTheDocument()
  })

  it('cancels the confirmation without detaching anything', async () => {
    reportMock.mockResolvedValue(foundReport())
    const user = userEvent.setup()
    renderCard()
    await check(user)

    await user.click(screen.getByRole('button', { name: /odpojit a smazat/i }))
    await user.click(screen.getByRole('button', { name: /^zrušit$/i }))

    expect(detachMock).not.toHaveBeenCalled()
    expect(screen.getByRole('button', { name: /odpojit a smazat/i })).toBeInTheDocument()
  })

  it('shows the refusal when the undo file could not be handed over', async () => {
    reportMock.mockResolvedValue(foundReport())
    detachMock.mockRejectedValue(new ApiError(500, 'reading the undo snapshot failed'))
    const user = userEvent.setup()
    renderCard()
    await check(user)

    await user.click(screen.getByRole('button', { name: /odpojit a smazat/i }))
    await user.click(screen.getByRole('button', { name: /^ano, odpojit$/i }))

    expect(await screen.findByText(/odpojení se nezdařilo/i)).toBeInTheDocument()
    // The refusal must not read as a success: no undo file was downloaded.
    expect(screen.queryByText(/uschovejte ho\./i)).not.toBeInTheDocument()
  })

  it('reports a repair that is not wired as such', async () => {
    reportMock.mockResolvedValue(foundReport())
    detachMock.mockRejectedValue(new ApiError(503, 'nameless-subject repair not available'))
    const user = userEvent.setup()
    renderCard()
    await check(user)

    await user.click(screen.getByRole('button', { name: /odpojit a smazat/i }))
    await user.click(screen.getByRole('button', { name: /^ano, odpojit$/i }))

    expect(await screen.findByText(/tahle oprava na tomhle serveru není/i)).toBeInTheDocument()
  })

  it('replays an uploaded undo file', async () => {
    reportMock.mockResolvedValue(cleanReport())
    restoreMock.mockResolvedValue({ queued: 1 })
    const user = userEvent.setup()
    const { container } = renderCard()

    const input = container.querySelector('input[type="file"]')
    await waitFor(() => {
      expect(input).toBeEnabled()
    })
    expect(input).not.toBeNull()
    const file = new File(['{"subjects":[]}'], 'undo.json', { type: 'application/json' })
    await user.upload(input as HTMLInputElement, file)

    await waitFor(() => {
      expect(restoreMock).toHaveBeenCalledWith('v', file)
    })
    expect(await screen.findByText(/naplánováno obnovení 1 osob/i)).toBeInTheDocument()
  })

  it('rejects a file that is not a usable undo file', async () => {
    reportMock.mockResolvedValue(cleanReport())
    restoreMock.mockRejectedValue(new ApiError(400, 'invalid undo file'))
    const user = userEvent.setup()
    const { container } = renderCard()

    const input = container.querySelector('input[type="file"]')
    await waitFor(() => {
      expect(input).toBeEnabled()
    })
    // Still a .json file: the picker filters on `accept`, so what makes this
    // unusable is its contents, which only the backend can judge.
    const file = new File(['nope'], 'undo.json', { type: 'application/json' })
    await user.upload(input as HTMLInputElement, file)

    expect(await screen.findByText(/není použitelný soubor pro vrácení zpět/i)).toBeInTheDocument()
  })

  it.each([
    ['undo_wrong_project', /patří k jinému projektu/i],
    ['undo_empty', /nezaznamenává žádnou odpojenou osobu/i],
    ['undo_invalid', /není použitelný soubor pro vrácení zpět/i],
  ] as const)('words the %s refusal of its own', async (code, sentence) => {
    reportMock.mockResolvedValue(cleanReport())
    restoreMock.mockRejectedValue(new NamelessRestoreError(400, 'refused', code))
    const user = userEvent.setup()
    const { container } = renderCard()

    const input = container.querySelector('input[type="file"]')
    await waitFor(() => {
      expect(input).toBeEnabled()
    })
    await user.upload(
      input as HTMLInputElement,
      new File(['{}'], 'undo.json', { type: 'application/json' }),
    )

    expect(await screen.findByText(sentence)).toBeInTheDocument()
  })

  it('asks for a project on a multi-project instance and checks only the one picked', async () => {
    projectsMock.mockResolvedValue([
      adminProject('v', 'Veselice'),
      adminProject('rodina', 'Rodina'),
      adminProject('stary', 'Starý archiv', false),
    ])
    reportMock.mockResolvedValue(foundReport())
    const user = userEvent.setup()
    const { container } = renderCard()

    const picker = await screen.findByRole('combobox', { name: /projekt/i })
    // Every project of the instance is offered, a deactivated one marked so.
    expect(within(picker).getByRole('option', { name: 'Rodina' })).toBeInTheDocument()
    expect(
      within(picker).getByRole('option', { name: 'Starý archiv (vypnutý)' }),
    ).toBeInTheDocument()
    // Nothing is picked for the maintainer: no request can go anywhere yet.
    expect(screen.getByText(/nejdřív vyberte projekt/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /zkontrolovat/i })).toBeDisabled()
    expect(container.querySelector('input[type="file"]')).toBeDisabled()

    await user.selectOptions(picker, 'rodina')
    await check(user)

    expect(reportMock).toHaveBeenCalledWith('rodina')
    expect(reportMock).not.toHaveBeenCalledWith('v')
  })

  it('names the picked project in the detach confirmation and detaches there', async () => {
    projectsMock.mockResolvedValue([
      adminProject('v', 'Veselice'),
      adminProject('rodina', 'Rodina'),
    ])
    reportMock.mockResolvedValue(foundReport())
    detachMock.mockResolvedValue(undoFile())
    const user = userEvent.setup()
    renderCard()

    await user.selectOptions(await screen.findByRole('combobox', { name: /projekt/i }), 'rodina')
    await check(user)
    await user.click(screen.getByRole('button', { name: /odpojit a smazat/i }))

    expect(screen.getByText('Projekt: Rodina')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /^ano, odpojit$/i }))
    await waitFor(() => {
      expect(detachMock).toHaveBeenCalledWith('rodina')
    })
  })

  it('forgets the previous report when another project is picked', async () => {
    projectsMock.mockResolvedValue([
      adminProject('v', 'Veselice'),
      adminProject('rodina', 'Rodina'),
    ])
    reportMock.mockResolvedValue(foundReport())
    const user = userEvent.setup()
    renderCard()

    const picker = await screen.findByRole('combobox', { name: /projekt/i })
    await user.selectOptions(picker, 'v')
    await check(user)
    await user.selectOptions(picker, 'rodina')

    // The table belonged to Veselice; Rodina has not been checked yet.
    expect(screen.queryByText('Osoba bez jména')).not.toBeInTheDocument()
    expect(screen.getByText(/zatím neproběhla žádná kontrola/i)).toBeInTheDocument()
  })

  it('says so when the project list cannot be loaded', async () => {
    projectsMock.mockRejectedValue(new ApiError(500, 'boom'))
    renderCard()

    expect(await screen.findByText(/seznam projektů se nepodařilo načíst/i)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /zkontrolovat/i })).toBeDisabled()
  })
})
