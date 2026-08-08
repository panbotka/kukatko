# The flags are called "Oko", "Palec nahoru", "Palec dolů"

Next to the star rating in the photo viewer header there are three icon buttons. Their
accessible names — which are also the tooltips and what a screen reader announces — are:

```
button "Oko"
button "Palec nahoru"
button "Palec dolů"
```

That describes the shape of the icon, not the action. In the library filter the same
property is called "Označení" with the values "Vše / Vybrané / Zamítnuté / Označené okem" —
so one value is literally named after the icon that draws it.

Nobody learns from "Palec nahoru" that they are marking a photo as picked for further work.
Users will either never touch these three buttons, or flag something other than what they
meant.

## Requirements
- Rename the three flags to what they mean — for example "Vybrat" / "Zamítnout" /
  "Prohlédnout později", matching the intended `pick` / `reject` / `eye` semantics. Keep the
  wording short enough for a tooltip and an `aria-label`.
- Use the same names for the corresponding values of the "Označení" filter in the library, so
  the filter and the viewer agree.
- Add a one-sentence tooltip to each icon explaining what the flag is for.
- Update both Czech and English translations. Do not rename the underlying API values or
  query-language keys — this is a wording change in the UI only.

## Where it is
`web/src/components/library/FlagControl.tsx`,
`web/src/components/library/FilterBar.tsx` (the "Označení" values), `web/src/i18n`.

## Out of scope
No API changes, no data changes.