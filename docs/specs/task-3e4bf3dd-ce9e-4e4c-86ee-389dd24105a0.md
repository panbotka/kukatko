# One notification settings card with push and e-mail as channels

The owner wants a single place where each user chooses which notifications they get and how: push and e-mail are channels of the same notification kind. The backend already stores preferences per kind and channel: `GET /api/v1/notifications/preferences` returns `{preferences:[{kind, channel, enabled, is_default}]}` with channels `push` and `email`, and `PUT` accepts `{preferences:[{kind, channel, enabled}]}` (replace semantics; left-out pairs return to default). If the API you find does not have `channel` yet, stop and report that in the task output instead of inventing it.

Today the card is `web/src/components/account/NotificationsCard.tsx`, mounted on `/account` (Můj účet). It hides everything except a "push is off" alert when the server has push disabled (early return around L392; the effect around L242 skips `loadPrefs` when `isPushEnabled()` is false). Kinds are listed in `KINDS` with `adminOnly` (`registration_pending` is admin/maintainer only via `isAdmin(role)`). Whether mail is on comes from `useMailEnabled()` (`web/src/hooks/useMailEnabled.ts`, backed by `GET /settings/public` → `mail_enabled`).

## Requirements
- The card becomes "Notifikace" with two parts:
  1. **Devices (push)**: the existing per-browser push enable/disable and the device list, unchanged, shown only when push is on for the instance (otherwise the existing "push is off" note, but it no longer hides part 2).
  2. **What to notify about**: one row per kind the user can receive (role gating as today), with one toggle per channel the kind supports: a "Push" column and an "E-mail" column. A kind that lacks a channel shows an empty cell (e.g. "Když mě někdo označí na fotce" has push only; "Když na schválení čeká nová registrace" has push and e-mail).
- Channel availability:
  - Push toggles are enabled only when push is on for the instance; otherwise they are disabled with a tooltip "Push notifikace nejsou na této instanci zapnuté".
  - E-mail toggles are enabled only when mail is on for the instance (`mail_enabled`) and the account has a real e-mail address (not a `.invalid` placeholder, not empty); otherwise disabled with an explanatory tooltip ("E-maily nejsou na této instanci zapnuté" / "Doplň si e-mail v účtu").
- The table/grid works on a phone (390px): rows stack as label + two compact switches with column headers repeated or as labelled switches ("Push", "E-mail"); no horizontal scroll.
- Saving: toggling saves immediately (optimistic, as today) and sends the full set of non-default pairs plus the changed one, each with its channel; on error revert the toggle and show the existing save-error text.
- Show a short hint under the e-mail column header for admins: "E-mail o nové registraci je ve výchozím stavu vypnutý." (and English).
- Czech and English strings under `account.notifications.*`; reuse the existing kind labels.

## Tests
- `NotificationsCard.test.tsx`: renders the kind × channel grid from a mocked preferences response; push off still shows the e-mail toggles; mail off disables them with the tooltip; a viewer does not see the admin-only kind; toggling e-mail sends `{kind, channel:'email', enabled}`; error reverts. Update `AccountPage.test.tsx` and service tests if needed.

## Verification and done
- Check in a browser at 1440px and 390px (box staging or a throwaway serve) as an admin and as a viewer.
- `docs/FRONTEND.md` (NotificationsCard), README if it describes notification settings.
- Per CLAUDE.md Definition of Done: `make check-box`, commit, push. Other autonomous tasks may edit the same tree (the Account page, locale files): stage only your own hunks and verify the commit in a worktree.