import { describe, expect, it } from 'vitest'

import {
  boxWidth,
  cardCentre,
  COUPLE_GAP,
  edgePath,
  emptyLayout,
  type FamilyLayout,
  type LayoutFamily,
  type LayoutNode,
  layoutNetwork,
  LEVEL_GAP,
  LEVEL_STRIDE,
  NODE_HEIGHT,
  PERSON_WIDTH,
  SIBLING_GAP,
  TREE_PADDING,
} from './familyLayout'

/** A family box, spelled out with as little ceremony as a fixture allows. */
function family(uid: string, partners: string[], children: string[] = []): LayoutFamily {
  return { uid, partnerUids: partners, childUids: children }
}

/** Lays out a network rooted at `root`, the generations given as a plain record. */
function layout(
  root: string,
  generations: Record<string, number>,
  families: LayoutFamily[],
): FamilyLayout {
  return layoutNetwork({
    rootUid: root,
    generations: new Map(Object.entries(generations)),
    families,
  })
}

/** The node of a given id, failing the test rather than returning undefined. */
function node(drawing: FamilyLayout, id: string): LayoutNode {
  const found = drawing.nodes.find((candidate) => candidate.id === id)
  if (found === undefined) {
    throw new Error(`the drawing has no node ${id}`)
  }
  return found
}

/** Every box a person stands in. */
function boxesOf(drawing: FamilyLayout, personUid: string): LayoutNode[] {
  return drawing.nodes.filter((candidate) => candidate.personUids.includes(personUid))
}

/** The horizontal centre of a box, which is what the drawing lines up on. */
function centre(box: LayoutNode): number {
  return box.x + box.width / 2
}

/**
 * Asserts that no two boxes of one layer come closer than the sibling gap. This
 * is the invariant the coordinate step exists for, and it is worth checking on
 * every shape rather than only on the one that broke.
 */
function expectNoOverlap(drawing: FamilyLayout): void {
  const byLayer = new Map<number, LayoutNode[]>()
  for (const box of drawing.nodes) {
    byLayer.set(box.y, [...(byLayer.get(box.y) ?? []), box])
  }
  for (const layer of byLayer.values()) {
    const ordered = [...layer].sort((a, b) => a.x - b.x)
    for (let i = 1; i < ordered.length; i += 1) {
      const gap = ordered[i].x - (ordered[i - 1].x + ordered[i - 1].width)
      expect(gap, `gap between ${ordered[i - 1].id} and ${ordered[i].id}`).toBeGreaterThanOrEqual(
        SIBLING_GAP - 0.001,
      )
    }
  }
}

/**
 * The production shape the network exists for: Ludmila and Aleš with their son
 * Tomáš, Ludmila in a parentless sibling group with her sister Dagmar, and
 * Dagmar's daughter Petra. Five people, three generations' worth of lines, one
 * of them sideways.
 */
function kozaks(): FamilyLayout {
  return layout('tomas', { tomas: 0, ludmila: -1, ales: -1, dagmar: -1, petra: 0 }, [
    family('fm_couple', ['ludmila', 'ales'], ['tomas']),
    family('fm_sisters', [], ['ludmila', 'dagmar']),
    family('fm_dagmar', ['dagmar'], ['petra']),
  ])
}

describe('boxWidth', () => {
  it('is one card wide for a lone parent and two for a couple', () => {
    expect(boxWidth(1)).toBe(PERSON_WIDTH)
    expect(boxWidth(2)).toBe(2 * PERSON_WIDTH + COUPLE_GAP)
  })

  it('never collapses to nothing', () => {
    expect(boxWidth(0)).toBe(PERSON_WIDTH)
  })
})

describe('emptyLayout', () => {
  it('is a canvas with nothing on it', () => {
    const empty = emptyLayout()
    expect(empty.nodes).toEqual([])
    expect(empty.edges).toEqual([])
    expect(empty.width).toBe(2 * TREE_PADDING)
    expect(empty.height).toBe(2 * TREE_PADDING)
  })
})

