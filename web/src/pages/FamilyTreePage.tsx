import { useCallback, useEffect, useMemo, useState } from 'react'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useLocation, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BackLink } from '../components/BackLink'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { AddRelationModal } from '../components/people/AddRelationModal'
import { FamilyPedigreeCanvas } from '../components/people/FamilyPedigreeCanvas'
import { FamilyTreeCanvas } from '../components/people/FamilyTreeCanvas'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import {
  emptyLayout,
  emptyPedigree,
  type FamilyLayout,
  type LayoutFamily,
  layoutAncestors,
  layoutDescendants,
  MAX_PEDIGREE_GENERATIONS,
  type Pedigree,
} from '../lib/familyLayout'
import { useUrlState, writeUrlState } from '../lib/urlState'
import {
  fetchTree,
  type FamilyTree,
  type Relative,
  type TreeDirection,
  type TreeMember,
} from '../services/family'

/**
 * The view state kept in the URL: which way the tree is walked, how far up a
 * pedigree climbs, and which boxes are folded shut (a comma-separated list of
 * node ids). All three belong in the address because all three are *what is
 * being looked at* rather than where the eye is — so Back undoes a fold or a
 * change of direction, a bookmark keeps the branch the reader opened, and a link
 * to "this bit of the family" is a link like any other.
 *
 * Module scope, because {@link useUrlState} needs its defaults stable across
 * renders.
 */
const TREE_DEFAULTS = { direction: 'descendants', generations: '3', closed: '' }

/**
 * How far up the pedigree climbs by default: the root, their parents,
 * grandparents and great-grandparents — the four-generation chart genealogy has
 * printed on one sheet of paper for two centuries, and about as much as this
 * library can fill in for anybody.
 */
const DEFAULT_GENERATIONS = 3

/** What the page knows at any moment. */
type State = { status: 'loading' } | { status: 'error' } | { status: 'ready'; tree: FamilyTree }

/** The direction named in the address, an unknown value walking downwards. */
function readDirection(raw: string): TreeDirection {
  return raw === 'ancestors' ? 'ancestors' : 'descendants'
}

/** The climb named in the address, bounded to what the layout will draw. */
function readGenerations(raw: string): number {
  const value = Number.parseInt(raw, 10)
  if (Number.isNaN(value) || value < 1) {
    return DEFAULT_GENERATIONS
  }
  return Math.min(value, MAX_PEDIGREE_GENERATIONS)
}

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
 * The parents a person already has in the walked payload — what the add dialog
 * needs to offer the sibling role, and what tells the reader where the person
 * they are about to record will hang.
 */
function parentsOf(
  tree: FamilyTree | null,
  childUid: string,
  people: ReadonlyMap<string, TreeMember>,
): Relative[] {
  const family = tree?.families.find((candidate) => candidate.child_uids.includes(childUid))
  if (family === undefined) {
    return []
  }
  return [family.partner_a_uid, family.partner_b_uid]
    .map((uid) => (uid === null ? undefined : people.get(uid)))
    .filter((relative): relative is TreeMember => relative !== undefined)
}

/**
 * The whole family of one person, drawn: **`/people/:uid/tree`**.
 *
 * The root is the route's own parameter, and the direction, the pedigree's depth
 * and the folded branches are query parameters — so every navigation on this
 * page (re-rooting on a grandchild, turning round to look upwards, folding a
 * branch away) is an ordinary link that Back undoes and a bookmark keeps. That
 * is the project's standing rule ("Back always works") applied to a drawing,
 * which would otherwise hold all of its state in a component and lose it at the
 * first refresh.
 *
 * The two directions are two different problems and therefore two different
 * drawings: descendants are a genuine tree once a couple is one box, and are
 * laid out as a tidy tree that folds; ancestors are a binary pedigree, bounded
 * by generation, in which **an unknown grandparent is a visible gap** — and,
 * for an editor, a gap that can be clicked straight into the dialog that fills
 * it. The page itself only fetches and wires: `GET /subjects/{uid}/tree` gives
 * the people and the family boxes, `lib/familyLayout` decides the coordinates in
 * pure functions, and the two canvases paint them. The three are separate
 * because each fails differently — a fetch fails loudly, a layout fails
 * arithmetically (and is unit-tested for it), and a rendering fails by looking
 * wrong, which only a pair of eyes can catch.
 */
