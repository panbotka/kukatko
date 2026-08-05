import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'

/**
 * Reads a stylesheet by its `web/`-relative path. Vitest runs with the `web/`
 * package as its cwd, but resolve from the repo root too so the guard holds
 * whoever launches it — `import.meta.url` is unusable here, as the jsdom
 * environment reports a non-`file:` document URL.
 */
export function readCss(relPath: string): string {
  const candidate = [relPath, `web/${relPath}`]
    .map((rel) => resolve(process.cwd(), rel))
    .find((path) => existsSync(path))
  if (candidate === undefined) {
    throw new Error(`${relPath} not found from cwd ${process.cwd()}`)
  }
  return readFileSync(candidate, 'utf8')
}

/**
 * Returns the body of the block that opens at the first `{` at or after `from`,
 * brace-matched so nested rules inside an at-rule come back whole.
 */
export function blockBodyAt(css: string, from: number): string {
  const open = css.indexOf('{', from)
  if (open === -1) {
    throw new Error('no block found')
  }
  let depth = 0
  for (let i = open; i < css.length; i += 1) {
    if (css[i] === '{') {
      depth += 1
    } else if (css[i] === '}') {
      depth -= 1
      if (depth === 0) {
        return css.slice(open + 1, i)
      }
    }
  }
  throw new Error('unbalanced braces in stylesheet')
}

/**
 * Returns the body of the first rule whose selector/prelude matches `prelude` and
 * whose body also satisfies `contains` (used to pick one of several `@media`
 * blocks apart). Undefined when no such rule exists.
 */
export function ruleBody(css: string, prelude: RegExp, contains?: RegExp): string | undefined {
  const scan = new RegExp(prelude.source, prelude.flags.includes('g') ? prelude.flags : 'g')
  let match = scan.exec(css)
  while (match !== null) {
    const body = blockBodyAt(css, match.index + match[0].length)
    if (contains === undefined || contains.test(body)) {
      return body
    }
    match = scan.exec(css)
  }
  return undefined
}

/**
 * Hands one real rule out of a stylesheet to jsdom, which loads none of its own,
 * and answers with the function that takes it away again. It lets a component test
 * assert what an element *computes to* — `getComputedStyle(el).whiteSpace` — rather
 * than which class name it happens to carry, while still failing if the shipped
 * stylesheet stops declaring the rule: the body installed here is read out of
 * `app.css`, never written by the test.
 */
export function installRule(relPath: string, selector: string): () => void {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const body = ruleBody(readCss(relPath), new RegExp(`${escaped}\\s*(?=\\{)`))
  if (body === undefined) {
    throw new Error(`${relPath} declares no ${selector} rule`)
  }
  const style = document.createElement('style')
  style.textContent = `${selector} {${body}}`
  document.head.append(style)
  return () => {
    style.remove()
  }
}

/** Parses a rule body's declarations into a name → value map. */
export function declarations(body: string): Map<string, string> {
  const out = new Map<string, string>()
  // Strip comments and any nested block so only this rule's own declarations remain.
  const own = body.replace(/\/\*[\s\S]*?\*\//g, '').replace(/[^{}]*\{[^{}]*\}/g, '')
  for (const line of own.split(';')) {
    const [name, ...rest] = line.split(':')
    if (rest.length > 0) {
      out.set(name.trim(), rest.join(':').trim())
    }
  }
  return out
}
