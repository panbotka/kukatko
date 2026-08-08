import { type ParseKeys } from 'i18next'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import { useTranslation } from 'react-i18next'

import { type LabelsView, type LabelSort, LABEL_SORTS, toLabelSort } from '../../lib/labelBrowse'
import { type SetUrlState } from '../../lib/urlState'
import { Icon } from '../Icon'

/** The i18n label key of each ordering. */
const SORT_LABEL_KEY: Record<LabelSort, ParseKeys> = {
  count: 'labels.sort.count',
  name: 'labels.sort.name',
}

/** Props for {@link LabelFilterBar}. */
export interface LabelFilterBarProps {
  /** The current URL-encoded view of the labels index. */
  view: LabelsView
  /** Applies a patch to that view (and so to the URL). */
  onChange: SetUrlState<LabelsView>
}

/**
 * The labels index's own filter bar: a search over label names and the ordering
 * — the most-used labels first, or alphabetically.
 *
 * The search matches the way the library's "Štítek" facet does: folded, so
 * `dovolena` finds `Dovolená`. It offers no filter beyond the search because a
 * label has nothing to be filtered by — it is a name and a count, and the two
 * questions a hundred of them raise are "where is the one I mean?" and "which
 * ones carry the library?".
 *
 * Every control writes straight into the URL, so Back steps through the views and
 * a link carries the exact one; only live typing replaces the history entry
 * instead of pushing one per keystroke.
 */
export function LabelFilterBar({ view, onChange }: LabelFilterBarProps) {
  const { t } = useTranslation()
  const sort = toLabelSort(view.sort)

  return (
    <Form
      className="mb-3 d-flex flex-wrap align-items-center gap-2"
      role="search"
      aria-label={t('labels.filters.barLabel')}
      // Nothing here is submitted — both controls filter in place — so Enter in
      // the search box must not reload the page out from under the view.
      onSubmit={(event) => {
        event.preventDefault()
      }}
    >
      <InputGroup className="kukatko-filter-search">
        <InputGroup.Text aria-hidden="true">
          <Icon name="search" />
        </InputGroup.Text>
        <Form.Control
          type="search"
          value={view.q}
          aria-label={t('labels.filters.search')}
          placeholder={t('labels.filters.searchPlaceholder')}
          onChange={(event) => {
            // Replace rather than push: a history entry per keystroke would turn
            // Back into a backspace key.
            onChange({ q: event.target.value }, { replace: true })
          }}
        />
      </InputGroup>

      <Form.Select
        className="kukatko-filter-sort w-auto"
        value={sort}
        aria-label={t('labels.filters.sort')}
        onChange={(event) => {
          onChange({ sort: event.target.value })
        }}
      >
        {LABEL_SORTS.map((name) => (
          <option key={name} value={name}>
            {t(SORT_LABEL_KEY[name])}
          </option>
        ))}
      </Form.Select>
    </Form>
  )
}
