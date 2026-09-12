import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { avatarInitial, avatarTone } from '../../lib/avatarIdentity'
import { AVATAR_CIRCLE, NODE_HEIGHT, PERSON_WIDTH } from '../../lib/familyLayout'
import { formatLifeSpan } from '../../lib/lifeYears'
import { truncateText } from '../../lib/text'
import { subjectAvatarUrl } from '../../services/people'

/** The person a card draws: only what fits inside a box of {@link PERSON_WIDTH}. */
export interface TreePerson {
  uid: string
  name: string
  birth_year: number | null
  death_year: number | null
  photo_count: number
}

/** The id of the clip that rounds an avatar; {@link TreeStage} defines it once. */
export const AVATAR_CLIP_ID = 'kk-tree-avatar-clip'

/** How many letters of a name fit beside the face before it has to be cut. */
const NAME_LIMIT = 17

/** Props for {@link TreePersonCard}. */
export interface TreePersonCardProps {
  /** The person to draw; undefined draws the uid, which is better than nothing. */
  person: TreePerson | undefined
  /** Who this is, for the avatar request and for the fallback caption. */
  uid: string
  /** Where clicking the card leads. */
  href: string
  /** Where the pointer is told it is going — also the card's accessible name. */
  label: string
  /** A longer hint for the hover tooltip; the label is used when it is absent. */
  title?: string
  /** What a repeated card says it is; each drawing repeats a person for its own reason. */
  repeatTitle?: string
  /** Whether this person is drawn somewhere else in the same drawing too. */
  repeat?: boolean
  /** Whether this is the person the drawing is rooted at. */
  root?: boolean
}

/**
 * One person inside a box of either drawing: a round face, their name, and the
 * years anybody recorded for them.
 *
 * It is shared by the descendants tree and the ancestors pedigree because a
 * person looks the same whichever way the drawing walks — only what surrounds
 * them differs — and two cards drifting apart would be two things to keep
 * consistent for no gain.
 *
 * The face is the server-cut square (`GET /subjects/{uid}/avatar`), falling back
 * to the coloured initial for somebody with no photographs at all — which in a
 * family tree is an ordinary case and not a failure, since a great-grandmother
 * nobody photographed is exactly the kind of person the tree is drawn for. The
 * name is cut to the card's width with an ellipsis (SVG has no
 * `text-overflow`), and the whole of it lives in the link's `title`.
 */
export function TreePersonCard({
  person,
  uid,
  href,
  label,
  title,
  repeatTitle,
  repeat = false,
  root = false,
}: TreePersonCardProps) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)
  const name = person?.name ?? uid
  const lifeSpan = formatLifeSpan(person?.birth_year ?? null, person?.death_year ?? null)
  const hasPhoto = (person?.photo_count ?? 0) > 0 && !failed

  return (
    <Link to={href} className="kk-tree-person" aria-label={label}>
      <title>
        {repeat ? (repeatTitle ?? t('familyTree.repeatHint', { name })) : (title ?? label)}
      </title>
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
          x={AVATAR_CIRCLE.cx - AVATAR_CIRCLE.r}
          y={AVATAR_CIRCLE.cy - AVATAR_CIRCLE.r}
          width={AVATAR_CIRCLE.r * 2}
          height={AVATAR_CIRCLE.r * 2}
          clipPath={`url(#${AVATAR_CLIP_ID})`}
          preserveAspectRatio="xMidYMid slice"
          onError={() => {
            setFailed(true)
          }}
        />
      ) : (
        <>
          <circle
            className={`kk-tree-person__initial kk-tree-person__initial--tone-${avatarTone(name)}`}
            cx={AVATAR_CIRCLE.cx}
            cy={AVATAR_CIRCLE.cy}
            r={AVATAR_CIRCLE.r}
          />
          <text className="kk-tree-person__letter" x={AVATAR_CIRCLE.cx} y={AVATAR_CIRCLE.cy}>
            {avatarInitial(name)}
          </text>
        </>
      )}
      <text
        className="kk-tree-person__name"
        x={AVATAR_CIRCLE.cx + AVATAR_CIRCLE.r + 10}
        y={lifeSpan === null ? 34 : 26}
      >
        {truncateText(name, NAME_LIMIT)}
      </text>
      {lifeSpan !== null && (
        <text className="kk-tree-person__years" x={AVATAR_CIRCLE.cx + AVATAR_CIRCLE.r + 10} y={43}>
          {lifeSpan}
        </text>
      )}
    </Link>
  )
}
