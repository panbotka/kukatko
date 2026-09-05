import { useCallback, useEffect, useMemo, useState } from 'react'
import Alert from 'react-bootstrap/Alert'
import Button from 'react-bootstrap/Button'
import Form from 'react-bootstrap/Form'
import Modal from 'react-bootstrap/Modal'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { useAuth } from '../../auth/AuthContext'
import { foldedIncludes } from '../../lib/text'
import { markWelcomeSeen, setMySubject } from '../../services/auth'
import { fetchSubjects, type SubjectCount } from '../../services/people'
import { fetchWelcomeMarkdown } from '../../services/settings'
import { Icon } from '../Icon'
import { Markdown } from '../Markdown'
import { SubjectSummary } from '../people/SubjectSummary'

/**
 * How many people the picker offers at once. The list is faces, not names, so a
 * long one costs both scrolling and thumbnails; six is enough to hold the whole
 * household of a small archive before anybody types, and enough to show that a
 * query narrowed things down.
 */
const MAX_CANDIDATES = 6

/** Which question the welcome is on. */
type Step = 'text' | 'person'

/**
 * The welcome's lifecycle. `idle` is before the session is known, `loading` is
 * the one request it makes before it dares to appear, and `done` is final —
 * nothing reopens it for the rest of the page's life.
 */
type Phase = 'idle' | 'loading' | 'open' | 'done'

/** Props for {@link WelcomeTextStep}. */
interface WelcomeTextStepProps {
  /** The administrator's greeting, already known to be non-empty. */
  markdown: string
  /**
   * Moves on to the question about who the reader is, or `null` when the library
   * has named nobody to ask about — then the greeting is the whole welcome and
   * the step ends it rather than promising a second page.
   */
  onNext: (() => void) | null
  /** Ends the welcome without asking anything else. */
  onFinish: () => void
}

/**
 * Step one: whatever the administrator wrote, rendered as Markdown.
 *
 * It goes through the app's one {@link Markdown} renderer, so the text an
 * administrator previews in the settings screen and the text a newcomer is met
 * with are produced by literally the same component — sanitised, with links
 * opening in a new tab under `noopener noreferrer`.
 */
function WelcomeTextStep({ markdown, onNext, onFinish }: WelcomeTextStepProps) {
  const { t } = useTranslation()

  return (
    <>
      <Modal.Body>
        <Markdown className="text-break">{markdown}</Markdown>
      </Modal.Body>
      <Modal.Footer>
        {onNext === null ? (
          <Button variant="primary" onClick={onFinish}>
            {t('welcome.done')}
          </Button>
        ) : (
          <>
            <Button variant="link" onClick={onFinish}>
              {t('welcome.skip')}
            </Button>
            <Button variant="primary" onClick={onNext}>
              {t('welcome.next')}
            </Button>
          </>
        )}
      </Modal.Footer>
    </>
  )
}

/** Props for {@link WelcomePersonStep}. */
interface WelcomePersonStepProps {
  /**
   * The library's named people, fetched by the welcome before it opened. Passed
   * in rather than fetched here because the same list decides whether this step
   * happens at all: an empty one means the step is never rendered.
   */
  subjects: SubjectCount[]
  /** The person this account already says it is, or null when nobody has said. */
  linkedUid: string | null
  /** Ends the welcome — after a successful link, or because it was skipped. */
  onFinish: () => void
}

/**
 * Step two: "who are you in the photographs?"
 *
 * A search field over the library's people, each candidate shown with the face
 * the app already picked for them ({@link SubjectSummary}) — a family archive is
 * full of namesakes and of people whose spelling nobody agrees on, and a face
 * settles both faster than a name does. Nothing is written until the reader
 * confirms: choosing a row only marks it.
 *
 * An account that is already linked is not asked a question it has answered. It
 * is told who it is linked to and offered to change it, which is the same
 * control one step later.
 */