describe('layoutNetwork', () => {
  it('draws the empty graph as an empty canvas', () => {
    expect(layout('nobody', {}, [])).toEqual(emptyLayout())
  })

  it('draws a person with no family at all as a single box', () => {
    const drawing = layout('solo', { solo: 0 }, [])
    expect(drawing.nodes).toHaveLength(1)
    const box = node(drawing, 'person:solo')
    expect(box).toMatchObject({ x: TREE_PADDING, y: TREE_PADDING, generation: 0 })
    expect(drawing.edges).toEqual([])
    expect(drawing.width).toBe(PERSON_WIDTH + 2 * TREE_PADDING)
    expect(drawing.height).toBe(NODE_HEIGHT + 2 * TREE_PADDING)
  })

  it('puts every box on the layer of its signed generation, the oldest on top', () => {
    const drawing = kozaks()
    // The oldest generation present is −1, so it is the top row — pushed down
    // by half a gap, because the sisters' bar hangs above it.
    const top = TREE_PADDING + LEVEL_GAP / 2
    expect(node(drawing, 'fm_couple').y).toBe(top)
    expect(node(drawing, 'fm_dagmar').y).toBe(top)
    expect(node(drawing, 'person:tomas').y).toBe(top + LEVEL_STRIDE)
    expect(node(drawing, 'person:petra').y).toBe(top + LEVEL_STRIDE)
    expect(node(drawing, 'fm_couple').generation).toBe(-1)
    expect(drawing.height).toBe(top + LEVEL_STRIDE + NODE_HEIGHT + TREE_PADDING)
  })

  it('reaches sideways: the aunt and the cousin are drawn beside the parents', () => {
    const drawing = kozaks()
    const drawn = drawing.nodes.flatMap((box) => box.personUids).sort()
    expect(drawn).toEqual(['ales', 'dagmar', 'ludmila', 'petra', 'tomas'])
    expectNoOverlap(drawing)
  })

  it('draws a couple as one box, their children hanging off the bar between them', () => {
    const drawing = kozaks()
    const couple = node(drawing, 'fm_couple')
    expect([...couple.personUids].sort()).toEqual(['ales', 'ludmila'])
    expect(couple.width).toBe(boxWidth(2))
    const edge = drawing.edges.find((candidate) => candidate.childId === 'tomas')
    expect(edge).toMatchObject({
      parentId: 'fm_couple',
      x1: centre(couple),
      y1: couple.y + NODE_HEIGHT,
      x2: centre(node(drawing, 'person:tomas')),
      y2: node(drawing, 'person:tomas').y,
    })
  })

  it('turns a couple towards the partner’s own family', () => {
    const drawing = kozaks()
    const couple = node(drawing, 'fm_couple')
    const dagmar = node(drawing, 'fm_dagmar')
    // Ludmila is drawn on the side of her sister, so the sibling bar does not
    // have to run across Aleš to reach her.
    const ludmilaSide = cardCentre(couple, 'ludmila') < cardCentre(couple, 'ales') ? -1 : 1
    const sisterSide = centre(dagmar) < centre(couple) ? -1 : 1
    expect(ludmilaSide).toBe(sisterSide)
  })

  it('joins a parentless sibling group with one bar just above the siblings', () => {
    const drawing = kozaks()
    const bar = drawing.edges.filter((edge) => edge.parentId === 'fm_sisters')
    expect(bar.map((edge) => edge.childId).sort()).toEqual(['dagmar', 'ludmila'])
    const couple = node(drawing, 'fm_couple')
    const dagmar = node(drawing, 'fm_dagmar')
    const [first, second] = bar
    // One shared start, halfway between the two sisters' cards, in the gap above
    // their row — a sibling group has no box to hang off.
    expect(first.x1).toBe(second.x1)
    expect(first.x1).toBe((cardCentre(couple, 'ludmila') + cardCentre(dagmar, 'dagmar')) / 2)
    expect(first.y1).toBe(couple.y - LEVEL_GAP / 2)
    // Over the top row the bar still lands on the canvas, inside its padding.
    expect(first.y1).toBe(TREE_PADDING)
    expect(bar.find((edge) => edge.childId === 'ludmila')?.x2).toBe(cardCentre(couple, 'ludmila'))
  })

  it('packs a wide sibship without overlap and centres the parents over it', () => {
    const drawing = layout('p', { p: 0, q: 0, a: 1, b: 1, c: 1, d: 1 }, [
      family('f', ['p', 'q'], ['a', 'b', 'c', 'd']),
    ])
    expectNoOverlap(drawing)
    const kids = ['a', 'b', 'c', 'd'].map((uid) => node(drawing, `person:${uid}`))
    // The order given is the order drawn.
    expect(kids.map((kid) => kid.x)).toEqual([...kids.map((kid) => kid.x)].sort((x, y) => x - y))
    const middle = (centre(kids[0]) + centre(kids[3])) / 2
    expect(centre(node(drawing, 'f'))).toBeCloseTo(middle, 6)
  })

  it('never lets two boxes of one layer overlap, however hard they pull', () => {
    // Three couples whose children all married into one generation, every one
    // pulling towards the same place.
    const drawing = layout(
      'x1',
      { a1: 0, a2: 0, b1: 0, b2: 0, c1: 0, c2: 0, x1: 1, x2: 1, y1: 1, y2: 1, z1: 1, z2: 1, k: 2 },
      [
        family('fa', ['a1', 'a2'], ['x1', 'y1']),
        family('fb', ['b1', 'b2'], ['x2', 'z1']),
        family('fc', ['c1', 'c2'], ['y2', 'z2']),
        family('fx', ['x1', 'x2'], ['k']),
        family('fy', ['y1', 'y2']),
        family('fz', ['z1', 'z2']),
      ],
    )
    expectNoOverlap(drawing)
    expect(drawing.nodes).toHaveLength(7)
  })

  it('lays out a cycle — a cousin marriage — without drawing anybody twice', () => {
    // Grandparents g1+g2; their children s and t each marry out; s's son u
    // marries t's daughter v, and they have w. Every person is reachable from w
    // along two paths.
    const drawing = layout(
      'w',
      { g1: -3, g2: -3, s: -2, sp: -2, t: -2, tp: -2, u: -1, v: -1, w: 0 },
      [
        family('fg', ['g1', 'g2'], ['s', 't']),
        family('fs', ['s', 'sp'], ['u']),
        family('ft', ['t', 'tp'], ['v']),
        family('fu', ['u', 'v'], ['w']),
      ],
    )
    for (const uid of ['g1', 'g2', 's', 'sp', 't', 'tp', 'u', 'v', 'w']) {
      expect(boxesOf(drawing, uid), uid).toHaveLength(1)
    }
    expect(drawing.nodes.every((box) => box.repeatUids.length === 0)).toBe(true)
    expect(drawing.edges).toHaveLength(5)
    expectNoOverlap(drawing)
  })

  it('draws a remarriage as two boxes sharing a marked partner', () => {
    const drawing = layout('m', { m: 0, w1: 0, w2: 0, a: 1, b: 1 }, [
      family('f1', ['m', 'w1'], ['a']),
      family('f2', ['m', 'w2'], ['b']),
    ])
    const boxes = boxesOf(drawing, 'm')
    expect(boxes.map((box) => box.id).sort()).toEqual(['f1', 'f2'])
    expect(boxes.filter((box) => box.repeatUids.includes('m'))).toHaveLength(1)
    // The root's own box is drawn first, and is the one the repeat is not in.
    expect(node(drawing, 'f1').repeatUids).toEqual([])
    expectNoOverlap(drawing)
  })

  it('draws the whole payload even when it is not one component', () => {
    const drawing = layout('a', { a: 0, stray: 0 }, [])
    expect(drawing.nodes.map((box) => box.id).sort()).toEqual(['person:a', 'person:stray'])
    expectNoOverlap(drawing)
  })

  it('ignores a family member the payload gave no generation for', () => {
    const drawing = layout('a', { a: 0, c: 1 }, [family('f', ['a', 'ghost'], ['c', 'nobody'])])
    expect(node(drawing, 'f').personUids).toEqual(['a'])
    expect(drawing.edges.map((edge) => edge.childId)).toEqual(['c'])
  })

  it('is deterministic: the same family comes out the same', () => {
    expect(kozaks()).toEqual(kozaks())
  })

  it('starts a drawing with no sibling bar on top right at the padding', () => {
    const drawing = layout('c', { p: -1, c: 0 }, [family('f', ['p'], ['c'])])
    expect(node(drawing, 'f').y).toBe(TREE_PADDING)
  })

  it('sizes the canvas around everything it drew', () => {
    const drawing = kozaks()
    for (const box of drawing.nodes) {
      expect(box.x).toBeGreaterThanOrEqual(TREE_PADDING)
      expect(box.x + box.width).toBeLessThanOrEqual(drawing.width - TREE_PADDING + 0.001)
      expect(box.y + box.height).toBeLessThanOrEqual(drawing.height - TREE_PADDING)
    }
    expect(Math.min(...drawing.nodes.map((box) => box.x))).toBe(TREE_PADDING)
  })
})

describe('edgePath', () => {
  it('drops, runs across and drops again', () => {
    expect(edgePath({ parentId: 'p', childId: 'c', x1: 100, y1: 60, x2: 40, y2: 120 })).toBe(
      'M 100 60 V 90 H 40 V 120',
    )
  })
})
