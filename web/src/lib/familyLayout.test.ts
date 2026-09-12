import { describe, expect, it } from 'vitest'

import {
  boxWidth,
  COUPLE_GAP,
  edgePath,
  emptyLayout,
  emptyPedigree,
  type FamilyLayout,
  type LayoutFamily,
  type LayoutNode,
  layoutAncestors,
  layoutDescendants,
  LEVEL_GAP,
  NODE_HEIGHT,
  type Pedigree,
  type PedigreeSlot,
  PERSON_WIDTH,
  SIBLING_GAP,
  TREE_PADDING,
} from './familyLayout'

/** A family box, spelled out with as little ceremony as a fixture allows. */
function family(uid: string, partners: string[], children: string[] = []): LayoutFamily {
  return { uid, partnerUids: partners, childUids: children }
}

/** The node of a given id, failing the test rather than returning undefined. */
function node(layout: FamilyLayout, id: string): LayoutNode {
  const found = layout.nodes.find((candidate) => candidate.id === id)
  if (found === undefined) {
    throw new Error(`the drawing has no node ${id}`)
  }
  return found
}

/** The horizontal centre of a box, which is what the drawing lines up on. */
function centre(box: LayoutNode): number {
  return box.x + box.width / 2
}

/**
 * Asserts that no two boxes on one level come closer than the sibling gap. This
 * is the invariant the whole tidy layout exists for, and it is worth checking
 * on every shape rather than only on the one that broke.
 */
function expectNoOverlap(layout: FamilyLayout): void {
  const byDepth = new Map<number, LayoutNode[]>()
  for (const box of layout.nodes) {
    byDepth.set(box.depth, [...(byDepth.get(box.depth) ?? []), box])
  }
  for (const level of byDepth.values()) {
    const ordered = [...level].sort((a, b) => a.x - b.x)
    for (let i = 1; i < ordered.length; i += 1) {
      const gap = ordered[i].x - (ordered[i - 1].x + ordered[i - 1].width)
      expect(gap, `gap between ${ordered[i - 1].id} and ${ordered[i].id}`).toBeGreaterThanOrEqual(
        SIBLING_GAP - 0.001,
      )
    }
  }
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
    const layout = emptyLayout()
    expect(layout.nodes).toEqual([])
    expect(layout.edges).toEqual([])
    expect(layout.width).toBe(2 * TREE_PADDING)
  })
})

