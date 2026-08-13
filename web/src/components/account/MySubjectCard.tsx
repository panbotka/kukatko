import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../../auth/AuthContext'
import { useSubjects } from '../../hooks/useSubjects'
import { setMySubject } from '../../services/auth'
import { Icon } from '../Icon'
import { AddAutocomplete } from '../photo/AddAutocomplete'

/** What the card is doing right now. */
type State = { status: 'idle' } | { status: 'saving' } | { status: 'error' }

/**
 * "Who am I in the library?" — the one place a user says which person of the
 * photographs their account belongs to, and the one place that says what
 * saying so does.
 *
 * The link is optional and nothing depends on it: an account without it works
 * exactly as it always did. With it, three things start working — the "my
 * photos" entry in the menu, `person:me` in a search, and the "new photos of
 * you" line of the returning-visitor digest — and one thing becomes visible to
 * everybody else: that person's cover photo appears beside every comment this
 * account has written. That is stated **before** the field rather than
 * discovered afterwards, and it is stated plainly: a family archive is shared,
 * and the person marked "private" is not hidden from anyone today either (the
 * flag is recorded, and gates no reading).
 *
 * The picker is the app's ordinary subject typeahead — the same
 * {@link AddAutocomplete} over {@link useSubjects} that names a face or merges
 * two people — so choosing yourself works exactly like choosing anybody else.
 * It cannot create a person: an account belongs to somebody the library already
 * knows, and inventing an empty person here would only make a record with no
 * photographs on it.
 */
export function MySubjectCard() {
  const { t } = useTranslation()
  const { user, refresh } = useAuth()
  const { subjects, loading } = useSubjects()
  const [state, setState] = useState<State>({ status: 'idle' })

  const linkedUid = user?.subject_uid ?? null
  const linked = subjects.find((candidate) => candidate.uid === linkedUid)

  async function save(subjectUid: string | null) {
    setState({ status: 'saving' })
    try {
      await setMySubject(subjectUid)
      // The whole app reads the link off the session (the menu entry, the
      // avatar), so re-read it rather than patching a local copy.
      await refresh()
      setState({ status: 'idle' })
    } catch {
      setState({ status: 'error' })
    }
  }

  const busy = state.status === 'saving'

  return (
    <Card text="light" className="mb-4">
      <Card.Body>
        <Card.Title as="h2" className="kk-section-title mb-3">
          {t('account.subject.title')}
        </Card.Title>
        <p className="text-secondary">{t('account.subject.hint')}</p>

        {state.status === 'error' && (
          <Alert variant="danger" role="alert">
            {t('account.subject.error')}
          </Alert>
        )}

        {linkedUid === null ? (
          <>
            {/* Said before the field, not after the save: linking publishes that
                person's face next to everything this account has written. */}
            <Alert variant="warning" className="py-2">
              {t('account.subject.publishWarning')}
            </Alert>
            <AddAutocomplete
              id="account-subject"
              label={t('account.subject.pickLabel')}
              disabled={loading || busy}
              options={subjects.map((candidate) => ({
                uid: candidate.uid,
                label: candidate.name,
                // The same hint the other subject pickers show: how much of the
                // library that person is on, which is what tells two namesakes
                // apart.
                hint: String(candidate.photo_count),
              }))}
              onAdd={(uid) => {
                void save(uid)
              }}
            />
          </>
        ) : (
          <>
            <p className="mb-3 d-flex align-items-center gap-2 flex-wrap">
              <Icon name="person-hearts" />
              {/* A link that has lost its person (deleted from the library)
                  still has a UID and no name; say so rather than print a blank. */}
              {linked === undefined ? (
                <span className="text-secondary">{t('account.subject.unknownPerson')}</span>
              ) : (
                <Link to={`/people/${linked.uid}`}>{linked.name}</Link>
              )}
            </p>
            <Button
              variant="outline-light"
              disabled={busy}
              onClick={() => {
                void save(null)
              }}
            >
              {busy && (
                <Spinner
                  animation="border"
                  size="sm"
                  role="status"
                  aria-hidden="true"
                  className="me-2"
                />
              )}
              {t('account.subject.clear')}
            </Button>
          </>
        )}
      </Card.Body>
    </Card>
  )
}
