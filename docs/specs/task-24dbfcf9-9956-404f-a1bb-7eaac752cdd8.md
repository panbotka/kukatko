# The strongest feature has no entry in the menu, the least used one has a top-level slot

Neither the desktop navbar nor the mobile drawer contains an item for "Hledání" or
"Uložená hledání". `/search` — the only place with the query-language help and the search
mode selector — is reachable only through an unlabelled magnifier circle whose `title`
reveals the `/` and Ctrl+K shortcuts on hover, i.e. never on a phone. `/saved` sits one
level deeper still, inside a dropdown on `/search`.

Meanwhile "Žebříček" has its own top-level slot next to Library and Albums. On the live
instance it holds one player and 38 answers in total, and its only button ("Začněte třídit"
→ `/review`) sends viewers straight back to the library.

## Requirements
- Add **Hledání** as a top-level navigation item (desktop navbar and mobile drawer), with a
  magnifier icon and a text label. The unlabelled circle may stay as a shortcut.
- Move **Uložená hledání** into the "Procházet" group — they are smart albums and belong
  next to Favourites.
- Move **Žebříček** out of the top level into "Procházet".
- The mobile bottom bar has three slots (Knihovna · Alba · Štítky). Replace **Štítky** with
  **Hledat**; labels stay reachable from the drawer.
- Keep the desktop bar from overflowing: the inline `md`+ navbar already runs past the
  container for editor/maintainer roles below roughly 1600 px, so check the widest role
  before and after the change and do not make it worse.
- Update Czech and English translations for any new label.

## Where it is
`web/src/components/navItems.ts`, `web/src/components/Layout.tsx`,
`web/src/components/MobileTabBar.tsx`, `web/src/components/MobileNavDrawer.tsx`.

## Out of scope
No changes to the pages themselves, no data changes.