describe('layoutDescendants', () => {
  it('draws a person with no family at all as a single box', () => {
    const layout = layoutDescendants({ rootUid: 'a', families: [] })

    expect(layout.nodes).toHaveLength(1)
    expect(layout.nodes[0].personUids).toEqual(['a'])
    expect(layout.nodes[0].familyUid).toBeNull()
    expect(layout.nodes[0].x).toBe(TREE_PADDING)
    expect(layout.nodes[0].y).toBe(TREE_PADDING)
    expect(layout.edges).toEqual([])
  })

  it('stacks a straight line of generations on one vertical axis', () => {
    const layout = layoutDescendants({
      rootUid: 'a',
      families: [
        family('f1', ['a', 'a2'], ['b']),
        family('f2', ['b', 'b2'], ['c']),
        family('f3', ['c', 'c2']),
      ],
    })

    expect(layout.nodes.map((box) => box.id)).toEqual(['f1', 'f2', 'f3'])
    expect(layout.nodes.map((box) => box.depth)).toEqual([0, 1, 2])
    // Each parent is centred over its only child, so the whole line shares one axis.
    expect(centre(node(layout, 'f2'))).toBeCloseTo(centre(node(layout, 'f1')))
    expect(centre(node(layout, 'f3'))).toBeCloseTo(centre(node(layout, 'f1')))
    // And every generation sits one stride below the previous one.
    expect(node(layout, 'f2').y - node(layout, 'f1').y).toBe(
      node(layout, 'f3').y - node(layout, 'f2').y,
    )
    expect(layout.edges).toHaveLength(2)
    expectNoOverlap(layout)
  })

  it('spreads a wide sibship in order and centres the parents over it', () => {
    const children = ['c1', 'c2', 'c3', 'c4', 'c5']
    const layout = layoutDescendants({
      rootUid: 'a',
      families: [family('f1', ['a', 'a2'], children)],
    })

    expect(layout.nodes).toHaveLength(6)
    const boxes = children.map((uid) => node(layout, `person:${uid}`))
    // Drawn left to right in the order they were given.
    for (let i = 1; i < boxes.length; i += 1) {
      expect(boxes[i].x).toBeGreaterThan(boxes[i - 1].x)
    }
    const parents = node(layout, 'f1')
    expect(centre(parents)).toBeCloseTo((centre(boxes[0]) + centre(boxes[4])) / 2)
    expectNoOverlap(layout)
  })

  it('interlocks a deep narrow branch with a shallow wide one', () => {
    const layout = layoutDescendants({
      rootUid: 'a',
      families: [
        family('f1', ['a'], ['b', 'c']),
        // b's line runs three generations down and stays one box wide.
        family('f2', ['b'], ['b1']),
        family('f3', ['b1'], ['b2']),
        // c's line is one generation and four children wide.
        family('f4', ['c'], ['c1', 'c2', 'c3', 'c4']),
      ],
    })

    expectNoOverlap(layout)
    // The deep branch is only pushed aside by what is actually beside it: the
    // bottom of b's line clears c's *box*, not c's four children.
    expect(centre(node(layout, 'f3'))).toBeLessThan(node(layout, 'person:c1').x)
  })

  it('draws a remarriage as two boxes sharing a marked partner', () => {
    const layout = layoutDescendants({
      rootUid: 'a',
      families: [family('f1', ['a', 'x'], ['c1']), family('f2', ['a', 'y'], ['c2'])],
    })

    const first = node(layout, 'f1')
    const second = node(layout, 'f2')
    expect(first.depth).toBe(0)
    expect(second.depth).toBe(0)
    expect(first.personUids).toEqual(['a', 'x'])
    expect(second.personUids).toEqual(['a', 'y'])
    // The shared partner is drawn twice and said to be a repeat the second time.
    expect(first.repeatUids).toEqual([])
    expect(second.repeatUids).toEqual(['a'])
    // Each marriage keeps its own children.
    expect(first.childIds).toEqual(['person:c1'])
    expect(second.childIds).toEqual(['person:c2'])
    expectNoOverlap(layout)
  })

  it('folds a collapsed branch away and says how many people it hides', () => {
    const families = [
      family('f1', ['a'], ['b', 'c']),
      family('f2', ['b', 'b2'], ['b1', 'b3']),
      family('f3', ['c']),
    ]
    const open = layoutDescendants({ rootUid: 'a', families })
    expect(open.nodes).toHaveLength(5)

    const folded = layoutDescendants({ rootUid: 'a', families, collapsed: ['f2'] })

    expect(folded.nodes.map((box) => box.id)).toEqual(['f1', 'f2', 'f3'])
    const hidden = node(folded, 'f2')
    expect(hidden.collapsed).toBe(true)
    expect(hidden.childIds).toEqual([])
    // b1 and b3 are hidden; b and b2 are drawn in the box that was folded.
    expect(hidden.hiddenCount).toBe(2)
    expect(folded.edges.map((edge) => edge.childId)).toEqual(['f2', 'f3'])
    // Folding narrows the drawing, which is the point of folding.
    expect(folded.width).toBeLessThan(open.width)
  })

  it('draws a cousin marriage once, however many paths reach it', () => {
    const layout = layoutDescendants({
      rootUid: 'r',
      families: [
        family('f0', ['r', 'r2'], ['a', 'b']),
        family('fa', ['a', 'a2'], ['c1']),
        family('fb', ['b', 'b2'], ['c2']),
        // The cousins marry: c1 and c2 are both descendants of r.
        family('fm', ['c1', 'c2'], ['d']),
      ],
    })

    const ids = layout.nodes.map((box) => box.id)
    expect(ids.filter((id) => id === 'fm')).toHaveLength(1)
    expect(ids.filter((id) => id === 'person:d')).toHaveLength(1)
    // Everybody appears in exactly one box.
    const drawn = layout.nodes.flatMap((box) => box.personUids)
    expect(new Set(drawn).size).toBe(drawn.length)
    // The shared box hangs off whichever cousin was reached first, and the
    // couple is drawn as one box rather than as two competing halves.
    expect(node(layout, 'fm').personUids).toEqual(['c1', 'c2'])
    expect(node(layout, 'fm').parentId).toBe('fa')
    expectNoOverlap(layout)
  })

  it('stops at the collapsed box even when the branch below it is a diamond', () => {
    const layout = layoutDescendants({
      rootUid: 'r',
      families: [
        family('f0', ['r'], ['a', 'b']),
        family('fa', ['a'], ['c1']),
        family('fb', ['b'], ['c2']),
        family('fm', ['c1', 'c2'], ['d']),
      ],
      collapsed: ['fa'],
    })

    // c1 is no longer drawn under a, so the cousin marriage hangs off b instead.
    expect(node(layout, 'fm').parentId).toBe('fb')
    expect(node(layout, 'fa').hiddenCount).toBe(0)
    expectNoOverlap(layout)
  })

  it('sizes the canvas around everything it drew', () => {
    const layout = layoutDescendants({
      rootUid: 'a',
      families: [family('f1', ['a', 'a2'], ['b', 'c'])],
    })

    const right = Math.max(...layout.nodes.map((box) => box.x + box.width))
    const bottom = Math.max(...layout.nodes.map((box) => box.y + box.height))
    expect(layout.width).toBe(right + TREE_PADDING)
    expect(layout.height).toBe(bottom + TREE_PADDING)
    expect(bottom - TREE_PADDING).toBeGreaterThan(NODE_HEIGHT)
  })
})

