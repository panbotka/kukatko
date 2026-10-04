import { Trans, useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { UPLOAD_LINKS_PATH, uploadLinkSender } from '../../lib/uploadLinks'
import { type PhotoUploadLinkRef } from '../../services/photos'
import { Icon } from '../Icon'

/** Props for {@link UploadLinkLine}. */
export interface UploadLinkLineProps {
  /** The photo detail's upload-link provenance. */
  link: PhotoUploadLinkRef
}

/**
 * The one-line upload-link provenance on the photo's info view — "Nahráno přes
 * odkaz „Pouť 2026“ · od: Jana", or "… · bez jména" when nobody said who they
 * are, never an empty "od:". The link part opens the curators' upload-link page;
 * the backend serves the block to curators and above only, so whoever sees the
 * line may follow it. An untitled link is named as such rather than as empty
 * quotes.
 */
export function UploadLinkLine({ link }: UploadLinkLineProps) {
  const { t } = useTranslation()
  const sender = uploadLinkSender(link)
  const title = link.title.trim()
  return (
    <p className="small text-secondary mb-0 d-flex align-items-baseline gap-2">
      <Icon name="link-45deg" />
      <span>
        <Trans
          i18nKey={title === '' ? 'photo.uploadLink.viaUntitled' : 'photo.uploadLink.via'}
          values={{ title }}
          components={{
            // Not <link>: that is a void element to the HTML parser behind
            // Trans, which would drop the title inside it.
            page: <Link to={UPLOAD_LINKS_PATH} className="text-decoration-none" />,
          }}
        />
        {' · '}
        {sender === undefined
          ? t('photo.uploadLink.nameless')
          : t('photo.uploadLink.from', { name: sender })}
      </span>
    </p>
  )
}
