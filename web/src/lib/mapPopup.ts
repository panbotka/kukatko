import { type MapFeature } from '../services/map'

/**
 * Builds the popup content for a map feature: a thumbnail (and title, when
 * present) wrapped in a link to the photo detail. The link carries a real `href`
 * for accessibility and middle-click / open-in-new-tab, but a plain left click
 * is intercepted so navigation stays within the SPA via `onSelect`.
 *
 * The href is the photo page's own path run through `projectPath` — the
 * caller's `useProjectPath()` — so a new tab opens the photo inside the current
 * project. Without it the href is the bare `/photos/<uid>`, which the app's
 * legacy-address redirect still forwards into a project.
 *
 * An image that fails to load is replaced by the same "no preview available"
 * stand-in `ThumbUnavailable` draws (its markup, built by hand: a Leaflet popup
 * is plain DOM), named `unavailableLabel`.
 *
 * Kept out of the Leaflet component module so it can be unit-tested in isolation
 * and so the component file exports only a component (fast-refresh friendly).
 */
export function buildPopupElement(
  feature: MapFeature,
  onSelect: (uid: string) => void,
  thumbAlt: string,
  projectPath: (path: string) => string = (path) => path,
  unavailableLabel = '',
): HTMLElement {
  const { uid, title, thumb } = feature.properties
  const link = document.createElement('a')
  link.href = projectPath(`/photos/${encodeURIComponent(uid)}`)
  link.className = 'kukatko-map-popup d-block text-decoration-none'

  const img = document.createElement('img')
  img.src = thumb
  img.alt = title !== '' ? title : thumbAlt
  img.loading = 'lazy'
  img.style.display = 'block'
  img.style.maxWidth = '220px'
  img.style.height = 'auto'
  // A photo without a preview (its thumbnail answers 404) gets the library's
  // "no preview available" stand-in, never the browser's broken-image icon.
  img.addEventListener('error', () => {
    img.replaceWith(unavailableElement(unavailableLabel))
  })
  link.appendChild(img)

  if (title !== '') {
    const caption = document.createElement('span')
    caption.className = 'd-block small mt-1'
    caption.textContent = title
    link.appendChild(caption)
  }

  link.addEventListener('click', (event) => {
    // Plain left click navigates within the SPA; let modified clicks (new tab,
    // download) fall through to the browser's default handling of the href.
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey) {
      return
    }
    event.preventDefault()
    onSelect(uid)
  })

  return link
}

/**
 * The "no preview available" stand-in as plain DOM: `ThumbImage`'s box (a
 * 220 × 165 well, the popup image's own width) holding `ThumbUnavailable`'s
 * markup, announced as one image named `label`.
 */
function unavailableElement(label: string): HTMLElement {
  const box = document.createElement('span')
  box.className = 'kk-thumb-unavailable-box'
  box.style.width = '220px'
  box.style.height = '165px'
  const placeholder = document.createElement('span')
  placeholder.className = 'kk-thumb-unavailable text-secondary'
  placeholder.setAttribute('role', 'img')
  placeholder.setAttribute('aria-label', label)
  const body = document.createElement('span')
  body.className = 'kk-thumb-unavailable-body'
  const icon = document.createElement('i')
  icon.className = 'bi bi-image kk-thumb-unavailable-icon'
  icon.setAttribute('aria-hidden', 'true')
  const text = document.createElement('span')
  text.className = 'kk-thumb-unavailable-text'
  text.textContent = label
  body.append(icon, text)
  placeholder.appendChild(body)
  box.appendChild(placeholder)
  return box
}
