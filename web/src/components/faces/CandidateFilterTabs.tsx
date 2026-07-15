import Badge from 'react-bootstrap/Badge'
import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import { useTranslation } from 'react-i18next'

import { type FilterTab, FILTER_TABS, TAB_LABEL_KEY } from '../../lib/candidateReview'

/** Props for {@link CandidateFilterTabs}. */
export interface CandidateFilterTabsProps {
  /** The currently selected tab. */
  active: FilterTab
  /** Live per-tab counts. */
  counts: Record<FilterTab, number>
  /** Selects a tab, which also re-scopes what "Confirm all" applies to. */
  onSelect: (tab: FilterTab) => void
  /** Locks the strip while "Confirm all" is running. */
  disabled: boolean
}

/**
 * CandidateFilterTabs is the Vše / Nové / Přiřadit / Hotovo segmented control. Each
 * segment carries its live count, and selecting one both filters the grid and
 * narrows the "Confirm all" batch. The whole strip locks while a batch runs so the
 * target set cannot shift under it.
 */
export function CandidateFilterTabs({
  active,
  counts,
  onSelect,
  disabled,
}: CandidateFilterTabsProps) {
  const { t } = useTranslation()

  return (
    <ButtonGroup aria-label={t('faceSearch.tabs.label')} className="flex-wrap">
      {FILTER_TABS.map((tab) => (
        <Button
          key={tab}
          type="button"
          variant={tab === active ? 'primary' : 'outline-secondary'}
          disabled={disabled}
          aria-pressed={tab === active}
          onClick={() => {
            onSelect(tab)
          }}
          className="d-flex align-items-center gap-2"
        >
          {t(TAB_LABEL_KEY[tab])}
          <Badge
            bg={tab === active ? 'light' : 'secondary'}
            text={tab === active ? 'dark' : undefined}
            pill
          >
            {counts[tab]}
          </Badge>
        </Button>
      ))}
    </ButtonGroup>
  )
}
