import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { markdownPhotoUids } from '../../lib/markdownLinks'
import { photoRefHref } from '../../lib/photoRefs'
import { Markdown } from '../Markdown'
import { useProjectSlug } from '../../project/ProjectContext'
import { ProjectLink } from '../../project/ProjectLink'
import { type CommentMention } from '../../services/comments'
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
  /**
   * The accounts the body mentions, as the server resolved them with the
   * comment (`Comment.mentions`). Every `@[name](user:uid)` token renders as a
   * `MentionChip` from it. Defaults to none resolved, so a token still shows
   * as a chip with the name it was written with.
   */
  mentions?: CommentMention[]
  /** The reader's own account, so a mention of them stands out. */
  currentUserUid?: string | null
}

/**
 * A comment's body as the reader sees it: the text rendered as Markdown, with
 * every photo uid in it turned into a link, and under it a strip of small
 * thumbnails of the photographs it refers to.
 *
 * The body goes through the app's one {@link Markdown} renderer — sanitised, raw
 * HTML inert — so `**bold**`, `` `code` ``, a `[name](url)` link and a table
 * read as they were meant, in a task's thread and a photo's alike. A single
 * newline is a line break (`breaks`), so a short typed comment looks as typed.
 * A typed heading starts at level 4 and is drawn small, under the thread's own.
 *
 * An `@`-mention — `@[Petr Kolář](user:<uid>)`, as the composer writes it —
 * renders as a `MentionChip`: the account's current name on a small pill,
 * linking to the person it is linked to, the reader's own drawn stronger
 * (`lib/mentions` `remarkMentions`, inside the sanitised pipeline).
 *
 * A bare photo uid token in the text still becomes a link with the uid itself
 * as its text; a reviewer's "these three are wrong" followed by three uids
 * reads the same in the CLI and clicks in the web. That substitution sees text
 * only — a uid inside `code` or already inside a link is left alone. Uids that
 * are not in the current task's group still link — the reference is to the
 * photograph, not to its membership.
 *
 * The strip shows every photograph the comment refers to — bare uid tokens and
 * own-site `/photos/<uid>` links alike, each once, in order of first mention.
 * It caps at {@link COMMENT_STRIP_MAX} and says "+N" for the rest: a batch
 * comment can name sixty photographs, and sixty thumbnails under one remark
 * would be the wall over again. A thumbnail that fails to load (a photo since
 * deleted, a mistyped uid of the right shape) drops out rather than showing a
 * broken image.
 */
export function CommentBody({ body, taskUid, mentions, currentUserUid = null }: CommentBodyProps) {
  const { t } = useTranslation()
  // The project the comment is read in: a photo link naming another project is
  // followed there, never previewed through this one's API.
  const project = useProjectSlug()
  const uids = useMemo(
    () => markdownPhotoUids(body, window.location.origin, project),
    [body, project],
  )
  const shown = uids.slice(0, COMMENT_STRIP_MAX)
  const rest = uids.length - shown.length
  const resolved = useMemo(() => mentions ?? [], [mentions])

  return (
    <>
      <Markdown
        className="kk-comment__body"
        breaks
        photoRefs
        headingFloor={4}
        taskUid={taskUid}
        mentions={resolved}
        currentUserUid={currentUserUid}
      >
        {body}
      </Markdown>
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
    <ProjectLink
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
    </ProjectLink>
  )
}
