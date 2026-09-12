import { type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'

import {
  AVATAR_CIRCLE,
  edgePath,
  NODE_HEIGHT,
  type Pedigree,
  type PedigreeSlot,
  PERSON_WIDTH,
} from '../../lib/familyLayout'

import { type TreePerson, TreePersonCard } from './TreePersonCard'
import { TreeStage } from './TreeStage'

/** Props for {@link FamilyPedigreeCanvas}. */
export interface FamilyPedigreeCanvasProps {
  /** The pedigree, already decided by `lib/familyLayout`. */
  pedigree: Pedigree
  /** Everybody in it, by UID. A slot whose person is missing draws their uid. */
  people: ReadonlyMap<string, TreePerson>
  /** The person the pedigree is climbed from, drawn at the bottom. */
  rootUid: string
  /** Where clicking a person leads — re-rooting, or the person's own page. */
  personHref: (uid: string) => string
  /**
   * Opens the "record a relative" dialog on the child a blank slot belongs to —
   * a parent is recorded on their child. Null for a reader who may not write, in
   * which case the gaps are drawn all the same but say nothing can be done.
   */
  onAddParent: ((childUid: string) => void) | null
}

/**
 * An ancestor nobody has recorded: a dashed slot where a person would be.
 *
 * It is drawn rather than left out, and that is the whole point of the pedigree.
 * A missing great-grandmother in a library of 118 people is not an error to be
 * hidden — it is the most interesting thing on the page, because it is the one
 * somebody can still fix. Under `canWrite` the gap is a button straight into the
 * dialog that fills it, opened on the **child** the slot belongs to, since a
 * parent is recorded on their child and not the other way round.
 */
function PedigreeGap({
  slot,
  childName,
  onAdd,
}: {
  slot: PedigreeSlot
  childName: string
  onAdd: (() => void) | null
}) {
  const { t } = useTranslation()
  const label =
    onAdd === null
      ? t('familyTree.unknownParent', { name: childName })
      : t('familyTree.addParent', { name: childName })

  const onKeyDown = (event: ReactKeyboardEvent<SVGGElement>) => {
    if (onAdd !== null && (event.key === 'Enter' || event.key === ' ')) {
      event.preventDefault()
      onAdd()
    }
  }

  return (
    <g
      className={`kk-tree-gap${onAdd === null ? '' : ' kk-tree-gap--addable'}`}
      // A gap nobody can fill in is still something a reader has to be told
      // about — a screen reader that skipped it would report a pedigree with
      // fewer people in it rather than one with holes — so it keeps a name and
      // loses only the button.
      role={onAdd === null ? 'img' : 'button'}
      tabIndex={onAdd === null ? undefined : 0}
      aria-label={label}
      onClick={onAdd ?? undefined}
      onKeyDown={onKeyDown}
    >
      <title>{label}</title>
      <rect
        className="kk-tree-gap__plate"
        x={0.5}
        y={0.5}
        width={PERSON_WIDTH - 1}
        height={slot.height - 1}
        rx={10}
      />
      <circle
        className="kk-tree-gap__ring"
        cx={AVATAR_CIRCLE.cx}
        cy={AVATAR_CIRCLE.cy}
        r={AVATAR_CIRCLE.r}
      />
      <text className="kk-tree-gap__sign" x={AVATAR_CIRCLE.cx} y={AVATAR_CIRCLE.cy}>
        {onAdd === null ? '?' : '+'}
      </text>
      <text
        className="kk-tree-gap__label"
        x={AVATAR_CIRCLE.cx + AVATAR_CIRCLE.r + 10}
        y={NODE_HEIGHT / 2}
      >
        {onAdd === null ? t('familyTree.unknown') : t('familyTree.addParentShort')}
      </text>
    </g>
  )
}

/**
 * The ancestors pedigree, painted onto the shared {@link TreeStage}: the person
 * at the bottom, each generation of parents on the row above, and a visible gap
 * wherever the library does not know who stood there.
 *
 * It is a second renderer rather than a second mode of the descendants tree
 * because the two are different shapes — a tidy tree packs an unbounded fan of
 * children, a pedigree is a binary grid bounded by generation — and the design
 * splits the page by direction for exactly that reason. What they *do* share is
 * the sheet of paper (pan, zoom, fit) and the person's card, both lifted into
 * their own components so the two drawings cannot drift apart.
 *
 * As in the tree, clicking a person re-roots the drawing on them and the root's
 * own card leads to their page, both as plain links in the URL.
 */
export function FamilyPedigreeCanvas({
  pedigree,
  people,
  rootUid,
  personHref,
  onAddParent,
}: FamilyPedigreeCanvasProps) {
  const { t } = useTranslation()
  const root = pedigree.slots.find((slot) => slot.personUid === rootUid)

  return (
    <TreeStage
      content={pedigree}
      resetKey={`${rootUid}:${pedigree.slots.length}`}
      focus={
        root === undefined ? null : { x: root.x + root.width / 2, y: root.y + root.height / 2 }
      }
    >
      <g className="kk-tree-edges">
        {pedigree.edges.map((edge) => (
          <path key={`${edge.parentId}-${edge.childId}`} d={edgePath(edge)} />
        ))}
      </g>
      {pedigree.slots.map((slot) => (
        <g key={slot.id} transform={`translate(${slot.x}, ${slot.y})`}>
          {slot.personUid === null ? (
            <PedigreeGap
              slot={slot}
              childName={
                slot.childUid === null ? '' : (people.get(slot.childUid)?.name ?? slot.childUid)
              }
              onAdd={
                onAddParent === null || slot.childUid === null
                  ? null
                  : () => {
                      onAddParent(slot.childUid ?? '')
                    }
              }
            />
          ) : (
            <TreePersonCard
              person={people.get(slot.personUid)}
              uid={slot.personUid}
              href={personHref(slot.personUid)}
              label={
                slot.personUid === rootUid
                  ? t('familyTree.openPerson', {
                      name: people.get(slot.personUid)?.name ?? slot.personUid,
                    })
                  : t('familyTree.rerootUp', {
                      name: people.get(slot.personUid)?.name ?? slot.personUid,
                    })
              }
              repeat={slot.repeat}
              repeatTitle={t('familyTree.repeatUpHint', {
                name: people.get(slot.personUid)?.name ?? slot.personUid,
              })}
              root={slot.personUid === rootUid}
            />
          )}
        </g>
      ))}
    </TreeStage>
  )
}
