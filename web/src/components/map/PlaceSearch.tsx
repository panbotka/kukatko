import { useRef, useState } from 'react'
import Form from 'react-bootstrap/Form'
import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'

import { usePlaceSearch } from '../../hooks/usePlaceSearch'
import { type Place } from '../../services/map'

/** Props for {@link PlaceSearch}. */
export interface PlaceSearchProps {
  /** Unique id tying the label, input and listbox together. */
  id: string
  /** Called with the chosen place; the caller writes its coordinates wherever they belong. */
  onPick: (place: Place) => void
  /** Disables the field while a save is in flight. */
  disabled?: boolean
}

/**
 * The third way into a photo's location: type a place name, pick it from the
 * dropdown, and the caller gets the coordinates. For a scanned photo you know was
 * taken in Veselí nad Moravou, panning a map to find the spot is the wrong
 * interaction — you know the name, not the numbers.
 *
 * It sits beside the coordinate field and the map picker rather than replacing
 * them: a coordinate pasted from elsewhere and a click on the map are both still
 * the fastest route for the cases they suit.
 *
 * Each row names the place *and* what contains it, because the disambiguation is
 * the entire point — "Veselí nad Moravou" is a town, a chateau and a municipality
 * part, and a list of three identical-looking rows would be useless. Typing is
 * debounced and in-flight lookups are cancelled ({@link usePlaceSearch}): every
 * uncached lookup costs a mapy.com credit, so a request per keystroke is how you
 * burn a monthly quota in an afternoon.
 *
 * Built on react-bootstrap primitives with combobox/listbox ARIA roles and
 * keyboard handling (arrows to move, Enter to pick, Esc to close), mirroring
 * {@link import('../MultiSelect').MultiSelect} — it is a form control and behaves
 * like one. When place search is unavailable (no API key, provider down) it says
 * so in one line and leaves the rest of the editor alone.
 */
export function PlaceSearch({ id, onPick, disabled }: PlaceSearchProps) {
  const { t } = useTranslation()
  // What the field shows, and — separately — what is actually searched for.
  // Picking a suggestion writes its name into the field for confirmation but
  // clears the term, so choosing "Veselí nad Moravou" does not immediately search
  // for "Veselí nad Moravou" again.
  const [query, setQuery] = useState('')
  const [term, setTerm] = useState('')
  const [open, setOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(-1)
  const containerRef = useRef<HTMLDivElement>(null)

  const { status, places } = usePlaceSearch(term)
  const listboxId = `${id}-listbox`
  const showList = open && term.trim() !== ''

  function pick(place: Place) {
    setQuery(place.name)
    setTerm('')
    setOpen(false)
    setActiveIndex(-1)
    onPick(place)
  }

  function close() {
    setOpen(false)
    setActiveIndex(-1)
  }

  function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    switch (event.key) {
      case 'ArrowDown':
        event.preventDefault()
        setOpen(true)
        setActiveIndex((i) => Math.min(i + 1, places.length - 1))
        break
      case 'ArrowUp':
        event.preventDefault()
        setActiveIndex((i) => Math.max(i - 1, -1))
        break
      case 'Enter':
        // Never submit the surrounding metadata form from this field: Enter here
        // means "take this suggestion", not "save the photo".
        event.preventDefault()
        if (activeIndex >= 0 && activeIndex < places.length) {
          pick(places[activeIndex])
        } else if (places.length > 0) {
          // Nothing highlighted but suggestions are up: take the best match.
          pick(places[0])
        }
        break
      case 'Escape':
        close()
        break
      default:
        break
    }
  }

  return (
    <Form.Group
      className="mb-2"
      onBlur={(event: React.FocusEvent<HTMLDivElement>) => {
        // Close only when focus leaves the whole widget (not on inner moves).
        if (!containerRef.current?.contains(event.relatedTarget)) {
          close()
        }
      }}
    >
      <Form.Label htmlFor={id} className="small text-secondary mb-1">
        {t('photo.metadata.placeSearch')}
      </Form.Label>
      <div ref={containerRef} className="position-relative">
        <Form.Control
          id={id}
          type="text"
          className="kukatko-tap-target"
          value={query}
          placeholder={t('photo.metadata.placeSearchPlaceholder')}
          role="combobox"
          aria-expanded={showList}
          aria-controls={listboxId}
          aria-autocomplete="list"
          autoComplete="off"
          disabled={disabled}
          aria-describedby={`${id}-help`}
          onFocus={() => {
            setOpen(true)
          }}
          onKeyDown={handleKeyDown}
          onChange={(event) => {
            setQuery(event.target.value)
            setTerm(event.target.value)
            setOpen(true)
            setActiveIndex(-1)
          }}
        />
        {status === 'loading' && (
          <Spinner
            animation="border"
            size="sm"
            role="status"
            className="position-absolute top-50 end-0 translate-middle-y me-2 text-secondary"
          >
            <span className="visually-hidden">{t('photo.metadata.placeSearching')}</span>
          </Spinner>
        )}

        {showList && (status === 'ready' || status === 'loading') && (
          <ul
            id={listboxId}
            role="listbox"
            aria-label={t('photo.metadata.placeSearch')}
            className="dropdown-menu show w-100 mt-1 shadow overflow-auto"
            style={{ top: '100%', maxHeight: '50vh' }}
          >
            {status === 'ready' && places.length === 0 && (
              <li className="dropdown-item-text text-secondary small">
                {t('photo.metadata.placeNoMatch')}
              </li>
            )}
            {places.map((place, index) => (
              <li key={`${place.type}-${place.lat}-${place.lng}-${place.name}`}>
                <button
                  type="button"
                  role="option"
                  aria-selected={index === activeIndex}
                  className={`dropdown-item kukatko-tap-target ${index === activeIndex ? 'active' : ''}`}
                  // Keep the input focused so the blur handler does not close the
                  // menu before the click lands.
                  onMouseDown={(event) => {
                    event.preventDefault()
                  }}
                  onClick={() => {
                    pick(place)
                  }}
                >
                  <span className="d-flex justify-content-between align-items-baseline gap-2">
                    <span className="text-truncate">{place.name}</span>
                    {place.label !== '' && (
                      <span className="small text-secondary flex-shrink-0">{place.label}</span>
                    )}
                  </span>
                  {place.location !== '' && (
                    <span className="d-block small text-secondary text-truncate">
                      {place.location}
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {status === 'unavailable' && (
        <Form.Text className="text-secondary d-block">
          {t('photo.metadata.placeSearchUnavailable')}
        </Form.Text>
      )}
      {status === 'error' && (
        <Form.Text className="text-danger d-block">
          {t('photo.metadata.placeSearchError')}
        </Form.Text>
      )}
      <Form.Text id={`${id}-help`} className="text-secondary d-block">
        {t('photo.metadata.placeSearchHelp')}
      </Form.Text>
    </Form.Group>
  )
}
