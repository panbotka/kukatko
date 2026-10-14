import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ComponentType, type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, useLocation, useNavigate } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AppRoutes } from './App'
import { AuthContext, type AuthContextValue } from './auth/AuthContext'
import { type TileGridLayout } from './components/TileGrid'
import i18n from './i18n'
import { ProjectProvider } from './project/ProjectProvider'
import { getCurrentProjectSlug } from './services/api'
import type { ProjectMembership } from './services/auth'
import { type AlbumSummary } from './services/organize'

// jsdom has no layout, so the real virtualized grid mounts no tile; this
// stand-in renders every album through the real `List`.
interface MockGridProps {
  data: AlbumSummary[]
  context: TileGridLayout
  components: { List: ComponentType<{ context: TileGridLayout; children: ReactNode }> }
  itemContent: (index: number, item: AlbumSummary) => ReactNode
  computeItemKey: (index: number, item: AlbumSummary) => string
}
vi.mock('react-virtuoso', () => ({
  VirtuosoGrid: ({ components, context, data, itemContent, computeItemKey }: MockGridProps) => (
    <components.List context={context}>
      {data.map((item, index) => (
        <div key={computeItemKey(index, item)}>{itemContent(index, item)}</div>
      ))}
    </components.List>
  ),
}))

vi.mock('./services/organize', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/organize')>()
  return { ...actual, fetchAlbums: vi.fn() }
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

// The request is a stand-in; the URL rule (`projectUploadUrl`) is the real one.
vi.mock('./services/upload', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./services/upload')>()
  return { ...actual, uploadFile: vi.fn() }
})

const { fetchAlbums } = await import('./services/organize')
const fetchAlbumsMock = vi.mocked(fetchAlbums)
const { uploadFile } = await import('./services/upload')
const uploadMock = vi.mocked(uploadFile)

function membership(slug: string, name: string): ProjectMembership {
  return { uid: `p_${slug}`, slug, name, active: true, role: 'viewer', approved: true }
}

/** A viewer in two projects, `a` and `b`. */
const auth = {
  status: 'authenticated',
  user: { uid: 'u1', username: 'u', display_name: 'U', role: 'viewer' },
  role: 'viewer',
  instanceRole: 'user',
  projects: [membership('a', 'Alfa'), membership('b', 'Bravo')],
  lastProjectSlug: 'a',
  downloadToken: null,
  canWrite: false,
  isAdmin: false,
  isMaintainer: false,
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
} as unknown as AuthContextValue

function album(uid: string, title: string): AlbumSummary {
  return {
    uid,
    slug: title.toLowerCase(),
    title,
    description: '',
    type: 'album',
    private: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 3,
  }
}

/** Each project's albums, as `GET /api/v1/p/<slug>/albums` would answer them. */
const ALBUMS: Record<string, AlbumSummary[]> = {
  a: [album('al_a1', 'Alfa summer'), album('al_a2', 'Alfa private')],
  b: [album('al_b1', 'Bravo wedding')],
}

/**
 * One fetch of the album list as the page made it: the project its URL named
 * (read the way `apiPath` reads it, when the call is made), whether it was
 * aborted, and the handle that answers it.
 */
interface AlbumsCall {
  project: string | null
  signal?: AbortSignal
  answer: () => void
}

let calls: AlbumsCall[] = []

/** Navigation controls standing in for the switcher and the browser's Back. */
function Controls() {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  return (
    <>
      <span data-testid="pathname">{pathname}</span>
      <button
        type="button"
        onClick={() => {
          void navigate('/p/b/albums')
        }}
      >
        __to-b
      </button>
      <button
        type="button"
        onClick={() => {
          void navigate('/p/b/upload')
        }}
      >
        __to-b-upload
      </button>
      <button
        type="button"
        onClick={() => {
          void navigate(-1)
        }}
      >
        __back
      </button>
    </>
  )
}

function renderAt(path: string) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth}>
        <MemoryRouter initialEntries={[path]}>
          <ProjectProvider>
            <AppRoutes />
            <Controls />
          </ProjectProvider>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** Answers every call made so far that is still open. */
async function answerAll(): Promise<void> {
  await act(async () => {
    calls.forEach((call) => {
      call.answer()
    })
    await Promise.resolve()
  })
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  calls = []
  // Held open until the test answers, so it decides the order the answers land
  // in — the late answer for the project the reader already left included.
  fetchAlbumsMock.mockReset().mockImplementation((signal?: AbortSignal) => {
    const project = getCurrentProjectSlug()
    return new Promise<AlbumSummary[]>((resolve) => {
      calls.push({
        project,
        signal,
        answer: () => {
          resolve(ALBUMS[project ?? ''] ?? [])
        },
      })
    })
  })
})

