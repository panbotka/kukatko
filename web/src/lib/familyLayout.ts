/**
 * The pure geometry of the family tree, in both of its directions: families in,
 * coordinates out. No DOM, no React, no fetching — which is the point, because
 * this is where the risk of the whole page lives and a pure function is what
 * makes it unit-testable. The SVG renderers on top of it
 * (`components/people/FamilyTreeCanvas` and `FamilyPedigreeCanvas`) only paint
 * what this module decided.
 *
 * **Downwards** ({@link layoutDescendants}) is a classic tidy tree
 * (Reingold–Tilford): each subtree is laid out on its own, subtrees are packed
 * against each other by their **contours** — the leftmost and rightmost edge at
 * every level — and a parent is then centred over its children. That is what
 * keeps a deep narrow branch from being pushed halfway across the page by a
 * shallow wide one, and what makes the drawing readable at all once a family has
 * four generations in it. It is a genuine tree only because **a couple is one
 * box**: a person is a child in at most one family (the database enforces it),
 * so the boxes hang off each other.
 *
 * **Upwards** ({@link layoutAncestors}) is a different shape and therefore a
 * different function: a binary pedigree, bounded by construction (2, 4, 8, 16 …)
 * and read as a grid of generations, in which every known person carries two
 * slots whether or not the library can fill them. That is why the design splits
 * the page by direction instead of drawing one graph both ways.
 *
 * No dependency is taken for either. The frontend's dependency list is
 * deliberately lean — leaflet is its only visualisation library — and this is
 * arithmetic, not a reason to add d3, dagre or elkjs.
 */

/** The width of one person's card inside a box, in layout units (≈ CSS px). */
export const PERSON_WIDTH = 176

/** The height of a box, which is one person's card tall whoever is in it. */
export const NODE_HEIGHT = 60

/** Where the round face sits inside a person's card, in layout units. */
export const AVATAR_CIRCLE = { cx: 32, cy: NODE_HEIGHT / 2, r: 20 }

/** The gap between the two partners inside one couple's box. */
export const COUPLE_GAP = 10

/** The least horizontal gap between two boxes on the same level. */
export const SIBLING_GAP = 26

/** The vertical gap between one generation and the next. */
export const LEVEL_GAP = 58

/** The blank margin kept around the drawing, so nothing touches the viewport. */
export const TREE_PADDING = 24

/** The distance from one generation's top edge to the next one's. */
const LEVEL_STRIDE = NODE_HEIGHT + LEVEL_GAP

/**
 * One family as the layout needs it: who is partnered in it and which of their
 * children the walk actually reached. Children outside the walked set are left
 * out by the backend deliberately — a box must never be handed an edge to a
 * person it was given no node for.
 */
export interface LayoutFamily {
  /** The family row's UID, which is also the box's node id. */
  uid: string
  /** The one or two people partnered in it. A lone parent is a family too. */
  partnerUids: readonly string[]
  /** The children of it that were walked, in the order they should be drawn. */
  childUids: readonly string[]
}

/** What {@link layoutDescendants} is asked to draw. */
export interface FamilyLayoutInput {
  /** The person the tree is rooted at. */
  rootUid: string
  /** Every family box available; the walk picks the ones it can reach. */
  families: readonly LayoutFamily[]
  /** The ids of the boxes whose branches are folded away. */
  collapsed?: Iterable<string>
}

/**
 * One box of the drawing: a couple (or a lone parent, or a childless leaf) with
 * its place on the page already decided. `x`/`y` are the box's top-left corner.
 */
export interface LayoutNode {
  /** The box's identity: the family's UID, or `person:<uid>` for a lone leaf. */
  id: string
  /** The family drawn here, or null for a person who is in no family at all. */
  familyUid: string | null
  /** The people in the box, left to right. One or two. */
  personUids: string[]
  /**
   * The people here who are already drawn in an earlier box — the second box of
   * a remarriage repeats the partner both families share. The renderer marks
   * them, so a reader is not left counting the same person twice.
   */
  repeatUids: string[]
  /** The box this one hangs off, or null for a root box. */
  parentId: string | null
  /** The boxes hanging off this one, left to right. Empty when collapsed. */
  childIds: string[]
  /** Generations between this box and the root's, the root's being 0. */
  depth: number
  x: number
  y: number
  width: number
  height: number
  /** Whether this box's branch is folded away. */
  collapsed: boolean
  /**
   * How many people the fold is hiding. Zero for a box that is not collapsed,
   * and zero for one whose children happen to be drawn elsewhere anyway.
   */
  hiddenCount: number
}

