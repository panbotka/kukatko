import { type TFunction } from 'i18next'
import { type PointerEvent as ReactPointerEvent, useEffect, useMemo, useRef, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { avatarInitial, avatarTone } from '../../lib/avatarIdentity'
import {
  COUPLE_GAP,
  edgePath,
  type FamilyLayout,
  type LayoutNode,
  NODE_HEIGHT,
  PERSON_WIDTH,
} from '../../lib/familyLayout'
import { formatLifeSpan } from '../../lib/lifeYears'
import { truncateText } from '../../lib/text'
import { centreOn, fitView, panBy, type TreeView, ZOOM_STEP, zoomAt } from '../../lib/treeView'
import { subjectAvatarUrl } from '../../services/people'
import { Icon } from '../Icon'

import './familyTree.css'

/** The person a card draws: only what fits inside a box of {@link PERSON_WIDTH}. */
export interface TreePerson {
  uid: string
  name: string
  birth_year: number | null
  death_year: number | null
  photo_count: number
}

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

/** Where the round face sits inside a person's card, in layout units. */
const AVATAR = { cx: 32, cy: NODE_HEIGHT / 2, r: 20 }

/** How many letters of a name fit beside the face before it has to be cut. */
const NAME_LIMIT = 17

/** How far a pointer may travel during a click before it counts as a drag. */
const DRAG_SLOP_PX = 4

/** The stage size assumed before the container has been measured (jsdom, chiefly). */
const FALLBACK_STAGE = { width: 1024, height: 640 }

/** The live size of the stage element, in CSS pixels. */
function useStageSize(ref: React.RefObject<HTMLElement | null>): { width: number; height: number } {
  const [size, setSize] = useState(FALLBACK_STAGE)

  useEffect(() => {
    const element = ref.current
    if (element === null) {
      return
    }
    const measure = () => {
      const rect = element.getBoundingClientRect()
      if (rect.width <= 0 || rect.height <= 0) {
        return
      }
      setSize((current) =>
        Math.abs(current.width - rect.width) < 0.5 && Math.abs(current.height - rect.height) < 0.5
          ? current
          : { width: rect.width, height: rect.height },
      )
    }
    measure()
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(measure) : null
    observer?.observe(element)
    if (observer === null) {
      window.addEventListener('resize', measure)
    }
    return () => {
      observer?.disconnect()
      if (observer === null) {
        window.removeEventListener('resize', measure)
      }
    }
  }, [ref])

  return size
}

/**
 * One person inside a box: a round face, their name, and the years anybody
 * recorded for them.
 *
 * The face is the server-cut square (`GET /subjects/{uid}/avatar`), falling back
 * to the coloured initial for somebody with no photographs at all — which in a
 * family tree is an ordinary case and not a failure, since a great-grandmother
 * nobody photographed is exactly the kind of person the tree is drawn for. The
 * name is cut to the card's width with an ellipsis (SVG has no
 * `text-overflow`), and the whole of it lives in the link's `title`.
 */
function PersonCard({
  person,
  uid,
  href,
  repeat,
  root,
}: {
  person: TreePerson | undefined
  uid: string
  href: string
  repeat: boolean
  root: boolean
}) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)
  const name = person?.name ?? uid
  const lifeSpan = formatLifeSpan(person?.birth_year ?? null, person?.death_year ?? null)
  const hasPhoto = (person?.photo_count ?? 0) > 0 && !failed
  const hint = root ? t('familyTree.openPerson', { name }) : t('familyTree.reroot', { name })

  return (
    <Link to={href} className="kk-tree-person" aria-label={hint}>
      <title>{repeat ? t('familyTree.repeatHint', { name }) : hint}</title>
      <rect
        className={`kk-tree-person__plate${root ? ' kk-tree-person__plate--root' : ''}${
          repeat ? ' kk-tree-person__plate--repeat' : ''
        }`}
        x={0.5}
        y={0.5}
        width={PERSON_WIDTH - 1}
        height={NODE_HEIGHT - 1}
        rx={10}
      />
      {hasPhoto ? (
        <image
          href={subjectAvatarUrl(uid)}
          x={AVATAR.cx - AVATAR.r}
          y={AVATAR.cy - AVATAR.r}
          width={AVATAR.r * 2}
          height={AVATAR.r * 2}
          clipPath="url(#kk-tree-avatar-clip)"
          preserveAspectRatio="xMidYMid slice"
          onError={() => {
            setFailed(true)
          }}
        />
      ) : (
        <>
          <circle
            className={`kk-tree-person__initial kk-tree-person__initial--tone-${avatarTone(name)}`}
            cx={AVATAR.cx}
            cy={AVATAR.cy}
            r={AVATAR.r}
          />
          <text className="kk-tree-person__letter" x={AVATAR.cx} y={AVATAR.cy}>
            {avatarInitial(name)}
          </text>
        </>
      )}
      <text
        className="kk-tree-person__name"
        x={AVATAR.cx + AVATAR.r + 10}
        y={lifeSpan === null ? 34 : 26}
      >
        {truncateText(name, NAME_LIMIT)}
      </text>
      {lifeSpan !== null && (
        <text className="kk-tree-person__years" x={AVATAR.cx + AVATAR.r + 10} y={43}>
          {lifeSpan}
        </text>
      )}
    </Link>
  )
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
      {node.personUids.map((uid, index) => (
        <g key={uid} transform={`translate(${index * (PERSON_WIDTH + COUPLE_GAP)}, 0)`}>
          <PersonCard
            person={people.get(uid)}
            uid={uid}
            href={personHref(uid)}
            repeat={repeats.has(uid)}
            root={uid === rootUid}
          />
        </g>
      ))}
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
 * The family tree as an SVG stage: pan by dragging, zoom with the wheel or the
 * buttons, fold a branch away, click a person to re-root the drawing on them.
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
 *
 * The view itself (pan and zoom) is *not* in the URL: it is where the reader's
 * eye is rather than what they are looking at, and a history entry per wheel
 * notch would bury the two states that matter.
 */
