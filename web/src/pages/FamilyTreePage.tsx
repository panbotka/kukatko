import { useCallback, useEffect, useMemo, useState } from 'react'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useParams } from 'react-router-dom'

import { BackLink } from '../components/BackLink'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { FamilyTreeCanvas } from '../components/people/FamilyTreeCanvas'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import {
  emptyLayout,
  type FamilyLayout,
  type LayoutFamily,
  layoutDescendants,
} from '../lib/familyLayout'
import { useUrlState, writeUrlState } from '../lib/urlState'
import { fetchTree, type FamilyTree, type TreeDirection, type TreeMember } from '../services/family'

/**
 * The view state kept in the URL: which way the tree is walked, and which boxes
 * are folded shut (a comma-separated list of node ids). Both belong in the
 * address because both are *what is being looked at* rather than where the eye
 * is — so Back undoes a fold, a bookmark keeps the branch the reader opened, and
 * a link to "this bit of the family" is a link like any other.
 *
 * Module scope, because {@link useUrlState} needs its defaults stable across
 * renders.
 */
const TREE_DEFAULTS = { direction: 'descendants', closed: '' }

/**
 * The direction this page can actually draw today. Descendants are a genuine
 * tree once a couple is one box, which is what `lib/familyLayout` lays out; the
 * ancestors pedigree is a different shape — a binary grid bounded by generation
 * — with a renderer of its own still to come. Until it does, a URL asking for
 * `direction=ancestors` is answered with the tree that exists rather than with
 * an empty page, and the parameter round-trips through every link on the page so
 * that adding the second direction changes one constant and nothing else.
 */
const DRAWN_DIRECTION: TreeDirection = 'descendants'

/** What the page knows at any moment. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; tree: FamilyTree }

/**
 * Orders a sibship the way a family remembers it: oldest first where anybody
 * recorded a birth year, and alphabetically among those nobody did. The layout
 * is pure and deliberately knows nothing about birthdays — it draws children in
 * the order it is given — so the ordering is decided here, where the people are.
 */
function orderChildren(childUids: readonly string[], people: ReadonlyMap<string, TreeMember>) {
  return [...childUids].sort((a, b) => {
    const left = people.get(a)
    const right = people.get(b)
    const leftYear = left?.birth_year ?? null
    const rightYear = right?.birth_year ?? null
    if (leftYear !== null && rightYear !== null && leftYear !== rightYear) {
      return leftYear - rightYear
    }
    if (leftYear !== rightYear) {
      return leftYear === null ? 1 : -1
    }
    return (left?.name ?? a).localeCompare(right?.name ?? b, 'cs')
  })
}

/** The walked payload, turned into the plain shape the layout takes. */
function toLayoutFamilies(
  tree: FamilyTree,
  people: ReadonlyMap<string, TreeMember>,
): LayoutFamily[] {
  return tree.families.map((family) => ({
    uid: family.uid,
    partnerUids: [family.partner_a_uid, family.partner_b_uid].filter(
      (uid): uid is string => uid !== null,
    ),
    childUids: orderChildren(family.child_uids, people),
  }))
}

/**
 * The whole family of one person, drawn: **`/people/:uid/tree`**.
 *
 * The root is the route's own parameter and the folded branches are a query
 * parameter, so every navigation on this page — re-rooting on a grandchild,
 * folding a branch away — is an ordinary link that Back undoes and a bookmark
 * keeps. That is the project's standing rule ("Back always works") applied to a
 * drawing, which would otherwise hold all of its state in a component and lose
 * it at the first refresh.
 *
 * The page itself only fetches and wires: `GET /subjects/{uid}/tree` gives the
 * people and the family boxes, `lib/familyLayout` decides the coordinates in a
 * pure function, and `FamilyTreeCanvas` paints them. The three are separate
 * because each fails differently — a fetch fails loudly, a layout fails
 * arithmetically (and is unit-tested for it), and a rendering fails by looking
 * wrong, which only a pair of eyes can catch.
 */