/** One line from a box down to a child's box, as four plain numbers. */
export interface LayoutEdge {
  parentId: string
  childId: string
  /** The foot of the drop line: the bottom centre of the parent's box. */
  x1: number
  y1: number
  /** The head of the child's line: the top centre of the child's box. */
  x2: number
  y2: number
}

/** The finished drawing: boxes, lines, and the canvas they need. */
export interface FamilyLayout {
  nodes: LayoutNode[]
  edges: LayoutEdge[]
  /** The drawing's own size, padding included — the SVG's `viewBox`. */
  width: number
  height: number
}

/** The width of a box holding `count` people. */
export function boxWidth(count: number): number {
  const people = Math.max(count, 1)
  return people * PERSON_WIDTH + (people - 1) * COUPLE_GAP
}

/** A drawing with nothing in it, which still has a canvas to be rendered into. */
export function emptyLayout(): FamilyLayout {
  return { nodes: [], edges: [], width: 2 * TREE_PADDING, height: 2 * TREE_PADDING }
}

/** The contour of a subtree: its leftmost and rightmost edge at every level. */
interface Contour {
  left: number[]
  right: number[]
}

/** A node while it is being placed: its subtree, its contour, its offset. */
interface Placed {
  node: LayoutNode
  children: Placed[]
  contour: Contour
  /** Centre of this subtree relative to its parent's centre. */
  offset: number
}

/**
 * Lays out the descendants of `rootUid` as a tidy tree of family boxes.
 *
 * The walk is breadth-first, which is what makes the drawing honest when the
 * same person is reachable by two paths — and in a village, once cousins marry,
 * they are. Every person and every family is drawn **once**, at the shallowest
 * place it was reached; a second path to an already-drawn family simply does not
 * produce a second box, so the diamond closes instead of being duplicated into
 * two identical branches.
 *
 * A person partnered in several families (a remarriage) yields one box per
 * family, side by side, with the shared partner marked as a repeat in all but
 * the first: two marriages are two boxes because their children hang off
 * different couples, and pretending otherwise would attach a child to the wrong
 * parent.
 *
 * The order of `families` and of each family's `childUids` is preserved as the
 * drawing order, so a caller who wants children by birth year sorts them before
 * calling rather than asking this function to know about birthdays.
 */
export function layoutDescendants(input: FamilyLayoutInput): FamilyLayout {
  const collapsed = new Set(input.collapsed ?? [])
  const built = buildNodes(input.rootUid, input.families, collapsed)
  if (built.nodes.length === 0) {
    return emptyLayout()
  }
  countHidden(built, input.families, collapsed)
  place(built)
  return { nodes: built.nodes, edges: edgesOf(built), ...canvasOf(built.nodes) }
}

/** The structure of the drawing, before anything has coordinates. */
interface Built {
  nodes: LayoutNode[]
  byId: Map<string, LayoutNode>
  roots: string[]
  /** Everybody who ended up in a box, so a fold knows who it is hiding. */
  drawn: Set<string>
}

/** One person waiting to be turned into boxes, with where they hang. */
interface Pending {
  personUid: string
  parentId: string | null
  depth: number
}

/**
 * Walks the families breadth-first from the root and emits one box per family
 * reached, plus a bare box for a person who is in no family at all (a childless,
 * unmarried descendant is still somebody's child and has to be drawn).
 */
