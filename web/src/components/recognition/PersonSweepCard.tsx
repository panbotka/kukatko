import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { candidateKey } from '../../lib/candidateReview'
import { focusKey, type PersonState, personActionableCount } from '../../lib/recognitionSweep'
import { type Candidate } from '../../services/faces'
import { CandidateCard } from '../faces/CandidateCard'
import { Icon } from '../Icon'

/** Live "confirm all" progress for this card, or null when none is running. */
export interface PersonConfirmAll {
  current: number
  total: number
}

/** Props for {@link PersonSweepCard}. */
export interface PersonSweepCardProps {
  /** The person and its live review list. */
  person: PersonState
  /** The page-global focus key, so the right card draws the ring. */
  focusedKey: string | null
  /** Set while a "confirm all" is walking this person's list. */
  running: PersonConfirmAll | null
  /** Confirms one candidate (assigns the person). */
  onConfirm: (candidate: Candidate) => void
  /** Rejects one candidate (persists "not this person" and removes the card). */
  onReject: (candidate: Candidate) => void
  /** Confirms every actionable candidate of this person, in order. */
  onConfirmAll: () => void
  /** Stops a running "confirm all". */
  onCancelConfirmAll: () => void
}

/**
 * PersonSweepCard is one person's block on the /recognition sweep: a header with the
 * name, the count still to decide and a "Potvrdit vše" batch button, above the same
 * bbox-annotated candidate grid the /faces page uses (reused {@link CandidateCard}).
 * The card is meant to disappear once its last candidate is cleared — the parent drops
 * a fully-cleared person from the list, so this component just renders what it is given.
 */
export function PersonSweepCard({
  person,
  focusedKey,
  running,
  onConfirm,
  onReject,
  onConfirmAll,
  onCancelConfirmAll,
}: PersonSweepCardProps) {
  const { t } = useTranslation()
  const actionable = personActionableCount(person)

  return (
    <section className="mb-4" data-testid="person-sweep-card" data-subject={person.subject.uid}>
      <div className="d-flex flex-wrap align-items-center gap-2 mb-2">
        <h2 className="h5 mb-0">{person.subject.name}</h2>
        <Badge bg="info" pill>
          {t('recognition.person.actionable', { count: actionable })}
        </Badge>
        <span className="ms-auto d-flex align-items-center gap-2">
          {running !== null ? (
            <>
              <span className="small text-secondary">
                {t('recognition.person.confirmingProgress', {
                  current: running.current,
                  total: running.total,
                })}
              </span>
              <Button variant="outline-secondary" size="sm" onClick={onCancelConfirmAll}>
                {t('recognition.person.cancelConfirmAll')}
              </Button>
            </>
          ) : (
            <Button variant="success" size="sm" onClick={onConfirmAll} disabled={actionable === 0}>
              <Icon name="check-lg" className="me-1" />
              {t('recognition.person.confirmAll', { count: actionable })}
            </Button>
          )}
        </span>
      </div>

      <div
        className="d-grid gap-3"
        style={{ gridTemplateColumns: 'repeat(auto-fill, minmax(16rem, 1fr))' }}
      >
        {person.items.map((item) => (
          <CandidateCard
            key={candidateKey(item.candidate)}
            item={item}
            focused={focusKey(person.subject.uid, item.candidate) === focusedKey}
            onConfirm={() => {
              onConfirm(item.candidate)
            }}
            onReject={() => {
              onReject(item.candidate)
            }}
          />
        ))}
      </div>
    </section>
  )
}
