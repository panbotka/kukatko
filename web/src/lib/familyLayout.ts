/**
 * The pure geometry of the family tree: people, their generations and the
 * families tying them together in, coordinates out. No DOM, no React, no
 * fetching — which is the point, because this is where the risk of the whole
 * page lives and a pure function is what makes it unit-testable. The SVG
 * renderer on top of it (`components/people/FamilyNetworkCanvas`) only paints
 * what this module decided.
 *
 * The page draws **everybody the family links reach** from one person — up, down
 * and sideways through a sibling group to aunts and cousins — so what it draws is
 * a graph, not a tree: once cousins marry it has cycles, and it grows upwards as
 * readily as downwards. {@link layoutNetwork} is therefore a **layered**
 * (Sugiyama-style) layout rather than a tidy tree or a pedigree:
 *
 * 1. the **layer** is given: it is the signed generation the server computed, so
 *    a parent sits one row above their child whichever way the walk found them;
 * 2. the **order** within a layer is seeded breadth-first from the root and then
 *    improved by a few barycentre sweeps, a box moving towards the mean of the
 *    boxes it is tied to — its parents, its children, its siblings and the other
 *    marriage of a remarried partner — which is what keeps a family's lines short
 *    and mostly uncrossed;
 * 3. the **coordinates** are packed left to right, never closer than
 *    {@link SIBLING_GAP}, and nudged towards that same barycentre, a box with more
 *    ties pulling harder — solved exactly, per layer, as a weighted isotonic
 *    regression, so the nudging can never make two boxes overlap.
 *
 * **A couple is one box**, exactly as it always was: two partners side by side
 * with the bar that joins them, and their children hanging off the bar.
 *
 * No dependency is taken. The frontend's dependency list is deliberately lean —
 * leaflet is its only visualisation library — and the design chose this
 * hand-written pure layout over d3, dagre or elkjs on purpose.
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
export const LEVEL_STRIDE = NODE_HEIGHT + LEVEL_GAP

/**
 * How many barycentre sweeps the layout makes, each one down the layers and back
 * up. A handful is what the method needs to settle — the families of a village
 * archive are a few generations deep — and a fixed number keeps the drawing
 * deterministic: the same family always comes out the same.
 */
const SWEEPS = 6

/**
 * One family as the layout needs it: who is partnered in it and who its children
 * are. A family with no partners at all is a **sibling group** — two sisters whose
 * parents nobody recorded — and draws no box of its own, only the bar that joins
 * the siblings.
 */
export interface LayoutFamily {
  /** The family row's UID, which is also the box's node id. */
  uid: string
  /** The partners in it: two for a couple, one for a lone parent, none for a sibling group. */
  partnerUids: readonly string[]
  /** Its children, in the order they should be drawn. */
  childUids: readonly string[]
}

/** What {@link layoutNetwork} is asked to draw. */
export interface NetworkInput {
  /** The person the drawing is about; the breadth-first seed starts from them. */
  rootUid: string
  /**
   * Everybody to draw, with the signed generation the server gave them: the root
   * 0, a parent −1, a child +1, a partner the generation of the one they married.
   */
  generations: ReadonlyMap<string, number>
  /** Every family tying them together. */
  families: readonly LayoutFamily[]
}

/**
 * One box of the drawing: a couple, a lone parent, or a person who is nobody's
 * partner, with its place on the page already decided. `x`/`y` are the box's
 * top-left corner.
 */
export interface LayoutNode {
  /** The box's identity: the family's UID, or `person:<uid>` for a lone person. */
  id: string
  /** The family drawn here, or null for a person who is in no box as a partner. */
  familyUid: string | null
  /** The people in the box, left to right. One or two. */
  personUids: string[]
  /**
   * The people here who are already drawn in an earlier box — the second box of
   * a remarriage repeats the partner both families share. The renderer marks
   * them, so a reader is not left counting the same person twice.
   */
  repeatUids: string[]
  /** The layer: the signed generation of the people in the box. */
  generation: number
  x: number
  y: number
  width: number
  height: number
}

