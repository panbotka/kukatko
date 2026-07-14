import { type KeyboardEvent, useState } from 'react'
import CloseButton from 'react-bootstrap/CloseButton'
import Form from 'react-bootstrap/Form'
import { useTranslation } from 'react-i18next'

import { addKeywords } from '../../lib/photoFacts'
import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'

/** Props for {@link KeywordsInput}. */
export interface KeywordsInputProps {
  /** The DOM id of the text input, so its label points at the right control. */
  id: string
  /** The (already translated) field label. */
  label: string
  /** The current keywords, in order — trimmed and free of duplicates. */
  value: string[]
  /** Called with the next keyword list whenever a chip is added or removed. */
  onChange: (next: string[]) => void
  /** The cap, in runes, on the joined comma-separated string the backend stores. */
  maxRunes: number
}

/**
 * The IPTC keywords as an editable chip field: the current keywords are removable
 * chips, typing one and pressing Enter (or a comma, or pasting "beach, sunset")
 * adds it, and backspace in an empty input takes the last one back. The list is
 * the component's value; the caller joins it into the single comma-separated
 * string the column stores.
 *
 * The chips wear the shared tag look (`badge rounded-pill` + `ENTITY_STYLE.tag`)
 * so the field is recognisably the same kind of control as the label editor — but
 * they are **not** labels: they carry no link to `/labels/:uid`, because an IPTC
 * keyword is verbatim text from the source file, not an entity in the catalogue.
 *
 * Pending text is committed on blur as well, so tabbing or clicking straight to
 * Save cannot silently drop a keyword the user has typed but not yet entered.
 */
export function KeywordsInput({ id, label, value, onChange, maxRunes }: KeywordsInputProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState('')

  /** Turns the typed/pasted text into chips, dropping what the list already has. */
  function commit(raw: string) {
    const next = addKeywords(value, raw, maxRunes)
    if (next.length !== value.length) {
      onChange(next)
    }
    setDraft('')
  }

  function handleKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'Enter') {
      // Without this the key would submit the whole metadata form instead.
      event.preventDefault()
      commit(draft)
      return
    }
    if (event.key === 'Backspace' && draft === '' && value.length > 0) {
      onChange(value.slice(0, -1))
    }
  }

  return (
    <Form.Group className="mb-2">
      <Form.Label htmlFor={id} className="small text-secondary mb-1">
        {label}
      </Form.Label>
      {value.length > 0 && (
        <div className="d-flex flex-wrap gap-2 mb-2">
          {value.map((keyword) => (
            <span
              key={keyword}
              className={`badge rounded-pill ${ENTITY_STYLE.tag.className} d-inline-flex align-items-center gap-1`}
            >
              <Icon name={ENTITY_STYLE.tag.icon} />
              {keyword}
              <CloseButton
                variant="white"
                aria-label={t('photo.metadata.removeKeyword', { name: keyword })}
                onClick={() => {
                  onChange(value.filter((current) => current !== keyword))
                }}
              />
            </span>
          ))}
        </div>
      )}
      <Form.Control
        id={id}
        value={draft}
        placeholder={t('photo.metadata.keywordsPlaceholder')}
        aria-describedby={`${id}-help`}
        onChange={(event) => {
          // A comma — typed or pasted — ends a keyword, so it never reaches the draft.
          if (event.target.value.includes(',')) {
            commit(event.target.value)
            return
          }
          setDraft(event.target.value)
        }}
        onKeyDown={handleKeyDown}
        onBlur={() => {
          commit(draft)
        }}
      />
      <Form.Text id={`${id}-help`} className="text-secondary d-block">
        {t('photo.metadata.keywordsHelp')}
      </Form.Text>
    </Form.Group>
  )
}
