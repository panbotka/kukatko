import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import i18n from '../../i18n'
import type { ProjectMembership } from '../../services/auth'
import { type SubjectCount } from '../../services/people'

import { MySubjectCard } from './MySubjectCard'

vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchSubjects: vi.fn() }
})
vi.mock('../../services/auth', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/auth')>()
  return { ...actual, setMySubject: vi.fn() }
})

const { fetchSubjects } = await import('../../services/people')
const { setMySubject } = await import('../../services/auth')
const fetchSubjectsMock = vi.mocked(fetchSubjects)
const setMySubjectMock = vi.mocked(setMySubject)

/** One person of a library, as the subject list returns them. */
function subject(uid: string, name: string): SubjectCount {
  return { uid, slug: name.toLowerCase(), name, photo_count: 12 } as unknown as SubjectCount
}

/** Each project's people: v knows Jarmila and Petr, rodina knows Babička. */
const PEOPLE: Record<string, SubjectCount[]> = {
  v: [subject('sub1', 'Jarmila'), subject('sub2', 'Petr')],
  rodina: [subject('subR', 'Babička')],
}

/** An approved membership of an active project, overridable. */
function membership(
  slug: string,
  name: string,
  extra: Partial<ProjectMembership> = {},
): ProjectMembership {
  return {
    uid: `pj-${slug}`,
    slug,
    name,
    active: true,
    role: 'viewer',
    approved: true,
    subject_uid: null,
    ...extra,
  }
}

const refresh = vi.fn()

/** A signed-in session belonging to `projects`. */
function auth(projects: ProjectMembership[]): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'User One', subject_uid: null },
    role: 'viewer',
    projects,
    refresh,
  } as unknown as AuthContextValue
}

function renderCard(projects: ProjectMembership[]) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(projects)}>
        <MemoryRouter>
          <MySubjectCard />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The row of the project `slug`. */
function row(slug: string): HTMLElement {
  return screen.getByTestId(`account-subject-row-${slug}`)
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  fetchSubjectsMock.mockImplementation((_signal, project) =>
    Promise.resolve(PEOPLE[project ?? ''] ?? []),
  )
  setMySubjectMock.mockResolvedValue({} as never)
})

describe('MySubjectCard', () => {
  it('warns that linking publishes the face, before anything is picked', async () => {
    renderCard([membership('v', 'Veselice')])

    // The consequence is stated where the decision is made, not after it.
    expect(
      await screen.findByText(/cover photo is shown next to every comment/),
    ).toBeInTheDocument()
  })

  it('gives every project its own named row, each over its own people', async () => {
    renderCard([
      membership('v', 'Veselice', { subject_uid: 'sub1' }),
      membership('rodina', 'Rodina'),
    ])

    expect(within(row('v')).getByRole('heading', { name: 'Veselice' })).toBeInTheDocument()
    expect(within(row('rodina')).getByRole('heading', { name: 'Rodina' })).toBeInTheDocument()
    // Each row read its own project's people, never the current project's.
    await waitFor(() => {
      expect(fetchSubjectsMock).toHaveBeenCalledWith(expect.anything(), 'v')
    })
    expect(fetchSubjectsMock).toHaveBeenCalledWith(expect.anything(), 'rodina')
    expect(await within(row('v')).findByRole('link', { name: 'Jarmila' })).toHaveAttribute(
      'href',
      '/p/v/people/sub1',
    )
    expect(within(row('rodina')).getByLabelText('Find a person in Rodina')).toBeInTheDocument()
  })

  it('links the person in the project of the row, and only there', async () => {
    const user = userEvent.setup()
    renderCard([membership('v', 'Veselice'), membership('rodina', 'Rodina')])

    const field = within(row('rodina')).getByRole('combobox')
    // Enabled once that project's people have arrived.
    await waitFor(() => {
      expect(field).toBeEnabled()
    })
    await user.type(field, 'Bab')
    await user.click(await screen.findByRole('option', { name: /Babička/ }))

    await waitFor(() => {
      expect(setMySubjectMock).toHaveBeenCalledWith('subR', { project: 'rodina' })
    })
    expect(setMySubjectMock).toHaveBeenCalledTimes(1)
    // The whole app reads the link off the session, so it is re-read rather
    // than patched locally.
    expect(refresh).toHaveBeenCalled()
  })

  it('names the linked person, and offers to take it back in that project', async () => {
    const user = userEvent.setup()
    renderCard([membership('v', 'Veselice', { subject_uid: 'sub1' })])

    expect(await screen.findByRole('link', { name: 'Jarmila' })).toBeInTheDocument()
    // No warning once every link exists: it is a decision already made.
    expect(screen.queryByText(/cover photo is shown next to every comment/)).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Unlink in Veselice' }))
    await waitFor(() => {
      expect(setMySubjectMock).toHaveBeenCalledWith(null, { project: 'v' })
    })
  })

  it('offers no row for a membership still waiting, or a deactivated project', () => {
    renderCard([
      membership('v', 'Veselice'),
      membership('ceka', 'Čeká', { approved: false }),
      membership('stary', 'Starý', { active: false }),
    ])

    expect(screen.getByTestId('account-subject-row-v')).toBeInTheDocument()
    expect(screen.queryByTestId('account-subject-row-ceka')).not.toBeInTheDocument()
    expect(screen.queryByTestId('account-subject-row-stary')).not.toBeInTheDocument()
  })

  it('says there is nobody to pick without a project', () => {
    renderCard([])

    expect(screen.getByText(/not an approved member of any project/)).toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
  })

  it('says so when the linked person is no longer in the library', async () => {
    // The database clears a link when its person is deleted, so this is the
    // narrow window between that happening and the session being re-read.
    renderCard([membership('v', 'Veselice', { subject_uid: 'sub_gone' })])

    expect(await screen.findByText(/no longer in the library/)).toBeInTheDocument()
  })

  it('reports a failed save instead of pretending it landed', async () => {
    const user = userEvent.setup()
    setMySubjectMock.mockRejectedValue(new Error('nope'))
    renderCard([membership('v', 'Veselice')])

    const field = await screen.findByRole('combobox')
    await user.type(field, 'Petr')
    await user.click(await screen.findByRole('option', { name: /Petr/ }))

    // The publish warning is an alert too, so match on the failure's own words.
    expect(await within(row('v')).findByText(/could not be saved/)).toBeInTheDocument()
  })
})
