import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { type LabelEntry, type LabelFamilyEntry } from '../../lib/labelBrowse'
import { Icon } from '../Icon'

import { type LabelChipActions, LabelChip } from './LabelChip'

/** Props for {@link LabelCloud}. */
export interface LabelCloudProps {
  /** What to draw, already searched, folded and ordered by `lib/labelBrowse`. */
  entries: LabelEntry[]
  /** The editor actions each chip offers, or `undefined` for a read-only cloud. */
  actions?: LabelChipActions
  /** Expands or folds a numbered family (and so writes the URL). */
  onToggleFamily: (key: string) => void
}

/**
 * The labels index as a wrapping cloud of pills.
 *
 * A label is one word and a number; given a full-width row each, a hundred of
 * them made a document five screens tall in which the alphabet alone decided
 * what you met first. Wrapped as chips the same hundred fit into a screen or
 * two, and the ordering — most photos first by default — decides what leads.
 *
 * It is a real list (`ul`/`li`), so a screen reader announces how many labels
 * there are instead of reading a heap of links. A folded family renders as one
 * chip that takes a whole line when open, with its members in their own list
 * below it — indented and boxed, so an expanded `Dum…` cannot be mistaken for
 * the cloud continuing.
 */
export function LabelCloud({ entries, actions, onToggleFamily }: LabelCloudProps) {
  const { t } = useTranslation()

  return (
    <ul className="kk-label-cloud list-unstyled mb-0" aria-label={t('labels.cloud')}>
      {entries.map((entry) =>
        entry.kind === 'label' ? (
          <li key={entry.key}>
            <LabelChip label={entry.label} actions={actions} />
          </li>
        ) : (
          <li key={entry.key} className={entry.expanded ? 'kk-label-cloud__group' : undefined}>
            <FamilyToggle family={entry} onToggle={onToggleFamily} />
            {entry.expanded && (
              <ul
                id={familyListId(entry.key)}
                className="kk-label-cloud kk-label-cloud__members list-unstyled mb-0 mt-2"
                aria-label={t('labels.group.members', { prefix: entry.prefix })}
              >
                {entry.labels.map((label) => (
                  <li key={label.uid}>
                    <LabelChip label={label} actions={actions} />
                  </li>
                ))}
              </ul>
            )}
          </li>
        ),
      )}
    </ul>
  )
}

/** DOM id of a family's member list, so its toggle can point `aria-controls` at it. */
function familyListId(key: string): string {
  return `label-family-${key}`
}

/**
 * The chip standing in for a folded family: the shared prefix, how many labels
 * hide behind it and a chevron saying which way it currently sits.
 *
 * It shows the number of *labels*, not of photos, because that is the question
 * it answers — "how much is hidden here?" — and the accessible name says the
 * word out loud, since a bare number beside a name reads as a photo count
 * everywhere else in the cloud.
 */
function FamilyToggle({
  family,
  onToggle,
}: {
  family: LabelFamilyEntry
  onToggle: (key: string) => void
}) {
  const { t } = useTranslation()
  const name = t('labels.group.toggle', { prefix: family.prefix, count: family.labels.length })

  return (
    <Button
      type="button"
      variant="outline-secondary"
      className="kk-label-family rounded-pill d-inline-flex align-items-center gap-2"
      aria-expanded={family.expanded}
      aria-controls={family.expanded ? familyListId(family.key) : undefined}
      aria-label={name}
      title={name}
      onClick={() => {
        onToggle(family.key)
      }}
    >
      <Icon name={family.expanded ? 'chevron-down' : 'chevron-right'} />
      <span className="kk-label-chip__name">
        {t('labels.group.name', { prefix: family.prefix })}
      </span>
      <span className="kk-label-chip__count">{family.labels.length}</span>
    </Button>
  )
}
