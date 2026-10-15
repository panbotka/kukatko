import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { BrowserRouter, useLocation, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AppRoutes } from './App'
import { AuthContext, type AuthContextValue } from './auth/AuthContext'
import i18n from './i18n'
import { ProjectProvider } from './project/ProjectProvider'
import type { ProjectMembership } from './services/auth'
import { listenForHistoryLeaves } from './hooks/useLeaveGuard'

// Before any router mounts, as main.tsx does.
listenForHistoryLeaves()

vi.mock('./services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/organize')>()
  return { ...actual, fetchAlbums: vi.fn(() => Promise.resolve([])) }
})

// The shell's first-run welcome asks for the greeting and who is named; neither
// matters here, so the greeting never arrives and nobody is named.
vi.mock('./services/settings', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/settings')>()
  return { ...actual, fetchWelcomeMarkdown: vi.fn(() => new Promise(() => undefined)) }
})
vi.mock('./services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/people')>()
  return { ...actual, fetchSubjects: vi.fn(() => Promise.resolve([])) }
})

// The request is a stand-in, held open until the test ends it.
vi.mock('./services/upload', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/upload')>()
  return { ...actual, uploadFile: vi.fn() }
})

const { uploadFile } = await import('./services/upload')
const uploadMock = vi.mocked(uploadFile)

function membership(slug: string, name: string): ProjectMembership {
  return { uid: `p_${slug}`, slug, name, active: true, role: 'viewer', approved: true }
}

/** A viewer of project `a`. */
const auth = {
  status: 'authenticated',
  user: { uid: 'u1', username: 'u', display_name: 'U', role: 'viewer' },
  role: 'viewer',
  instanceRole: 'user',
  projects: [membership('a', 'Alfa')],
  lastProjectSlug: 'a',
  downloadToken: null,
  canWrite: false,
  isAdmin: false,
  isMaintainer: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
} as unknown as AuthContextValue

/** Where the router is, and a way in to the upload page made in code. */
function Controls() {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <span data-testid="pathname">{pathname}</span>
      <button
        type="button"
        onClick={() => {
          void navigate('/p/a/upload')
        }}
      >
        __to-upload
      </button>
    </>
  )
}

/** One upload request: its abort signal and the handle that finishes it. */
interface UploadCall {
  signal?: AbortSignal
  finish: () => void
}

let uploads: UploadCall[] = []

beforeEach(async () => {
  await i18n.changeLanguage('en')
  uploads = []
  uploadMock.mockReset().mockImplementation(
    (file, options) =>
      new Promise((resolve, reject) => {
        uploads.push({
          signal: options?.signal,
          finish: () => {
            resolve({
              filename: file.name,
              status: 201,
              outcome: 'created',
              photo_uid: `ph_${file.name}`,
            })
          },
        })
        options?.signal?.addEventListener('abort', () => {
          reject(new DOMException('aborted', 'AbortError'))
        })
      }),
  )
})

/**
 * Albums → the upload page through the app (a real browser router, so the
 * history entries are the router's), then five files picked: three upload at
 * once and two wait.
 */
async function uploadingFive(user: ReturnType<typeof userEvent.setup>): Promise<void> {
  window.history.replaceState(null, '', '/p/a/albums')
  render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <BrowserRouter>
          <ProjectProvider>
            <AppRoutes />
            <Controls />
          </ProjectProvider>
        </BrowserRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
  await user.click(screen.getByRole('button', { name: '__to-upload' }))
  const files = ['1.jpg', '2.jpg', '3.jpg', '4.jpg', '5.jpg'].map(
    (name) => new File(['data'], name, { type: 'image/jpeg' }),
  )
  await user.upload(
    await screen
      .findAllByLabelText('Choose photos or videos to upload')
      .then((inputs) => inputs[0]),
    files,
  )
  expect(uploadMock).toHaveBeenCalledTimes(3)
}

/** The browser's Back button: a history traversal, popstate on a later task. */
async function browserBack(): Promise<void> {
  await act(async () => {
    window.history.back()
    await new Promise((resolve) => setTimeout(resolve, 20))
  })
}

/**
 * The browser's Back used to reach the router unopposed: the page unmounted,
 * the three requests were aborted, the two waiting files were never sent, and
 * nothing said so (review III, W1).
 */
describe('browser Back during an upload', () => {
  it('asks how many files would be dropped and keeps the batch running meanwhile', async () => {
    const user = userEvent.setup()
    await uploadingFive(user)

    await browserBack()

    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByText(/5 files are still uploading/)).toBeInTheDocument()
    expect(screen.getByTestId('pathname')).toHaveTextContent('/p/a/upload')
    await waitFor(() => {
      expect(window.location.pathname).toBe('/p/a/upload')
    })
    expect(uploads.some((call) => call.signal?.aborted === true)).toBe(false)
  })

  it('stays and finishes the whole batch on "Stay here"', async () => {
    const user = userEvent.setup()
    await uploadingFive(user)
    await browserBack()

    await user.click(screen.getByRole('button', { name: 'Stay here' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    // The first three land; the two waiting files are sent too.
    await act(async () => {
      uploads.slice(0, 3).forEach((call) => {
        call.finish()
      })
      await Promise.resolve()
    })
    await waitFor(() => {
      expect(uploadMock).toHaveBeenCalledTimes(5)
    })
    expect(uploads.some((call) => call.signal?.aborted === true)).toBe(false)
  })

  it('goes Back and stops the batch only once the reader agrees', async () => {
    const user = userEvent.setup()
    await uploadingFive(user)
    await browserBack()
    await waitFor(() => {
      expect(window.location.pathname).toBe('/p/a/upload')
    })

    await user.click(screen.getByRole('button', { name: 'Leave and stop uploading' }))

    await waitFor(() => {
      expect(screen.getByTestId('pathname')).toHaveTextContent('/p/a/albums')
    })
    expect(window.location.pathname).toBe('/p/a/albums')
    expect(uploads.every((call) => call.signal?.aborted === true)).toBe(true)
    expect(uploadMock).toHaveBeenCalledTimes(3)
  })
})
