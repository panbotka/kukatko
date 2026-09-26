/**
 * Turns a raw user-agent string into the two words a person recognises their
 * device by — "Chrome" and "Android" — for the list of browsers registered to
 * receive notifications.
 *
 * Deliberately small: a handful of browsers and systems that actually show up
 * on a family archive's phones and laptops, matched in the order that the
 * user-agent strings' own history demands (every Chromium browser also says
 * "Chrome" and "Safari", every Chrome also says "Safari"). Anything it does not
 * know comes back as `null`, and the caller says "unknown" rather than printing
 * the raw string.
 */

/** A system the summary names; its words live in the locale files. */
export type DeviceOs = 'android' | 'ios' | 'ipados' | 'windows' | 'macos' | 'chromeos' | 'linux'

/** What {@link describeUserAgent} recognised; either half may be unknown. */
export interface DeviceSummary {
  /** The browser's own name ("Chrome", "Firefox", …), a proper noun, never translated. */
  browser: string | null
  /** The operating system, or null when the string names none this module knows. */
  os: DeviceOs | null
}

/**
 * Browsers in match order: the first whose pattern matches wins. The Chromium
 * derivatives come before Chrome, and Chrome before Safari, because each later
 * one's token also appears in the earlier ones' strings. On iOS every browser is
 * WebKit underneath, but it still announces itself (`CriOS`, `FxiOS`, `EdgiOS`),
 * and the name on the icon is what a person recognises.
 */
const BROWSERS: readonly (readonly [RegExp, string])[] = [
  [/\bEdg(e|A|iOS)?\//, 'Edge'],
  [/\bOPR\/|\bOpera\b/, 'Opera'],
  [/\bSamsungBrowser\//, 'Samsung Internet'],
  [/\bYaBrowser\//, 'Yandex'],
  [/\bVivaldi\//, 'Vivaldi'],
  [/\bFirefox\/|\bFxiOS\//, 'Firefox'],
  [/\bChrome\/|\bCriOS\/|\bChromium\//, 'Chrome'],
  [/\bVersion\/[\d.]+.*\bSafari\/|\b(iPhone|iPad|iPod)\b.*AppleWebKit/, 'Safari'],
]

/**
 * Systems in match order. Android before Linux (Android says both), the iOS
 * devices before macOS (an iPhone says "like Mac OS X"), ChromeOS before Linux.
 */
const SYSTEMS: readonly (readonly [RegExp, DeviceOs])[] = [
  [/\bAndroid\b/, 'android'],
  [/\biPad\b/, 'ipados'],
  [/\b(iPhone|iPod)\b/, 'ios'],
  [/\bCrOS\b/, 'chromeos'],
  [/\bWindows\b/, 'windows'],
  [/\bMac OS X\b|\bMacintosh\b/, 'macos'],
  [/\bLinux\b|\bX11\b/, 'linux'],
]

/** The first entry of `table` whose pattern matches `ua`, or null. */
function firstMatch<T>(table: readonly (readonly [RegExp, T])[], ua: string): T | null {
  for (const [pattern, value] of table) {
    if (pattern.test(ua)) {
      return value
    }
  }
  return null
}

/**
 * Summarises a user-agent string as a browser name and an operating system.
 * An empty or unrecognisable string yields `{ browser: null, os: null }`.
 */
export function describeUserAgent(ua: string): DeviceSummary {
  return { browser: firstMatch(BROWSERS, ua), os: firstMatch(SYSTEMS, ua) }
}
