import { type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import {
  COUPLE_GAP,
  edgePath,
  type FamilyLayout,
  type LayoutNode,
  NODE_HEIGHT,
  PERSON_WIDTH,
} from '../../lib/familyLayout'

import { type TreePerson, TreePersonCard } from './TreePersonCard'
import { TreeStage } from './TreeStage'

/** Where the `+` sits on a card: on its top edge, clear of the line that lands mid-card. */
const ADD_BADGE = { cx: PERSON_WIDTH - 20, cy: 0, r: 10 }

/** Props for {@link FamilyNetworkCanvas}. */
export interface FamilyNetworkCanvasProps {
  /** The drawing, already decided by `lib/familyLayout`. */
  layout: FamilyLayout
  /** Everybody in it, by UID. A box whose person is missing draws a blank card. */
  people: ReadonlyMap<string, TreePerson>
  /** The person the drawing is about, drawn as the one card nothing re-roots to. */
  rootUid: string
  /** Where clicking a person leads — re-rooting, or the person's own page. */
  personHref: (uid: string) => string
  /** The people with a parent the library does not know yet: where the `+` goes. */
  missingParent: ReadonlySet<string>
  /**
   * Opens the "record a relative" dialog on a person to add a parent — a parent
   * is recorded on their child. Null for a reader who may not write, who then
   * sees no `+` at all.
   */
  onAddParent: ((childUid: string) => void) | null
}

/**
 * The `+` on a card: record a parent this person is missing.
 *
 * It is where the pedigree's empty slot went. A layered drawing has no fixed
 * places for parents nobody recorded, so the invitation to fill one in moves onto
 * the person whose parent it would be — which also makes it work for everybody
 * in the drawing, the aunts and the in-laws included, rather than only for the
 * line straight up from the root. It is a sibling of the card rather than a part
 * of it, because the card is a link and a button inside a link is two controls
 * fighting over one click.
 */
function AddParentBadge({ name, onAdd }: { name: string; onAdd: () => void }) {
  const { t } = useTranslation()
  const label = t('familyTree.addParent', { name })

  const onKeyDown = (event: ReactKeyboardEvent<SVGGElement>) => {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault()
      onAdd()
    }
  }

  return (
    <g
      className="kk-tree-add"
      role="button"
      tabIndex={0}
      aria-label={label}
      onClick={onAdd}
      onKeyDown={onKeyDown}
    >
      <title>{label}</title>
      <circle className="kk-tree-add__dot" cx={ADD_BADGE.cx} cy={ADD_BADGE.cy} r={ADD_BADGE.r} />
      <text className="kk-tree-add__sign" x={ADD_BADGE.cx} y={ADD_BADGE.cy}>
        +
      </text>
    </g>
  )
}

/**
 * One box of the drawing: a couple side by side with the bar that joins them, a
 * lone parent, or a person who is nobody's partner — each card with its `+` where
 * the person is missing a parent and the reader may record one.
 */
function TreeBox({
  node,
  people,
  rootUid,
  personHref,
  missingParent,
  onAddParent,
}: {
  node: LayoutNode
} & Omit<FamilyNetworkCanvasProps, 'layout'>) {
  const { t } = useTranslation()
  const repeats = new Set(node.repeatUids)

  return (
    <g transform={`translate(${node.x}, ${node.y})`}>
      {node.personUids.map((uid, index) => {
        const name = people.get(uid)?.name ?? uid
        // A repeated card is a cross-reference to the person's first box; the
        // `+` stays on that one, so nobody is offered twice.
        const addable = onAddParent !== null && missingParent.has(uid) && !repeats.has(uid)
        return (
          <g key={uid} transform={`translate(${index * (PERSON_WIDTH + COUPLE_GAP)}, 0)`}>
            <TreePersonCard
              person={people.get(uid)}
              uid={uid}
              href={personHref(uid)}
              label={
                uid === rootUid
                  ? t('familyTree.openPerson', { name })
                  : t('familyTree.reroot', { name })
              }
              repeat={repeats.has(uid)}
              root={uid === rootUid}
            />
            {addable && (
              <AddParentBadge
                name={name}
                onAdd={() => {
                  onAddParent(uid)
                }}
              />
            )}
          </g>
        )
      })}
      {node.personUids.length > 1 && (
        // The bar that joins a couple. A line rather than the ⚭ glyph: the
        // marriage sign is not in the theme's typeface and falls back to
        // whatever the system has, at which point two partners are joined by a
        // smudge whose size nobody chose.
        <line
          className="kk-tree-box__union"
          x1={PERSON_WIDTH}
          y1={NODE_HEIGHT / 2}
          x2={PERSON_WIDTH + COUPLE_GAP}
          y2={NODE_HEIGHT / 2}
        />
      )}
    </g>
  )
}

/**
 * The family network, painted onto the shared {@link TreeStage}: everybody the
 * family links reach from one person, one generation per row, and a click on a
 * person re-roots the drawing on them.
 *
 * Nothing here decides *where* anything goes — `lib/familyLayout` did that, in a
 * pure function with unit tests, and this component only paints its answer. The
 * two halves are deliberately separate, because a layout bug is arithmetic that
 * can be proven and a rendering bug is something you have to look at.
 *
 * Re-rooting is a plain **link**: the root is a route parameter, so Back undoes
 * it and a middle click opens the person's family in a new tab.
 */
export function FamilyNetworkCanvas({ layout, ...boxProps }: FamilyNetworkCanvasProps) {
  const { rootUid } = boxProps
  const root = layout.nodes.find((node) => node.personUids.includes(rootUid))

  return (
    <TreeStage
      content={layout}
      // A recorded parent redraws the family around the same person; the reader
      // is shown the whole of the new drawing rather than left zoomed into a
      // corner of the old one.
      resetKey={`${rootUid}:${layout.nodes.length}`}
      focus={
        root === undefined ? null : { x: root.x + root.width / 2, y: root.y + root.height / 2 }
      }
    >
      <g className="kk-tree-edges">
        {layout.edges.map((edge) => (
          <path key={`${edge.parentId}-${edge.childId}`} d={edgePath(edge)} />
        ))}
      </g>
      {layout.nodes.map((node) => (
        <TreeBox key={node.id} node={node} {...boxProps} />
      ))}
    </TreeStage>
  )
}
