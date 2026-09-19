import { type KeyboardEvent, useState } from 'react'
import { Button, Form } from 'react-bootstrap'
import { useTranslation } from 'react-i18next'

import { MAX_TASK_OPTION_LENGTH, MAX_TASK_OPTIONS } from '../../services/tasks'
import { Icon } from '../Icon'

/** Props for {@link AnswerOptionsEditor}. */
export interface AnswerOptionsEditorProps {
  /** The options as they stand, in order. */
  value: readonly string[]
  /** Called with the whole set after every addition or removal. */
  onChange: (next: string[]) => void
  /** True while the surrounding form is saving, so the editor stands down with it. */
  disabled?: boolean
  /** Prefix for the field ids, so two editors on one page do not collide. */
  idPrefix: string
}

/**
 * Edits the short answers a question offers: up to five entries, added one at a
 * time and removable as chips.
 *
 * One entry at a time rather than a comma-separated field, because an answer may
 * itself contain a comma ("1936, spíš") and because a chip per answer shows the
 * exact strings that will become buttons — and, verbatim, comments. What cannot
 * be added is refused quietly: the plus stays disabled for a blank, an over-long
 * or a repeated entry, and the field disappears once the set is full, with a
 * line saying why. Enter adds without submitting the form around it.
 */
export function AnswerOptionsEditor({
  value,
  onChange,
  disabled = false,
  idPrefix,
}: AnswerOptionsEditorProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState('')

  const trimmed = draft.trim()
  const full = value.length >= MAX_TASK_OPTIONS
  const canAdd =
    !disabled &&
    !full &&
    trimmed !== '' &&
    trimmed.length <= MAX_TASK_OPTION_LENGTH &&
    !value.includes(trimmed)

  function add() {
    if (!canAdd) {
      return
    }
    onChange([...value, trimmed])
    setDraft('')
  }

  function onKeyDown(event: KeyboardEvent<HTMLInputElement>) {
    if (event.key === 'Enter') {
      // The editor lives inside forms whose Enter means "save"; here it means
      // "one more option", and it must not do both.
      event.preventDefault()
      add()
    }
  }

  const inputId = `${idPrefix}-option`

  return (
    <Form.Group className="mb-3">
      <Form.Label htmlFor={inputId}>{t('taskDetail.controls.options.label')}</Form.Label>
      {value.length > 0 && (
        <ul className="list-unstyled d-flex flex-wrap gap-2 mb-2">
          {value.map((option) => (
            <li key={option}>
              <span className="badge text-bg-secondary d-inline-flex align-items-center gap-1 fs-6 fw-normal">
                {option}
                <Button
                  variant="link"
                  size="sm"
                  className="p-0 lh-1 text-reset"
                  disabled={disabled}
                  aria-label={t('taskDetail.controls.options.remove', { text: option })}
                  onClick={() => {
                    onChange(value.filter((entry) => entry !== option))
                  }}
                >
                  <Icon name="x-lg" />
                </Button>
              </span>
            </li>
          ))}
        </ul>
      )}
      {full ? (
        <Form.Text>{t('taskDetail.controls.options.full')}</Form.Text>
      ) : (
        <>
          <div className="d-flex gap-2">
            <Form.Control
              id={inputId}
              value={draft}
              disabled={disabled}
              maxLength={MAX_TASK_OPTION_LENGTH}
              placeholder={t('taskDetail.controls.options.placeholder')}
              onChange={(event) => {
                setDraft(event.target.value)
              }}
              onKeyDown={onKeyDown}
            />
            <Button
              variant="outline-secondary"
              disabled={!canAdd}
              aria-label={t('taskDetail.controls.options.add')}
              title={t('taskDetail.controls.options.add')}
              onClick={add}
            >
              <Icon name="plus-lg" />
            </Button>
          </div>
          <Form.Text>{t('taskDetail.controls.options.hint')}</Form.Text>
        </>
      )}
    </Form.Group>
  )
}
