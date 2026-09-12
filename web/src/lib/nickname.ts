/**
 * A subject's nickname: how it is written next to the name, and how it is
 * searched for.
 *
 * Every person in a village archive has a name on their documents and, very
 * often, the thing everybody actually calls them. The library stores both
 * (`services/people` — `Subject.nickname`, empty when nobody recorded one, which
 * is the usual case). Two rules hold everywhere it is shown:
 *
 *   * it appears **beside** the name, never instead of it — Bohumil Nečas
 *     („Bohouš") — because the name is what identifies the person and the
 *     nickname is how they are addressed;
 *   * when it is empty, nothing at all is drawn. Empty quotation marks beside
 *     every second person would be noise standing in for a fact nobody recorded.
 *
 * It is matched wherever the name is matched, which is the whole point: the
 * handle somebody naming faces remembers is usually the nickname.
 */

import { foldedIncludes } from './text'

/**
 * The nickname as it is written beside a name — `„Bohouš"`, in Czech quotation
 * marks — or `null` when there is nothing to show.
 *
 * A blank or absent nickname yields `null` rather than an empty string, so a
 * caller has to decide what to render instead of silently drawing empty quotes.
 * The quotes live here rather than in each component so the form is the same in
 * the page header, on a tile and in a suggestion row.
 */
export function nicknameTag(nickname: string | null | undefined): string | null {
  if (nickname === null || nickname === undefined) {
    return null
  }
  const trimmed = nickname.trim()
  if (trimmed === '') {
    return null
  }
  return `„${trimmed}"`
}

/**
 * A name with the nickname appended — `Bohumil Nečas („Bohouš")` — or the name
 * alone when there is no nickname.
 *
 * It is for the places that have a single line of text and no room for a second
 * one: a suggestion row in the search box, an option in a picker. Where a layout
 * can hold two elements, {@link nicknameTag} beside the name reads better, since
 * the nickname can then be styled as the aside it is.
 */
export function withNickname(name: string, nickname: string | null | undefined): string {
  const tag = nicknameTag(nickname)
  return tag === null ? name : `${name} (${tag})`
}

/**
 * Reports whether a subject answers to `query` by either its name or its
 * nickname, matched case- and accent-insensitively as a substring
 * ({@link foldedIncludes}) — the same rule the backend's subject search applies.
 *
 * An empty nickname can never widen a match: a missing one folds to the empty
 * string, which contains nothing but the empty needle — and an empty needle
 * already matched through the name.
 */
export function matchesSubjectName(
  name: string,
  nickname: string | null | undefined,
  query: string,
): boolean {
  return foldedIncludes(name, query) || foldedIncludes(nickname ?? '', query)
}

/**
 * Reports whether `query` found the subject through its **nickname alone** — the
 * name does not contain it, the nickname does.
 *
 * It is what decides whether a search result shows the nickname: a row matched by
 * the name needs no explanation, while a row whose name has nothing to do with
 * what was typed looks like a bug until the nickname is on screen. An empty query
 * matched nothing in particular, so it is never a nickname match.
 */
export function matchedByNickname(
  name: string,
  nickname: string | null | undefined,
  query: string,
): boolean {
  if (query.trim() === '') {
    return false
  }
  return !foldedIncludes(name, query) && foldedIncludes(nickname ?? '', query)
}