export function FamilyTreePage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const location = useLocation()
  const [view] = useUrlState(TREE_DEFAULTS)
  const [state, setState] = useState<State>({ status: 'loading' })

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchTree(uid, DRAWN_DIRECTION, undefined, controller.signal)
      .then((tree) => {
        setState({ status: 'ready', tree })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [uid])

  const tree = state.status === 'ready' ? state.tree : null
  useDocumentTitle(tree === null ? null : t('familyTree.title', { name: tree.root.name }))

  const people = useMemo(() => {
    const map = new Map<string, TreeMember>()
    for (const member of tree?.members ?? []) {
      map.set(member.uid, member)
    }
    return map
  }, [tree])

  const closed = useMemo(() => (view.closed === '' ? [] : view.closed.split(',')), [view.closed])

  const layout: FamilyLayout = useMemo(() => {
    if (tree === null) {
      return emptyLayout()
    }
    return layoutDescendants({
      rootUid: tree.root.uid,
      families: toLayoutFamilies(tree, people),
      collapsed: closed,
    })
  }, [tree, people, closed])

  const hrefWith = useCallback(
    (patch: Partial<typeof TREE_DEFAULTS>) => {
      const params = writeUrlState({ ...view, ...patch }, TREE_DEFAULTS)
      const query = params.toString()
      return query === '' ? location.pathname : `${location.pathname}?${query}`
    },
    [view, location.pathname],
  )

  const toggleHref = useCallback(
    (nodeId: string) => {
      const next = closed.includes(nodeId)
        ? closed.filter((id) => id !== nodeId)
        : [...closed, nodeId]
      return hrefWith({ closed: next.join(',') })
    },
    [closed, hrefWith],
  )

  // Clicking a person re-roots the drawing on them — which is how a reader walks
  // sideways through a village, one family at a time. The root itself has
  // nowhere to re-root to, so its own card leads to the person's page instead.
  const personHref = useCallback(
    (personUid: string) =>
      personUid === uid
        ? `/people/${personUid}`
        : `/people/${personUid}/tree${view.direction === TREE_DEFAULTS.direction ? '' : `?direction=${view.direction}`}`,
    [uid, view.direction],
  )

  return (
    <>
      <BackLink to={`/people/${uid}`} label={t('familyTree.back')} className="mb-3" />
      {state.status === 'loading' && (
        <div className="d-flex justify-content-center py-5">
          <Spinner animation="border" role="status">
            <span className="visually-hidden">{t('familyTree.loading')}</span>
          </Spinner>
        </div>
      )}
      {state.status === 'error' && <ErrorState title={t('familyTree.error')} />}
      {tree !== null && (
        <>
          <div className="d-flex flex-wrap align-items-baseline gap-3 mb-3">
            <h1 className="kk-page-title mb-0">
              {t('familyTree.title', { name: tree.root.name })}
            </h1>
            <span className="text-secondary">
              {t('familyTree.summary', {
                people: tree.members.length,
                generations: generationCount(tree),
              })}
            </span>
            <Link to={`/people/${tree.root.uid}`} className="ms-auto">
              {t('familyTree.openRoot')}
            </Link>
          </div>
          {tree.families.length === 0 ? (
            <EmptyState title={t('familyTree.empty')} hint={t('familyTree.emptyHint')} />
          ) : (
            <>
              <FamilyTreeCanvas
                layout={layout}
                people={people}
                rootUid={tree.root.uid}
                personHref={personHref}
                toggleHref={toggleHref}
              />
              <p className="kk-text-caption text-secondary mt-2 mb-0">{t('familyTree.hint')}</p>
            </>
          )}
        </>
      )}
    </>
  )
}

/** How many generations the walk reached, the root's own counting as one. */
function generationCount(tree: FamilyTree): number {
  let deepest = 0
  for (const member of tree.members) {
    deepest = Math.max(deepest, member.depth)
  }
  return deepest + 1
}