describe('edgePath', () => {
  it('drops, runs across and drops again', () => {
    expect(edgePath({ parentId: 'p', childId: 'c', x1: 100, y1: 60, x2: 40, y2: 120 })).toBe(
      'M 100 60 V 90 H 40 V 120',
    )
  })
})

/**
 * A four-generation pedigree of the kind the page is drawn for: Jan, his
 * parents, all four grandparents and all eight great-grandparents.
 *
 * The uids spell out where everybody stands — `p` for the paternal side, `m` for
 * the maternal one — because a pedigree test that has to be decoded is a test
 * nobody will keep.
 */
function fullPedigree(): LayoutFamily[] {
  return [
    family('f_jan', ['pa', 'ma'], ['jan']),
    family('f_pa', ['pp', 'pm'], ['pa']),
    family('f_ma', ['mp', 'mm'], ['ma']),
    family('f_pp', ['ppp', 'ppm'], ['pp']),
    family('f_pm', ['pmp', 'pmm'], ['pm']),
    family('f_mp', ['mpp', 'mpm'], ['mp']),
    family('f_mm', ['mmp', 'mmm'], ['mm']),
  ]
}

/** The slot of a given id, failing the test rather than returning undefined. */
function slot(pedigree: Pedigree, id: string): PedigreeSlot {
  const found = pedigree.slots.find((candidate) => candidate.id === id)
  if (found === undefined) {
    throw new Error(`the pedigree has no slot ${id}`)
  }
  return found
}

/** The slot standing for a given person, of which there may be several. */
function slotsOf(pedigree: Pedigree, personUid: string): PedigreeSlot[] {
  return pedigree.slots.filter((candidate) => candidate.personUid === personUid)
}