function buildNodes(
  rootUid: string,
  families: readonly LayoutFamily[],
  collapsed: ReadonlySet<string>,
): Built {
  const byPartner = indexByPartner(families)
  const built: Built = { nodes: [], byId: new Map(), roots: [], drawn: new Set() }
  const drawnFamilies = new Set<string>()
  const processed = new Set<string>()
  const queue: Pending[] = [{ personUid: rootUid, parentId: null, depth: 0 }]

  // The queue is appended to while it is walked, which an array iterator
  // handles: it re-reads the length on every step, so a child pushed at depth 2
  // is reached in the same loop. That is what makes this breadth-first.
  for (const item of queue) {
    if (processed.has(item.personUid)) {
      continue
    }
    processed.add(item.personUid)

    const open = (byPartner.get(item.personUid) ?? []).filter((f) => !drawnFamilies.has(f.uid))
    if (open.length === 0) {
      // Nothing left to draw for this person. A person already in somebody
      // else's box (the other half of a cousin marriage) gets no second box;
      // one nobody has drawn yet gets a box of their own.
      if (!built.drawn.has(item.personUid)) {
        addNode(built, leafNode(item), item.parentId)
        built.drawn.add(item.personUid)
      }
      continue
    }
    for (const family of open) {
      drawnFamilies.add(family.uid)
      const node = familyNode(family, item, built.drawn)
      addNode(built, node, item.parentId)
      for (const uid of node.personUids) {
        built.drawn.add(uid)
      }
      if (collapsed.has(node.id)) {
        node.collapsed = true
        continue
      }
      for (const child of family.childUids) {
        queue.push({ personUid: child, parentId: node.id, depth: item.depth + 1 })
      }
    }
  }
  return built
}

/** Every family indexed by each of its partners, keeping the given order. */
function indexByPartner(families: readonly LayoutFamily[]): Map<string, LayoutFamily[]> {
  const byPartner = new Map<string, LayoutFamily[]>()
  for (const family of families) {
    for (const uid of family.partnerUids) {
      const existing = byPartner.get(uid)
      if (existing === undefined) {
        byPartner.set(uid, [family])
      } else {
        existing.push(family)
      }
    }
  }
  return byPartner
}

/** A box for one person who is in no family the walk can draw. */
function leafNode(item: Pending): LayoutNode {
  return blankNode(`person:${item.personUid}`, null, [item.personUid], [], item.depth)
}

/**
 * A box for one family, with the person the walk arrived at drawn first and
 * their partner beside them, so a reader's eye follows the line it came down.
 */
function familyNode(family: LayoutFamily, item: Pending, drawn: ReadonlySet<string>): LayoutNode {
  const others = family.partnerUids.filter((uid) => uid !== item.personUid)
  const people = [item.personUid, ...others]
  return blankNode(
    family.uid,
    family.uid,
    people,
    people.filter((uid) => drawn.has(uid)),
    item.depth,
  )
}

/** A node with its structure filled in and its geometry still at the origin. */
function blankNode(
  id: string,
  familyUid: string | null,
  personUids: string[],
  repeatUids: string[],
  depth: number,
): LayoutNode {
  return {
    id,
    familyUid,
    personUids,
    repeatUids,
    parentId: null,
    childIds: [],
    depth,
    x: 0,
    y: depth * LEVEL_STRIDE,
    width: boxWidth(personUids.length),
    height: NODE_HEIGHT,
    collapsed: false,
    hiddenCount: 0,
  }
}

/** Files a node under its parent, or as another root of the forest. */
function addNode(built: Built, node: LayoutNode, parentId: string | null): void {
  node.parentId = parentId
  built.nodes.push(node)
  built.byId.set(node.id, node)
  if (parentId === null) {
    built.roots.push(node.id)
    return
  }
  const parent = built.byId.get(parentId)
  if (parent !== undefined) {
    parent.childIds.push(node.id)
  }
}

/**
 * Counts, for every folded box, how many people the fold is actually hiding —
 * the descendants below it and the partners they brought, minus anyone who is
 * drawn elsewhere in the tree anyway. A fold that hides nobody says so with a
 * zero rather than tempting a reader to open an empty branch.
 */