export function FamilyTreeCanvas({
  layout,
  people,
  rootUid,
  personHref,
  toggleHref,
}: FamilyTreeCanvasProps) {
  const { t } = useTranslation()
  const stageRef = useRef<HTMLDivElement>(null)
  const size = useStageSize(stageRef)
  const [moved, setMoved] = useState<TreeView | null>(null)
  const dragRef = useRef<{ x: number; y: number; travelled: number } | null>(null)
  const draggedRef = useRef(false)

  const fitted = useMemo(() => fitView(layout, size), [layout, size])
  const view = moved ?? fitted

  // A new root is a new drawing; whatever the reader had panned to belongs to
  // the tree they just left. Folding a branch is not — the view they built up
  // stays where it was, which is the whole reason to fold in the first place.
  useEffect(() => {
    setMoved(null)
  }, [rootUid])

  // The wheel listener is attached by hand because React's own is passive, and
  // a passive listener cannot preventDefault — without which the wheel scrolls
  // the page instead of zooming the tree.
  useEffect(() => {
    const element = stageRef.current
    if (element === null) {
      return
    }
    const onWheel = (event: WheelEvent) => {
      event.preventDefault()
      const rect = element.getBoundingClientRect()
      const factor = event.deltaY < 0 ? ZOOM_STEP : 1 / ZOOM_STEP
      setMoved((current) =>
        zoomAt(current ?? fitted, factor, event.clientX - rect.left, event.clientY - rect.top),
      )
    }
    element.addEventListener('wheel', onWheel, { passive: false })
    return () => {
      element.removeEventListener('wheel', onWheel)
    }
  }, [fitted])

  const onPointerDown = (event: ReactPointerEvent<SVGSVGElement>) => {
    if (event.button !== 0) {
      return
    }
    dragRef.current = { x: event.clientX, y: event.clientY, travelled: 0 }
    draggedRef.current = false
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  const onPointerMove = (event: ReactPointerEvent<SVGSVGElement>) => {
    const drag = dragRef.current
    if (drag === null) {
      return
    }
    const dx = event.clientX - drag.x
    const dy = event.clientY - drag.y
    drag.x = event.clientX
    drag.y = event.clientY
    drag.travelled += Math.abs(dx) + Math.abs(dy)
    if (drag.travelled > DRAG_SLOP_PX) {
      draggedRef.current = true
    }
    setMoved((current) => panBy(current ?? fitted, dx, dy))
  }

  const endDrag = (event: ReactPointerEvent<SVGSVGElement>) => {
    dragRef.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId)
    }
  }

  const zoom = (factor: number) => {
    setMoved((current) => zoomAt(current ?? fitted, factor, size.width / 2, size.height / 2))
  }

  return (
    <div className="kk-tree-stage" ref={stageRef}>
      <svg
        className="kk-tree-stage__svg"
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        // A drag that ends over a person must not also follow that person's
        // link: panning the stage is one gesture, not a gesture and a click.
        onClickCapture={(event) => {
          if (draggedRef.current) {
            event.preventDefault()
            event.stopPropagation()
            draggedRef.current = false
          }
        }}
      >
        <defs>
          <clipPath id="kk-tree-avatar-clip">
            <circle cx={AVATAR.cx} cy={AVATAR.cy} r={AVATAR.r} />
          </clipPath>
        </defs>
        <g transform={`translate(${view.x}, ${view.y}) scale(${view.scale})`}>
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
        </g>
      </svg>
      <div className="kk-tree-stage__controls">
        <Button
          variant="secondary"
          size="sm"
          aria-label={t('familyTree.zoomIn')}
          title={t('familyTree.zoomIn')}
          onClick={() => {
            zoom(ZOOM_STEP)
          }}
        >
          <Icon name="zoom-in" />
        </Button>
        <Button
          variant="secondary"
          size="sm"
          aria-label={t('familyTree.zoomOut')}
          title={t('familyTree.zoomOut')}
          onClick={() => {
            zoom(1 / ZOOM_STEP)
          }}
        >
          <Icon name="zoom-out" />
        </Button>
        <Button
          variant="secondary"
          size="sm"
          aria-label={t('familyTree.fit')}
          title={t('familyTree.fit')}
          onClick={() => {
            setMoved(null)
          }}
        >
          <Icon name="arrows-angle-contract" />
        </Button>
        <Button
          variant="secondary"
          size="sm"
          aria-label={t('familyTree.centreRoot')}
          title={t('familyTree.centreRoot')}
          onClick={() => {
            const root = layout.nodes.find((node) => node.personUids.includes(rootUid))
            if (root === undefined) {
              return
            }
            setMoved((current) =>
              centreOn(
                current ?? fitted,
                { x: root.x + root.width / 2, y: root.y + root.height / 2 },
                size,
              ),
            )
          }}
        >
          <Icon name="crosshair" />
        </Button>
      </div>
    </div>
  )
}