/** Asserts that no two slots of one generation overlap or touch. */
function expectRowsClear(pedigree: Pedigree): void {
  const byGeneration = new Map<number, PedigreeSlot[]>()
  for (const place of pedigree.slots) {
    byGeneration.set(place.generation, [...(byGeneration.get(place.generation) ?? []), place])
  }
  for (const row of byGeneration.values()) {
    const ordered = [...row].sort((a, b) => a.x - b.x)
    for (let i = 1; i < ordered.length; i += 1) {
      const gap = ordered[i].x - (ordered[i - 1].x + ordered[i - 1].width)
      expect(gap, `gap between ${ordered[i - 1].id} and ${ordered[i].id}`).toBeGreaterThanOrEqual(
        -0.001,
      )
    }
  }
}

describe('layoutAncestors', () => {
  it('draws a full four-generation pedigree, oldest row on top', () => {
    const pedigree = layoutAncestors({ rootUid: 'jan', families: fullPedigree(), generations: 3 })

    // 1 + 2 + 4 + 8: a pedigree is bounded by construction.
    expect(pedigree.slots).toHaveLength(15)
    expect(pedigree.slots.filter((place) => place.personUid === null)).toHaveLength(0)
    expect(slot(pedigree, 'a1').personUid).toBe('jan')
    expect(slot(pedigree, 'a2').personUid).toBe('pa')
    expect(slot(pedigree, 'a3').personUid).toBe('ma')
    // Ahnentafel: the father of n is 2n, the mother 2n+1, all the way up.
    expect(slot(pedigree, 'a8').personUid).toBe('ppp')
    expect(slot(pedigree, 'a15').personUid).toBe('mmm')

    // The root sits at the bottom and each generation one row above the last.
    const rowStride = NODE_HEIGHT + LEVEL_GAP
    expect(slot(pedigree, 'a1').y).toBe(TREE_PADDING + 3 * rowStride)
    expect(slot(pedigree, 'a2').y).toBe(TREE_PADDING + 2 * rowStride)
    expect(slot(pedigree, 'a8').y).toBe(TREE_PADDING)
    expectRowsClear(pedigree)
  })

  it('centres a person between the two parents above them', () => {
    const pedigree = layoutAncestors({ rootUid: 'jan', families: fullPedigree(), generations: 3 })

    for (const place of pedigree.slots) {
      const parents = pedigree.edges.filter((edge) => edge.childId === place.id)
      if (parents.length === 0) {
        continue
      }
      const centres = parents.map((edge) => edge.x1)
      const between = (Math.min(...centres) + Math.max(...centres)) / 2
      expect(place.x + place.width / 2).toBeCloseTo(between, 5)
    }
  })

  it('leaves an unknown parent as a visible slot naming the child to fill it on', () => {
    const pedigree = layoutAncestors({
      rootUid: 'jan',
      families: [family('f_jan', ['pa', 'ma'], ['jan']), family('f_pa', ['pp'], ['pa'])],
      generations: 2,
    })

    // Jan's father has one recorded parent; the other side of that couple is a
    // slot with nobody in it, to be filled in on the child it belongs to.
    expect(slot(pedigree, 'a4').personUid).toBe('pp')
    expect(slot(pedigree, 'a5').personUid).toBeNull()
    expect(slot(pedigree, 'a5').childUid).toBe('pa')
    // Jan's mother has no recorded parents at all: two blanks, not a missing row.
    expect(slot(pedigree, 'a6').personUid).toBeNull()
    expect(slot(pedigree, 'a7').personUid).toBeNull()
    expect(slot(pedigree, 'a6').childUid).toBe('ma')
    expectRowsClear(pedigree)
  })

  it('does not climb above a slot nobody has filled in', () => {
    const pedigree = layoutAncestors({ rootUid: 'jan', families: [], generations: 4 })

    // An unknown parent has no knowable parents of their own, so the drawing
    // stops rather than fanning sixteen blanks out of nothing.
    expect(pedigree.slots).toHaveLength(3)
    expect(pedigree.edges).toHaveLength(2)
  })

  it('narrows to the branch that exists instead of reserving empty columns', () => {
    const line = layoutAncestors({
      rootUid: 'jan',
      families: [
        family('f_jan', ['pa'], ['jan']),
        family('f_pa', ['pp'], ['pa']),
        family('f_pp', ['ppp'], ['pp']),
      ],
      generations: 3,
    })
    const full = layoutAncestors({ rootUid: 'jan', families: fullPedigree(), generations: 3 })

    // One recorded line four generations deep is half the paper the full
    // pedigree of the same depth needs: each unknown half of a couple is one
    // blank slot, not the eight columns its descendants would have filled.
    expect(line.slots).toHaveLength(7)
    expect(line.width).toBeLessThan(full.width * 0.6)
    expect(line.height).toBe(full.height)
  })

  it('draws a shared ancestor on both sides without looping', () => {
    // Petr and Eva are cousins: their fathers Josef and Karel are brothers, so
    // Bohumil and Marie are Jan's great-grandparents twice over.
    const pedigree = layoutAncestors({
      rootUid: 'jan',
      families: [
        family('f_jan', ['petr', 'eva'], ['jan']),
        family('f_petr', ['josef', 'ludmila'], ['petr']),
        family('f_eva', ['karel', 'anna'], ['eva']),
        family('f_old', ['bohumil', 'marie'], ['josef', 'anna']),
      ],
      generations: 3,
    })

    expect(slotsOf(pedigree, 'bohumil')).toHaveLength(2)
    expect(slotsOf(pedigree, 'bohumil').filter((place) => place.repeat)).toHaveLength(1)
    // Josef and Anna are the siblings the collapse runs through; each is drawn
    // once, on the side they descend to.
    expect(slotsOf(pedigree, 'josef')).toHaveLength(1)
    expect(slotsOf(pedigree, 'anna')).toHaveLength(1)
    expectRowsClear(pedigree)
  })

  it('stops a person who is their own ancestor instead of climbing for ever', () => {
    const pedigree = layoutAncestors({
      rootUid: 'jan',
      families: [family('f_jan', ['jan', 'eva'], ['jan'])],
      generations: 6,
    })

    // Jan is drawn again as his own father, marked as the repeat it is, and the
    // climb ends there rather than recursing into itself. Eva's own parents are
    // two blanks, which is why the drawing is five slots and not three.
    expect(slotsOf(pedigree, 'jan')).toHaveLength(2)
    expect(slot(pedigree, 'a2').repeat).toBe(true)
    expect(slot(pedigree, 'a2').x).not.toBeNaN()
    expect(pedigree.slots).toHaveLength(5)
  })

  it('bounds how far up it will draw', () => {
    const deep = layoutAncestors({
      rootUid: 'jan',
      families: fullPedigree(),
      generations: 99,
    })
    const none = layoutAncestors({ rootUid: 'jan', families: fullPedigree(), generations: 0 })

    // A silly request is clamped, and above the last recorded generation there
    // is only one row of blanks — an unknown ancestor has no knowable parents —
    // so the drawing stops at 15 people and the 16 gaps above them…
    expect(deep.slots).toHaveLength(31)
    expect(Math.max(...deep.slots.map((place) => place.generation))).toBe(4)
    // …and a request for nothing still yields the parents, which is the least a
    // pedigree can say.
    expect(none.slots).toHaveLength(3)
  })

  it('sizes the canvas around everything it drew', () => {
    const pedigree = layoutAncestors({ rootUid: 'jan', families: fullPedigree(), generations: 3 })

    const right = Math.max(...pedigree.slots.map((place) => place.x + place.width))
    const bottom = Math.max(...pedigree.slots.map((place) => place.y + place.height))
    expect(pedigree.width).toBeCloseTo(right + TREE_PADDING, 5)
    expect(pedigree.height).toBeCloseTo(bottom + TREE_PADDING, 5)
    expect(Math.min(...pedigree.slots.map((place) => place.x))).toBeGreaterThanOrEqual(TREE_PADDING)
  })

  it('has a canvas even with nothing on it', () => {
    expect(emptyPedigree()).toEqual({
      slots: [],
      edges: [],
      width: 2 * TREE_PADDING,
      height: 2 * TREE_PADDING,
    })
  })
})
