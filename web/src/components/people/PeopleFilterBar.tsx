import { type ParseKeys } from 'i18next'
import Form from 'react-bootstrap/Form'
import InputGroup from 'react-bootstrap/InputGroup'
import { useTranslation } from 'react-i18next'

import {
  type PeopleSort,
  type PeopleTab,
  type PeopleView,
  PEOPLE_SORTS,
  PEOPLE_TABS,
  toPeopleSort,
  toPeopleTab,
} from '../../lib/peopleBrowse'
import { type SetUrlState } from '../../lib/urlState'
import { Icon } from '../Icon'

/** The i18n label key of each type option, so the strip and the copy cannot drift apart. */
const TAB_LABEL_KEY: Record<PeopleTab, ParseKeys> = {
  all: 'people.filters.anyType',
  person: 'people.tabs.person',
  pet: 'people.tabs.pet',
  other: 'people.tabs.other',
}

/** The i18n label key of each ordering. */
const SORT_LABEL_KEY: Record<PeopleSort, ParseKeys> = {
  name: 'people.sort.name',
  count: 'people.sort.count',
}

/** Props for {@link PeopleFilterBar}. */
export interface PeopleFilterBarProps {
  /** The current URL-encoded view of the people index. */
  view: PeopleView
  /** Applies a patch to that view (and so to the URL). */
  onChange: SetUrlState<PeopleView>
  /** How many subjects each type holds under the current search. */
  counts: Record<PeopleTab, number>
}

/**
 * The people index's own filter bar: a search over names, the kind of subject
 * (everyone · people · animals · other, each with its live count) and the
 * ordering — alphabetically, or the people with the most photos first.
 *
 * The search matches the way the library's "Osoba" facet does: folded, so
 * `nemcova` finds `Němcová`. Unlike the album index this offers the types as a
 * select rather than a tab strip, because one of them holds nearly everybody —
 * four buttons of which three read zero would be a strip that only takes space.
 *
 * Every control writes straight into the URL, so Back steps through the views and
 * a link carries the exact one; only live typing replaces the history entry
 * instead of pushing one per keystroke.
 */
export function PeopleFilterBar({ view, onChange, counts }: PeopleFilterBarProps) {
  const { t } = useTranslation()
  const tab = toPeopleTab(view.type)
  const sort = toPeopleSort(view.sort)

  return (
    <Form
      className="mb-3 d-flex flex-wrap align-items-center gap-2"
      role="search"
      aria-label={t('people.filters.barLabel')}
      // Nothing here is submitted — every control filters in place — so Enter in
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
          aria-label={t('people.filters.search')}
          placeholder={t('people.filters.searchPlaceholder')}
          onChange={(event) => {
            // Replace rather than push: a history entry per keystroke would turn
            // Back into a backspace key.
            onChange({ q: event.target.value }, { replace: true })
          }}
        />
      </InputGroup>

      <Form.Select
        className="w-auto"
        value={tab}
        aria-label={t('people.filters.type')}
        onChange={(event) => {
          onChange({ type: event.target.value })
        }}
      >
        {PEOPLE_TABS.map((name) => (
          <option key={name} value={name}>
            {t('people.filters.typeOption', {
              label: t(TAB_LABEL_KEY[name]),
              count: counts[name],
            })}
          </option>
        ))}
      </Form.Select>

      <Form.Select
        className="kukatko-filter-sort w-auto"
        value={sort}
        aria-label={t('people.filters.sort')}
        onChange={(event) => {
          onChange({ sort: event.target.value })
        }}
      >
        {PEOPLE_SORTS.map((name) => (
          <option key={name} value={name}>
            {t(SORT_LABEL_KEY[name])}
          </option>
        ))}
      </Form.Select>
    </Form>
  )
}
