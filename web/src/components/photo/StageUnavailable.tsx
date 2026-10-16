import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { ThumbUnavailable } from '../ThumbUnavailable'

/** Props for {@link StageUnavailable}. */
export interface StageUnavailableProps {
  /** The original's download address (`photo.download_url`). */
  downloadHref: string
}

/**
 * What the viewer's stage shows for a photo whose preview cannot be loaded — an
 * original the server cannot decode answers its thumbnail with a 404 — instead
 * of the browser's broken-image icon over the photo's name in an empty frame:
 * the library's own "no preview available" placeholder in a photo-shaped well,
 * and under it the one way forward there is, downloading the original.
 */
export function StageUnavailable({ downloadHref }: StageUnavailableProps) {
  const { t } = useTranslation()
  return (
    <div className="kk-viewer__unavailable">
      <div className="kk-viewer__unavailable-well">
        <ThumbUnavailable />
      </div>
      <Button as="a" href={downloadHref} variant="outline-secondary" size="sm" download>
        {t('photo.download')}
      </Button>
    </div>
  )
}
