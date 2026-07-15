import Button from 'react-bootstrap/Button'
import Col from 'react-bootstrap/Col'
import Form from 'react-bootstrap/Form'
import Row from 'react-bootstrap/Row'
import Spinner from 'react-bootstrap/Spinner'
import ToggleButton from 'react-bootstrap/ToggleButton'
import ToggleButtonGroup from 'react-bootstrap/ToggleButtonGroup'
import { useTranslation } from 'react-i18next'

import {
  clampExpandLimit,
  EXPAND_LIMIT_MAX,
  EXPAND_LIMIT_MIN,
  type ExpandSource,
} from '../../lib/expandSearch'
import {
  THRESHOLD_MAX_PERCENT,
  THRESHOLD_MIN_PERCENT,
  THRESHOLD_STEP_PERCENT,
} from '../../lib/faceThreshold'
import { type ExpandKind } from '../../services/expand'
import { AddAutocomplete } from '../photo/AddAutocomplete'
import { Icon } from '../Icon'

/** Props for {@link ExpandSearchForm}. */
export interface ExpandSearchFormProps {
  /** Which kind of collection is being expanded. */
  kind: ExpandKind
  /**
   * The pickable collections of the current kind, already sorted by photo count
   * descending with empty ones dropped (see `expandSources`).
   */
  sources: ExpandSource[]
  /** True while the collection lists are still loading. */
  sourcesLoading: boolean
  /** True when the collection lists failed to load. */
  sourcesError: boolean
  /** The chosen collection's UID, or null when none is picked yet. */
  sourceUid: string | null
  /** The similarity threshold as a whole percentage (20–80). */
  thresholdPercent: number
  /** The result cap (1–200). */
  limit: number
  /** True while a search is running (disables submit, shows a spinner). */
  loading: boolean
  /** Switches between expanding an album and expanding a label. */
  onKindChange: (kind: ExpandKind) => void
  /** Picks the collection to expand. */
  onSourceChange: (uid: string) => void
  /** Updates the threshold percentage from the slider. */
  onThresholdChange: (percent: number) => void
  /** Updates the result cap. */
  onLimitChange: (limit: number) => void
  /** Runs the search explicitly (the query is expensive — never on drag). */
  onSearch: () => void
}

/**
 * ExpandSearchForm is the config panel at the top of the /expand page: choose
 * album or label, pick the collection to grow (a typeahead ordered by photo
 * count, since the collections worth expanding are the ones that already have
 * material), set how similar a photo must be, cap the results, and search. The
 * threshold speaks percent throughout — the caller converts it to a cosine
 * distance — and the slider is bookended with the trade-off ("more results" ↔
 * "better matches") so it needs no manual.
 */
export function ExpandSearchForm({
  kind,
  sources,
  sourcesLoading,
  sourcesError,
  sourceUid,
  thresholdPercent,
  limit,
  loading,
  onKindChange,
  onSourceChange,
  onThresholdChange,
  onLimitChange,
  onSearch,
}: ExpandSearchFormProps) {
  const { t } = useTranslation()
  const selected = sources.find((source) => source.uid === sourceUid)

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
          <span className="form-label d-block mb-1" id="expand-kind-heading">
            {t('expand.form.sourceHeading')}
          </span>
          <div className="d-flex gap-2 align-items-start flex-wrap">
            <ToggleButtonGroup
              type="radio"
              name="expand-kind"
              value={kind}
              aria-labelledby="expand-kind-heading"
              onChange={(value: ExpandKind) => {
                onKindChange(value)
              }}
            >
              <ToggleButton
                id="expand-kind-album"
                value="album"
                variant="outline-secondary"
                size="sm"
                className="kukatko-tap-target"
              >
                {t('expand.form.kindAlbum')}
              </ToggleButton>
              <ToggleButton
                id="expand-kind-label"
                value="label"
                variant="outline-secondary"
                size="sm"
                className="kukatko-tap-target"
              >
                {t('expand.form.kindLabel')}
              </ToggleButton>
            </ToggleButtonGroup>
            <div className="flex-grow-1">
              <AddAutocomplete
                id="expand-source"
                label={
                  kind === 'album'
                    ? t('expand.form.sourceLabelAlbum')
                    : t('expand.form.sourceLabelLabel')
                }
                disabled={sourcesLoading || sourcesError}
                options={sources.map((source) => ({
                  uid: source.uid,
                  label: source.name,
                  hint: t('expand.form.sourceHint', { count: source.photoCount }),
                }))}
                onAdd={onSourceChange}
              />
            </div>
          </div>
          <Form.Text className={sourcesError ? 'text-danger' : undefined}>
            {sourcesError
              ? t('expand.form.sourcesError')
              : selected === undefined
                ? t('expand.form.sourceNone')
                : t('expand.form.sourceSelected', { name: selected.name })}
          </Form.Text>
        </Col>

        <Col md={6} lg={4}>
          <Form.Label htmlFor="expand-threshold" className="d-flex justify-content-between mb-1">
            <span>{t('expand.form.thresholdLabel')}</span>
            <span className="fw-semibold">
              {t('expand.form.thresholdValue', { percent: thresholdPercent })}
            </span>
          </Form.Label>
          <Form.Range
            id="expand-threshold"
            min={THRESHOLD_MIN_PERCENT}
            max={THRESHOLD_MAX_PERCENT}
            step={THRESHOLD_STEP_PERCENT}
            value={thresholdPercent}
            onChange={(event) => {
              onThresholdChange(Number(event.target.value))
            }}
          />
          <div className="d-flex justify-content-between small text-secondary">
            <span>{t('expand.form.thresholdMore')}</span>
            <span>{t('expand.form.thresholdBetter')}</span>
          </div>
        </Col>

        <Col xs={6} md={4} lg={2}>
          <Form.Label htmlFor="expand-limit">{t('expand.form.limitLabel')}</Form.Label>
          <Form.Control
            id="expand-limit"
            type="number"
            min={EXPAND_LIMIT_MIN}
            max={EXPAND_LIMIT_MAX}
            value={limit}
            onChange={(event) => {
              onLimitChange(clampExpandLimit(Number(event.target.value)))
            }}
          />
          <Form.Text>{t('expand.form.limitHint')}</Form.Text>
        </Col>

        <Col xs={6} md={2} lg={1} className="d-flex align-items-start">
          <Button
            type="submit"
            variant="primary"
            disabled={sourceUid === null || loading}
            className="mt-md-4 d-flex align-items-center gap-2"
          >
            {loading ? (
              <Spinner animation="border" size="sm" role="status" aria-hidden="true" />
            ) : (
              <Icon name="search" />
            )}
            {t('expand.form.search')}
          </Button>
        </Col>
      </Row>
    </Form>
  )
}
