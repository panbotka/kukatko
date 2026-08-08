/**
 * Display names for the English country names the library has stored in its own
 * data.
 *
 * The importers that are gone composed names out of English geo data: place
 * albums called `Czech Republic 2026`, photo titles reading
 * `Jméno / Czech Republic / 2026`. Those strings are catalogue data — rewriting
 * them in the database is out of the question — so the Czech UI translates them
 * **at display time only**, through the dictionary below, and falls back to the
 * raw string whenever it has no translation.
 *
 * The dictionary lives here rather than in `locales/*.json` because it is keyed
 * by a *stored* English string, not by a UI key: it translates data, not labels,
 * and the English UI wants those names exactly as they are stored.
 *
 * {@link localizeCountryNames} is deliberately strict about *where* it will
 * substitute — only a whole `/`- or `,`-separated segment, optionally trailed by
 * a four-digit year — so a hand-written album called `New Zealand trip` can
 * never be half-translated into `Nový Zéland trip`.
 */

/**
 * English → Czech country names, keyed lower-cased. Both official variants are
 * listed where the English name has two common forms (`Czechia` /
 * `Czech Republic`), and the constituent countries of the United Kingdom are in
 * too, because a place album can be named after any of them.
 */
const CS_COUNTRIES: Readonly<Record<string, string | undefined>> = {
  albania: 'Albánie',
  argentina: 'Argentina',
  australia: 'Austrálie',
  austria: 'Rakousko',
  belarus: 'Bělorusko',
  belgium: 'Belgie',
  'bosnia and herzegovina': 'Bosna a Hercegovina',
  brazil: 'Brazílie',
  bulgaria: 'Bulharsko',
  cambodia: 'Kambodža',
  canada: 'Kanada',
  chile: 'Chile',
  china: 'Čína',
  croatia: 'Chorvatsko',
  cuba: 'Kuba',
  cyprus: 'Kypr',
  czechia: 'Česko',
  'czech republic': 'Česko',
  denmark: 'Dánsko',
  egypt: 'Egypt',
  england: 'Anglie',
  estonia: 'Estonsko',
  finland: 'Finsko',
  france: 'Francie',
  germany: 'Německo',
  greece: 'Řecko',
  hungary: 'Maďarsko',
  iceland: 'Island',
  india: 'Indie',
  indonesia: 'Indonésie',
  ireland: 'Irsko',
  israel: 'Izrael',
  italy: 'Itálie',
  japan: 'Japonsko',
  kenya: 'Keňa',
  latvia: 'Lotyšsko',
  lithuania: 'Litva',
  luxembourg: 'Lucembursko',
  malaysia: 'Malajsie',
  malta: 'Malta',
  mexico: 'Mexiko',
  moldova: 'Moldavsko',
  montenegro: 'Černá Hora',
  morocco: 'Maroko',
  netherlands: 'Nizozemsko',
  'new zealand': 'Nový Zéland',
  'north macedonia': 'Severní Makedonie',
  norway: 'Norsko',
  peru: 'Peru',
  poland: 'Polsko',
  portugal: 'Portugalsko',
  romania: 'Rumunsko',
  russia: 'Rusko',
  scotland: 'Skotsko',
  serbia: 'Srbsko',
  singapore: 'Singapur',
  slovakia: 'Slovensko',
  slovenia: 'Slovinsko',
  'south africa': 'Jihoafrická republika',
  'south korea': 'Jižní Korea',
  spain: 'Španělsko',
  'sri lanka': 'Srí Lanka',
  sweden: 'Švédsko',
  switzerland: 'Švýcarsko',
  tanzania: 'Tanzanie',
  thailand: 'Thajsko',
  tunisia: 'Tunisko',
  turkey: 'Turecko',
  türkiye: 'Turecko',
  ukraine: 'Ukrajina',
  'united arab emirates': 'Spojené arabské emiráty',
  'united kingdom': 'Spojené království',
  'united states': 'Spojené státy',
  'united states of america': 'Spojené státy',
  vietnam: 'Vietnam',
  wales: 'Wales',
}

/** A trailing year in a machine-composed name: exactly four digits. */
const TRAILING_YEAR_RE = /^(.*?)\s+(\d{4})$/

/** The separators a machine-composed name is built from, kept by the split. */
const SEPARATORS = /([/,])/

/** Whether a piece of a split name is one of the separators, not a segment. */
function isSeparator(part: string): boolean {
  return part === '/' || part === ','
}

/**
 * Reports whether the active UI language is Czech. Region subtags (`cs-CZ`)
 * count, so a browser-negotiated language still gets the translated names.
 * Shared with `albumNames`, which asks the same question about the same data.
 */
export function isCzech(language: string): boolean {
  return language.toLowerCase().startsWith('cs')
}

/**
 * The Czech name of a country stored in English, or undefined when the
 * dictionary has none — the caller then shows the stored string, which is the
 * only other honest option. Always undefined outside Czech: English is the
 * language those names are already in.
 *
 * @example
 *   countryDisplayName('Czech Republic', 'cs') // 'Česko'
 *   countryDisplayName('Narnia', 'cs')         // undefined
 */
export function countryDisplayName(name: string, language: string): string | undefined {
  if (!isCzech(language)) {
    return undefined
  }
  return CS_COUNTRIES[name.trim().toLowerCase()]
}

/**
 * The Czech reading of one whole segment: a known country on its own
 * (`Czech Republic`), or a known country followed by a four-digit year
 * (`Czech Republic 2026`). Undefined when the segment is neither.
 */
function translateSegment(trimmed: string, language: string): string | undefined {
  const country = countryDisplayName(trimmed, language)
  if (country !== undefined) {
    return country
  }
  const dated = TRAILING_YEAR_RE.exec(trimmed)
  if (dated === null) {
    return undefined
  }
  const named = countryDisplayName(dated[1], language)
  return named === undefined ? undefined : `${named} ${dated[2]}`
}

/**
 * Translates one segment of a composed name in place, keeping the spacing that
 * separated it from the delimiters around it. Anything the dictionary does not
 * recognise comes back unchanged.
 */
function localizeSegment(segment: string, language: string): string {
  const trimmed = segment.trim()
  if (trimmed === '') {
    return segment
  }
  const translated = translateSegment(trimmed, language)
  if (translated === undefined) {
    return segment
  }
  const lead = segment.slice(0, segment.indexOf(trimmed))
  const tail = segment.slice(lead.length + trimmed.length)
  return `${lead}${translated}${tail}`
}

/**
 * Translates the English country names in a machine-composed title — an album's
 * or a photo's — for display, leaving everything it does not recognise exactly
 * as it is stored (and leaving everything alone outside Czech).
 *
 * A name is only substituted when it is a whole `/`- or `,`-separated segment,
 * optionally trailed by a four-digit year. That is the shape the importers
 * composed, and the strictness is the point: a partial match would rewrite the
 * middle of somebody's own album name.
 *
 * @example
 *   localizeCountryNames('Jan / Czech Republic / 2026', 'cs') // 'Jan / Česko / 2026'
 *   localizeCountryNames('Czech Republic 2026', 'cs')         // 'Česko 2026'
 *   localizeCountryNames('New Zealand trip', 'cs')            // untouched
 */
export function localizeCountryNames(text: string, language: string): string {
  if (!isCzech(language) || text.trim() === '') {
    return text
  }
  return text
    .split(SEPARATORS)
    .map((part) => (isSeparator(part) ? part : localizeSegment(part, language)))
    .join('')
}
