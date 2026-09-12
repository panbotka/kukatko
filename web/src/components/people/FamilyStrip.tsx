import { useCallback, useEffect, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { formatLifeSpan } from '../../lib/lifeYears'
import { fetchRelations, type Relations, type Relative } from '../../services/family'
import { ENTITY_STYLE } from '../entityStyle'
import { Icon } from '../Icon'

import { AddRelationModal, type RelationKind } from './AddRelationModal'
import { SubjectAvatar } from './SubjectAvatar'

/** Props for {@link FamilyStrip}. */
export interface FamilyStripProps {
  /** The person whose family this is. */
  subjectUid: string
  /** Their name, for the add dialog's copy. */
  subjectName: string
  /** Whether the current user may record relations (editor/admin). */
  canWrite: boolean
}

/** The empty relations, used while the fetch is in flight or after it failed. */
const NO_RELATIONS: Relations = { parents: [], siblings: [], partners: [], children: [] }

/**
 * The pill shared by every chip here, and by the person chips of the photo
 * panel: a rounded badge pulled in around its round face so the picture reads as
 * a portrait rather than as a coloured dot beside the text.
 */
const CHIP_CLASS = 'badge rounded-pill d-inline-flex align-items-center gap-2 ps-1 pe-3'

/**
 * One relative as a round face chip linking to their page.
 *
 * A person with no photograph is drawn as their initials, not as a hole: in a
 * family archive the great-grandmother nobody photographed is an ordinary node,
 * and a chip that looked broken would make the whole strip look broken. The life
 * span rides inside the pill where anybody recorded it — with eight Nečases and
 * four Novotnýs in the library, the years are what tell two people of one name
 * apart.
 */
function RelativeChip({ relative }: { relative: Relative }) {
  const lifeSpan = formatLifeSpan(relative.birth_year, relative.death_year)

  return (
    <Link
      to={`/people/${relative.uid}`}
      className={`${CHIP_CLASS} ${ENTITY_STYLE.person.className} text-white text-decoration-none`}
    >
      <SubjectAvatar uid={relative.uid} name={relative.name} photoCount={relative.photo_count} />
      {relative.name}
      {lifeSpan !== null && <span className="opacity-75">{lifeSpan}</span>}
    </Link>
  )
}

/** Props for one row of the strip. */
interface FamilyRowProps {
  /** The row's own name — Rodiče, Sourozenci, Partner, Děti. */
  label: string
  /** The people in it, already resolved to relatives. */
  people: readonly Relative[]
  /** The accessible name of the row's `+`, e.g. "Přidat rodiče". */
  addLabel: string
  /** Whether the `+` is offered at all. */
  canWrite: boolean
  /** Opens the add dialog on this row's relation. */
  onAdd: () => void
}

/**
 * One row of the strip: its name, its chips, and — for an editor — the `+` that
 * fills it.
 *
 * A row nobody has filled in is left out rather than drawn empty, because four
 * empty rows say nothing four times over. Under `canWrite` it stays: there the
 * `+` is the invitation, and a row that vanished would take the invitation with
 * it.
 */
function FamilyRow({ label, people, addLabel, canWrite, onAdd }: FamilyRowProps) {
  if (people.length === 0 && !canWrite) {
    return null
  }
  return (
    <div className="d-flex align-items-center flex-wrap gap-2 mb-2">
      <span className="kk-family-row__label kk-text-caption text-secondary">{label}</span>
      {people.map((relative) => (
        <RelativeChip key={relative.uid} relative={relative} />
      ))}
      {canWrite && (
        <Button
          variant="outline-secondary"
          size="sm"
          className="rounded-pill d-inline-flex align-items-center gap-1"
          aria-label={addLabel}
          title={addLabel}
          onClick={onAdd}
        >
          <Icon name="person-plus" />
        </Button>
      )}
    </div>
  )
}

/**
 * The family of the person whose page this is, as four rows of round face chips:
 * parents, siblings, partners, children. It sits between the header and the
 * gallery because in nine cases out of ten this — not the full tree — is what a
 * reader wants to know about somebody: who they belong to.
 *
 * The presentation is the photo panel's person chip, deliberately: the app
 * already answers "who is this" with a round face crop and a name, and a second
 * way of drawing the same thing would only be a second thing to keep consistent.
 * A relative with no photograph (see {@link SubjectAvatar}) is an ordinary chip
 * with their initials in it.
 *
 * Every list is *derived* from the family rows rather than stored, which is why
 * they cannot contradict each other — and why the siblings row has no relation of
 * its own to add: a sibling is recorded as a child of the same parents, which
 * {@link AddRelationModal} does on the reader's behalf.
 *
 * The whole strip is one request (`GET /subjects/{uid}/relations`), which carries
 * each person's cover and photo count, so a family of a dozen costs one call and
 * not one per chip. A strip that cannot be loaded draws nothing at all: it is
 * secondary to the person's page, and an error banner over a gallery that loaded
 * perfectly well would be louder than what it reports.
 */
export function FamilyStrip({ subjectUid, subjectName, canWrite }: FamilyStripProps) {
  const { t } = useTranslation()
  const [relations, setRelations] = useState<Relations>(NO_RELATIONS)
  const [adding, setAdding] = useState<RelationKind | null>(null)

  const load = useCallback(
    (signal?: AbortSignal) => {
      fetchRelations(subjectUid, signal)
        .then((loaded) => {
          setRelations(loaded)
        })
        .catch((err: unknown) => {
          if (err instanceof DOMException && err.name === 'AbortError') {
            return
          }
          setRelations(NO_RELATIONS)
        })
    },
    [subjectUid],
  )

  useEffect(() => {
    const controller = new AbortController()
    // Walking from one person to the next reuses this page, so the previous
    // person's family must not linger under the new name while the fetch runs.
    setRelations(NO_RELATIONS)
    load(controller.signal)
    return () => {
      controller.abort()
    }
  }, [load])

  // A lone-parent family has nobody on the other side; it is where that person's
  // children hang, and there is no partner chip to draw for it.
  const partners = relations.partners
    .map((partnership) => partnership.partner)
    .filter((partner): partner is Relative => partner !== null)

  const empty =
    relations.parents.length === 0 &&
    relations.siblings.length === 0 &&
    partners.length === 0 &&
    relations.children.length === 0

  // Nothing recorded and nothing to record with: no heading over four rows that
  // are all missing. An editor keeps the section, because the + is the point.
  if (empty && !canWrite) {
    return null
  }

  return (
    <section className="mb-4">
      <h2 className="kk-section-title">{t('family.title')}</h2>
      <FamilyRow
        label={t('family.rows.parents')}
        people={relations.parents}
        addLabel={t('family.add.parent')}
        canWrite={canWrite}
        onAdd={() => {
          setAdding('parent')
        }}
      />
      <FamilyRow
        label={t('family.rows.siblings')}
        people={relations.siblings}
        addLabel={t('family.add.sibling')}
        canWrite={canWrite}
        onAdd={() => {
          setAdding('sibling')
        }}
      />
      <FamilyRow
        label={t('family.rows.partners')}
        people={partners}
        addLabel={t('family.add.partner')}
        canWrite={canWrite}
        onAdd={() => {
          setAdding('partner')
        }}
      />
      <FamilyRow
        label={t('family.rows.children')}
        people={relations.children}
        addLabel={t('family.add.child')}
        canWrite={canWrite}
        onAdd={() => {
          setAdding('child')
        }}
      />

      {/* Mounted only while open: the dialog loads every subject in the library
          to pick from, and a page that never opens it must not pay for that. */}
      {canWrite && adding !== null && (
        <AddRelationModal
          subjectUid={subjectUid}
          subjectName={subjectName}
          parents={relations.parents}
          kind={adding}
          show
          onHide={() => {
            setAdding(null)
          }}
          onAdded={() => {
            load()
          }}
        />
      )}
    </section>
  )
}
