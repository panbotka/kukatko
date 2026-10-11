import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import {
  THRESHOLD_MAX_PERCENT,
  THRESHOLD_MIN_PERCENT,
  THRESHOLD_STEP_PERCENT,
} from '../../lib/faceThreshold'
import { type SubjectCount } from '../../services/people'
import { type SelectOption, SearchableSelect } from '../library/SearchableSelect'
import { Icon } from '../Icon'
import {
  ToolField,
  ToolLimitField,
  ToolRangeField,
  ToolSearchForm,
  ToolSubmit,
} from '../tools/ToolSearchForm'

/** Props for {@link CandidateSearchForm}. */
export interface CandidateSearchFormProps {
  /** Every subject, with its photo (marker) count, to pick from. */
  subjects: SubjectCount[]
  /** True while the subject list is still loading. */
  subjectsLoading: boolean
  /** The chosen subject's UID, or null when none is picked yet. */
  subjectUid: string | null
  /**
   * The chosen person's name when {@link CandidateSearchFormProps.subjects} does
   * not carry it yet — a reload lands with the uid from the URL and nothing else.
   * Null while it is still unknown; the picker then shows its placeholder rather
   * than a raw identifier.
   */
  subjectName: string | null
  /** The similarity threshold as a whole percentage (20–80). */
  thresholdPercent: number
  /** The result cap; 0 means no limit. */
  limit: number
  /** True while a search is running (disables submit, shows a spinner). */
  loading: boolean
  /** Picks the subject to search for, or null when the picker is cleared. */
  onSubjectChange: (uid: string | null) => void
  /** Updates the threshold percentage from the slider. */
  onThresholdChange: (percent: number) => void
  /** Updates the result cap. */
  onLimitChange: (limit: number) => void
  /** Runs the search explicitly (the query is expensive — never on drag). */
  onSearch: () => void
}

/**
 * CandidateSearchForm is the config panel at the top of the /faces page: pick a
 * person (typeahead showing each one's face count), set how sure the match must be,
 * cap the results, and search. The threshold speaks percent throughout — the caller
 * converts it to a cosine distance — and the slider is bookended with the trade-off
 * ("more results" ↔ "better matches") so it needs no manual.
 *
 * The person picker **displays** its choice rather than merely collecting it: the
 * page's whole state lives in the URL, so a reload or a Back has to put the person
 * back into the control, not only into the results. That is also why it can be
 * cleared from inside — the "no person" row hands the caller a null, which is how
 * the search is taken back off the URL.
 */
export function CandidateSearchForm({
  subjects,
  subjectsLoading,
  subjectUid,
  subjectName,
  thresholdPercent,
  limit,
  loading,
  onSubjectChange,
  onThresholdChange,
  onLimitChange,
  onSearch,
}: CandidateSearchFormProps) {
  const { t } = useTranslation()

  // A person the subject list does not carry (yet) still gets an option, so the
  // control can show the name instead of the uid the URL gave it. Until that name
  // is known there is no option to select and the field falls back to its
  // placeholder — a uid on screen would be worse than nothing.
  const options: SelectOption[] = subjects.map((subject) => ({
    value: subject.uid,
    label: subject.name,
    // The search matches face against face, so the count that says how much
    // material it has to go on is the marker count, never the photo count.
    count: subject.marker_count,
  }))
  if (subjectUid !== null && subjectName !== null && !subjects.some((s) => s.uid === subjectUid)) {
    options.unshift({ value: subjectUid, label: subjectName })
  }
  const selected = options.find((option) => option.value === subjectUid)

  return (
    <ToolSearchForm onSubmit={onSearch}>
      <ToolField md={6} lg={5}>
        <SearchableSelect
          id="face-search-subject"
          label={t('faceSearch.form.subjectHeading')}
          value={selected === undefined ? '' : selected.value}
          options={options}
          anyLabel={t('faceSearch.form.subjectLabel')}
          disabled={subjectsLoading}
          onChange={(value) => {
            onSubjectChange(value === '' ? null : value)
          }}
        />
        <Form.Text>
          {selected === undefined
            ? t('faceSearch.form.subjectNone')
            : t('faceSearch.form.subjectSelected', { name: selected.label })}
        </Form.Text>
      </ToolField>

      <ToolRangeField
        md={6}
        lg={4}
        id="face-search-threshold"
        label={t('faceSearch.form.thresholdLabel')}
        valueText={t('faceSearch.form.thresholdValue', { percent: thresholdPercent })}
        min={THRESHOLD_MIN_PERCENT}
        max={THRESHOLD_MAX_PERCENT}
        step={THRESHOLD_STEP_PERCENT}
        value={thresholdPercent}
        onChange={onThresholdChange}
        lowHint={t('faceSearch.form.thresholdMore')}
        highHint={t('faceSearch.form.thresholdBetter')}
      />

      <ToolLimitField
        md
        id="face-search-limit"
        label={t('faceSearch.form.limitLabel')}
        hint={t('faceSearch.form.limitHint')}
        min={0}
        value={limit}
        onChange={(value) => {
          onLimitChange(Math.max(0, Math.trunc(value) || 0))
        }}
      />

      <ToolSubmit disabled={subjectUid === null || loading}>
        {loading ? (
          <Spinner animation="border" size="sm" role="status" aria-hidden="true" />
        ) : (
          <Icon name="search" />
        )}
        {t('faceSearch.form.search')}
      </ToolSubmit>
    </ToolSearchForm>
  )
}
