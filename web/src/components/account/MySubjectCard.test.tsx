import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import i18n from '../../i18n'
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

/** One person of the library, as the subject list returns them. */
function subject(uid: string, name: string): SubjectCount {
  return { uid, slug: name.toLowerCase(), name, photo_count: 12 } as unknown as SubjectCount
}

const refresh = vi.fn()

/** A signed-in session, linked to a person or not. */
function auth(subjectUid: string | null): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'User One', subject_uid: subjectUid },
    role: 'viewer',
    refresh,
  } as unknown as AuthContextValue
}

function renderCard(subjectUid: string | null = null) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(subjectUid)}>
        <MemoryRouter>
          <MySubjectCard />
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  fetchSubjectsMock.mockResolvedValue([subject('sub1', 'Jarmila'), subject('sub2', 'Petr')])
  setMySubjectMock.mockResolvedValue({} as never)
})

describe('MySubjectCard', () => {
  it('warns that linking publishes the face, before anything is picked', async () => {
    renderCard(null)

    // The consequence is stated where the decision is made, not after it.
    expect(
      await screen.findByText(/cover photo is shown next to every comment/),
    ).toBeInTheDocument()
  })

  it('links the account to the person the user picks', async () => {
    const user = userEvent.setup()
    renderCard(null)

    const field = await screen.findByRole('combobox')
    await user.type(field, 'Jar')
    await user.click(await screen.findByRole('option', { name: /Jarmila/ }))

    await waitFor(() => {
      expect(setMySubjectMock).toHaveBeenCalledWith('sub1')
    })
    // The whole app reads the link off the session, so it is re-read rather
    // than patched locally.
    expect(refresh).toHaveBeenCalled()
  })

  it('names the linked person, and offers to take it back', async () => {
    const user = userEvent.setup()
    renderCard('sub1')

    expect(await screen.findByRole('link', { name: 'Jarmila' })).toHaveAttribute(
      'href',
      '/people/sub1',
    )
    // No warning once the link exists: it is a decision already made.
    expect(screen.queryByText(/cover photo is shown next to every comment/)).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Unlink' }))
    await waitFor(() => {
      expect(setMySubjectMock).toHaveBeenCalledWith(null)
    })
  })

  it('says so when the linked person is no longer in the library', async () => {
    // The database clears a link when its person is deleted, so this is the
    // narrow window between that happening and the session being re-read.
    renderCard('sub_gone')

    expect(await screen.findByText(/no longer in the library/)).toBeInTheDocument()
  })

  it('reports a failed save instead of pretending it landed', async () => {
    const user = userEvent.setup()
    setMySubjectMock.mockRejectedValue(new Error('nope'))
    renderCard(null)

    const field = await screen.findByRole('combobox')
    await user.type(field, 'Petr')
    await user.click(await screen.findByRole('option', { name: /Petr/ }))

    // The publish warning is an alert too, so match on the failure's own words.
    expect(await screen.findByText(/could not be saved/)).toBeInTheDocument()
  })
})
