import { readCss } from './css'

/**
 * The colour tokens as the browser would compute them — without a browser.
 *
 * jsdom evaluates neither `var()` nor `color-mix()`, so a test that wants to know
 * what `--kk-surface-raised` actually *is* has to work it out itself. This module
 * reads the custom properties declared on `:root` (and the dark theme root) in
 * `palette.css` and `tokens.css` and resolves any expression over them — a hex,
 * `transparent`, `var()` chains and nested `color-mix(in srgb, …)` — to an RGBA
 * value, with the same premultiplied interpolation CSS Color 5 specifies. That is
 * what lets `styles/contrast.test.ts` measure a palette swap before anyone looks
 * at it.
 */

/** One colour: sRGB channels 0–255 and alpha 0–1. */
export interface Rgba {
  r: number
  g: number
  b: number
  a: number
}

/** The stylesheets whose `:root` custom properties make up the token set, in cascade order. */
export const TOKEN_SHEETS = ['src/styles/palette.css', 'src/styles/tokens.css'] as const

/** Strips `/* … *\/` comments, keeping line breaks so offsets stay meaningful. */
export function stripCssComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, (comment) => comment.replace(/[^\n]/g, ' '))
}

/** Splits `text` on `separator` wherever it is outside parentheses and quotes. */
function splitTopLevel(text: string, separator: string): string[] {
  const parts: string[] = []
  let depth = 0
  let quote: string | null = null
  let start = 0
  for (let i = 0; i < text.length; i += 1) {
    const c = text[i]
    if (quote !== null) {
      if (c === quote) {
        quote = null
      }
    } else if (c === '"' || c === "'") {
      quote = c
    } else if (c === '(') {
      depth += 1
    } else if (c === ')') {
      depth -= 1
    } else if (c === separator && depth === 0) {
      parts.push(text.slice(start, i))
      start = i + 1
    }
  }
  parts.push(text.slice(start))
  return parts
}

/**
 * Every custom property declared directly in a top-level `:root` block (the
 * plain one and `:root[data-bs-theme='dark']`) of the given stylesheets, the
 * last declaration winning — the order the cascade applies them in.
 */
export function rootCustomProperties(
  sheets: readonly string[] = TOKEN_SHEETS,
): Map<string, string> {
  const found = new Map<string, string>()
  for (const sheet of sheets) {
    const css = stripCssComments(readCss(sheet))
    const opener = /(^|\})\s*(:root(?:\[data-bs-theme='dark'\])?)\s*\{/g
    let match = opener.exec(css)
    while (match !== null) {
      const open = match.index + match[0].length
      let depth = 1
      let i = open
      for (; i < css.length && depth > 0; i += 1) {
        if (css[i] === '{') {
          depth += 1
        } else if (css[i] === '}') {
          depth -= 1
        }
      }
      for (const declaration of splitTopLevel(css.slice(open, i - 1), ';')) {
        const colon = declaration.indexOf(':')
        const name = declaration.slice(0, colon).trim()
        if (colon > 0 && name.startsWith('--')) {
          found.set(
            name,
            declaration
              .slice(colon + 1)
              .trim()
              .replace(/\s+/g, ' '),
          )
        }
      }
      // Rewind onto the closing brace: the next block's `(^|\})` anchor is that brace.
      opener.lastIndex = i - 1
      match = opener.exec(css)
    }
  }
  return found
}

/** Parses a 3/4/6/8-digit hex colour. */
export function parseHex(hex: string): Rgba {
  const h = hex.replace('#', '')
  const full = h.length <= 4 ? h.replace(/./g, (c) => c + c) : h
  const channel = (i: number): number => parseInt(full.slice(i, i + 2), 16)
  return {
    r: channel(0),
    g: channel(2),
    b: channel(4),
    a: full.length === 8 ? channel(6) / 255 : 1,
  }
}

/** The arguments of a function call whose name ends right before `open`. */
function callArguments(expr: string, open: number): string {
  let depth = 0
  for (let i = open; i < expr.length; i += 1) {
    if (expr[i] === '(') {
      depth += 1
    } else if (expr[i] === ')') {
      depth -= 1
      if (depth === 0) {
        return expr.slice(open + 1, i)
      }
    }
  }
  throw new Error(`unbalanced parentheses in ${expr}`)
}

