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
// gap opens the dialog on whom — and the real one loads every subject in the
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

/** One person, in the shape the walk answers with. */
function member(
  uid: string,
  name: string,
  depth: number,
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
    depth,
    partner: false,
  }
}

/** One family box, with whichever of its children the walk reached. */
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
 * Three generations: Marie and Josef, their two children, and one grandchild —
 * enough shape for a fold to have something to hide and for a re-root to lead
 * somewhere other than the root.
 */
function tree(root: Relative): FamilyTree {
  return {
    root,
    direction: 'descendants',
    members: [
      member('s1', 'Marie Nečasová', 0, 1921),
      member('s2', 'Josef Nečas', 0, 1918),
      member('s3', 'Anna Nečasová', 1, 1945),
      member('s4', 'Bohumil Nečas', 1, 1948),
      member('s5', 'Eva Nečasová', 2, 1970),
    ],
    families: [family('fm_1', 's1', 's2', ['s3', 's4']), family('fm_2', 's4', null, ['s5'])],
  }
}

/**
 * Four generations upwards from Eva: her parents, her father's parents, and
 * nobody at all on her mother's side — which is the shape a pedigree of this
 * library actually has, and the one the gaps are drawn for.
 */
function pedigree(): FamilyTree {
  return {
    root: { ...ROOT, uid: 's5', slug: 's5', name: 'Eva Nečasová', birth_year: 1970 },
    direction: 'ancestors',
    members: [
      member('s5', 'Eva Nečasová', 0, 1970),
      member('s4', 'Bohumil Nečas', 1, 1948),
      member('s6', 'Jana Nečasová', 1, 1950),
      member('s1', 'Marie Nečasová', 2, 1921),
      member('s2', 'Josef Nečas', 2, 1918),
    ],
    families: [family('fm_2', 's4', 's6', ['s5']), family('fm_1', 's1', 's2', ['s4'])],
  }
}

/** The root as the endpoint answers with it. */
const ROOT: Relative = {
  uid: 's1',
  slug: 's1',
  name: 'Marie Nečasová',
  type: 'person',
  birth_year: 1921,
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
  fetchTreeMock.mockResolvedValue(tree(ROOT))
})

