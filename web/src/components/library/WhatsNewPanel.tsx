import { Fragment, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Collapse from 'react-bootstrap/Collapse'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../../auth/AuthContext'
import { useWhatsNew } from '../../hooks/useWhatsNew'
import { formatDateTimeMinutes } from '../../lib/format'
import { LIBRARY_PATH } from '../../lib/libraryView'
import { readDismissedWhatsNew, writeDismissedWhatsNew } from '../../lib/whatsNewDismissal'
import { Icon } from '../Icon'

/**
 * Where "X new photos" goes: the library in "recently added" order, so the newest
 * arrivals are the first tiles rather than something the reader has to hunt for
 * down a capture-time timeline (a scan of grandma's 1962 negatives is *new* but
 * not *recent*).
 */
const RECENTLY_ADDED_HREF = `${LIBRARY_PATH}?sort=added`

/** DOM id of the collapsible detail, so the toggle can point `aria-controls` at it. */
const DETAIL_ID = 'whats-new-detail'

/** One named, linkable entity in the digest: a new album or a newly named person. */
interface DigestLink {
  uid: string
  label: string
  href: string
}

/**
 * A line of the digest: a lead-in ("New albums:") followed by up to a handful of
 * links and, when the visit produced more than fit, a plain "and N more".
 *
 * The overflow tail is deliberately not a link: there is no page that lists
 * "albums created since Tuesday", and a link that lands somewhere approximate is
 * worse than a number that is honest about being just a number.
 */
function DigestLine({
  label,
  links,
  total,
}: {
  label: string
  links: DigestLink[]
  total: number
}) {
  const { t } = useTranslation()
  const rest = total - links.length

  return (
    <li>
      <span className="opacity-75">{label} </span>
      {links.map((link, index) => (
        <Fragment key={link.uid}>
          {index > 0 && <span>, </span>}
          <Link to={link.href}>{link.label}</Link>
        </Fragment>
      ))}
      {rest > 0 && <span className="opacity-75"> {t('whatsNew.more', { count: rest })}</span>}
    </li>
  )
}

/**
 * "What's new since your last visit" — the digest a returning reader sees above
 * the library grid: how many photos arrived, which albums were created, which
 * people were named, how many comments were written.
 *
 * The library is shared by a family, so somebody else's evening of uploading is
 * otherwise invisible to the next person who opens the app; this panel is the
 * one place that says so. It is deliberately a summary and not a feed — a second
 * timeline competing with the real one would be worse than nothing.
 *
 * **At rest it is one line**: the counts, joined, with the named albums and
 * people, the exact moment of the last visit and every link folded into a
 * detail the reader opens. It sits above everything on the library page, so
 * whatever height it takes it takes from the photographs; four lines of it was
 * the price of a piece of news most visits do not act on. Opening the detail is
 * one click, and it says the same as before, in the same order.
 *
 * It renders nothing at all while loading, on a first-ever visit, when the visit
 * found nothing, or once the reader has closed it. Dismissal is keyed on the
 * digest's `since` (see {@link readDismissedWhatsNew}), which is constant for
 * the length of a visit: closing the panel closes it for this visit — through
 * every reload and every walk around the app — and the next visit brings its own.
 *
 * Every role sees it, viewers included: learning what the family added is not a
 * curation power.
 */
export function WhatsNewPanel() {
  const { t, i18n } = useTranslation()
  const summary = useWhatsNew()
  const { user } = useAuth()
  const [dismissedSince, setDismissedSince] = useState<string>(() => readDismissedWhatsNew())
  // Shut by default and not remembered: the resting state of the library is the
  // one that shows photographs, and a digest is read once and then acted on (or
  // not) rather than kept open across visits.
  const [expanded, setExpanded] = useState(false)
  // Where "N new photos of you" goes: the reader's own person, in recently-added
  // order like the line above it. The count came from the same link the server
  // read off this account, so the two always describe the same photographs.
  const linked = user?.subject_uid
  const mineHref =
    linked === null || linked === undefined || linked === ''
      ? null
      : `${LIBRARY_PATH}?person=${encodeURIComponent(linked)}&sort=added`

  if (!summary?.has_news) {
    return null
  }
  const since = summary.since ?? ''
  if (since !== '' && dismissedSince === since) {
    return null
  }

  const photos = summary.photos ?? 0
  const minePhotos = summary.mine_photos ?? 0
  const comments = summary.comments ?? 0
  const albumTotal = summary.album_count ?? 0
  const personTotal = summary.person_count ?? 0
  const albums: DigestLink[] = (summary.albums ?? []).map((album) => ({
    uid: album.uid,
    label: album.title,
    href: `/albums/${album.uid}`,
  }))
  const people: DigestLink[] = (summary.people ?? []).map((person) => ({
    uid: person.uid,
    label: person.name,
    href: `/people/${person.uid}`,
  }))

  // The one line, built from exactly the lines the detail below will show, in
  // the same order — the summary and its elaboration can therefore never
  // disagree about what happened. Each part is the same worded count the detail
  // uses ("5 new photos"), because a bare number would need a legend and the
  // point of the line is that it needs none.
  const summaryParts = [
    photos > 0 ? t('whatsNew.photos', { count: photos }) : '',
    minePhotos > 0 && mineHref !== null ? t('whatsNew.minePhotos', { count: minePhotos }) : '',
    albumTotal > 0 ? t('whatsNew.albumsCount', { count: albumTotal }) : '',
    personTotal > 0 ? t('whatsNew.peopleCount', { count: personTotal }) : '',
    comments > 0 ? t('whatsNew.comments', { count: comments }) : '',
  ].filter((part) => part !== '')

  const toggleLabel = t(expanded ? 'whatsNew.collapse' : 'whatsNew.expand')

  return (
    <Alert
      // A quiet slate rather than the loud `info` cyan: this appears on every
      // return, not on an incident, and it must read as part of the library
      // rather than as an interruption of it.
      variant="dark"
      // Half the vertical padding Bootstrap gives an alert, so one line of text
      // costs one line of the page (see `.kk-whats-new`).
      className="kk-whats-new"
      dismissible
      closeLabel={t('whatsNew.dismiss')}
      onClose={() => {
        writeDismissedWhatsNew(since)
        setDismissedSince(since)
      }}
    >
      <div className="d-flex align-items-center gap-2">
        <Icon name="clock-history" className="flex-shrink-0" />
        {/* One line, and one line only: it truncates rather than wraps, because
            a digest that grows a second row is back to taking a block of the
            first screen — and everything it elides is a click away below. */}
        <div className="text-truncate flex-grow-1">
          <span className="fw-semibold">{t('whatsNew.title')}</span>
          {summaryParts.length > 0 && (
            <span className="opacity-75">{` — ${summaryParts.join(' · ')}`}</span>
          )}
        </div>
        {/* Inside a filled alert neither `variant="link"` nor `alert-link` is
            legible in this theme, so the toggle inherits the alert's own colour
            and earns its underline instead. Below `sm` the word gives way to the
            chevron alone: on a 390 px screen it was eating a third of the line
            the summary has to fit into, and the accessible name is carried by
            `aria-label` either way. */}
        <Button
          type="button"
          variant="link"
          size="sm"
          className="p-0 text-reset flex-shrink-0 d-inline-flex align-items-center gap-1"
          aria-expanded={expanded}
          aria-controls={DETAIL_ID}
          aria-label={toggleLabel}
          onClick={() => {
            setExpanded((prev) => !prev)
          }}
        >
          <span className="d-none d-sm-inline text-decoration-underline">{toggleLabel}</span>
          <Icon name={expanded ? 'chevron-up' : 'chevron-down'} />
        </Button>
      </div>
      <Collapse in={expanded}>
        <div id={DETAIL_ID}>
          {/* The inner element carries the spacing: a margin on the collapsing
              element itself would be part of the height it animates to zero and
              would leak a gap in the closed state. */}
          <div className="text-break mt-2">
            {since !== '' && (
              <div className="small opacity-75">
                {t('whatsNew.since', { date: formatDateTimeMinutes(since, i18n.language) })}
              </div>
            )}
            <ul className="list-unstyled mb-0 mt-2 d-flex flex-column gap-1">
              {photos > 0 && (
                <li>
                  <Link to={RECENTLY_ADDED_HREF}>{t('whatsNew.photos', { count: photos })}</Link>
                </li>
              )}
              {/* The one line of the digest that is about the reader rather than
                  about the library. It appears only for an account that has said
                  which person it is, and only when some of the new photographs
                  are of that person — an empty "0 new photos of you" would be
                  noise, and for most readers this line simply never exists. */}
              {minePhotos > 0 && mineHref !== null && (
                <li>
                  <Link to={mineHref}>{t('whatsNew.minePhotos', { count: minePhotos })}</Link>
                </li>
              )}
              {albumTotal > 0 && (
                <DigestLine label={t('whatsNew.albums')} links={albums} total={albumTotal} />
              )}
              {personTotal > 0 && (
                <DigestLine label={t('whatsNew.people')} links={people} total={personTotal} />
              )}
              {comments > 0 && <li>{t('whatsNew.comments', { count: comments })}</li>}
            </ul>
          </div>
        </div>
      </Collapse>
    </Alert>
  )
}
