import { type MouseEvent, useId } from 'react'
import Button, { type ButtonProps } from 'react-bootstrap/Button'

/** Props for {@link ReasonedButton}. */
export interface ReasonedButtonProps extends Omit<ButtonProps, 'disabled'> {
  /**
   * Why the button cannot be pressed right now, as one finished sentence — or
   * `undefined`/`null`/`''` when it can. This is the **only** way to switch the
   * button off: there is deliberately no `disabled` prop, so a dead control in
   * this app can never exist without a reason the reader can find.
   *
   * The sentence is the caller's own translated copy, because a good reason is
   * specific ("Nejdřív vyberte fotky") rather than generic ("nelze").
   */
  disabledReason?: string | null
  /**
   * The id of an element that **already shows** {@link ReasonedButtonProps.disabledReason}
   * on screen — a hint line under a cluster of buttons that share one reason.
   * Given one, the button points `aria-describedby` there instead of rendering
   * its own hidden copy, so a screen reader reads the sentence once rather than
   * twice and a phone (where no `title` ever appears) can read it at all.
   */
  reasonId?: string
}

/**
 * A button that, when it is off, always says why — in a `title` for the mouse and
 * in an `aria-describedby` note for everyone else.
 *
 * **Why not the native `disabled` attribute.** A `<button disabled>` is removed
 * from the tab order, so a keyboard or screen-reader user can never reach it to
 * hear the explanation; and Bootstrap puts `pointer-events: none` on `.btn:disabled`,
 * which stops the browser from ever showing the `title` tooltip on hover. A
 * `title` on a natively disabled Bootstrap button is therefore invisible to
 * *both* input methods — decoration, not information. This button instead marks
 * itself `aria-disabled`, which keeps it focusable and hoverable, swallows the
 * click in JS, and takes the greyed-out look from `.kk-btn-inert` (the same
 * opacity Bootstrap uses, minus the `pointer-events` kill).
 *
 * The rule this implements, and the one case it deliberately does not cover:
 * a control the reader's **role** forbids is not rendered at all (see
 * `docs/UX_AUDIT.md` — "Why a control is off"). So a greyed-out control in
 * Kukátko always means "not right now" — never "not you" — and the two are
 * told apart without pressing anything.
 */
export function ReasonedButton({
  disabledReason,
  reasonId,
  title,
  onClick,
  children,
  className,
  ...rest
}: ReasonedButtonProps) {
  const reactId = useId()
  const reason = disabledReason ?? ''
  const inert = reason !== ''
  const ownNote = reasonId === undefined
  const noteId = reasonId ?? `${reactId}-reason`

  const classes: string[] = []
  if (inert) {
    classes.push('kk-btn-inert')
  }
  if (className !== undefined && className !== '') {
    classes.push(className)
  }

  return (
    <>
      <Button
        {...rest}
        className={classes.length > 0 ? classes.join(' ') : undefined}
        // Not `disabled`: see the note above. `aria-disabled` says the same thing
        // to assistive technology while leaving the control reachable.
        aria-disabled={inert || undefined}
        title={inert ? reason : title}
        aria-describedby={inert ? noteId : undefined}
        onClick={(event: MouseEvent<HTMLButtonElement>) => {
          if (inert) {
            // Swallow it completely — a `type="submit"` inside a form would
            // otherwise still submit it.
            event.preventDefault()
            event.stopPropagation()
            return
          }
          onClick?.(event)
        }}
      >
        {children}
      </Button>
      {inert && ownNote && (
        // `visually-hidden` is absolutely positioned, so this sibling adds no
        // box to the toolbar it sits in — it only gives the button something to
        // point `aria-describedby` at.
        <span id={noteId} className="visually-hidden">
          {reason}
        </span>
      )}
    </>
  )
}
