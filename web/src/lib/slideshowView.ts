import { LIBRARY_DEFAULTS, type LibraryView } from './libraryView'
import { writeUrlState } from './urlState'

/**
 * A slideshow source scope: an album, a label, or neither (the whole library /
 * a filtered grid). At most one of `album` / `label` is set, mirroring
 * {@link import('../hooks/useScopedPhotos').PhotoScope}.
 */
export interface SlideshowScope {
  album?: string
  label?: string
}

/**
 * Builds the link to launch the fullscreen slideshow for the given scope while
 * preserving the current library filters/sort (so the slideshow plays the same
 * photos, in the same order, as the grid the user launched it from). Default
 * filter values are omitted to keep the URL minimal and shareable, exactly like
 * the grid's own URL state.
 */
export function slideshowHref(scope: SlideshowScope, view: LibraryView): string {
  const params = writeUrlState(view, LIBRARY_DEFAULTS)
  if (scope.album !== undefined && scope.album !== '') {
    params.set('album', scope.album)
  }
  if (scope.label !== undefined && scope.label !== '') {
    params.set('label', scope.label)
  }
  const query = params.toString()
  return query === '' ? '/slideshow' : `/slideshow?${query}`
}
