# The library should open onto photographs, not onto its own controls

The library page spends most of the first screen on chrome. Measured on the live instance at
1280 px wide: the first row of photographs began roughly 490 px down the page, below the page
heading, the "Co je nového" summary, the search row, and a row of four always-visible filter
pickers (period, album, label, person). The library is the app's front door and it currently
opens onto its own settings.

## What this task is deciding

Those four pickers are always visible on desktop **on purpose** — the filter panel behind the
"Filtry" control deliberately holds only the advanced filters, and this is stated in the filter
bar's own comments. This task revisits that decision, so treat it as a deliberate change rather
than as fixing an oversight, and leave the reasoning in the code where the next reader will
find it. If the pickers move, the compactness must not be bought with a filter the reader
cannot see.

## Requirements

- Target outcome: at 1280 x 800 the first row of photographs is visible without scrolling.
- Any filter that is currently set stays visible on the page without opening anything — as a
  chip that names it and can be cleared in one click. Only *setting a new* filter may cost an
  extra click.
- Condense the "Co je nového" summary to a single line that can be expanded for the detail. It
  stays dismissible exactly as it is today.
- Reconsider the page heading: the navigation bar already says which section is open, so the
  large "Knihovna" title can go or become much smaller.
- Filter state keeps living in URL query parameters so the back button and a shared link keep
  working.
- The phone layout must not regress; verify at about 390 x 844. On a phone everything already
  folds into the filter panel, so that path should be left alone.

## Edge cases

- Several filters set at once — the chip row must not itself grow into a new tall block.
- A filter set from a URL the reader opened directly: it must show as a chip, not sit invisible
  behind a collapsed control.
- A reader who has dismissed the "Co je nového" summary sees the photographs even higher; make
  sure nothing depends on that block being present.