function countHidden(
  built: Built,
  families: readonly LayoutFamily[],
  collapsed: ReadonlySet<string>,
): void {
  if (collapsed.size === 0) {
    return
  }
  const byPartner = indexByPartner(families)
  const byUID = new Map(families.map((family) => [family.uid, family]))
  for (const node of built.nodes) {
    if (!node.collapsed) {
      continue
    }
    const family = node.familyUid === null ? undefined : byUID.get(node.familyUid)
    if (family === undefined) {
      continue
    }
    node.hiddenCount = countBelow(family, byPartner, built.drawn)
  }
}

/** The people below one family that no box in the drawing contains. */
function countBelow(
  family: LayoutFamily,
  byPartner: ReadonlyMap<string, LayoutFamily[]>,
  drawn: ReadonlySet<string>,
): number {
  const seen = new Set<string>()
  const queue = [...family.childUids]
  // Appended to while walked, exactly as the drawing's own queue is.
  for (const uid of queue) {
    if (seen.has(uid)) {
      continue
    }
    seen.add(uid)
    for (const below of byPartner.get(uid) ?? []) {
      queue.push(...below.partnerUids, ...below.childUids)
    }
  }
  let hidden = 0
  for (const uid of seen) {
    if (!drawn.has(uid)) {
      hidden += 1
    }
  }
  return hidden
}

/**
 * Gives every box its coordinates. Each subtree is placed relative to its own
 * root, siblings are packed by contour so that no two boxes on any level come
 * closer than {@link SIBLING_GAP}, and every parent is then centred over its
 * children. The roots of the forest — a remarried root person makes more than
 * one — are packed with each other exactly the same way.
 */
function place(built: Built): void {
  const roots = built.roots.map((id) => placeSubtree(built, id))
  const offsets = packSiblings(roots)
  for (const [index, root] of roots.entries()) {
    assign(root, offsets[index])
  }
  shiftIntoView(built.nodes)
}

/** Places one subtree, returning it with its own contour and its children's. */
function placeSubtree(built: Built, id: string): Placed {
  const node = built.byId.get(id)
  if (node === undefined) {
    throw new Error(`familyLayout: unknown node ${id}`)
  }
  const children = node.childIds.map((childId) => placeSubtree(built, childId))
  const half = node.width / 2
  if (children.length === 0) {
    return { node, children, contour: { left: [-half], right: [half] }, offset: 0 }
  }
  const offsets = packSiblings(children)
  const centre = (offsets[0] + offsets[offsets.length - 1]) / 2
  for (const [index, child] of children.entries()) {
    child.offset = offsets[index] - centre
  }
  return { node, children, contour: contourOf(half, children), offset: 0 }
}

/**
 * Packs already-placed subtrees left to right, returning each one's centre. A
 * subtree is pushed right only as far as its own contour demands against the
 * ones before it, which is what a tidy layout means: two deep branches interlock
 * where they are narrow instead of both clearing the widest level of the other.
 */
function packSiblings(subtrees: readonly Placed[]): number[] {
  const offsets: number[] = []
  const acc: Contour = { left: [], right: [] }
  for (const subtree of subtrees) {
    let offset = 0
    const overlap = Math.min(acc.right.length, subtree.contour.left.length)
    for (let depth = 0; depth < overlap; depth += 1) {
      offset = Math.max(offset, acc.right[depth] + SIBLING_GAP - subtree.contour.left[depth])
    }
    offsets.push(offset)
    mergeContour(acc, subtree.contour, offset)
  }
  return offsets
}

/** Widens `acc` to also cover `contour` shifted right by `offset`. */
function mergeContour(acc: Contour, contour: Contour, offset: number): void {
  for (let depth = 0; depth < contour.left.length; depth += 1) {
    const left = contour.left[depth] + offset
    const right = contour.right[depth] + offset
    if (depth < acc.left.length) {
      acc.left[depth] = Math.min(acc.left[depth], left)
      acc.right[depth] = Math.max(acc.right[depth], right)
    } else {
      acc.left.push(left)
      acc.right.push(right)
    }
  }
}

/** The contour of a parent of half-width `half` over its centred children. */
function contourOf(half: number, children: readonly Placed[]): Contour {
  const below: Contour = { left: [], right: [] }
  for (const child of children) {
    mergeContour(below, child.contour, child.offset)
  }
  return { left: [-half, ...below.left], right: [half, ...below.right] }
}

