import { type TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

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

/** Props for {@link FamilyTreeCanvas}. */
export interface FamilyTreeCanvasProps {
  /** The drawing, already decided by `lib/familyLayout`. */
  layout: FamilyLayout
  /** Everybody in it, by UID. A box whose person is missing draws a blank card. */
  people: ReadonlyMap<string, TreePerson>
  /** The person the tree is rooted at, drawn as the one box nothing re-roots to. */
  rootUid: string
  /** Where clicking a person leads — re-rooting, or the person's own page. */
  personHref: (uid: string) => string
  /** The URL that folds or unfolds one box's branch. */
  toggleHref: (nodeId: string) => string
}

/**
 * What the fold handle says it will do: close an open branch, or open a closed
 * one — naming the couple it hangs under (a page has as many handles as it has
 * families, and „collapse this branch" names none of them) and how many people
 * are behind it where any are, so a reader can tell a branch worth opening from
 * one whose people are all drawn elsewhere anyway.
 */
function foldLabelOf(node: LayoutNode, name: string, t: TFunction): string {
  if (!node.collapsed) {
    return t('familyTree.collapse', { name })
  }
  return node.hiddenCount > 0
    ? t('familyTree.expandCount', { name, hidden: node.hiddenCount })
    : t('familyTree.expand', { name })
}

/**
 * One box of the tree: a couple side by side (with the marriage glyph between
 * them), a lone parent, or a childless descendant — plus the handle that folds
 * the branch below it away.
 */
function TreeBox({
  node,
  people,
  rootUid,
  personHref,
  toggleHref,
}: {
  node: LayoutNode
  people: ReadonlyMap<string, TreePerson>
  rootUid: string
  personHref: (uid: string) => string
  toggleHref: (nodeId: string) => string
}) {
  const { t } = useTranslation()
  const foldable = node.childIds.length > 0 || node.collapsed
  const repeats = new Set(node.repeatUids)
  const foldLabel = foldLabelOf(node, people.get(node.personUids[0])?.name ?? '', t)

  return (
    <g transform={`translate(${node.x}, ${node.y})`}>
      {node.personUids.map((uid, index) => {
        const name = people.get(uid)?.name ?? uid
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
      {foldable && (
        <Link to={toggleHref(node.id)} className="kk-tree-fold" aria-label={foldLabel}>
          <title>{foldLabel}</title>
          <circle className="kk-tree-fold__dot" cx={node.width / 2} cy={NODE_HEIGHT} r={10} />
          <text className="kk-tree-fold__sign" x={node.width / 2} y={NODE_HEIGHT}>
            {node.collapsed ? '+' : '−'}
          </text>
          {node.collapsed && node.hiddenCount > 0 && (
            <text className="kk-tree-fold__count" x={node.width / 2} y={NODE_HEIGHT + 26}>
              {node.hiddenCount}
            </text>
          )}
        </Link>
      )}
    </g>
  )
}

/**
 * The descendants tree, painted onto the shared {@link TreeStage}: fold a branch
 * away, click a person to re-root the drawing on them.
 *
 * Nothing here decides *where* anything goes — `lib/familyLayout` did that, in a
 * pure function with unit tests, and this component only paints its answer. The
 * two halves are deliberately separate, because a layout bug is arithmetic that
 * can be proven and a rendering bug is something you have to look at.
 *
 * Re-rooting and folding are both plain **links**: the root is a route parameter
 * and the folded branches are a query parameter, so Back undoes either one, the
 * drawing can be bookmarked as the reader left it, and a middle click opens a
 * branch in a new tab. That is the project's "Back always works" rule applied to
 * a view that would otherwise be a pile of component state.
 */
export function FamilyTreeCanvas({
  layout,
  people,
  rootUid,
  personHref,
  toggleHref,
}: FamilyTreeCanvasProps) {
  const root = layout.nodes.find((node) => node.personUids.includes(rootUid))

  return (
    <TreeStage
      content={layout}
      resetKey={rootUid}
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
        <TreeBox
          key={node.id}
          node={node}
          people={people}
          rootUid={rootUid}
          personHref={personHref}
          toggleHref={toggleHref}
        />
      ))}
    </TreeStage>
  )
}
