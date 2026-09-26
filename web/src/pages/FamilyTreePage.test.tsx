import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../auth/AuthContext'
import i18n from '../i18n'
import {
  type FamilyTree,
  type Relative,
  type TreeFamily,
  type TreeMember,
} from '../services/family'

import { FamilyTreePage } from './FamilyTreePage'

// The add dialog is stubbed: this file is about the page's own wiring — which
// card's `+` opens the dialog on whom — and the real one loads every subject in the
// library to pick from, which is `AddRelationModal`'s business and not this
// page's.
vi.mock('../components/people/AddRelationModal', () => ({
  AddRelationModal: ({ subjectUid, kind }: { subjectUid: string; kind: string }) => (
    <div data-testid="add-relation">{`${kind} of ${subjectUid}`}</div>
  ),
}))

vi.mock('../services/family', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/family')>()
  return { ...actual, fetchTree: vi.fn() }
})

const { fetchTree } = await import('../services/family')
const fetchTreeMock = vi.mocked(fetchTree)

/** One person, in the shape the network walk answers with. */
function member(
  uid: string,
  name: string,
  generation: number,
  birthYear: number | null = null,
): TreeMember {
  return {
    uid,
    slug: uid,
    name,
    type: 'person',
    birth_year: birthYear,
    death_year: null,
    photo_count: 0,
    generation,
  }
}

/** One family box with all of its children; both partners null is a sibling group. */
function family(uid: string, a: string | null, b: string | null, children: string[]): TreeFamily {
  return {
    uid,
    partner_a_uid: a,
    partner_b_uid: b,
    kind: 'marriage',
    from_year: null,
    to_year: null,
    note: '',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    child_uids: children,
  }
}

/**
 * The production shape the network exists for, from Tomáš: his parents Ludmila
 * and Aleš, Ludmila in a parentless sibling group with her sister Dagmar, and
 * Dagmar's daughter Petra — who, with her mother, is reachable only sideways.
 */
function network(): FamilyTree {
  return {
    root: ROOT,
    members: [
      member('s1', 'Tomáš Kozák', 0, 1980),
      member('s2', 'Ludmila Kozáková', -1, 1955),
      member('s3', 'Aleš Kozák', -1, 1953),
      member('s4', 'Dagmar Andrlíková', -1, 1958),
      member('s5', 'Petra Houdková', 0, 1984),
    ],
    families: [
      family('fm_couple', 's2', 's3', ['s1']),
      family('fm_sisters', null, null, ['s2', 's4']),
      family('fm_dagmar', 's4', null, ['s5']),
    ],
    truncated: false,
    total: 5,
  }
}

/** The root as the endpoint answers with it. */
const ROOT: Relative = {
  uid: 's1',
  slug: 's1',
  name: 'Tomáš Kozák',
  type: 'person',
  birth_year: 1980,
  death_year: null,
  photo_count: 0,
}

/** Prints the current address, so a test can assert on the URL state itself. */
function Address() {
  const location = useLocation()
  return <div data-testid="address">{`${location.pathname}${location.search}`}</div>
}

/** A signed-in reader, with or without the right to record a relation. */
function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canCurate: canWrite,
    canWrite,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

function renderPage(entry = '/people/s1/tree', canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <MemoryRouter initialEntries={[entry]}>
          <Address />
          <Routes>
            <Route path="/people/:uid/tree" element={<FamilyTreePage />} />
            <Route path="/people/:uid" element={<div>person page</div>} />
          </Routes>
        </MemoryRouter>
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

/** The current address as the router sees it. */
function address(): string {
  return screen.getByTestId('address').textContent
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchTreeMock.mockReset()
  fetchTreeMock.mockResolvedValue(network())
})

