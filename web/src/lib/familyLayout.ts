/**
 * The pure geometry of the descendants family tree: family boxes in, coordinates
 * out. No DOM, no React, no fetching — which is the point, because this is where
 * the risk of the whole page lives and a pure function is what makes it
 * unit-testable. The SVG renderer on top of it (`pages/FamilyTreePage.tsx`) only
 * paints what this module decided.
 *
 * It is a classic tidy tree (Reingold–Tilford): each subtree is laid out on its
 * own, subtrees are packed against each other by their **contours** — the
 * leftmost and rightmost edge at every level — and a parent is then centred over
 * its children. That is what keeps a deep narrow branch from being pushed halfway
 * across the page by a shallow wide one, and what makes the drawing readable at
 * all once a family has four generations in it.
 *
 * Descendants are a genuine tree only once **a couple is one box**. That is the
 * whole reason the design splits the two directions: a person is a child in at
 * most one family (the database enforces it), so the family boxes hang off each
 * other in a tree, while a pedigree upwards is a different shape and a different
 * layout. This module draws the downward half.
 *
 * No dependency is taken for it. The frontend's dependency list is deliberately
 * lean — leaflet is its only visualisation library — and this is two hundred
 * lines of arithmetic, not a reason to add d3, dagre or elkjs.
 */

/** The width of one person's card inside a box, in layout units (≈ CSS px). */
export const PERSON_WIDTH = 176

/** The height of a box, which is one person's card tall whoever is in it. */
export const NODE_HEIGHT = 60

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
