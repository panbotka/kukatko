import { localizeCountryNames } from '../i18n/countryNames'
import { type PhotoDetail } from '../services/photos'

/**
 * The pieces {@link photoDisplayTitle} needs from a photo. Narrower than
 * {@link PhotoDetail} so the rule can be reasoned about (and tested) without
 * building a whole photo.
 */
export interface TitleSource {
  /** The user's title for the photo, empty when unset. */
  title: string
  /** The capture time, absent when unknown. */
  taken_at?: string
  /** The cached reverse-geocoded place, absent when the photo has none. */
  place?: { city: string; place_name: string; country: string }
}

/** What {@link photoDisplayTitle} decided to show, for the caller to render. */
export type PhotoTitle =
  | { kind: 'title'; text: string }
  /** A date, optionally qualified by a place — both already formatted. */
  | { kind: 'facts'; date: string; place: string }
  /** The photo has no identity to show at all. */
  | { kind: 'unknown' }

/**
 * Picks the most specific place name the photo's geocoded hierarchy offers,
 * narrowest first: the named place ("Špilberk") beats the city, which beats the
 * country. The geocoder leaves levels it did not resolve empty, so this skips the
 * blanks rather than rendering them.
 */
function placeName(place: TitleSource['place']): string {
  if (place === undefined) {
    return ''
  }
  return [place.place_name, place.city, place.country].find((name) => name.trim() !== '') ?? ''
}

/**
 * Decides what a photo is *called*, for the detail page's heading.
 *
 * A filename is the camera's name for a photo, not the user's: "IMG_8423.jpeg"
 * tells the reader nothing they came to the page to learn, and putting it in the
 * `<h1>` spends the page's most prominent line on the least meaningful fact about
 * it. So the heading falls back through what the photo actually *is*, not what its
 * file happens to be called:
 *
 * 1. The title, when someone gave it one. Nothing beats a human's own words.
 * 2. Otherwise the facts the photo carries — its capture date, plus its place when
 *    the geocoder resolved one. "12 July 2026 — Brno" identifies a photo to the
 *    person who took it in a way no filename ever will.
 * 3. Failing both, the caller's "untitled" wording. An undated photo with no place
 *    genuinely has no name, and saying so is more honest than reaching for the
 *    filename — which still exists, in the technical details, where it belongs.
 *
 * The date and place are passed in already formatted, since formatting is
 * locale-dependent and this rule is not.
 */
export function photoDisplayTitle(photo: TitleSource, date: string): PhotoTitle {
  if (photo.title.trim() !== '') {
    return { kind: 'title', text: photo.title.trim() }
  }
  const place = placeName(photo.place)
  if (date !== '' || place !== '') {
    return { kind: 'facts', date, place }
  }
  return { kind: 'unknown' }
}

/**
 * Renders a {@link PhotoTitle} as one plain string, joining a date and a place
 * with an en dash. It is the form needed where only text will do — an `alt`
 * attribute, a player's title, the lightbox caption — while the heading itself
 * renders the parts separately.
 */
export function photoTitleText(title: PhotoTitle, untitled: string): string {
  if (title.kind === 'title') {
    return title.text
  }
  if (title.kind === 'unknown') {
    return untitled
  }
  return [title.date, title.place].filter((part) => part !== '').join(' – ')
}

/**
 * Narrows a full {@link PhotoDetail} to what the title rule reads, translating
 * the English country names the importers composed into the stored title
 * (`Jméno / Czech Republic / 2026`) on the way through. That is a display
 * decision, not a rule about what a photo is called, which is why it happens
 * here and not inside {@link photoDisplayTitle}; the catalogue keeps the title
 * it has.
 */
export function titleSource(photo: PhotoDetail, language: string): TitleSource {
  return {
    title: localizeCountryNames(photo.title, language),
    taken_at: photo.taken_at,
    place: photo.place,
  }
}

/**
 * What a photo is called in a grid tile, a strip or a search hit: its title when
 * it has one, otherwise the file name — the one place a filename does belong,
 * since the alternative in a dense grid is a tile with no label at all. The
 * title goes through the same country dictionary as the detail page's heading,
 * so `Jméno / Czech Republic / 2026` does not read half in English here either.
 */
export function photoLabel(photo: { title: string; file_name: string }, language: string): string {
  return photo.title !== '' ? localizeCountryNames(photo.title, language) : photo.file_name
}