describe('FamilyTreePage', () => {
  it('walks the network from the person in the route and names them', async () => {
    renderPage()

    expect(
      await screen.findByRole('heading', { name: 'Family tree — Tomáš Kozák' }),
    ).toBeInTheDocument()
    expect(fetchTreeMock).toHaveBeenCalledTimes(1)
    expect(fetchTreeMock).toHaveBeenCalledWith('s1', expect.anything())
  })

  it('counts the people and the generations the network spans', async () => {
    renderPage()

    // Generations −1 and 0: two, whatever sign they carry.
    expect(await screen.findByText('5 people · 2 generations')).toBeInTheDocument()
  })

  it('draws everybody once in one drawing, the aunt and the cousin included', async () => {
    const { container } = renderPage()

    await screen.findByLabelText('Open Tomáš Kozák')
    expect(container.querySelectorAll('.kk-tree-stage')).toHaveLength(1)
    for (const name of ['Ludmila Kozáková', 'Aleš Kozák', 'Dagmar Andrlíková', 'Petra Houdková']) {
      expect(screen.getAllByLabelText(`Redraw the family around ${name}`)).toHaveLength(1)
    }
    // The couple's line, the two sisters' bar, and Dagmar's line to Petra.
    expect(container.querySelectorAll('.kk-tree-edges path')).toHaveLength(4)
  })

  it('has no direction switch and no generations control any more', async () => {
    renderPage()

    await screen.findByLabelText('Open Tomáš Kozák')
    expect(screen.queryByRole('group')).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Ancestors' })).not.toBeInTheDocument()
  })

  it('re-roots through the URL, so Back undoes it', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByLabelText('Redraw the family around Petra Houdková'))

    await waitFor(() => {
      expect(address()).toBe('/people/s5/tree')
    })
    await waitFor(() => {
      expect(fetchTreeMock).toHaveBeenLastCalledWith('s5', expect.anything())
    })
  })

  it('ignores the old direction and depth in a bookmarked address', async () => {
    renderPage('/people/s1/tree?direction=ancestors&generations=4&closed=fm_2')

    await screen.findByLabelText('Open Tomáš Kozák')
    expect(fetchTreeMock).toHaveBeenCalledWith('s1', expect.anything())
    expect(screen.getByLabelText('Redraw the family around Petra Houdková')).toBeInTheDocument()
  })

  it('leads from the root card to the person page, not back to itself', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByLabelText('Open Tomáš Kozák'))

    await waitFor(() => {
      expect(address()).toBe('/people/s1')
    })
  })

  it('says plainly when the server cut the network short', async () => {
    fetchTreeMock.mockResolvedValue({ ...network(), truncated: true, total: 812 })
    renderPage()

    expect(
      await screen.findByText(
        'This family is larger than one drawing holds — showing the nearest 5 of 812 people.',
      ),
    ).toBeInTheDocument()
  })

  it('shows no banner for a network drawn whole', async () => {
    renderPage()

    await screen.findByLabelText('Open Tomáš Kozák')
    expect(screen.queryByText(/showing the nearest/)).not.toBeInTheDocument()
  })

  it('puts a + on the card of everybody missing a parent, and it records one', async () => {
    const user = userEvent.setup()
    renderPage()

    await screen.findByLabelText('Open Tomáš Kozák')
    // Tomáš has both parents; Petra has only her mother; the sisters' parents
    // and Aleš's are not in the library at all.
    expect(
      screen.queryByRole('button', { name: 'Record a parent of Tomáš Kozák' }),
    ).not.toBeInTheDocument()
    for (const name of ['Ludmila Kozáková', 'Aleš Kozák', 'Dagmar Andrlíková']) {
      expect(screen.getByRole('button', { name: `Record a parent of ${name}` })).toBeInTheDocument()
    }

    await user.click(screen.getByRole('button', { name: 'Record a parent of Petra Houdková' }))

    expect(screen.getByTestId('add-relation')).toHaveTextContent('parent of s5')
  })

  it('opens the + from the keyboard too', async () => {
    const user = userEvent.setup()
    renderPage()

    const plus = await screen.findByRole('button', { name: 'Record a parent of Aleš Kozák' })
    plus.focus()
    await user.keyboard('{Enter}')

    expect(screen.getByTestId('add-relation')).toHaveTextContent('parent of s3')
  })

  it('offers no + to a reader who may not record anything', async () => {
    renderPage('/people/s1/tree', false)

    await screen.findByLabelText('Open Tomáš Kozák')
    expect(screen.queryByRole('button', { name: /Record a parent/ })).not.toBeInTheDocument()
  })

  it('says so to a reader when nobody has recorded a family yet', async () => {
    fetchTreeMock.mockResolvedValue({
      ...network(),
      members: [member('s1', 'Tomáš Kozák', 0)],
      families: [],
      total: 1,
    })
    renderPage('/people/s1/tree', false)

    expect(await screen.findByText('No family has been recorded here yet.')).toBeInTheDocument()
  })

  it('gives a curator the lone card with its + instead of the empty state', async () => {
    fetchTreeMock.mockResolvedValue({
      ...network(),
      members: [member('s1', 'Tomáš Kozák', 0)],
      families: [],
      total: 1,
    })
    renderPage()

    expect(
      await screen.findByRole('button', { name: 'Record a parent of Tomáš Kozák' }),
    ).toBeInTheDocument()
    expect(screen.queryByText('No family has been recorded here yet.')).not.toBeInTheDocument()
  })

  it('reports a walk that could not be loaded', async () => {
    fetchTreeMock.mockRejectedValue(new Error('nope'))
    renderPage()

    expect(await screen.findByText('The family tree could not be loaded.')).toBeInTheDocument()
  })
})
