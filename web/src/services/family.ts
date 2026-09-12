import { ApiError } from './auth'
import { type SubjectType } from './people'

/**
 * Genealogy client, mirroring `internal/familyapi` / `internal/family`. It reads
 * one subject's immediate relations (`GET /subjects/{uid}/relations`) and records
 * a new one (`POST /subjects/{uid}/relations`).
 *
 * It is a client of its own rather than more of `people.ts` — which is already
 * 21 kB of subjects, markers and faces — because the family is a separate model:
 * the node is the *family* (a couple or lone parent plus their children) and
 * parents, siblings, partners and children are all derived from it, which is why
 * they cannot contradict each other. The types below mirror the Go structs field
 * for field, so a change on either side shows up as a type error here rather than
 * as a silently missing value.
 *
 * The session cookie is sent automatically (same-origin); every call throws
 * {@link ApiError} on a non-OK response — a status the caller reads, because the
 * backend distinguishes a refusal about the *state* of the tree (409: a cycle, a
 * second parentage) from a malformed request (400).
 */

const API_BASE = '/api/v1'

/** Standard backend error envelope shared by every API group. */
interface ErrorBody {
  error?: string
}

/** Extracts the backend error message from a non-OK response, if present. */
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as ErrorBody
    if (typeof body.error === 'string' && body.error !== '') {
      return body.error
    }
  } catch {
    // Body was empty or not JSON; fall back to the status text below.
  }
  return res.statusText || `request failed: ${res.status}`
}

/** Issues a GET and parses the JSON reply, throwing on non-OK. */
async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/** Sends a JSON body and parses the JSON reply, throwing on non-OK. */
async function sendJSON<T>(
  method: string,
  path: string,
  body: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  return (await res.json()) as T
}

/** What ties a family's two partners together (`family.Kind`). */
export type FamilyKind = 'marriage' | 'partnership' | 'unknown'

/** How a child belongs to the family they hang off (`family.ChildKind`). */
export type ChildKind = 'birth' | 'adopted' | 'step'

/**
 * The side the *other* person takes in a recorded relation (`family.Role`). It is
 * the whole vocabulary the endpoint accepts: a sibling is not among them, because
 * siblings are derived from a shared parentage rather than stored (see
 * {@link AddRelationRequest}).
 */
export type RelationRole = 'parent' | 'child' | 'partner'

/**
 * One family row (`family.Family`): a couple, or a lone parent with the second
 * column left null — a great-grandmother whose husband nobody remembers still has
 * children, and they hang off her family.
 */
export interface Family {
  uid: string
  partner_a_uid: string | null
  partner_b_uid: string | null
  kind: FamilyKind
  from_year: number | null
  to_year: number | null
  note: string
  created_at: string
  updated_at: string
}

/**
 * One related person in the shape a chip renders them (`family.Relative`): enough
 * of the subject to draw the chip, plus how many photos they appear on, so the
 * strip renders from one response instead of a request per person.
 *
 * `photo_count` of zero is ordinary here, not an anomaly: a person nobody
 * photographed is exactly the kind of relative a tree is drawn for, and the chip
 * then draws their initials.
 */
export interface Relative {
  uid: string
  slug: string
  name: string
  type: SubjectType
  birth_year: number | null
  death_year: number | null
  /** The photo chosen to illustrate the person; absent when nobody chose one. */
  cover_photo_uid?: string
  /** Visible photos the person appears on, counted as the people index counts them. */
  photo_count: number
  /** The family row this relation is recorded in; absent where none single one applies. */
  family_uid?: string
  /** How the child of the relation belongs to their family; absent for a partner. */
  child_kind?: ChildKind
}

/**
 * One family the subject is a partner in (`family.Partnership`), with the person
 * on the other side. `partner` is null for a lone-parent family, which is a
 * family all the same — it is where that person's children hang.
 */
export interface Partnership {
  family: Family
  partner: Relative | null
}

/**
 * The derived view of one subject's immediate family (`family.Relations`) — the
 * four lists the strip draws. Every list is derived from the family rows rather
 * than stored, which is why none of them can disagree with the others. All four
 * are always present; an empty relation is an empty array, never null.
 */
