import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import {
  type FamilyTree,
  type Relative,
  type TreeFamily,
  type TreeMember,
} from '../services/family'

import { FamilyTreePage } from './FamilyTreePage'

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

function renderPage(entry = '/people/s1/tree') {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={[entry]}>
        <Address />
        <Routes>
          <Route path="/people/:uid/tree" element={<FamilyTreePage />} />
          <Route path="/people/:uid" element={<div>person page</div>} />
        </Routes>
      </MemoryRouter>
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

  it('carries a direction it cannot draw yet through its links', async () => {
    const user = userEvent.setup()
    renderPage('/people/s1/tree?direction=ancestors')

    // The descendants tree is drawn rather than an empty page…
    await user.click(await screen.findByLabelText('Redraw the tree from Bohumil Nečas'))

    // …and the parameter survives the re-root, ready for the pedigree renderer.
    await waitFor(() => {
      expect(address()).toBe('/people/s4/tree?direction=ancestors')
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
})
