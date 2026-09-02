import { type ReactNode, useState } from 'react'
import Button from 'react-bootstrap/Button'
import { useTranslation } from 'react-i18next'

import { Icon } from './Icon'

/** Props for {@link TechnicalDetail}. */
export interface TechnicalDetailProps {
  /**
   * DOM id of the disclosed region, referenced by the toggle's `aria-controls`.
   * It has to be unique on the page, so a caller inside a table row keys it on
   * the row.
   */
  id: string
  /** Summary label; defaults to the shared "technical details" wording. */
  label?: string
  /** Classes for the wrapper, so a caller can place it in its own layout. */
  className?: string
  /** What is disclosed: the identifiers, paths and verbatim errors. */
  children: ReactNode
}

/**
 * The one way an admin page shows a machine-readable fact: closed by default,
 * one click away, never in the reading path.
 *
 * The admin pages are read by whoever runs the family's library, not by whoever
 * wrote it, so their sentences are plain — but a raw server error, a file path,
 * a UID or a job-type id is exactly what a maintainer chasing a real problem
 * needs, and dropping it would trade one bad page for another. So the page says
 * what happened in a sentence and parks the precise text behind this
 * disclosure: the technical truth stays two seconds away without being the
 * first thing anyone reads.
 *
 * It is the {@link DeveloperGroup} idiom of the photo page generalised — the
 * label *is* the button (a separate toggle beside a label is two targets for
 * one action), the chevron says which way it will move, and the region is only
 * mounted while open, so nothing collapsed can be found by a page search or
 * read out by a screen reader.
 */
export function TechnicalDetail({ id, label, className, children }: TechnicalDetailProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <div className={className}>
      <Button
        variant="link"
        size="sm"
        className="px-0 kk-text-caption text-secondary text-decoration-none"
        aria-expanded={open}
        aria-controls={id}
        onClick={() => {
          setOpen(!open)
        }}
      >
        <Icon name={open ? 'chevron-down' : 'chevron-right'} className="me-1" />
        {label ?? t('technicalDetails.label')}
      </Button>
      {open && (
        <div id={id} className="mt-1">
          {children}
        </div>
      )}
    </div>
  )
}
