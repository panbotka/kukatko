import type { ParseKeys } from 'i18next'
import { Fragment } from 'react'
import Accordion from 'react-bootstrap/Accordion'
import Col from 'react-bootstrap/Col'
import Row from 'react-bootstrap/Row'
import { useTranslation } from 'react-i18next'

import { useCapabilities } from '../capabilities/CapabilitiesContext'
import { Icon, type IconName } from '../components/Icon'
import { SearchQueryReference } from '../components/search/SearchQueryReference'
import { useDocumentTitle } from '../hooks/useDocumentTitle'
import { commitUrl, formatVersion } from '../lib/version'

/**
 * One collapsible help section: a stable `id` (used as the accordion event key
 * and as the anchor target its table-of-contents entry links to), a decorative
 * icon, and the i18n keys for its heading and prose. Content itself lives in the
 * `help.sections.*` namespace (cs default, en) — this list only wires each
 * section to its keys, so the page and its table of contents stay in sync.
 */
interface HelpSection {
  id: string
  icon: IconName
  titleKey: ParseKeys
  bodyKey: ParseKeys
}

/** The help sections, in reading order — mirrors the features that exist today. */
const SECTIONS: HelpSection[] = [
  {
    id: 'browsing',
    icon: 'grid-3x3-gap-fill',
    titleKey: 'help.sections.browsing.title',
    bodyKey: 'help.sections.browsing.body',
  },
  {
    id: 'search',
    icon: 'search',
    titleKey: 'help.sections.search.title',
    bodyKey: 'help.sections.search.body',
  },
  {
    id: 'albums',
    icon: 'collection',
    titleKey: 'help.sections.albums.title',
    bodyKey: 'help.sections.albums.body',
  },
  {
    id: 'labels',
    icon: 'tags',
    titleKey: 'help.sections.labels.title',
    bodyKey: 'help.sections.labels.body',
  },
  {
    id: 'favorites',
    icon: 'heart',
    titleKey: 'help.sections.favorites.title',
    bodyKey: 'help.sections.favorites.body',
  },
  {
    id: 'people',
    icon: 'people',
    titleKey: 'help.sections.people.title',
    bodyKey: 'help.sections.people.body',
  },
  {
    id: 'duplicates',
    icon: 'files',
    titleKey: 'help.sections.duplicates.title',
    bodyKey: 'help.sections.duplicates.body',
  },
  {
    id: 'stacks',
    icon: 'images',
    titleKey: 'help.sections.stacks.title',
    bodyKey: 'help.sections.stacks.body',
  },
  {
    id: 'map',
    icon: 'map',
    titleKey: 'help.sections.map.title',
    bodyKey: 'help.sections.map.body',
  },
  {
    id: 'deleting',
    icon: 'trash',
    titleKey: 'help.sections.deleting.title',
    bodyKey: 'help.sections.deleting.body',
  },
  {
    id: 'import',
    icon: 'box-arrow-in-down',
    titleKey: 'help.sections.import.title',
    bodyKey: 'help.sections.import.body',
  },
  {
    id: 'roles',
    icon: 'shield-lock',
    titleKey: 'help.sections.roles.title',
    bodyKey: 'help.sections.roles.body',
  },
  {
    id: 'account',
    icon: 'person-circle',
    titleKey: 'help.sections.account.title',
    bodyKey: 'help.sections.account.body',
  },
]

/**
 * The three worked queries the Search chapter opens its query-language part
 * with, before the full reference: one plain filter, two filters and a range,
 * then a quoted value beside a count. They are literal query text, so they are
 * the same in both languages (as the reference's own example is) — only the
 * sentence explaining each is translated, under `help.sections.search.example`.
 */
const QUERY_EXAMPLES: { id: 'year' | 'person' | 'album'; query: string }[] = [
  { id: 'year', query: 'year:1965' },
  { id: 'person', query: 'person:Jarmila rating:4-5' },
  { id: 'album', query: 'album:"Léto 2024" faces:2' },
]

/** The role ladder rows shown inside the "roles" section, low to high. */
const ROLE_ROWS: { role: ParseKeys; descKey: ParseKeys }[] = [
  { role: 'roles.viewer', descKey: 'help.sections.roles.viewer' },
  { role: 'roles.editor', descKey: 'help.sections.roles.editor' },
  { role: 'roles.admin', descKey: 'help.sections.roles.admin' },
  { role: 'roles.maintainer', descKey: 'help.sections.roles.maintainer' },
]

/**
 * Splits a translated body into paragraphs on newlines, so a single i18n string
 * can carry a couple of paragraphs without spawning a key per paragraph. Keyed
 * by paragraph text (help copy is static and every paragraph is unique).
 */
function renderParagraphs(body: string) {
  return body.split('\n').map((para) => (
    <p key={para} className="kk-text-body">
      {para}
    </p>
  ))
}

