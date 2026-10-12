import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Icon } from '../Icon'
import { type Mapset, MAPSETS } from '../../services/map'

/** Props for {@link MapsetControl}. */
export interface MapsetControlProps {
  /** The active mapset. */
  mapset: Mapset
  /** Switches the base map. */
  onChange: (mapset: Mapset) => void
  /**
   * Whether to fold the three choices behind one layers button (a phone, where
   * a row of three labels over the map would crowd the photos it is there for).
   */
  compact: boolean
}

/**
 * The base-map switch as a small control floating over the map's corner — the
 * map is the page, so its chrome stays on it rather than in a toolbar above.
 * Three mutually exclusive choices, each a pressed/unpressed button. On a phone
 * they hide behind a single layers button and fold away again once one is
 * picked.
 */
export function MapsetControl({ mapset, onChange, compact }: MapsetControlProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const showChoices = !compact || expanded

  return (
    <div
      className={compact ? 'kk-map-mapset kk-map-mapset--compact' : 'kk-map-mapset'}
      role="group"
      aria-label={t('map.mapset.label')}
    >
      {compact && (
        <button
          type="button"
          className="kk-map-mapset-toggle"
          aria-expanded={expanded}
          aria-label={t('map.mapset.switch')}
          onClick={() => {
            setExpanded((prev) => !prev)
          }}
        >
          <Icon name="layers" />
        </button>
      )}
      {showChoices &&
        MAPSETS.map((id) => (
          <button
            key={id}
            type="button"
            className={id === mapset ? 'kk-map-mapset-option is-active' : 'kk-map-mapset-option'}
            aria-pressed={id === mapset}
            onClick={() => {
              onChange(id)
              setExpanded(false)
            }}
          >
            {t(`map.mapset.${id}`)}
          </button>
        ))}
    </div>
  )
}
