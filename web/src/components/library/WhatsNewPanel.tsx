import { Fragment, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

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
 * one place that says so. It is deliberately a summary of four lines and not a
 * feed — a second timeline competing with the real one would be worse than
 * nothing.
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
  const [dismissedSince, setDismissedSince] = useState<string>(() => readDismissedWhatsNew())

  if (!summary?.has_news) {
    return null
  }
  const since = summary.since ?? ''
  if (since !== '' && dismissedSince === since) {
    return null
  }

  const photos = summary.photos ?? 0
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

  return (
    <Alert
      // A quiet slate rather than the loud `info` cyan: this appears on every
      // return, not on an incident, and it must read as part of the library
      // rather than as an interruption of it.
      variant="dark"
      dismissible
      closeLabel={t('whatsNew.dismiss')}
      onClose={() => {
        writeDismissedWhatsNew(since)
        setDismissedSince(since)
      }}
    >
      <div className="d-flex align-items-start gap-2">
        <Icon name="clock-history" className="mt-1 flex-shrink-0" />
        <div className="text-break">
          <div className="fw-semibold">{t('whatsNew.title')}</div>
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
    </Alert>
  )
}
