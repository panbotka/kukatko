import { useTranslation } from 'react-i18next'

import { localizeCountryNames } from '../../i18n/countryNames'
import { type SlideshowSettings } from '../../lib/slideshowSettings'
import { formatTakenLabel } from '../../lib/takenDate'
import { type Photo } from '../../services/photos'

/** Props for {@link SlideshowCaption}. */
export interface SlideshowCaptionProps {
  /** The photo currently on the stage. */
  photo: Photo
  /** The active settings; only the three caption toggles are read. */
  settings: SlideshowSettings
}

/**
 * What the photo on screen *is*: its title, its description and when it was
 * taken, laid over the picture itself.
 *
 * Each line appears only when the reader asked for it **and** the photo actually
 * carries it, so an untitled photo shows no empty line and a show of undated
 * scans is not a column of placeholders. With nothing to say it renders nothing
 * at all, leaving the picture alone.
 *
 * It belongs to the picture, not to the chrome: it is its own element, outside
 * the control bar and the header, so a later change that fades the controls out
 * cannot take the caption with it — a slideshow whose photos stop saying what
 * they are the moment the mouse goes still would be worse than one that never
 * said anything.
 *
 * Legibility cannot depend on the photo: white text on a snowfield is the case
 * the styling is designed for, which is why the block carries its own scrim
 * rather than trusting a text shadow alone. It stays deliberately small and
 * left-hugging — secondary to the photograph, not a lower third competing with
 * it — and the description is bounded to a few lines (see `slideshow.css`), so a
 * three-paragraph note cannot grow over the picture it describes.
 */
export function SlideshowCaption({ photo, settings }: SlideshowCaptionProps) {
  const { t, i18n } = useTranslation()

  // The same country dictionary the grid and the detail heading use, so an
  // imported title does not read half in English here either.
  const title = settings.showTitle ? localizeCountryNames(photo.title, i18n.language).trim() : ''
  const description = settings.showDescription ? photo.description.trim() : ''
  const taken = settings.showDate ? formatTakenLabel(photo, t, i18n.language) : ''

  if (title === '' && description === '' && taken === '') {
    return null
  }

  return (
    <div className="slideshow__meta">
      {title !== '' && <p className="slideshow__meta-title">{title}</p>}
      {taken !== '' && <p className="slideshow__meta-date">{taken}</p>}
      {description !== '' && <p className="slideshow__meta-description">{description}</p>}
    </div>
  )
}
