import { type MapFeature } from '../services/map'

/**
 * Builds the popup content for a map feature: a thumbnail (and title, when
 * present) wrapped in a link to the photo detail. The link carries a real `href`
 * for accessibility and middle-click / open-in-new-tab, but a plain left click
 * is intercepted so navigation stays within the SPA via `onSelect`.
 *
 * Kept out of the Leaflet component module so it can be unit-tested in isolation
 * and so the component file exports only a component (fast-refresh friendly).
 */
export function buildPopupElement(
  feature: MapFeature,
  onSelect: (uid: string) => void,
  thumbAlt: string,
): HTMLElement {
  const { uid, title, thumb } = feature.properties
  const link = document.createElement('a')
  link.href = `/photos/${encodeURIComponent(uid)}`
  link.className = 'kukatko-map-popup d-block text-decoration-none'

  const img = document.createElement('img')
  img.src = thumb
  img.alt = title !== '' ? title : thumbAlt
  img.loading = 'lazy'
  img.style.display = 'block'
  img.style.maxWidth = '220px'
  img.style.height = 'auto'
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