describe('FamilyTreePage', () => {
  it('walks from the person in the route and names them', async () => {
    renderPage()

    expect(
      await screen.findByRole('heading', { name: 'Family tree — Marie Nečasová' }),
    ).toBeInTheDocument()
    expect(fetchTreeMock).toHaveBeenCalledWith('s1', 'descendants', undefined, expect.anything())
  })

  it('draws every walked person once, the root included', async () => {
    renderPage()

    await screen.findByLabelText('Open Marie Nečasová')
    for (const name of ['Josef Nečas', 'Anna Nečasová', 'Bohumil Nečas', 'Eva Nečasová']) {
      expect(screen.getByLabelText(`Redraw the tree from ${name}`)).toBeInTheDocument()
    }
  })

  it('re-roots through the URL, so Back undoes it', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByLabelText('Redraw the tree from Bohumil Nečas'))

    await waitFor(() => {
      expect(address()).toBe('/people/s4/tree')
    })
    // The new root is fetched, and the old one is still one Back away.
    await waitFor(() => {
      expect(fetchTreeMock).toHaveBeenLastCalledWith(
        's4',
        'descendants',
        undefined,
        expect.anything(),
      )
    })
  })

  it('leads from the root card to the person page, not back to itself', async () => {
    const user = userEvent.setup()
    renderPage()

    await user.click(await screen.findByLabelText('Open Marie Nečasová'))

    await waitFor(() => {
      expect(address()).toBe('/people/s1')
    })
  })

  it('folds a branch into the URL and unfolds it again', async () => {
    const user = userEvent.setup()
    renderPage()

    // Bohumil's own family hides one person: his daughter Eva.
    await user.click(await screen.findByLabelText('Collapse the branch under Bohumil Nečas'))
    await waitFor(() => {
      expect(address()).toBe('/people/s1/tree?closed=fm_2')
    })
    expect(screen.queryByLabelText('Redraw the tree from Eva Nečasová')).not.toBeInTheDocument()

    await user.click(
      screen.getByLabelText('Expand the branch under Bohumil Nečas — 1 people hidden'),
    )
    await waitFor(() => {
      expect(address()).toBe('/people/s1/tree')
    })
    expect(screen.getByLabelText('Redraw the tree from Eva Nečasová')).toBeInTheDocument()
  })

  it('reads a fold straight out of the address on first render', async () => {
    renderPage('/people/s1/tree?closed=fm_2')

    expect(
      await screen.findByLabelText('Expand the branch under Bohumil Nečas — 1 people hidden'),
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Redraw the tree from Eva Nečasová')).not.toBeInTheDocument()
  })

  it('turns round through the URL, so Back undoes the direction too', async () => {
    const user = userEvent.setup()
    renderPage()
    await screen.findByLabelText('Open Marie Nečasová')

    fetchTreeMock.mockResolvedValue(pedigree())
    await user.click(screen.getByRole('link', { name: 'Ancestors' }))

    await waitFor(() => {
      expect(address()).toBe('/people/s1/tree?direction=ancestors')
    })
    // The default depth is not written into the address — a URL says what is
    // not the default and nothing else — but it is what gets asked for.
    await waitFor(() => {
      expect(fetchTreeMock).toHaveBeenLastCalledWith('s1', 'ancestors', 3, expect.anything())
    })
  })

  it('says so when nobody has recorded a family yet', async () => {
    fetchTreeMock.mockResolvedValue({
      root: ROOT,
      direction: 'descendants',
      members: [member('s1', 'Marie Nečasová', 0)],
      families: [],
    })
    renderPage()

    expect(await screen.findByText('No family has been recorded here yet.')).toBeInTheDocument()
  })

  it('reports a walk that could not be loaded', async () => {
    fetchTreeMock.mockRejectedValue(new Error('nope'))
    renderPage()

    expect(await screen.findByText('The family tree could not be loaded.')).toBeInTheDocument()
  })

  it('climbs the pedigree the address asks for', async () => {
    fetchTreeMock.mockResolvedValue(pedigree())
    renderPage('/people/s5/tree?direction=ancestors')

    expect(
      await screen.findByRole('heading', { name: 'Ancestors — Eva Nečasová' }),
    ).toBeInTheDocument()
    expect(fetchTreeMock).toHaveBeenCalledWith('s5', 'ancestors', 3, expect.anything())
    // The root is drawn once at the bottom, everybody above it once each.
    expect(screen.getByLabelText('Open Eva Nečasová')).toBeInTheDocument()
    for (const name of ['Bohumil Nečas', 'Jana Nečasová', 'Marie Nečasová', 'Josef Nečas']) {
      expect(screen.getByLabelText(`Show the ancestors of ${name}`)).toBeInTheDocument()
    }
  })

  it('round-trips the depth of the climb through the URL', async () => {
    const user = userEvent.setup()
    fetchTreeMock.mockResolvedValue(pedigree())
    renderPage('/people/s5/tree?direction=ancestors&generations=2')

    // What the address says is what is asked for and what the control shows…
    await waitFor(() => {
      expect(fetchTreeMock).toHaveBeenLastCalledWith('s5', 'ancestors', 2, expect.anything())
    })
    const depth = screen.getByLabelText('Generations')
    expect(depth).toHaveValue('2')

    // …and a change of depth goes back into the address, so Back undoes it.
    await user.selectOptions(depth, '5')
    await waitFor(() => {
      expect(address()).toBe('/people/s5/tree?direction=ancestors&generations=5')
    })
    await waitFor(() => {
      expect(fetchTreeMock).toHaveBeenLastCalledWith('s5', 'ancestors', 5, expect.anything())
    })
  })

  it('carries the direction through a re-root', async () => {
    const user = userEvent.setup()
    fetchTreeMock.mockResolvedValue(pedigree())
    renderPage('/people/s5/tree?direction=ancestors')

    await user.click(await screen.findByLabelText('Show the ancestors of Bohumil Nečas'))

    await waitFor(() => {
      expect(address()).toBe('/people/s4/tree?direction=ancestors')
    })
  })

  it('draws an unknown ancestor as a gap that records them', async () => {
    const user = userEvent.setup()
    fetchTreeMock.mockResolvedValue(pedigree())
    renderPage('/people/s5/tree?direction=ancestors')

    // Nobody recorded Jana's parents, so her two slots are blank — and each of
    // them opens the dialog on *her*, because a parent is recorded on a child.
    const gaps = await screen.findAllByRole('button', {
      name: 'Record a parent of Jana Nečasová',
    })
    // Two of them: a father and a mother, both unknown and both fillable.
    expect(gaps).toHaveLength(2)
    await user.click(gaps[0])

    expect(screen.getByTestId('add-relation')).toHaveTextContent('parent of s6')
  })

  it('shows the same gaps to a reader who may not fill them', async () => {
    fetchTreeMock.mockResolvedValue(pedigree())
    renderPage('/people/s5/tree?direction=ancestors', false)

    // The gap is still drawn — it is the honest answer to "who was her mother" —
    // but it is not a button, because this reader has nothing to click it for.
    expect(
      await screen.findAllByRole('img', { name: 'Unknown parent of Jana Nečasová' }),
    ).toHaveLength(2)
    expect(
      screen.queryByRole('button', { name: 'Record a parent of Jana Nečasová' }),
    ).not.toBeInTheDocument()
  })

  it('offers the pedigree even when the tree below is empty', async () => {
    fetchTreeMock.mockResolvedValue({
      root: ROOT,
      direction: 'ancestors',
      members: [member('s1', 'Marie Nečasová', 0)],
      families: [],
    })
    renderPage('/people/s1/tree?direction=ancestors')

    // An editor gets the invitation (two blank parents) rather than the sentence
    // about where relations are recorded: here is where they can be.
    expect(
      await screen.findAllByRole('button', { name: 'Record a parent of Marie Nečasová' }),
    ).toHaveLength(2)
    expect(screen.queryByText('No family has been recorded here yet.')).not.toBeInTheDocument()
  })
})