export interface Relations {
  parents: Relative[]
  siblings: Relative[]
  partners: Partnership[]
  children: Relative[]
}

/**
 * A subject to create as part of recording a relation to them
 * (`family.NewPerson`). It is the affordance that makes filling a tree bearable:
 * without it every great-grandmother is a trip to another screen and back, once
 * per person, for every generation nobody wrote down.
 */
export interface NewPerson {
  name: string
  type?: SubjectType
  birth_year?: number | null
  death_year?: number | null
  notes?: string
}

/**
 * One request to record a relation (`family.AddRelation`): which role the other
 * person takes, and who they are — an existing subject (`subject_uid`) or one
 * created by this very call (`new_subject`). Exactly one of the two must be
 * given, and the backend creates the person and the relation in a single audited
 * transaction, so a refused relation leaves no orphan person behind.
 */
export interface AddRelationRequest {
  role: RelationRole
  subject_uid?: string
  new_subject?: NewPerson
  child_kind?: ChildKind
}

/**
 * What recording a relation produced (`family.AddResult`): the family it ended up
 * in, the person on the other side as a chip renders them, and whether that
 * person was created by this call.
 */
export interface AddRelationResult {
  family: Family
  relative: Relative
  created: boolean
}

/**
 * Reads a subject's immediate relations via `GET /subjects/{uid}/relations`.
 * Throws {@link ApiError} 404 when the subject does not exist.
 */
export async function fetchRelations(subjectUid: string, signal?: AbortSignal): Promise<Relations> {
  return getJSON<Relations>(`/subjects/${encodeURIComponent(subjectUid)}/relations`, signal)
}

/**
 * Records one relation on the subject via `POST /subjects/{uid}/relations`,
 * answering with the family it landed in and the person on the other side.
 *
 * A refusal is an {@link ApiError} whose status says what kind it was: 409 for
 * something about the state of the tree (this would be a cycle; that person is
 * already a child elsewhere), 400 for a request that got itself wrong.
 */
export async function addRelation(
  subjectUid: string,
  req: AddRelationRequest,
  signal?: AbortSignal,
): Promise<AddRelationResult> {
  return sendJSON<AddRelationResult>(
    'POST',
    `/subjects/${encodeURIComponent(subjectUid)}/relations`,
    req,
    signal,
  )
}

/** Which way a tree is walked from its root (`family.Direction`). */
export type TreeDirection = 'descendants' | 'ancestors'

/**
 * One person in a walked tree (`family.Member`): the relative plus where the
 * walk found them.
 */
export interface TreeMember extends Relative {
  /**
   * Generations between this person and the root, which is itself at 0. When
   * two paths reach the same person — which happens as soon as cousins marry —
   * the shortest one wins.
   */
  depth: number
  /**
   * True for somebody who is in the set only because they are partnered with a
   * descendant: the "plus their partners" half of what a family means here.
   */
  partner: boolean
}

/**
 * One family box of a drawn tree (`family.TreeFamily`): the family plus the
 * children of it the walk actually reached. A child outside the walked set is
 * left out on purpose, so the drawing is never handed an edge to a person it was
 * given no node for.
 */
export interface TreeFamily extends Family {
  child_uids: string[]
}

/**
 * The layout-ready payload of one family tree (`family.Tree`). The layout itself
 * is a pure function in `lib/familyLayout`; this is only its input.
 */
export interface FamilyTree {
  root: Relative
  direction: TreeDirection
  members: TreeMember[]
  families: TreeFamily[]
}

/**
 * Reads the tree walked from a subject via `GET /subjects/{uid}/tree`.
 *
 * `generations` is optional and bounded by the backend: omitted means the whole
 * walk, which for a village archive is a page and not a denial of service. An
 * unknown subject is an {@link ApiError} 404.
 */
export async function fetchTree(
  subjectUid: string,
  direction: TreeDirection,
  generations?: number,
  signal?: AbortSignal,
): Promise<FamilyTree> {
  const params = new URLSearchParams({ direction })
  if (generations !== undefined) {
    params.set('generations', String(generations))
  }
  return getJSON<FamilyTree>(
    `/subjects/${encodeURIComponent(subjectUid)}/tree?${params.toString()}`,
    signal,
  )
}
