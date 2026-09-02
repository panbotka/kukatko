import type { ParseKeys } from 'i18next'
import { useState } from 'react'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { toMode } from '../../lib/searchView'
import { type SearchMode } from '../../services/photos'
import { Icon } from '../Icon'

/** The mode every reader gets without touching anything. */
const DEFAULT_MODE = 'hybrid'

/** The modes, in the order they are offered. */
const MODES: readonly SearchMode[] = ['hybrid', 'fulltext', 'semantic']

/** The plain-language name of each mode. */
const MODE_LABEL: Record<SearchMode, ParseKeys> = {
  hybrid: 'search.mode.hybrid',
  fulltext: 'search.mode.fulltext',
  semantic: 'search.mode.semantic',
}

/** One sentence saying what the picked mode actually does. */
const MODE_HINT: Record<SearchMode, ParseKeys> = {
  hybrid: 'search.modeHint.hybrid',
  fulltext: 'search.modeHint.fulltext',
  semantic: 'search.modeHint.semantic',
}

/** Props for {@link SearchModeControl}. */
export interface SearchModeControlProps {
  /** The mode the reader picked, exactly as it stands in the URL. */
  mode: string
  /** Called with the newly picked mode. */
  onChange: (mode: string) => void
  /**
   * Whether the instance can answer a semantic search at all. With the
   * embeddings box offline it cannot, so that option is taken off the menu
   * rather than silently answered by something else.
   */
  semanticAvailable: boolean
}

/**
 * The "how should I search" switch, kept out of the way.
 *
 * Naming the three ranking strategies — hybrid, full-text, semantic — is asking
 * a family member to pick a retrieval algorithm before they may look for a
 * photograph of a wedding. So the modes now carry plain names ("Chytré /
 * Podle textu / Podle obsahu fotky"), each with one sentence saying what it
 * does, and the switch itself sits behind an unobtrusive **Rozšířené** toggle:
 * everyone gets the smart mode, and only a reader who wants to say otherwise
 * ever meets the choice.
 *
 * The panel opens by itself whenever the search is *not* running in the default
 * mode — a shared `?mode=semantic` link, or Back into one — so results that
 * differ from what the same query gives anyone else always have their reason on
 * screen. Collapsed on a non-default mode (the reader tidied up after
 * switching), the toggle keeps saying which mode is in force rather than hiding
 * it.
 */
export function SearchModeControl({ mode, onChange, semanticAvailable }: SearchModeControlProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(() => mode !== DEFAULT_MODE)
  const panelId = 'search-mode-panel'
  const picked = toMode(mode)
  // Only while folded up: with the panel open the select right below already
  // says which mode is in force, and saying it twice reads as two settings.
  const label =
    open || mode === DEFAULT_MODE
      ? t('search.advanced')
      : t('search.advancedWithMode', { mode: t(MODE_LABEL[picked]) })

  return (
    <div>
      <Button
        variant="link"
        size="sm"
        className="p-0 text-secondary text-decoration-none d-inline-flex align-items-center gap-1"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => {
          setOpen((prev) => !prev)
        }}
      >
        <Icon name={open ? 'chevron-down' : 'chevron-right'} />
        {label}
      </Button>

      {open && (
        <div id={panelId} className="mt-2">
          <Form.Group controlId="search-mode">
            <Form.Label className="small mb-1">{t('search.modeLabel')}</Form.Label>
            <Form.Select
              className="w-auto"
              value={mode}
              onChange={(e) => {
                onChange(e.target.value)
              }}
            >
              {MODES.map((option) => (
                <option
                  key={option}
                  value={option}
                  // Semantic search cannot be served at all without the sidecar;
                  // hybrid stays, as full-text is a fair half of what it promises.
                  disabled={option === 'semantic' && !semanticAvailable}
                  title={
                    option === 'semantic' && !semanticAvailable
                      ? t('search.semanticUnavailable')
                      : undefined
                  }
                >
                  {t(MODE_LABEL[option])}
                </option>
              ))}
            </Form.Select>
            <Form.Text>{t(MODE_HINT[picked])}</Form.Text>
          </Form.Group>
        </div>
      )}
    </div>
  )
}