export function FamilyTreePage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const { canWrite } = useAuth()
  const location = useLocation()
  const [view, setView] = useUrlState(TREE_DEFAULTS)
  const [state, setState] = useState<State>({ status: 'loading' })
  // Recording a parent from a gap changes what the walk would answer, so the
  // fetch is re-run rather than the new person being patched into the old tree.
  const [reloads, setReloads] = useState(0)
  const [addingParentOf, setAddingParentOf] = useState<string | null>(null)

  const direction = readDirection(view.direction)
  const generations = readGenerations(view.generations)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchTree(
      uid,
      direction,
      direction === 'ancestors' ? generations : undefined,
      controller.signal,
    )
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
  }, [uid, direction, generations, reloads])

  const tree = state.status === 'ready' ? state.tree : null
  const titleKey = direction === 'ancestors' ? 'familyTree.titleUp' : 'familyTree.title'
  useDocumentTitle(tree === null ? null : t(titleKey, { name: tree.root.name }))

  const people = useMemo(() => {
    const map = new Map<string, TreeMember>()
    for (const member of tree?.members ?? []) {
      map.set(member.uid, member)
    }
    return map
  }, [tree])

  const closed = useMemo(() => (view.closed === '' ? [] : view.closed.split(',')), [view.closed])

  const families = useMemo(
    () => (tree === null ? [] : toLayoutFamilies(tree, people)),
    [tree, people],
  )

  const layout: FamilyLayout = useMemo(() => {
    if (tree === null || direction === 'ancestors') {
      return emptyLayout()
    }
    return layoutDescendants({ rootUid: tree.root.uid, families, collapsed: closed })
  }, [tree, direction, families, closed])

  const pedigree: Pedigree = useMemo(() => {
    if (tree === null || direction !== 'ancestors') {
      return emptyPedigree()
    }
    return layoutAncestors({ rootUid: tree.root.uid, families, generations })
  }, [tree, direction, families, generations])

  const hrefWith = useCallback(
    (patch: Partial<typeof TREE_DEFAULTS>, pathname = location.pathname) => {
      const params = writeUrlState({ ...view, ...patch }, TREE_DEFAULTS)
      const query = params.toString()
      return query === '' ? pathname : `${pathname}?${query}`
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
  // sideways through a village, one family at a time — keeping the direction and
  // the depth they were looking at, but not a fold, which names boxes of the
  // drawing being left behind. The root itself has nowhere to re-root to, so its
  // own card leads to the person's page instead.
  const personHref = useCallback(
    (personUid: string) =>
      personUid === uid
        ? `/people/${personUid}`
        : hrefWith({ closed: '' }, `/people/${personUid}/tree`),
    [uid, hrefWith],
  )

  const addingParents = useMemo(
    () => (addingParentOf === null ? [] : parentsOf(tree, addingParentOf, people)),
    [addingParentOf, tree, people],
  )

  // A pedigree with nothing recorded still says something an editor can act on:
  // two empty slots over the person, which is the invitation. A reader who
  // cannot fill them in is better served by the sentence that says where.
  const invites = direction === 'ancestors' && canWrite
  const showEmptyState = tree !== null && tree.families.length === 0 && !invites

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
            <h1 className="kk-page-title mb-0">{t(titleKey, { name: tree.root.name })}</h1>
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
          <div className="d-flex flex-wrap align-items-center gap-3 mb-3">
            {/* The direction switch is two links and not two buttons: the state
                it changes lives in the URL, so Back turns the drawing round
                again and a direction can be sent to somebody as a link. */}
            <div
              className="btn-group btn-group-sm"
              role="group"
              aria-label={t('familyTree.directionLabel')}
            >
              {(['descendants', 'ancestors'] as const).map((option) => (
                <Link
                  key={option}
                  to={hrefWith({ direction: option, closed: '' })}
                  className={`btn btn-outline-secondary${direction === option ? ' active' : ''}`}
                  aria-current={direction === option ? 'page' : undefined}
                >
                  {t(`familyTree.direction.${option}`)}
                </Link>
              ))}
            </div>
            {direction === 'ancestors' && (
              <Form.Group
                controlId="pedigree-generations"
                className="d-flex align-items-center gap-2"
              >
                <Form.Label className="mb-0 kk-text-caption text-secondary">
                  {t('familyTree.generationsLabel')}
                </Form.Label>
                <Form.Select
                  size="sm"
                  className="w-auto"
                  value={String(generations)}
                  onChange={(event) => {
                    setView({ generations: event.target.value })
                  }}
                >
                  {Array.from({ length: MAX_PEDIGREE_GENERATIONS }, (_, index) => index + 1).map(
                    (option) => (
                      <option key={option} value={option}>
                        {option}
                      </option>
                    ),
                  )}
                </Form.Select>
              </Form.Group>
            )}
          </div>
          {showEmptyState ? (
            <EmptyState title={t('familyTree.empty')} hint={t('familyTree.emptyHint')} />
          ) : (
            <>
              {direction === 'ancestors' ? (
                <FamilyPedigreeCanvas
                  pedigree={pedigree}
                  people={people}
                  rootUid={tree.root.uid}
                  personHref={personHref}
                  onAddParent={canWrite ? setAddingParentOf : null}
                />
              ) : (
                <FamilyTreeCanvas
                  layout={layout}
                  people={people}
                  rootUid={tree.root.uid}
                  personHref={personHref}
                  toggleHref={toggleHref}
                />
              )}
              <p className="kk-text-caption text-secondary mt-2 mb-0">
                {t(direction === 'ancestors' ? 'familyTree.hintUp' : 'familyTree.hint')}
              </p>
            </>
          )}
          {/* Mounted only while open: the dialog loads every subject in the
              library to pick from, and a page that never opens it must not pay
              for that. */}
          {canWrite && addingParentOf !== null && (
            <AddRelationModal
              subjectUid={addingParentOf}
              subjectName={people.get(addingParentOf)?.name ?? addingParentOf}
              parents={addingParents}
              kind="parent"
              show
              onHide={() => {
                setAddingParentOf(null)
              }}
              onAdded={() => {
                setReloads((count) => count + 1)
              }}
            />
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
