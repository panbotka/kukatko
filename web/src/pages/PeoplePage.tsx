import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { useAuth } from '../auth/AuthContext'
import { EmptyState } from '../components/EmptyState'
import { ErrorState } from '../components/ErrorState'
import { PeopleFilterBar } from '../components/people/PeopleFilterBar'
import { SubjectTile } from '../components/people/SubjectTile'
import { TileGridSkeleton } from '../components/Skeleton'
import { TileGrid } from '../components/TileGrid'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { useReloadKey } from '../hooks/useReloadKey'
import {
  type PeopleView,
  browsePeople,
  PEOPLE_DEFAULTS,
  peopleBrowseOptions,
} from '../lib/peopleBrowse'
import { useUrlState } from '../lib/urlState'
import { fetchSubjects, type SubjectCount } from '../services/people'

/** Fetch lifecycle of the people list. */
type State =
  | { status: 'loading' }
  | { status: 'error' }
  | { status: 'ready'; subjects: SubjectCount[] }

/** Nobody at all, so the type counts have nothing to report. */
const NO_SUBJECTS: SubjectCount[] = []

/** Minimum tile width in px, shared by the grid and its loading placeholder. */
const MIN_TILE = 140

/** Gap between tiles in px, likewise shared. */
const TILE_GAP = 12

/**
 * The people index: a responsive, virtualized grid of subjects (cover, name,
 * photo count), each linking to its page. Editors and admins also get a link to
 * the cluster review queue, the fast bulk-naming path. The whole view is
 * read-only here; naming and editing happen on the subject and cluster pages.
 *
 * The API returns everybody in one alphabetical list, which for a real library
 * is a wall of a hundred strangers ordered by first name — so the page carries a
 * name search, a filter by kind of subject (people, animals, other) and a choice
 * between alphabetical order and the people with the most photos first. All of
 * it lives in the URL, so Back steps through the views and a link carries the
 * exact one.
 *
 * The grid is virtualized (see `TileGrid`) because each tile's face crop is cut
 * from a preview measured in megapixels: mounting only the rows in view is what
 * keeps opening the page from starting a hundred of those downloads at once.
 */
export function PeoplePage() {
  const { t, i18n } = useTranslation()
  useDocumentTitle(t('people.title'))
  const { canWrite } = useAuth()
  const [state, setState] = useState<State>({ status: 'loading' })
  const [reloadKey, reload] = useReloadKey()
  const [view, setView] = useUrlState<PeopleView>(PEOPLE_DEFAULTS)

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' })
    fetchSubjects(controller.signal)
      .then((subjects) => {
        setState({ status: 'ready', subjects })
      })
      .catch((err: unknown) => {
        if (err instanceof DOMException && err.name === 'AbortError') {
          return
        }
        setState({ status: 'error' })
      })
    return () => {
      controller.abort()
    }
  }, [reloadKey])

  const subjects = state.status === 'ready' ? state.subjects : NO_SUBJECTS
  const language = i18n.language
  const { visible, counts, filteredOut } = useMemo(
    () => browsePeople(subjects, peopleBrowseOptions(view, language)),
    [subjects, view, language],
  )

  return (
    <>
      <div className="d-flex justify-content-between align-items-center mb-3 flex-wrap gap-2">
        <h1 className="kk-page-title mb-0">{t('people.title')}</h1>
        {canWrite && (
          <Link to="/people/clusters" className="btn btn-outline-primary">
            {t('people.reviewClusters')}
          </Link>
        )}
      </div>

      {state.status === 'loading' && (
        <TileGridSkeleton label={t('people.loading')} minTile={MIN_TILE} captionLines={1} />
      )}

      {state.status === 'error' && <ErrorState title={t('people.error')} onRetry={reload} />}

      {state.status === 'ready' && subjects.length === 0 && (
        <EmptyState title={t('people.empty.title')} hint={t('people.empty.hint')} />
      )}

      {state.status === 'ready' && subjects.length > 0 && (
        <>
          <PeopleFilterBar view={view} onChange={setView} counts={counts} />

          {visible.length === 0 && (
            <EmptyState
              title={t('people.noMatches.title')}
              hint={
                filteredOut > 0 ? t('people.noMatches.hintFiltered') : t('people.noMatches.hint')
              }
            />
          )}

          {visible.length > 0 && (
            // The geometry matches the skeleton above, so the grid doesn't shift
            // when the data lands.
            <TileGrid
              items={visible}
              itemKey={(subject) => subject.uid}
              renderItem={(subject) => <SubjectTile subject={subject} />}
              minTile={MIN_TILE}
              gap={TILE_GAP}
            />
          )}
        </>
      )}
    </>
  )
}