/**
 * A project switch keeps the reader on the same list page, at the same route
 * element — and before the project outlet was keyed by the project, React kept
 * that page mounted, its effects never re-ran, and project a's albums stayed on
 * screen under b's name.
 */
describe('switching project on a kept page', () => {
  it('re-fetches the albums in the new project and never shows the old ones', async () => {
    const user = userEvent.setup()
    renderAt('/p/a/albums')
    await answerAll()
    expect(await screen.findByRole('link', { name: 'Alfa summer' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '__to-b' }))
    expect(screen.getByTestId('pathname')).toHaveTextContent('/p/b/albums')
    // a's albums go the moment the address names b, before b has answered.
    expect(screen.queryByRole('link', { name: 'Alfa summer' })).not.toBeInTheDocument()

    await answerAll()
    expect(await screen.findByRole('link', { name: 'Bravo wedding' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Alfa summer' })).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Alfa private' })).not.toBeInTheDocument()
    expect(calls.map((call) => call.project)).toEqual(['a', 'b'])
  })

  it('ignores the old project’s answer when it lands after the switch', async () => {
    const user = userEvent.setup()
    renderAt('/p/a/albums')
    expect(calls.map((call) => call.project)).toEqual(['a'])

    // a's list is still on the way when the reader moves to b.
    await user.click(screen.getByRole('button', { name: '__to-b' }))
    await waitFor(() => {
      expect(calls.map((call) => call.project)).toEqual(['a', 'b'])
    })
    expect(calls[0].signal?.aborted).toBe(true)

    // b answers first, then a's late answer arrives.
    await act(async () => {
      calls[1].answer()
      await Promise.resolve()
    })
    expect(await screen.findByRole('link', { name: 'Bravo wedding' })).toBeInTheDocument()
    await act(async () => {
      calls[0].answer()
      await Promise.resolve()
    })

    expect(screen.getByRole('link', { name: 'Bravo wedding' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Alfa summer' })).not.toBeInTheDocument()
  })

  it('re-fetches again when Back returns to the first project', async () => {
    const user = userEvent.setup()
    renderAt('/p/a/albums')
    await answerAll()
    await user.click(screen.getByRole('button', { name: '__to-b' }))
    await answerAll()
    expect(await screen.findByRole('link', { name: 'Bravo wedding' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '__back' }))
    expect(screen.getByTestId('pathname')).toHaveTextContent('/p/a/albums')
    expect(screen.queryByRole('link', { name: 'Bravo wedding' })).not.toBeInTheDocument()
    await answerAll()

    expect(await screen.findByRole('link', { name: 'Alfa summer' })).toBeInTheDocument()
    expect(calls.map((call) => call.project)).toEqual(['a', 'b', 'a'])
  })
})

/**
 * The upload page is kept on a switch too, and its queue is the one piece of
 * page state that writes. The choice (docs/FRONTEND.md): a switch cancels the
 * batch — the files never follow the reader into the other project.
 */
describe('switching project during an upload', () => {
  it('cancels the batch and sends nothing to the new project', async () => {
    const signals: (AbortSignal | undefined)[] = []
    uploadMock.mockReset().mockImplementation(
      (_file, options) =>
        new Promise((_resolve, reject) => {
          signals.push(options?.signal)
          options?.signal?.addEventListener('abort', () => {
            reject(new DOMException('aborted', 'AbortError'))
          })
        }),
    )
    const user = userEvent.setup()
    renderAt('/p/a/upload')

    const files = ['1.jpg', '2.jpg', '3.jpg', '4.jpg', '5.jpg'].map(
      (name) => new File(['data'], name, { type: 'image/jpeg' }),
    )
    await user.upload(screen.getAllByLabelText('Choose photos or videos to upload')[0], files)
    expect(uploadMock).toHaveBeenCalledTimes(3)

    // A switch made in code, the way Back/Forward makes one: no link for the
    // leave guard to hold.
    await user.click(screen.getByRole('button', { name: '__to-b-upload' }))
    expect(screen.getByTestId('pathname')).toHaveTextContent('/p/b/upload')

    expect(signals.every((signal) => signal?.aborted === true)).toBe(true)
    // The two files still waiting never start, here or anywhere else.
    expect(uploadMock).toHaveBeenCalledTimes(3)
    expect(uploadMock.mock.calls.map(([, options]) => options?.url)).toEqual(
      Array.from({ length: 3 }, () => '/api/v1/p/a/upload'),
    )
    expect(screen.getByRole('heading', { name: 'Add photos to your library' })).toBeInTheDocument()
  })
})
