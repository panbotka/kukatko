import { useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Card from 'react-bootstrap/Card'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../../auth/AuthContext'
import { useSubjects } from '../../hooks/useSubjects'
import { projectPathFor } from '../../project/paths'
import { type ProjectMembership, setMySubject } from '../../services/auth'
import { Icon } from '../Icon'
import { AddAutocomplete } from '../photo/AddAutocomplete'

/** What a row is doing right now. */
type State = { status: 'idle' } | { status: 'saving' } | { status: 'error' }

/**
 * The memberships a person can be named in: approved ones of active projects.
 * A membership still waiting for approval sees no people, and a deactivated
 * project is entered by nobody.
 */
function linkableMemberships(projects: ProjectMembership[]): ProjectMembership[] {
  return projects.filter((membership) => membership.approved && membership.active)
}

/**
 * "Who am I in the library?" — the one place a user says which person of the
 * photographs their account belongs to, and the one place that says what
 * saying so does.
 *
 * Who you are is a fact about one project's people, and the account page
 * belongs to no project, so the card names the project every time: one row per
 * project the account is an approved member of, each headed with the project's
 * name, each linking and unlinking only in that project. It never edits
 * "the current project" implicitly — which project that is depends on where the
 * user happened to come from, and the row's heading would not have said so.
 *
 * The link is optional and nothing depends on it: an account without it works
 * exactly as it always did. With it, three things start working in that
 * project — the "my photos" entry in the menu, `person:me` in a search, and the
 * "new photos of you" line of the returning-visitor digest — and one thing
 * becomes visible to everybody else there: that person's cover photo appears
 * beside every comment this account has written. That is stated **before** the
 * field rather than discovered afterwards, and it is stated plainly: a family
 * archive is shared, and the person marked "private" is not hidden from anyone
 * today either (the flag is recorded, and gates no reading).
 *
 * The picker is the app's ordinary subject typeahead — the same
 * {@link AddAutocomplete} over {@link useSubjects}, here over the row's own
 * project — so choosing yourself works exactly like choosing anybody else. It
 * cannot create a person: an account belongs to somebody the library already
 * knows, and inventing an empty person here would only make a record with no
 * photographs on it.
 */
export function MySubjectCard() {
  const { t } = useTranslation()
  const { projects } = useAuth()
  const rows = linkableMemberships(projects)
  const anyUnlinked = rows.some((membership) => (membership.subject_uid ?? null) === null)

  return (
    <Card text="light" className="mb-4">
      <Card.Body>
        <Card.Title as="h2" className="kk-section-title mb-3">
          {t('account.subject.title')}
        </Card.Title>
        <p className="text-secondary">{t('account.subject.hint')}</p>

        {rows.length === 0 ? (
          <p className="text-secondary mb-0">{t('account.subject.noProjects')}</p>
        ) : (
          <>
            {/* Said before the fields, not after the save: linking publishes
                that person's face next to everything this account has written. */}
            {anyUnlinked && (
              <Alert variant="warning" className="py-2">
                {t('account.subject.publishWarning')}
              </Alert>
            )}
            <ul className="list-unstyled mb-0 d-flex flex-column gap-3">
              {rows.map((membership) => (
                <MySubjectRow key={membership.uid} membership={membership} />
              ))}
            </ul>
          </>
        )}
      </Card.Body>
    </Card>
  )
}

/** Props of {@link MySubjectRow}. */
interface MySubjectRowProps {
  /** The project this row links a person in, with the link it holds now. */
  membership: ProjectMembership
}

/**
 * One project's half of {@link MySubjectCard}: the project's name, then either
 * the person linked there (a link into that project's person page, and the way
 * to unlink) or the picker over that project's people.
 */
function MySubjectRow({ membership }: MySubjectRowProps) {
  const { t } = useTranslation()
  const { refresh } = useAuth()
  const { subjects, loading } = useSubjects(membership.slug)
  const [state, setState] = useState<State>({ status: 'idle' })

  const linkedUid = membership.subject_uid ?? null
  const linked = subjects.find((candidate) => candidate.uid === linkedUid)
  const headingId = `account-subject-${membership.slug}`
  const busy = state.status === 'saving'

  async function save(subjectUid: string | null) {
    setState({ status: 'saving' })
    try {
      await setMySubject(subjectUid, { project: membership.slug })
      // The whole app reads the link off the session (the menu entry, the
      // avatar), so re-read it rather than patching a local copy.
      await refresh()
      setState({ status: 'idle' })
    } catch {
      setState({ status: 'error' })
    }
  }

  return (
    <li aria-labelledby={headingId} data-testid={`account-subject-row-${membership.slug}`}>
      <h3 id={headingId} className="h6 mb-2 d-flex align-items-center gap-2">
        <Icon name="folder2-open" />
        {membership.name}
      </h3>

      {state.status === 'error' && (
        <Alert variant="danger" role="alert" className="py-2">
          {t('account.subject.error')}
        </Alert>
      )}

      {linkedUid === null ? (
        <AddAutocomplete
          id={`account-subject-${membership.slug}-pick`}
          label={t('account.subject.pickLabelIn', { project: membership.name })}
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
      ) : (
        <div className="d-flex align-items-center gap-2 flex-wrap">
          <Icon name="person-hearts" />
          {/* A link that has lost its person (deleted from the library) still
              has a UID and no name; say so rather than print a blank. While the
              list is still loading nothing is known yet, so nothing is said. */}
          {linked === undefined ? (
            !loading && <span className="text-secondary">{t('account.subject.unknownPerson')}</span>
          ) : (
            <Link to={projectPathFor(membership.slug, `/people/${linked.uid}`)}>{linked.name}</Link>
          )}
          <Button
            variant="outline-light"
            size="sm"
            className="ms-auto"
            disabled={busy}
            aria-label={t('account.subject.clearIn', { project: membership.name })}
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
        </div>
      )}
    </li>
  )
}
