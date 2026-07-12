import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { type PhotoDetail, updatePhoto } from '../../services/photos'
import { Icon } from '../Icon'

/** Props for {@link PrivacyToggle}. */
export interface PrivacyToggleProps {
  /** The photo whose `private` flag is toggled. */
  photo: PhotoDetail
  /** Called with the refreshed photo after the flag is flipped. */
  onUpdated: (photo: PhotoDetail) => void
}

/**
 * The private/visibility header toggle: a lock glyph that is closed when the
 * photo is private and open when it is public. Clicking it PATCHes the `private`
 * field via {@link updatePhoto} and hands the refreshed photo back, closing the
 * loop with the library's existing "private" filter. Editor/admin only (the page
 * gates rendering), so no role check is needed here; a failed request simply
 * leaves the current state in place. It sits beside the favorite heart so the
 * one editable photo-level flag the UI previously lacked is always visible.
 */
export function PrivacyToggle({ photo, onUpdated }: PrivacyToggleProps) {
  const { t } = useTranslation()
  const [pending, setPending] = useState(false)
  const isPrivate = photo.private

  async function toggle() {
    setPending(true)
    try {
      const updated = await updatePhoto(photo.uid, { private: !isPrivate })
      onUpdated(updated)
    } catch {
      // The server rejected the change; leave the visible state untouched.
    } finally {
      setPending(false)
    }
  }

  const label = isPrivate ? t('photo.privacy.makePublic') : t('photo.privacy.makePrivate')
  return (
    <button
      type="button"
      aria-pressed={isPrivate}
      aria-label={label}
      title={label}
      disabled={pending}
      onClick={() => void toggle()}
      className={`btn btn-sm p-1 lh-1 border-0 rounded-circle d-inline-flex align-items-center justify-content-center kukatko-tap-target ${
        isPrivate ? 'text-warning' : 'text-white'
      }`}
      style={{ backgroundColor: 'rgba(0, 0, 0, 0.45)', fontSize: '1.1rem' }}
    >
      <Icon name={isPrivate ? 'lock-fill' : 'unlock'} />
    </button>
  )
}