/** Writes absolute coordinates into a placed subtree, given its centre. */
function assign(placed: Placed, centre: number): void {
  placed.node.x = centre - placed.node.width / 2
  for (const child of placed.children) {
    assign(child, centre + child.offset)
  }
}

/** Slides the whole drawing so its left edge sits at {@link TREE_PADDING}. */
function shiftIntoView(nodes: readonly LayoutNode[]): void {
  let minX = Infinity
  for (const node of nodes) {
    minX = Math.min(minX, node.x)
  }
  const shift = TREE_PADDING - minX
  for (const node of nodes) {
    node.x += shift
    node.y += TREE_PADDING
  }
}

/** The line from every box down to each of its children. */
function edgesOf(built: Built): LayoutEdge[] {
  const edges: LayoutEdge[] = []
  for (const node of built.nodes) {
    for (const childId of node.childIds) {
      const child = built.byId.get(childId)
      if (child === undefined) {
        continue
      }
      edges.push({
        parentId: node.id,
        childId,
        x1: node.x + node.width / 2,
        y1: node.y + node.height,
        x2: child.x + child.width / 2,
        y2: child.y,
      })
    }
  }
  return edges
}

/** The canvas the placed boxes need, with {@link TREE_PADDING} all round. */
function canvasOf(nodes: readonly LayoutNode[]): { width: number; height: number } {
  let maxX = 0
  let maxY = 0
  for (const node of nodes) {
    maxX = Math.max(maxX, node.x + node.width)
    maxY = Math.max(maxY, node.y + node.height)
  }
  return { width: maxX + TREE_PADDING, height: maxY + TREE_PADDING }
}

/**
 * The elbow from a parent's box to a child's, as an SVG path: straight down to
 * halfway between the two levels, across, and down again. Genealogy charts are
 * drawn with square corners rather than curves because the horizontal run is
 * what makes a sibship read as one row of children instead of a fan of unrelated
 * lines.
 */
export function edgePath(edge: LayoutEdge): string {
  const midY = (edge.y1 + edge.y2) / 2
  return `M ${edge.x1} ${edge.y1} V ${midY} H ${edge.x2} V ${edge.y2}`
}

/* ------------------------------------------------------------------------- *
 * The pedigree: the other direction, and the other shape.
 * ------------------------------------------------------------------------- */

/**
 * The deepest pedigree this module will draw. A binary pedigree doubles every
 * generation — 2, 4, 8, 16, 32 — so the bound is not a matter of taste: six
 * generations above the root are already sixty-four slots and eleven thousand
 * layout units of paper, which is the last width a reader can still find their
 * way around by panning. A caller asking for more gets this.
 */
export const MAX_PEDIGREE_GENERATIONS = 6

/**
 * One place in a pedigree, filled or not. An unknown grandmother is a slot with
 * no person in it rather than a missing box, because the gap is the whole point:
 * it is what tells a reader the library does not know, and what invites them to
 * say so.
 */
export interface PedigreeSlot {
  /** The slot's identity, and its place: the Ahnentafel number, `a1` at the root. */
  id: string
  /** Who stands here, or null for an ancestor nobody has recorded. */
  personUid: string | null
  /**
   * The known person one row below whose parent this slot is — which is the
   * subject a blank slot is filled *on*, since a parent is recorded on their
   * child. Null for the root, who is nobody's parent here.
   */
  childUid: string | null
  /** Generations above the root, the root itself being 0. */
  generation: number
  /**
   * Whether this person is already drawn in an earlier slot. Once cousins marry
   * the same ancestor stands on both sides of the pedigree — pedigree collapse —
   * and a chart draws them in both places, because both places are true; the
   * mark is what stops a reader counting one person as two.
   */
  repeat: boolean
  x: number
  y: number
  width: number
  height: number
}

/** The finished pedigree: its slots, its lines, and the canvas they need. */
export interface Pedigree {
  slots: PedigreeSlot[]
  edges: LayoutEdge[]
  /** The drawing's own size, padding included — the SVG's `viewBox`. */
  width: number
  height: number
}