/**
 * One line from a family down to one of its children, as four plain numbers. For
 * a couple or a lone parent it starts at the foot of their box; for a sibling
 * group, which has no box, it starts just above the children, so the group
 * reads as one bar with a short stub where the unrecorded parents would hang.
 */
export interface LayoutEdge {
  /** The family the line comes from. */
  parentId: string
  /** The child it leads to — a person, since a child is drawn wherever their own box is. */
  childId: string
  /** Where the line starts. */
  x1: number
  y1: number
  /** Where it ends: the top centre of the child's own card. */
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

/** The horizontal centre of one person's card inside the box that holds them. */
export function cardCentre(node: LayoutNode, personUid: string): number {
  const index = Math.max(node.personUids.indexOf(personUid), 0)
  return node.x + index * (PERSON_WIDTH + COUPLE_GAP) + PERSON_WIDTH / 2
}

/** The boxes, before and while they are placed, with what ties them together. */
interface Graph {
  nodes: LayoutNode[]
  /** The box each person is drawn in first — where a line to them lands. */
  primary: Map<string, LayoutNode>
  /** Every box a box is tied to, once per tie, so a double tie pulls twice. */
  ties: Map<string, LayoutNode[]>
}

/**
 * Lays out everybody in `input.generations` as a layered drawing of family boxes.
 *
 * Every family with a partner is **one box**; a person partnered in no family
 * gets a box of their own; a partnerless sibling group gets no box, only the bar
 * joining its children. A person is drawn once — except the partner a remarriage
 * shares, who stands in both marriages' boxes (two marriages are two boxes
 * because their children hang off different couples), marked as a repeat in all
 * but the first. A cycle — a cousin marriage — therefore closes instead of
 * duplicating anybody.
 *
 * The order of `families` and of each family's `childUids` seeds the order within
 * a layer, so a caller who wants children by birth year sorts them before calling
 * rather than asking this function to know about birthdays.
 */
export function layoutNetwork(input: NetworkInput): FamilyLayout {
  if (input.generations.size === 0) {
    return emptyLayout()
  }
  const graph = buildGraph(input)
  const layers = layersOf(graph.nodes)
  for (const layer of layers) {
    packLayer(layer)
  }
  for (let sweep = 0; sweep < SWEEPS; sweep += 1) {
    for (const layer of layers) {
      settleLayer(layer, graph.ties)
    }
    for (const layer of [...layers].reverse()) {
      settleLayer(layer, graph.ties)
    }
  }
  faceParents(graph, input.families)
  moveIntoView(graph, input.families)
  return {
    nodes: graph.nodes,
    edges: edgesOf(graph, input.families),
    ...canvasOf(graph.nodes),
  }
}

/** Every family indexed by each person in it, as a partner or as a child, in order. */
function indexByPerson(families: readonly LayoutFamily[]): Map<string, LayoutFamily[]> {
  const byPerson = new Map<string, LayoutFamily[]>()
  for (const family of families) {
    for (const uid of [...family.partnerUids, ...family.childUids]) {
      const existing = byPerson.get(uid)
      if (existing === undefined) {
        byPerson.set(uid, [family])
      } else if (!existing.includes(family)) {
        existing.push(family)
      }
    }
  }
  return byPerson
}

/**
 * Builds the boxes breadth-first from the root, which seeds the order within a
 * layer — the root's own family first, the relatives it leads to after — and
 * then the ties the sweeps pull along. Anybody the walk from the root cannot
 * reach (a payload that is not one component) is started from in turn, so nobody
 * given is left undrawn.
 */
function buildGraph(input: NetworkInput): Graph {
  const known = (uid: string) => input.generations.has(uid)
  const families = input.families.map((family) => ({
    uid: family.uid,
    partnerUids: family.partnerUids.filter(known),
    childUids: family.childUids.filter(known),
  }))
  const byPerson = indexByPerson(families)
  const graph: Graph = { nodes: [], primary: new Map(), ties: new Map() }
  const seen = new Set<string>()
  const opened = new Set<string>()
  const starts = [input.rootUid, ...input.generations.keys()].filter(known)
  for (const start of starts) {
    if (seen.has(start)) {
      continue
    }
    seen.add(start)
    const queue = [start]
    // The queue is appended to while it is walked, which an array iterator
    // handles: it re-reads the length on every step. That is what makes this
    // breadth-first.
    for (const person of queue) {
      for (const family of byPerson.get(person) ?? []) {
        if (opened.has(family.uid)) {
          continue
        }
        opened.add(family.uid)
        if (family.partnerUids.length > 0) {
          addBox(graph, family, person, input.generations)
        }
        for (const next of [...family.partnerUids, ...family.childUids]) {
          if (!seen.has(next)) {
            seen.add(next)
            queue.push(next)
          }
        }
      }
      if (!graph.primary.has(person)) {
        addLoneBox(graph, person, input.generations.get(person) ?? 0)
      }
    }
  }
  tieUp(graph, families)
  return graph
}

/**
 * A box for one family, with the person the walk arrived at drawn first and
 * their partner beside them. The box sits on its partners' layer — on the upper
 * of the two, should a cycle have given them different ones.
 */
function addBox(
  graph: Graph,
  family: LayoutFamily,
  arrivedAt: string,
  generations: ReadonlyMap<string, number>,
): void {
  const people = family.partnerUids.includes(arrivedAt)
    ? [arrivedAt, ...family.partnerUids.filter((uid) => uid !== arrivedAt)]
    : [...family.partnerUids]
  const generation = Math.min(...people.map((uid) => generations.get(uid) ?? 0))
  const node = blankNode(family.uid, family.uid, people, generation)
  node.repeatUids = people.filter((uid) => graph.primary.has(uid))
  graph.nodes.push(node)
  for (const uid of people) {
    if (!graph.primary.has(uid)) {
      graph.primary.set(uid, node)
    }
  }
}

/** A box for a person who is partnered in no family: a child, a sibling, the root alone. */
function addLoneBox(graph: Graph, personUid: string, generation: number): void {
  const node = blankNode(`person:${personUid}`, null, [personUid], generation)
  graph.nodes.push(node)
  graph.primary.set(personUid, node)
}

/** A node with its structure filled in and its geometry still at the origin. */
function blankNode(
  id: string,
  familyUid: string | null,
  personUids: string[],
  generation: number,
): LayoutNode {
  return {
    id,
    familyUid,
    personUids,
    repeatUids: [],
    generation,
    x: 0,
    y: 0,
    width: boxWidth(personUids.length),
    height: NODE_HEIGHT,
  }
}

/**
 * Records what pulls on what: a family's box and each child's box pull on each
 * other, the children of a sibling group pull on each other (they have no box to
 * gather under), and the boxes of a remarriage pull on each other through the
 * partner they share.
 */
function tieUp(graph: Graph, families: readonly LayoutFamily[]): void {
  const byId = new Map(graph.nodes.map((node) => [node.id, node]))
  const tie = (a: LayoutNode | undefined, b: LayoutNode | undefined) => {
    if (a === undefined || b === undefined || a === b) {
      return
    }
    for (const [from, to] of [
      [a, b],
      [b, a],
    ]) {
      const existing = graph.ties.get(from.id)
      if (existing === undefined) {
        graph.ties.set(from.id, [to])
      } else {
        existing.push(to)
      }
    }
  }
  for (const family of families) {
    const children = family.childUids.map((uid) => graph.primary.get(uid))
    const box = byId.get(family.uid)
    if (box !== undefined) {
      for (const child of children) {
        tie(box, child)
      }
      continue
    }
    for (const [index, child] of children.entries()) {
      for (const sibling of children.slice(index + 1)) {
        tie(child, sibling)
      }
    }
  }
  for (const node of graph.nodes) {
    for (const uid of node.repeatUids) {
      tie(node, graph.primary.get(uid))
    }
  }
}

/** The boxes grouped by generation, oldest layer first, each in seed order. */
function layersOf(nodes: readonly LayoutNode[]): LayoutNode[][] {
  const byGeneration = new Map<number, LayoutNode[]>()
  for (const node of nodes) {
    const layer = byGeneration.get(node.generation)
    if (layer === undefined) {
      byGeneration.set(node.generation, [node])
    } else {
      layer.push(node)
    }
  }
  return [...byGeneration.entries()].sort(([a], [b]) => a - b).map(([, layer]) => layer)
}

/** Packs one layer tightly from zero, in its current order. */
function packLayer(layer: readonly LayoutNode[]): void {
  let x = 0
  for (const node of layer) {
    node.x = x
    x += node.width + SIBLING_GAP
  }
}

/** The horizontal centre of a box. */
function centreOf(node: LayoutNode): number {
  return node.x + node.width / 2
}

/**
 * One step of a sweep over one layer: every box's target is the mean centre of
 * the boxes it is tied to (its own centre when it is tied to nothing), the layer
 * is re-ordered by target, and the boxes are then placed as near their targets as
 * {@link SIBLING_GAP} allows, a box with more ties weighing more.
 */
function settleLayer(layer: LayoutNode[], ties: ReadonlyMap<string, LayoutNode[]>): void {
  const targets = new Map<LayoutNode, { centre: number; weight: number }>()
  for (const node of layer) {
    const tied = ties.get(node.id) ?? []
    const centre =
      tied.length === 0
        ? centreOf(node)
        : tied.reduce((sum, other) => sum + centreOf(other), 0) / tied.length
    targets.set(node, { centre, weight: 1 + tied.length })
  }
  const target = (node: LayoutNode) => targets.get(node) ?? { centre: centreOf(node), weight: 1 }
  // Array.prototype.sort is stable, so boxes with equal targets keep the order
  // they had — the breadth-first seed, or the previous sweep's answer.
  layer.sort((a, b) => target(a).centre - target(b).centre)
  placeNearest(
    layer,
    layer.map((node) => target(node)),
  )
}

/** A run of boxes that have been pushed against each other and move as one. */
interface Block {
  /** The weighted mean of the members' shifted targets. */
  value: number
  weight: number
  count: number
}

/**
 * Places a layer's boxes, in their given order, so that no two come closer than
 * {@link SIBLING_GAP} and the weighted squared distance of every centre from its
 * target is as small as it can be.
 *
 * Subtracting from each target the least distance its box can sit from the
 * first box turns the spacing constraint into "non-decreasing", which makes this
 * a weighted isotonic regression: solved exactly by pooling adjacent violators —
 * a box that wants to be left of its neighbour is merged with it into a block
 * that sits at the pair's weighted mean. That is the "nudge towards the
 * barycentre by priority" of the design, with overlap ruled out by construction
 * rather than repaired after the fact.
 */
function placeNearest(
  layer: readonly LayoutNode[],
  targets: readonly { centre: number; weight: number }[],
): void {
  const offsets: number[] = []
  for (const [index, node] of layer.entries()) {
    const previous = index === 0 ? undefined : layer[index - 1]
    offsets.push(
      previous === undefined
        ? 0
        : offsets[index - 1] + previous.width / 2 + SIBLING_GAP + node.width / 2,
    )
  }
  const blocks: Block[] = []
  for (const [index, target] of targets.entries()) {
    blocks.push({ value: target.centre - offsets[index], weight: target.weight, count: 1 })
    for (;;) {
      const last = blocks[blocks.length - 1]
      const before = blocks.length > 1 ? blocks[blocks.length - 2] : undefined
      if (before === undefined || before.value <= last.value) {
        break
      }
      blocks.splice(blocks.length - 2, 2, {
        value:
          (before.value * before.weight + last.value * last.weight) / (before.weight + last.weight),
        weight: before.weight + last.weight,
        count: before.count + last.count,
      })
    }
  }
  let index = 0
  for (const block of blocks) {
    for (let member = 0; member < block.count; member += 1) {
      const node = layer[index]
      node.x = block.value + offsets[index] - node.width / 2
      index += 1
    }
  }
}

/**
 * Turns each couple to face their own parents: the partner whose parents stand
 * further left is drawn on the left, so the line from each set of parents drops
 * onto their own child without crossing the other's. A partner with no parents in
 * the drawing looks towards their siblings instead, and one with neither leaves
 * the couple as it was.
 */
function faceParents(graph: Graph, families: readonly LayoutFamily[]): void {
  const byId = new Map(graph.nodes.map((node) => [node.id, node]))
  const anchor = new Map<string, number>()
  for (const family of families) {
    const box = byId.get(family.uid)
    for (const child of family.childUids) {
      if (box !== undefined) {
        anchor.set(child, centreOf(box))
        continue
      }
      const siblings = family.childUids
        .filter((uid) => uid !== child)
        .map((uid) => graph.primary.get(uid))
        .filter((node): node is LayoutNode => node !== undefined)
      if (siblings.length > 0) {
        anchor.set(child, siblings.reduce((sum, node) => sum + centreOf(node), 0) / siblings.length)
      }
    }
  }
  for (const node of graph.nodes) {
    if (node.personUids.length !== 2) {
      continue
    }
    const centre = centreOf(node)
    const [left, right] = node.personUids
    if ((anchor.get(left) ?? centre) > (anchor.get(right) ?? centre)) {
      node.personUids = [right, left]
    }
  }
}

/**
 * Slides the drawing so its left edge sits at {@link TREE_PADDING}, and gives
 * every box its row: the oldest generation on top, one {@link LEVEL_STRIDE} per
 * generation down. A sibling group on the top row hangs its bar in the gap
 * above it, so that row is pushed down by half a gap to keep the bar on the
 * canvas.
 */
function moveIntoView(graph: Graph, families: readonly LayoutFamily[]): void {
  let minX = Infinity
  let minGeneration = Infinity
  for (const node of graph.nodes) {
    minX = Math.min(minX, node.x)
    minGeneration = Math.min(minGeneration, node.generation)
  }
  const boxed = new Set(graph.nodes.map((node) => node.familyUid))
  const barOnTop = families.some(
    (family) =>
      !boxed.has(family.uid) &&
      family.childUids.some((uid) => graph.primary.get(uid)?.generation === minGeneration),
  )
  const top = TREE_PADDING + (barOnTop ? LEVEL_GAP / 2 : 0)
  for (const node of graph.nodes) {
    node.x += TREE_PADDING - minX
    node.y = (node.generation - minGeneration) * LEVEL_STRIDE + top
  }
}

/**
 * The line from every family to each of its children, landing on the child's own
 * card — which, in a couple's box, is one half of it, not the middle.
 */
function edgesOf(graph: Graph, families: readonly LayoutFamily[]): LayoutEdge[] {
  const byId = new Map(graph.nodes.map((node) => [node.id, node]))
  const edges: LayoutEdge[] = []
  for (const family of families) {
    const heads = family.childUids.flatMap((uid) => {
      const node = graph.primary.get(uid)
      return node === undefined ? [] : [{ uid, x: cardCentre(node, uid), y: node.y }]
    })
    if (heads.length === 0) {
      continue
    }
    const box = byId.get(family.uid)
    const groupX = heads.reduce((sum, head) => sum + head.x, 0) / heads.length
    for (const head of heads) {
      edges.push({
        parentId: family.uid,
        childId: head.uid,
        x1: box === undefined ? groupX : centreOf(box),
        y1: box === undefined ? head.y - LEVEL_GAP / 2 : box.y + box.height,
        x2: head.x,
        y2: head.y,
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