function WelcomePersonStep({ subjects, linkedUid, onFinish }: WelcomePersonStepProps) {
  const { t } = useTranslation()
  const { refresh } = useAuth()
  const [query, setQuery] = useState('')
  const [chosen, setChosen] = useState<SubjectCount | null>(null)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  // An already-linked account only sees the picker once it asks to change the
  // link — otherwise the answer it gave would be buried under the question.
  const [changing, setChanging] = useState(false)

  const linked = subjects.find((candidate) => candidate.uid === linkedUid)
  const asking = linkedUid === null || changing

  /**
   * The people to offer: everyone matching the query, most-photographed first.
   *
   * The ordering is the reason an empty query is still useful. Whoever carries
   * the most of the library is by a wide margin the likeliest answer to "who are
   * you", so the field opens on a shortlist rather than on the alphabet.
   */
  const candidates = useMemo(
    () =>
      subjects
        .filter((candidate) => foldedIncludes(candidate.name, query))
        .sort((a, b) => b.photo_count - a.photo_count)
        .slice(0, MAX_CANDIDATES),
    [subjects, query],
  )

  async function confirm() {
    if (chosen === null) {
      return
    }
    setBusy(true)
    setFailed(false)
    try {
      await setMySubject(chosen.uid)
      // The menu entry, `person:me` and the avatar beside a comment all read the
      // link off the session, so re-read it rather than patch a local copy.
      await refresh()
      onFinish()
    } catch {
      setFailed(true)
      setBusy(false)
    }
  }

  return (
    <>
      <Modal.Body>
        <p className="text-secondary">{t('welcome.person.hint')}</p>

        {failed && (
          <Alert variant="danger" role="alert" className="py-2">
            {t('welcome.person.error')}
          </Alert>
        )}

        {!asking && (
          <p className="mb-3 d-flex align-items-center gap-2 flex-wrap">
            <Icon name="person-hearts" />
            {/* A link can outlive its person (deleted from the library): it still
                has a UID and no name, and saying so beats printing a blank. */}
            {linked === undefined ? (
              <span className="text-secondary">{t('welcome.person.unknownPerson')}</span>
            ) : (
              <span className="fw-semibold">
                {t('welcome.person.linkedTo', { name: linked.name })}
              </span>
            )}
          </p>
        )}

        {asking && (
          <>
            {/* Said before the field rather than discovered after the save: the
                link publishes that person's face beside everything this account
                has written. */}
            <Alert variant="warning" className="py-2">
              {t('welcome.person.publishWarning')}
            </Alert>
            {/* The field sits at the top of the body: on a phone the keyboard
                covers the bottom half of the screen, and a search box under it
                cannot be read while it is being typed into. */}
            <Form.Group controlId="welcome-person-search" className="mb-3">
              <Form.Label>{t('welcome.person.searchLabel')}</Form.Label>
              <Form.Control
                type="search"
                autoComplete="off"
                placeholder={t('welcome.person.searchPlaceholder')}
                value={query}
                disabled={busy}
                onChange={(event) => {
                  setQuery(event.target.value)
                }}
              />
            </Form.Group>
            {candidates.length === 0 && (
              <p className="text-secondary">{t('welcome.person.noMatches')}</p>
            )}
            <div className="d-flex flex-column gap-2">
              {candidates.map((candidate) => (
                <Button
                  key={candidate.uid}
                  variant={chosen?.uid === candidate.uid ? 'outline-primary' : 'outline-light'}
                  className="text-start"
                  aria-pressed={chosen?.uid === candidate.uid}
                  disabled={busy}
                  onClick={() => {
                    setChosen(candidate)
                  }}
                >
                  <SubjectSummary subject={candidate} />
                </Button>
              ))}
            </div>
          </>
        )}
      </Modal.Body>
      <Modal.Footer>
        <Button variant="link" onClick={onFinish} disabled={busy}>
          {t('welcome.skip')}
        </Button>
        {asking ? (
          <Button
            variant="primary"
            disabled={busy || chosen === null}
            onClick={() => {
              void confirm()
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
            {t('welcome.person.confirm')}
          </Button>
        ) : (
          <>
            <Button
              variant="outline-light"
              onClick={() => {
                setChanging(true)
              }}
            >
              {t('welcome.person.change')}
            </Button>
            <Button variant="primary" onClick={onFinish}>
              {t('welcome.person.done')}
            </Button>
          </>
        )}
      </Modal.Footer>
    </>
  )
}

/**
 * The first-run welcome: shown once, over whatever the reader landed on, to an
 * account that has never seen it.
 *
 * It does two jobs in two steps. First it prints the greeting an administrator
 * wrote for this instance — the only place a family archive gets to explain
 * itself in its own words. Then it asks the one question the app cannot answer
 * on its own and that is otherwise buried three clicks deep in the account
 * screen: which person of the photographs the reader is. An instance with no
 * greeting opens straight on the question; an account that already answered it
 * is shown its answer instead of being asked again.
 *
 * The question is only asked where it has an answer. A library in which nobody
 * is named yet — a fresh instance, or one whose faces are all still unassigned —
 * gets the greeting and nothing else, because a picker over an empty set is a
 * dead end for the very first administrator to sign in. Whoever is skipped that
 * way still links their account whenever they like, in the account screen
 * (`MySubjectCard`). The list is read every time the welcome is about to open,
 * never remembered, so the instance that names its first person asks the next
 * newcomer properly.
 *
 * However it ends — confirmed, skipped, or closed with the X — the visit is
 * recorded and it does not come back. Recording it is deliberately
 * fire-and-forget: the modal closes first and a failed stamp is swallowed, so a
 * backend hiccup can at worst show the welcome again tomorrow, never trap
 * somebody behind a dialog that will not close.
 *
 * The modal is mounted by the app shell ({@link Layout}), so the immersive
 * routes outside it — the photo viewer, the slideshow, the review game — are
 * left alone. It renders nothing at all until it has decided it is wanted: an
 * account that has seen the welcome costs no request, and a greeting that could
 * not be fetched (a 5xx, an unreachable backend) means the welcome quietly does
 * not happen this time. That failure records nothing, so the next sign-in tries
 * again — the greeting is postponed, not lost.
 */
export function WelcomeModal() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [phase, setPhase] = useState<Phase>('idle')
  const [markdown, setMarkdown] = useState('')
  const [subjects, setSubjects] = useState<SubjectCount[]>([])
  const [step, setStep] = useState<Step>('text')

  // Decide once, off the session: an account carrying a `welcome_seen_at` stamp
  // is finished before it starts. The phase latches, so a later refresh of the
  // session (the link being saved re-reads /auth/me) cannot reopen it.
  useEffect(() => {
    if (phase !== 'idle' || user === null) {
      return
    }
    setPhase((user.welcome_seen_at ?? null) === null ? 'loading' : 'done')
  }, [phase, user])

  useEffect(() => {
    if (phase !== 'loading') {
      return
    }
    const controller = new AbortController()
    // A subject listing that fails is read as "nobody is named". The picker is
    // the part of the welcome that may be dropped, and dropping it costs the
    // reader nothing they cannot do later in the account screen — whereas an
    // empty one is precisely the dead end being avoided here.
    const people = fetchSubjects(controller.signal).catch((error: unknown) => {
      if (error instanceof DOMException && error.name === 'AbortError') {
        throw error
      }
      return [] as SubjectCount[]
    })
    Promise.all([fetchWelcomeMarkdown(controller.signal), people])
      .then(([text, list]) => {
        // Nothing written and nobody to ask about: a dialog whose only working
        // control is its close button. It does not open, and — since the reader
        // was shown nothing — the visit is not recorded either, so the greeting
        // an administrator writes tomorrow, or the first person somebody names,
        // still reaches them.
        if (text.trim() === '' && list.length === 0) {
          setPhase('done')
          return
        }
        setMarkdown(text)
        setSubjects(list)
        // An instance whose administrator wrote nothing has no step one to show;
        // an empty first page would only teach the reader that the dialog is
        // worth clicking through without reading.
        setStep(text.trim() === '' ? 'person' : 'text')
        setPhase('open')
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === 'AbortError') {
          return
        }
        setPhase('done')
      })
    return () => {
      controller.abort()
    }
  }, [phase])

  /**
   * Closes the welcome for good and records the visit in the background.
   *
   * The close is not conditional on the record: the reader asked to be let
   * through, and a request they cannot see failing is not a reason to keep them
   * in a dialog. The stamp is idempotent server-side, so nothing is corrupted by
   * a repeat next time.
   */
  const finish = useCallback(() => {
    setPhase('done')
    void markWelcomeSeen().catch(() => undefined)
  }, [])

  if (phase !== 'open') {
    return null
  }

  return (
    <Modal
      show
      onHide={finish}
      centered
      scrollable
      // Full-bleed on a phone: the welcome is a page, not an aside, and a
      // margin-boxed dialog would spend the little width it has on its own edges.
      fullscreen="sm-down"
      aria-labelledby="welcome-modal-title"
    >
      <Modal.Header closeButton closeLabel={t('welcome.close')}>
        <Modal.Title id="welcome-modal-title">{t('welcome.title')}</Modal.Title>
      </Modal.Header>
      {step === 'text' ? (
        <WelcomeTextStep
          markdown={markdown}
          onNext={
            subjects.length === 0
              ? null
              : () => {
                  setStep('person')
                }
          }
          onFinish={finish}
        />
      ) : (
        <WelcomePersonStep
          subjects={subjects}
          linkedUid={user?.subject_uid ?? null}
          onFinish={finish}
        />
      )}
    </Modal>
  )
}