/** What {@link layoutAncestors} is asked to draw. */
export interface PedigreeInput {
  /** The person the pedigree is climbed from, drawn at the bottom. */
  rootUid: string
  /** Every family box available; a person's parents are the family they are a child in. */
  families: readonly LayoutFamily[]
  /** How many generations above the root to draw; clamped into 1…{@link MAX_PEDIGREE_GENERATIONS}. */
  generations: number
}

/** A pedigree with nothing in it, which still has a canvas to be rendered into. */
export function emptyPedigree(): Pedigree {
  return { slots: [], edges: [], width: 2 * TREE_PADDING, height: 2 * TREE_PADDING }
}

/** One slot with its subtree, while the pedigree is still being measured. */
interface Branch {
  slot: PedigreeSlot
  /** The two slots above: father and mother, in the family's own order. Empty at a leaf. */
  parents: Branch[]
  /** The width of everything this branch spreads over, in layout units. */
  width: number
}

/** What the climb needs to know throughout, gathered once. */
interface PedigreeContext {
  /** The family each person is a child in — that family's partners are their parents. */
  parentsOf: Map<string, LayoutFamily>
  /** Everybody already given a slot, so a second appearance can be marked as one. */
  seen: Set<string>
  /** How many generations above the root are drawn. */
  generations: number
}

/**
 * Lays out the ancestors of `rootUid` as a pedigree: the root at the bottom and
 * each generation of parents on the row above, every known person carrying
 * **two** slots whether or not the library can fill them.
 *
 * This is not the tidy tree of {@link layoutDescendants} and deliberately so.
 * Descendants fan out unboundedly and have to be packed; ancestors are bounded
 * by construction (2, 4, 8, 16 …) and are read as a grid of generations, which
 * is why the design splits the page by direction rather than drawing one graph
 * both ways. A branch that stops early does not, however, reserve the empty
 * columns it would have filled: the drawing is laid out from the slots that
 * exist, so a pedigree with one long line in it is a narrow drawing rather than
 * a field of white.
 *
 * **Empty slots are the feature.** A person whose parents nobody recorded gets
 * two blank slots above them, each naming the child it belongs to, because a
 * parent is recorded *on their child* — that is what makes a gap clickable.
 *
 * **Pedigree collapse does not loop.** When cousins marry, the same ancestor is
 * reached along both sides; they are drawn in both places (a pedigree that hid
 * one of them would misstate the descent) with every appearance after the first
 * marked `repeat`. A person who is somehow their own ancestor terminates the
 * climb at the repeat instead of recursing, and the generation bound holds the
 * whole thing up regardless.
 */
export function layoutAncestors(input: PedigreeInput): Pedigree {
  const ctx: PedigreeContext = {
    parentsOf: indexByChild(input.families),
    seen: new Set(),
    generations: clampGenerations(input.generations),
  }
  const root = climb(input.rootUid, null, 1, 0, new Set(), ctx)
  const deepest = deepestGeneration(root)
  placeBranch(root, TREE_PADDING, deepest)
  const slots = inReadingOrder(root)
  return {
    slots,
    edges: pedigreeEdges(root),
    width: root.width + 2 * TREE_PADDING,
    height: (deepest + 1) * NODE_HEIGHT + deepest * LEVEL_GAP + 2 * TREE_PADDING,
  }
}

/** Bounds a requested depth into 1…{@link MAX_PEDIGREE_GENERATIONS}. */
function clampGenerations(generations: number): number {
  if (!Number.isFinite(generations) || generations < 1) {
    return 1
  }
  return Math.min(Math.floor(generations), MAX_PEDIGREE_GENERATIONS)
}

/**
 * Every family indexed by each of its children. The database keeps a person a
 * child in at most one family, which is what makes "my parents" a lookup rather
 * than a search — and what keeps this climb a walk instead of a graph traversal.
 */
function indexByChild(families: readonly LayoutFamily[]): Map<string, LayoutFamily> {
  const byChild = new Map<string, LayoutFamily>()
  for (const family of families) {
    for (const uid of family.childUids) {
      if (!byChild.has(uid)) {
        byChild.set(uid, family)
      }
    }
  }
  return byChild
}

