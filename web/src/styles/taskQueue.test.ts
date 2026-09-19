import { describe, expect, it } from 'vitest'

import { TASK_STATE_STYLE } from '../components/tasks/taskState'
import { declarations, readCss, ruleBody } from '../test/css'
import { TASK_STATES } from '../services/tasks'

/**
 * The work queue is read by scanning it, and what makes it scannable is colour:
 * a badge in the state's hue and a stripe of the same hue down the left of the
 * row. jsdom loads no stylesheet, so a component test can only prove the row
 * carries `data-state` and the badge its modifier class — whether either one
 * actually paints anything is a fact about the shipped CSS, which is what these
 * guards read.
 *
 * They pin the shape rather than the hex values: every state has a token, every
 * token is used by both the badge and the stripe, and no state is left out. A
 * sixth state added to `TASK_STATES` fails here until it has a colour.
 */

const tokens = readCss('src/styles/tokens.css')
const app = readCss('src/styles/app.css')

/** The `:root` block that declares the task hues. */
const root = declarations(ruleBody(tokens, /:root\s*(?=\{)/, /--kk-task-question-bg/) ?? '')

describe('task state hues', () => {
  it('gives every state its own token, all of them distinct', () => {
    const hues = TASK_STATES.map((state) => {
      const value = root.get(`--kk-task-${state}-bg`)
      expect(value, `${state} has no hue token`).toBeDefined()
      return value
    })
    expect(new Set(hues).size, 'two states share a hue').toBe(TASK_STATES.length)
  })

  it('paints each badge from its state token', () => {
    for (const state of TASK_STATES) {
      const body = ruleBody(
        tokens,
        new RegExp(`\\.${TASK_STATE_STYLE[state].className}\\s*(?=\\{)`),
      )
      expect(body, `no rule for ${state}`).toBeDefined()
      const decls = declarations(body ?? '')
      expect(decls.get('background-color')).toBe(`var(--kk-task-${state}-bg) !important`)
      // Every hue states its own foreground: amber needs the page's near-black,
      // the other four white, and a badge that inherits is a badge that can end
      // up unreadable the day the surrounding text colour changes.
      expect(decls.get('color'), `${state} declares no text colour`).toBeDefined()
    }
  })

  it('draws the row stripe from the same token as the badge', () => {
    const row = declarations(ruleBody(app, /\.kk-task-row\s*(?=\{)/) ?? '')
    // The stripe is the row's leading border, fed by a per-state custom property
    // so the width is declared once and only the colour varies.
    expect(row.get('border-inline-start')).toContain('var(--kk-task-stripe')

    for (const state of TASK_STATES) {
      const body = ruleBody(app, new RegExp(`\\.kk-task-row\\[data-state='${state}'\\]\\s*(?=\\{)`))
      expect(body, `no stripe rule for ${state}`).toBeDefined()
      expect(declarations(body ?? '').get('--kk-task-stripe')).toBe(`var(--kk-task-${state}-bg)`)
    }
  })

  it('reinforces every hue with a glyph, for a reader who sees no colour', () => {
    const icons = TASK_STATES.map((state) => TASK_STATE_STYLE[state].icon)
    expect(new Set(icons).size).toBe(TASK_STATES.length)
  })
})
