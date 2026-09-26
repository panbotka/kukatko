import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link, useParams } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { BackLink } from '../components/BackLink'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { AddRelationModal } from '../components/people/AddRelationModal'
import { FamilyNetworkCanvas } from '../components/people/FamilyNetworkCanvas'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { type FamilyLayout, type LayoutFamily, layoutNetwork } from '../lib/familyLayout'
import { fetchTree, type FamilyTree, type Relative, type TreeMember } from '../services/family'

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
 * Everybody in the network who is missing a parent: a child of no family, or of
 * a lone parent, or of a sibling group — whose parents are by definition not in
 * the library. That is where the card's `+` is offered, and why it is not
 * offered to somebody both of whose parents are already drawn.
 */
function missingParentOf(tree: FamilyTree | null): Set<string> {
  const missing = new Set(tree?.members.map((member) => member.uid) ?? [])
  for (const family of tree?.families ?? []) {
    if (family.partner_a_uid !== null && family.partner_b_uid !== null) {
      for (const child of family.child_uids) {
        missing.delete(child)
      }
    }
  }
  return missing
}

/** How many generations the network spans, the root's own counting as one. */
function generationCount(tree: FamilyTree): number {
  if (tree.members.length === 0) {
    return 1
  }
  const generations = tree.members.map((member) => member.generation)
  return Math.max(...generations) - Math.min(...generations) + 1
}

/**
 * The whole family of one person, drawn: **`/people/:uid/tree`**.
 *
 * It draws everybody the family links reach from the person — parents and
 * children, and sideways through a sibling group to aunts, cousins and their
 * in-laws — in one layered drawing, a generation per row. There is no direction
 * to choose and no depth to set: a reader looking at a family wants all of it,
 * and a drawing that grows is a drawing you pan. The server caps the walk at its
 * nearest few hundred people, and the page says so when it did.
 *
 * The root is the route's own parameter, so clicking a person re-roots the
 * drawing on them as an ordinary link that Back undoes and a bookmark keeps —
 * the project's "Back always works" rule applied to a drawing. The page itself
 * only fetches and wires: `GET /subjects/{uid}/tree` gives the people and the
 * family boxes, `lib/familyLayout` decides the coordinates in a pure function,
 * and the canvas paints them. The three are separate because each fails
 * differently — a fetch fails loudly, a layout fails arithmetically (and is
 * unit-tested for it), and a rendering fails by looking wrong, which only a pair
 * of eyes can catch.
 *
 * For a curator every card whose person is missing a parent carries a `+` into
 * the dialog that records one — the invitation the old pedigree's empty slots
 * made, moved onto the person it is about.
 */
export function FamilyTreePage() {
  const { t } = useTranslation()
  const { uid = '' } = useParams<{ uid: string }>()
  const { canCurate } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  // Recording a parent changes what the walk would answer, so the fetch is
  // re-run rather than the new person being patched into the old drawing.
  const [reloads, setReloads] = useState(0)
  const [addingParentOf, setAddingParentOf] = useState<string | null>(null)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchTree(uid, controller.signal)
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
  }, [uid, reloads])

  const tree = state.status === 'ready' ? state.tree : null
  useDocumentTitle(tree === null ? null : t('familyTree.title', { name: tree.root.name }))

  const people = useMemo(() => {
    const map = new Map<string, TreeMember>()
    for (const member of tree?.members ?? []) {
      map.set(member.uid, member)
    }
    return map
  }, [tree])

  const layout: FamilyLayout = useMemo(
    () =>
      layoutNetwork({
        rootUid: tree?.root.uid ?? uid,
        generations: new Map(
          (tree?.members ?? []).map((member) => [member.uid, member.generation]),
        ),
        families: tree === null ? [] : toLayoutFamilies(tree, people),
      }),
    [tree, people, uid],
  )

  const missingParent = useMemo(() => missingParentOf(tree), [tree])

  // Clicking a person re-roots the drawing on them — the same family, centred on
  // somebody else, with the generations counted from them. The root itself has
  // nowhere to re-root to, so its own card leads to the person's page instead.
  const personHref = useCallback(
    (personUid: string) =>
      personUid === uid ? `/people/${personUid}` : `/people/${personUid}/tree`,
    [uid],
  )

  const addingParents = useMemo(
    () => (addingParentOf === null ? [] : parentsOf(tree, addingParentOf, people)),
    [addingParentOf, tree, people],
  )

  // A person with no family recorded still gives a curator something to act on:
  // their own card with its `+`, which is the invitation. A reader who cannot
  // record anything is better served by the sentence that says where.
  const showEmptyState = tree !== null && tree.families.length === 0 && !canCurate

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
          {tree.truncated && (
            <Alert variant="info" className="py-2">
              {t('familyTree.truncated', { shown: tree.members.length, total: tree.total })}
            </Alert>
          )}
          {showEmptyState ? (
            <EmptyState title={t('familyTree.empty')} hint={t('familyTree.emptyHint')} />
          ) : (
            <>
              <FamilyNetworkCanvas
                layout={layout}
                people={people}
                rootUid={tree.root.uid}
                personHref={personHref}
                missingParent={missingParent}
                onAddParent={canCurate ? setAddingParentOf : null}
              />
              <p className="kk-text-caption text-secondary mt-2 mb-0">
                {t('familyTree.hint')}
                {canCurate && ` ${t('familyTree.hintAdd')}`}
              </p>
            </>
          )}
          {/* Mounted only while open: the dialog loads every subject in the
              library to pick from, and a page that never opens it must not pay
              for that. */}
          {canCurate && addingParentOf !== null && (
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