/**
 * Builds one slot and, for a known person below the generation bound, the two
 * above it. `path` is the line of descent leading here, which is what stops a
 * person who is their own ancestor from being climbed for ever.
 */
function climb(
  personUid: string | null,
  childUid: string | null,
  ahnentafel: number,
  generation: number,
  path: ReadonlySet<string>,
  ctx: PedigreeContext,
): Branch {
  const repeat = personUid !== null && ctx.seen.has(personUid)
  if (personUid !== null) {
    ctx.seen.add(personUid)
  }
  const slot: PedigreeSlot = {
    id: `a${ahnentafel}`,
    personUid,
    childUid,
    generation,
    repeat,
    x: 0,
    y: 0,
    width: PERSON_WIDTH,
    height: NODE_HEIGHT,
  }
  // A blank slot has no knowable parents, and neither has one whose person is
  // already below on this very line of descent — which is where a cycle that
  // reached the tables would otherwise send the climb.
  const climbable = personUid !== null && generation < ctx.generations && !path.has(personUid)
  if (!climbable) {
    return { slot, parents: [], width: PERSON_WIDTH }
  }
  const family = ctx.parentsOf.get(personUid)
  const pair = parentPair(family)
  const deeper = new Set(path).add(personUid)
  const parents = pair.map((uid, index) =>
    climb(uid, personUid, ahnentafel * 2 + index, generation + 1, deeper, ctx),
  )
  return { slot, parents, width: parents[0].width + SIBLING_GAP + parents[1].width }
}

/**
 * The two parent slots of one person: the partners of the family they are a
 * child in, in that family's own order, padded to two. A lone-parent family
 * yields one person and one blank, which is the honest drawing of a father
 * nobody remembers — and one the reader can fill in.
 */
function parentPair(family: LayoutFamily | undefined): (string | null)[] {
  const partners = family?.partnerUids ?? []
  return [partners[0] ?? null, partners[1] ?? null]
}

/** The highest generation the climb actually reached. */
function deepestGeneration(branch: Branch): number {
  let deepest = branch.slot.generation
  for (const parent of branch.parents) {
    deepest = Math.max(deepest, deepestGeneration(parent))
  }
  return deepest
}

/**
 * Gives a branch and everything above it their coordinates. The parents are laid
 * side by side across the branch's own width and the child is centred **between
 * them** rather than over the band, so the two lines meeting at a couple are
 * symmetrical whichever side turned out to be the deeper one.
 */
function placeBranch(branch: Branch, left: number, deepest: number): void {
  branch.slot.y = (deepest - branch.slot.generation) * (NODE_HEIGHT + LEVEL_GAP) + TREE_PADDING
  if (branch.parents.length === 0) {
    branch.slot.x = left
    return
  }
  const [first, second] = branch.parents
  placeBranch(first, left, deepest)
  placeBranch(second, left + first.width + SIBLING_GAP, deepest)
  branch.slot.x = (first.slot.x + second.slot.x) / 2
}

/**
 * The slots in the order a reader meets them: the person the pedigree is about
 * first, then their parents, then the row above. It is also the tab order, and
 * a pedigree is read from the person outwards rather than from the oldest
 * ancestor down.
 */
function inReadingOrder(root: Branch): PedigreeSlot[] {
  const slots: PedigreeSlot[] = []
  const queue: Branch[] = [root]
  // Appended to while it is walked, which is what makes this breadth-first.
  for (const branch of queue) {
    slots.push(branch.slot)
    queue.push(...branch.parents)
  }
  return slots
}

/** The line from every slot down to the child whose parent it is. */
function pedigreeEdges(root: Branch): LayoutEdge[] {
  const edges: LayoutEdge[] = []
  const queue: Branch[] = [root]
  for (const branch of queue) {
    for (const parent of branch.parents) {
      edges.push({
        parentId: parent.slot.id,
        childId: branch.slot.id,
        x1: parent.slot.x + parent.slot.width / 2,
        y1: parent.slot.y + parent.slot.height,
        x2: branch.slot.x + branch.slot.width / 2,
        y2: branch.slot.y,
      })
      queue.push(parent)
    }
  }
  return edges
}
