import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import { useTranslation } from 'react-i18next'

import { isTaskListView, TASK_LIST_VIEW } from '../../lib/libraryView'
import { Icon } from '../Icon'

/** Props for {@link TaskViewToggle}. */
export interface TaskViewToggleProps {
  /** The current `TaskView.view` value: `''` for the wall, `'list'` for the ledger. */
  value: string
  /** Called with the new `TaskView.view` value when the other layout is picked. */
  onChange: (view: string) => void
  /** Button size; `lg` matches the filter bar's controls, `sm` a caption line. */
  size?: 'sm' | 'lg'
}

/**
 * Wall or ledger: how a task's group is laid out. Two pressed-state buttons in
 * one group, each naming its layout to assistive technology and pointing at it
 * with a glyph, sitting beside the density stepper because both answer "how do
 * I want to look at these" rather than "which of these do I want to see".
 *
 * The choice is URL state (`?view=list`), not a stored preference: a reviewer
 * shares the ledger of a batch by sending its link, and Back out of a photo
 * lands on the layout the reader left.
 */
export function TaskViewToggle({ value, onChange, size = 'lg' }: TaskViewToggleProps) {
  const { t } = useTranslation()
  const list = isTaskListView(value)
  return (
    <ButtonGroup size={size} aria-label={t('taskDetail.view.label')}>
      <Button
        type="button"
        variant={list ? 'outline-secondary' : 'secondary'}
        aria-pressed={!list}
        aria-label={t('taskDetail.view.grid')}
        title={t('taskDetail.view.grid')}
        onClick={() => {
          if (list) {
            onChange('')
          }
        }}
      >
        <Icon name="grid-3x3-gap-fill" />
      </Button>
      <Button
        type="button"
        variant={list ? 'secondary' : 'outline-secondary'}
        aria-pressed={list}
        aria-label={t('taskDetail.view.list')}
        title={t('taskDetail.view.list')}
        onClick={() => {
          if (!list) {
            onChange(TASK_LIST_VIEW)
          }
        }}
      >
        <Icon name="list-ul" />
        <span className="ms-1 d-none d-sm-inline">{t('taskDetail.view.list')}</span>
      </Button>
    </ButtonGroup>
  )
}
