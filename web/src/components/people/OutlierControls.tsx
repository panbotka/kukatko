import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import {
  OUTLIER_THRESHOLD_MAX_PERCENT,
  OUTLIER_THRESHOLD_MIN_PERCENT,
  OUTLIER_THRESHOLD_STEP_PERCENT,
} from '../../lib/outlierReview'
import { type SubjectCount } from '../../services/people'
import { AddAutocomplete } from '../photo/AddAutocomplete'

/** Props for {@link OutlierControls}. */
export interface OutlierControlsProps {
  /** Every subject, with its face count, to pick from. */
  subjects: SubjectCount[]
  /** True while the subject list is still loading. */
  subjectsLoading: boolean
  /** The chosen subject's UID, or null when none is picked yet. */
  subjectUid: string | null
  /** The threshold as a whole percentage (0 = show everything). */
  thresholdPercent: number
  /** Picks the person whose faces are reviewed. */
  onSubjectChange: (uid: string) => void
  /** Updates the threshold percentage from the slider. */
  onThresholdChange: (percent: number) => void
}

/**
 * OutlierControls is the config strip of the /outliers page: pick a person
 * (typeahead showing each one's face count) and dial how far from the centroid a
 * face must sit to be worth showing.
 *
 * Unlike the /faces search there is no submit button — the query is a cheap
 * indexed read of faces the person already has, so picking someone simply shows
 * them. The slider speaks percent throughout (the caller converts to a cosine
 * distance) and is bookended with what the two ends mean, so it needs no manual.
 */
export function OutlierControls({
  subjects,
  subjectsLoading,
  subjectUid,
  thresholdPercent,
  onSubjectChange,
  onThresholdChange,
}: OutlierControlsProps) {
  const { t } = useTranslation()
  const selected = subjects.find((subject) => subject.uid === subjectUid)

  return (
    <Row className="g-3 mb-4">
      <Col md={6} lg={5}>
        <span className="form-label d-block mb-1">{t('outliersPage.form.subjectHeading')}</span>
        <AddAutocomplete
          id="outliers-subject"
          label={t('outliersPage.form.subjectLabel')}
          disabled={subjectsLoading}
          options={subjects.map((subject) => ({
            uid: subject.uid,
            label: subject.name,
            hint: t('outliersPage.form.subjectHint', { count: subject.marker_count }),
          }))}
          onAdd={onSubjectChange}
        />
        <Form.Text>
          {selected === undefined
            ? t('outliersPage.form.subjectNone')
            : t('outliersPage.form.subjectSelected', { name: selected.name })}
        </Form.Text>
      </Col>

      <Col md={6} lg={5}>
        <Form.Label htmlFor="outliers-threshold" className="d-flex justify-content-between mb-1">
          <span>{t('outliersPage.form.thresholdLabel')}</span>
          <span className="fw-semibold">
            {t('outliersPage.form.thresholdValue', { percent: thresholdPercent })}
          </span>
        </Form.Label>
        <Form.Range
          id="outliers-threshold"
          min={OUTLIER_THRESHOLD_MIN_PERCENT}
          max={OUTLIER_THRESHOLD_MAX_PERCENT}
          step={OUTLIER_THRESHOLD_STEP_PERCENT}
          value={thresholdPercent}
          disabled={subjectUid === null}
          onChange={(event) => {
            onThresholdChange(Number(event.target.value))
          }}
        />
        <div className="d-flex justify-content-between small text-secondary">
          <span>{t('outliersPage.form.thresholdAll')}</span>
          <span>{t('outliersPage.form.thresholdExtreme')}</span>
        </div>
      </Col>
    </Row>
  )
}
