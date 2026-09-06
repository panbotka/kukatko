/**
 * The photo viewer's drawer state as it lives in the URL: WHICH of the drawer's
 * three views — the metadata ("info"), the faces or the non-destructive edits —
 * is open, or none at all.
 *
 * It is one parameter rather than a flag per view because the drawer shows
 * exactly one view at a time: `panel=faces` and `panel=info` at once would be a
 * state the viewer cannot render, and two booleans could express it. Keeping it
 * in the URL is what makes a reload, a shared link and Back/Forward bring back
 * the panel the reader was actually looking at.
 */

/** The drawer's three views, as named in the `panel` query parameter. */
export type ViewerPanel = 'info' | 'faces' | 'edits'

/** The query parameter naming the open panel; absent means the drawer is shut. */
export const PANEL_PARAM = 'panel'

/**
 * The parameter the viewer used before the drawer had more than one view
 * (`info=1`). Links carrying it — shared URLs, bookmarks, the audit log's own
 * deep links — are still read as "open the info view"; writing the state drops
 * it, so a URL is rewritten to the new form the first time anything changes it.
 */
export const LEGACY_INFO_PARAM = 'info'

const PANELS: readonly ViewerPanel[] = ['info', 'faces', 'edits']

/**
 * Reads the open panel from a URL's query, or `null` when no panel is named (the
 * drawer is shut). An unknown value is treated as "not named" rather than as an
 * error — a hand-edited or future URL opens the plain photo instead of blowing
 * up — and the legacy `info=1` is then consulted as a last resort.
 */
export function readViewerPanel(params: URLSearchParams): ViewerPanel | null {
  const named = params.get(PANEL_PARAM)
  const known = PANELS.find((panel) => panel === named)
  if (known !== undefined) {
    return known
  }
  return params.get(LEGACY_INFO_PARAM) === '1' ? 'info' : null
}

/**
 * The query with the open panel written into it: the parameter set for a panel,
 * removed for none, and the legacy `info` flag dropped either way so the two can
 * never disagree. Pure — it returns a new `URLSearchParams` and leaves the input
 * untouched, so it composes with react-router's functional `setSearchParams`.
 */
export function writeViewerPanel(
  params: URLSearchParams,
  panel: ViewerPanel | null,
): URLSearchParams {
  const next = new URLSearchParams(params)
  next.delete(LEGACY_INFO_PARAM)
  if (panel === null) {
    next.delete(PANEL_PARAM)
  } else {
    next.set(PANEL_PARAM, panel)
  }
  return next
}
