import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import {
  THRESHOLD_MAX_PERCENT,
  THRESHOLD_MIN_PERCENT,
  THRESHOLD_STEP_PERCENT,
} from '../../lib/faceThreshold'
import { type SubjectCount } from '../../services/people'
import { AddAutocomplete } from '../photo/AddAutocomplete'
import { Icon } from '../Icon'

/** Props for {@link CandidateSearchForm}. */
export interface CandidateSearchFormProps {
  /** Every subject, with its photo (marker) count, to pick from. */
  subjects: SubjectCount[]
  /** True while the subject list is still loading. */
  subjectsLoading: boolean
  /** The chosen subject's UID, or null when none is picked yet. */
  subjectUid: string | null
  /** The similarity threshold as a whole percentage (20–80). */
  thresholdPercent: number
  /** The result cap; 0 means no limit. */
  limit: number
  /** True while a search is running (disables submit, shows a spinner). */
  loading: boolean
  /** Picks the subject to search for. */
  onSubjectChange: (uid: string) => void
  /** Updates the threshold percentage from the slider. */
  onThresholdChange: (percent: number) => void
  /** Updates the result cap. */
  onLimitChange: (limit: number) => void
  /** Runs the search explicitly (the query is expensive — never on drag). */
  onSearch: () => void
}

/**
 * CandidateSearchForm is the config panel at the top of the /faces page: pick a
 * person (typeahead showing each one's photo count), set how sure the match must be,
 * cap the results, and search. The threshold speaks percent throughout — the caller
 * converts it to a cosine distance — and the slider is bookended with the trade-off
 * ("more results" ↔ "better matches") so it needs no manual.
 */
export function CandidateSearchForm({
  subjects,
  subjectsLoading,
  subjectUid,
  thresholdPercent,
  limit,
  loading,
  onSubjectChange,
  onThresholdChange,
  onLimitChange,
  onSearch,
}: CandidateSearchFormProps) {
  const { t } = useTranslation()
  const selected = subjects.find((subject) => subject.uid === subjectUid)

  return (
    <Form
      className="mb-4"
      onSubmit={(event) => {
        event.preventDefault()
        onSearch()
      }}
    >
      <Row className="g-3">
        <Col md={6} lg={5}>
          <span className="form-label d-block mb-1">{t('faceSearch.form.subjectHeading')}</span>
          <AddAutocomplete
            id="face-search-subject"
            label={t('faceSearch.form.subjectLabel')}
            disabled={subjectsLoading}
            options={subjects.map((subject) => ({
              uid: subject.uid,
              label: subject.name,
              hint: t('faceSearch.form.subjectHint', { count: subject.marker_count }),
            }))}
            onAdd={onSubjectChange}
          />
          <Form.Text>
            {selected === undefined
              ? t('faceSearch.form.subjectNone')
              : t('faceSearch.form.subjectSelected', { name: selected.name })}
          </Form.Text>
        </Col>

        <Col md={6} lg={4}>
          <Form.Label
            htmlFor="face-search-threshold"
            className="d-flex justify-content-between mb-1"
          >
            <span>{t('faceSearch.form.thresholdLabel')}</span>
            <span className="fw-semibold">
              {t('faceSearch.form.thresholdValue', { percent: thresholdPercent })}
            </span>
          </Form.Label>
          <Form.Range
            id="face-search-threshold"
            min={THRESHOLD_MIN_PERCENT}
            max={THRESHOLD_MAX_PERCENT}
            step={THRESHOLD_STEP_PERCENT}
            value={thresholdPercent}
            onChange={(event) => {
              onThresholdChange(Number(event.target.value))
            }}
          />
          <div className="d-flex justify-content-between small text-secondary">
            <span>{t('faceSearch.form.thresholdMore')}</span>
            <span>{t('faceSearch.form.thresholdBetter')}</span>
          </div>
        </Col>

        <Col xs={6} md={4} lg={2}>
          <Form.Label htmlFor="face-search-limit">{t('faceSearch.form.limitLabel')}</Form.Label>
          <Form.Control
            id="face-search-limit"
            type="number"
            min={0}
            value={limit}
            onChange={(event) => {
              onLimitChange(Math.max(0, Math.trunc(Number(event.target.value)) || 0))
            }}
          />
          <Form.Text>{t('faceSearch.form.limitHint')}</Form.Text>
        </Col>

        <Col xs={6} md={2} lg={1} className="d-flex align-items-start">
          <Button
            type="submit"
            variant="primary"
            disabled={subjectUid === null || loading}
            className="mt-md-4 d-flex align-items-center gap-2"
          >
            {loading ? (
              <Spinner animation="border" size="sm" role="status" aria-hidden="true" />
            ) : (
              <Icon name="search" />
            )}
            {t('faceSearch.form.search')}
          </Button>
        </Col>
      </Row>
    </Form>
  )
}