/**
 * The query-language part of the Search chapter: three ready-made queries with
 * a sentence each, then {@link SearchQueryReference} — the very tables the `?`
 * beside every search field opens.
 *
 * It is embedded rather than retold because help is the one place a feature is
 * found without stumbling over it: a reader who never presses `?` would
 * otherwise never learn that `person:` exists. Embedding also keeps the syntax
 * to a single source of truth, so a filter added to `QUERY_HELP_ROWS` shows up
 * here too instead of leaving help quietly out of date.
 */
function SearchQueryChapter() {
  const { t } = useTranslation()

  return (
    <section className="mt-3" aria-labelledby="help-query-language">
      <h3 id="help-query-language" className="kk-section-title h6">
        {t('help.sections.search.queryTitle')}
      </h3>
      <p className="kk-text-body">{t('help.sections.search.queryIntro')}</p>
      <dl className="row">
        {QUERY_EXAMPLES.map((example) => (
          <Fragment key={example.id}>
            <dt className="col-sm-5">
              <code>{example.query}</code>
            </dt>
            <dd className="col-sm-7 kk-text-body">
              {t(`help.sections.search.example.${example.id}`)}
            </dd>
          </Fragment>
        ))}
      </dl>
      <SearchQueryReference />
    </section>
  )
}

/**
 * The build identity in full, at the foot of the page: the version, and — for a
 * stamped build — the commit as a link into the public repository, so anyone
 * reporting a problem can name exactly which build they saw it on. The user menu
 * shows only the version (it is a narrow place); this is where the whole of it
 * lives.
 *
 * A development build reports the `dev` / `none` placeholders: `dev` is shown as
 * it is and there is no commit to link to. Until the capabilities response
 * arrives — or if it never does — the block renders nothing at all rather than
 * an empty heading.
 */
function BuildInfo() {
  const { t } = useTranslation()
  const { version } = useCapabilities()
  const label = formatVersion(version)
  const href = commitUrl(version)

  if (!label) {
    return null
  }
  return (
    <section className="mt-4" aria-labelledby="help-version">
      <h2 id="help-version" className="kk-section-title h6 mb-1">
        {t('help.version.title')}
      </h2>
      <p className="text-secondary small mb-0">
        {label}
        {href !== null && (
          <>
            {' · '}
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              title={t('help.version.commitTitle')}
            >
              {version?.commit}
            </a>
          </>
        )}
      </p>
    </section>
  )
}

/**
 * End-user Help page: a plain-language tour of what Kukátko does and how each
 * feature behaves, reachable from the user menu by any authenticated role. A
 * short table of contents jumps to sections, each an open-by-default,
 * collapsible {@link Accordion} item so a reader can scan or fold away the noise.
 */
export function HelpPage() {
  const { t } = useTranslation()
  useDocumentTitle(t('help.title'))

  return (
    <Row className="justify-content-center">
      <Col xs={12} lg={9} xl={8}>
        <h1 className="kk-page-title mb-4">{t('help.title')}</h1>
        <p className="kk-text-body mb-4">{t('help.intro')}</p>

        <nav aria-label={t('help.tocTitle')} className="mb-4">
          <h2 className="kk-section-title h5 mb-2">{t('help.tocTitle')}</h2>
          <ul className="list-unstyled mb-0">
            {SECTIONS.map((section) => (
              <li key={section.id} className="mb-1">
                <a href={`#help-${section.id}`} className="d-inline-flex align-items-center gap-2">
                  <Icon name={section.icon} />
                  {t(section.titleKey)}
                </a>
              </li>
            ))}
          </ul>
        </nav>

        <Accordion alwaysOpen defaultActiveKey={SECTIONS.map((section) => section.id)}>
          {SECTIONS.map((section) => (
            <Accordion.Item key={section.id} eventKey={section.id} id={`help-${section.id}`}>
              <Accordion.Header>
                <span className="d-inline-flex align-items-center gap-2">
                  <Icon name={section.icon} />
                  {t(section.titleKey)}
                </span>
              </Accordion.Header>
              <Accordion.Body>
                {renderParagraphs(t(section.bodyKey))}
                {section.id === 'search' && <SearchQueryChapter />}
                {section.id === 'roles' && (
                  <dl className="row mb-0 mt-2">
                    {ROLE_ROWS.map((row) => (
                      <Fragment key={row.role}>
                        <dt className="col-sm-3">{t(row.role)}</dt>
                        <dd className="col-sm-9 kk-text-body">{t(row.descKey)}</dd>
                      </Fragment>
                    ))}
                  </dl>
                )}
              </Accordion.Body>
            </Accordion.Item>
          ))}
        </Accordion>

        <BuildInfo />
      </Col>
    </Row>
  )
}