/** Splits `colour [N%]` into its colour and its optional percentage. */
function mixOperand(text: string): { colour: string; percent: number | null } {
  const m = /^(.*?)\s+([\d.]+)%$/.exec(text.trim())
  return m === null
    ? { colour: text.trim(), percent: null }
    : { colour: m[1].trim(), percent: Number(m[2]) }
}

/** `color-mix(in srgb, a p1, b p2)` per CSS Color 5: premultiplied, normalised, alpha-scaled. */
function colorMix(a: Rgba, p1: number, b: Rgba, p2: number): Rgba {
  const sum = p1 + p2
  const [w1, w2] = [p1 / sum, p2 / sum]
  const alpha = a.a * w1 + b.a * w2
  const channel = (k: 'r' | 'g' | 'b'): number =>
    alpha === 0 ? 0 : (a[k] * a.a * w1 + b[k] * b.a * w2) / alpha
  return {
    r: channel('r'),
    g: channel('g'),
    b: channel('b'),
    a: alpha * Math.min(1, sum / 100),
  }
}

/**
 * Resolves a colour expression against the token set: a hex, `transparent`,
 * `var(--x)` (with or without fallback) or a nested `color-mix(in srgb, …)`.
 * Throws on anything else, so a token this cannot read fails loudly rather than
 * passing unmeasured.
 */
export function resolveColour(
  expr: string,
  tokens: Map<string, string>,
  seen: readonly string[] = [],
): Rgba {
  const value = expr.trim()
  if (/^#[0-9a-fA-F]{3,8}$/.test(value)) {
    return parseHex(value)
  }
  if (value === 'transparent') {
    return { r: 0, g: 0, b: 0, a: 0 }
  }
  if (value.startsWith('var(')) {
    const [name, ...fallback] = splitTopLevel(callArguments(value, 3), ',')
    const key = name.trim()
    if (seen.includes(key)) {
      throw new Error(`${key} refers to itself`)
    }
    const declared = tokens.get(key)
    if (declared !== undefined) {
      return resolveColour(declared, tokens, [...seen, key])
    }
    if (fallback.length > 0) {
      return resolveColour(fallback.join(','), tokens, seen)
    }
    throw new Error(`${key} is not a declared token`)
  }
  if (value.startsWith('color-mix(')) {
    const [space, first, second] = splitTopLevel(callArguments(value, 9), ',')
    if (space.trim() !== 'in srgb') {
      throw new Error(`only srgb mixes are measured: ${value}`)
    }
    const a = mixOperand(first)
    const b = mixOperand(second)
    const p1 = a.percent ?? (b.percent === null ? 50 : 100 - b.percent)
    const p2 = b.percent ?? 100 - p1
    return colorMix(
      resolveColour(a.colour, tokens, seen),
      p1,
      resolveColour(b.colour, tokens, seen),
      p2,
    )
  }
  throw new Error(`cannot resolve colour expression ${value}`)
}

/** `fg` painted over the opaque `bg` (source-over). */
export function composite(fg: Rgba, bg: Rgba): Rgba {
  const mixChannel = (k: 'r' | 'g' | 'b'): number => fg[k] * fg.a + bg[k] * (1 - fg.a)
  return { r: mixChannel('r'), g: mixChannel('g'), b: mixChannel('b'), a: 1 }
}

/** WCAG 2 relative luminance of an opaque colour. */
export function luminance(c: Rgba): number {
  const lin = (v: number): number => {
    const s = v / 255
    return s <= 0.04045 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b)
}

/** WCAG 2 contrast ratio of `fg` (composited over `bg` if translucent) against opaque `bg`. */
export function contrastRatio(fg: Rgba, bg: Rgba): number {
  const front = fg.a < 1 ? composite(fg, bg) : fg
  const [hi, lo] = [luminance(front), luminance(bg)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

/** An opaque colour as `#rrggbb`, for readable failure messages. */
export function toHex(c: Rgba): string {
  return `#${[c.r, c.g, c.b].map((v) => Math.round(v).toString(16).padStart(2, '0')).join('')}`
}
