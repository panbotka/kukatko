import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { photoRefHref, referencedPhotoUids, splitPhotoRefs } from '../../lib/photoRefs'
import { thumbUrl } from '../../services/photos'

/** How many referenced photographs the strip draws before folding into "+N". */
export const COMMENT_STRIP_MAX = 12

/** The rendition the strip draws — small squares, the same as the modal's preview. */
const STRIP_THUMB_SIZE = 'tile_100'

/** Props for {@link CommentBody}. */
export interface CommentBodyProps {
  /** The stored body, exactly as typed. */
  body: string
  /**
   * The task whose thread the comment is in, if it is in one. Every link then
   * carries `?task=`, so the viewer pages prev/next within the task's group and
   * Back lands on the question. A photo thread passes nothing.
   */
  taskUid?: string
}

/**
 * A comment's body as the reader sees it: the text as written, with every photo
 * uid in it turned into a link, and under it a strip of small thumbnails of the
 * photographs it refers to.
 *
 * The body stays **text** — React escapes it, nothing is parsed as HTML or
 * Markdown, and `pre-wrap` in the stylesheet keeps the writer's line breaks.
 * The only substitution is the recognised uid token, which becomes an anchor
 * with the uid itself as its text; a reviewer's "these three are wrong" followed
 * by three uids reads the same in the CLI and clicks in the web. Uids that are
 * not in the current task's group still link — the reference is to the
 * photograph, not to its membership.
 *
 * The strip caps at {@link COMMENT_STRIP_MAX} and says "+N" for the rest: a
 * batch comment can name sixty photographs, and sixty thumbnails under one
 * remark would be the wall over again. A thumbnail that fails to load (a photo
 * since deleted, a mistyped uid of the right shape) drops out rather than
 * showing a broken image.
 */
export function CommentBody({ body, taskUid }: CommentBodyProps) {
  const { t } = useTranslation()
  const segments = splitPhotoRefs(body)
  const uids = referencedPhotoUids(body)
  const shown = uids.slice(0, COMMENT_STRIP_MAX)
  const rest = uids.length - shown.length

  return (
    <>
      <p className="kk-comment__body">
        {segments.map((segment, index) =>
          segment.kind === 'text' ? (
            // Text segments have no identity of their own; their position is it.
            <span key={index}>{segment.text}</span>
          ) : (
            <Link key={index} to={photoRefHref(segment.uid, taskUid)} className="kk-comment__ref">
              {segment.uid}
            </Link>
          ),
        )}
      </p>
      {shown.length > 0 && (
        <ul className="kk-comment__strip" aria-label={t('photo.comments.referenced')}>
          {shown.map((uid) => (
            <li key={uid}>
              <CommentThumb uid={uid} taskUid={taskUid} />
            </li>
          ))}
          {rest > 0 && (
            <li
              className="kk-comment__strip-more"
              aria-label={t('photo.comments.moreRefs', { count: rest })}
            >
              +{rest}
            </li>
          )}
        </ul>
      )}
    </>
  )
}

/**
 * One thumbnail of the strip, linking where the uid in the text links. It
 * remembers its own load failure and renders nothing then: the strip is a
 * courtesy on top of the links, and a broken-image glyph would say something
 * went wrong about a comment that is perfectly fine.
 */
function CommentThumb({ uid, taskUid }: { uid: string; taskUid?: string }) {
  const { t } = useTranslation()
  const [failed, setFailed] = useState(false)
  if (failed) {
    return null
  }
  return (
    <Link
      to={photoRefHref(uid, taskUid)}
      className="kk-comment__thumb"
      aria-label={t('photo.comments.refPhoto', { uid })}
    >
      <img
        src={thumbUrl(uid, STRIP_THUMB_SIZE)}
        alt=""
        loading="lazy"
        decoding="async"
        onError={() => {
          setFailed(true)
        }}
      />
    </Link>
  )
}
