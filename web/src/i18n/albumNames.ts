/**
 * Display names for machine-made album titles.
 *
 * The library carries hundreds of albums PhotoPrism generated in English — month
 * folders like `January 2026` and place albums named after a country — next to
 * the albums somebody actually created and named in Czech. Renaming them in the
 * database is out of the question (the titles are data, and the importers that
 * wrote them are gone), so the Czech UI translates them **at display time only**
 * through the maps below.
 *
 * The rule is deliberately strict: a title is rewritten only when it matches a
 * known pattern *exactly* — a leading English month optionally followed by a
 * four-digit year, or the whole title being a known English country name.
 * Anything else is shown verbatim, so a hand-written album name can never be
 * mangled by a partial match.
 */

/** English month names as PhotoPrism writes them, index 0 = January. */
const EN_MONTHS = [
  'january',
  'february',
  'march',
  'april',
  'may',
  'june',
  'july',
  'august',
  'september',
  'october',
  'november',
  'december',
] as const

/**
 * Czech month names in the nominative, lower-cased the way Czech writes a month
 * beside a year (`leden 2026`, not `Leden 2026`).
 */
const CS_MONTHS = [
  'leden',
  'únor',
  'březen',
  'duben',
  'květen',
  'červen',
  'červenec',
  'srpen',
  'září',
  'říjen',
  'listopad',
  'prosinec',
] as const

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

/** The year a month album is named after: exactly four digits, nothing else. */
const YEAR_RE = /^\d{4}$/

/**
 * Reports whether the active UI language is Czech. Region subtags (`cs-CZ`)
 * count, so a browser-negotiated language still gets the translated names.
 */
function isCzech(language: string): boolean {
  return language.toLowerCase().startsWith('cs')
}

/**
 * Translates a machine-made English album title for display, returning `title`
 * unchanged whenever it does not match a known pattern exactly — and always
 * when the UI language is not Czech (English is the language those titles are
 * already in).
 *
 * @example
 *   albumDisplayTitle('January 2026', 'cs') // 'leden 2026'
 *   albumDisplayTitle('Czechia', 'cs')      // 'Česko'
 *   albumDisplayTitle('Dovolená 2019', 'cs')// 'Dovolená 2019' (untouched)
 */
export function albumDisplayTitle(title: string, language: string): string {
  if (!isCzech(language)) {
    return title
  }
  const trimmed = title.trim()
  if (trimmed === '') {
    return title
  }

  const country = CS_COUNTRIES[trimmed.toLowerCase()]
  if (country !== undefined) {
    return country
  }

  // A month name on its own, or a month name followed by a four-digit year —
  // and nothing else, so `May Day` and `January in Norway` stay as they are.
  const words = trimmed.split(/\s+/)
  if (words.length > 2) {
    return title
  }
  const index = (EN_MONTHS as readonly string[]).indexOf(words[0].toLowerCase())
  if (index < 0) {
    return title
  }
  const month = CS_MONTHS[index]
  if (words.length === 1) {
    return month
  }
  return YEAR_RE.test(words[1]) ? `${month} ${words[1]}` : title
}
