/**
 * Shared presentation of the global search's direct UID hit — the answer to
 * pasting an id into the search box. Both surfaces that render one (the command
 * palette and the search page's cross-entity sections) need the same vocabulary:
 * what the id named, what state the photo behind it is in, and which glyph
 * stands for the thing being opened. Keeping it here means the two never drift
 * apart, and the i18n keys stay literal (a template-built key would widen to an
 * unknown key and lose type checking).
 */

import type { ParseKeys, TFunction } from 'i18next'

import type { IconName } from '../components/Icon'
import type {
  GlobalSearchDirect,
  GlobalSearchPhotoState,
  GlobalSearchTargetKind,
  GlobalSearchUidKind,
} from '../services/search'

/** What each UID prefix names, for the hit's explanatory line. */
export const DIRECT_KIND_LABEL: Record<GlobalSearchUidKind, ParseKeys> = {
  photo: 'globalSearch.direct.kind.photo',
  album: 'globalSearch.direct.kind.album',
  label: 'globalSearch.direct.kind.label',
  person: 'globalSearch.direct.kind.person',
  marker: 'globalSearch.direct.kind.marker',
  stack: 'globalSearch.direct.kind.stack',
  photoprism: 'globalSearch.direct.kind.photoprism',
}

/**
 * What the ids that stand for something ELSE resolve to. Only those three have
 * an entry: for a photo, album, label or person id the thing named and the thing
 * opened are the same, and saying it twice would be noise.
 */
export const DIRECT_VIA_LABEL: Partial<Record<GlobalSearchUidKind, ParseKeys>> = {
  marker: 'globalSearch.direct.via.marker',
  stack: 'globalSearch.direct.via.stack',
  photoprism: 'globalSearch.direct.via.photoprism',
}

/** The states a direct photo hit can report, for that same line. */
export const DIRECT_STATE_LABEL: Record<GlobalSearchPhotoState, ParseKeys> = {
  archived: 'globalSearch.direct.state.archived',
  hidden: 'globalSearch.direct.state.hidden',
  private: 'globalSearch.direct.state.private',
  stack_member: 'globalSearch.direct.state.stackMember',
}

/** The medallion glyph for a direct hit, chosen by what it opens. */
export const DIRECT_TARGET_ICON: Record<GlobalSearchTargetKind, IconName> = {
  photo: 'images',
  album: 'collection',
  label: 'tags',
  person: 'person-circle',
}

/**
 * The direct hit's explanatory line: what the pasted id was (a marker, a stack,
 * a PhotoPrism id — the cases where the thing opened is not the thing named),
 * followed by any state that keeps the photo out of the library view, so a hit
 * that is archived or hidden says so instead of looking like an ordinary result.
 */
export function directHitSecondary(direct: GlobalSearchDirect, t: TFunction): string {
  const parts = [t(DIRECT_KIND_LABEL[direct.kind])]
  const via = DIRECT_VIA_LABEL[direct.kind]
  if (via !== undefined) {
    parts.push(t(via))
  }
  for (const state of direct.states ?? []) {
    parts.push(t(DIRECT_STATE_LABEL[state]))
  }
  return parts.join(' · ')
}

/** The hit's headline: its title, falling back to the bare id it resolved from. */
export function directHitTitle(direct: GlobalSearchDirect): string {
  return direct.title === undefined || direct.title === '' ? direct.uid : direct.title
}
