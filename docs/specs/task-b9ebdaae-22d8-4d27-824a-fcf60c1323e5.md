# Reflow the Users and Audit admin tables into stacked cards on mobile

The Users and Audit admin tables have many columns; on a phone they only
horizontal-scroll inside their `.table-responsive` wrapper, so the important
per-row actions and later columns are off-screen. Reflow them to stacked cards on
small screens. Also fix an audit payload that widens the table.

## Current behavior

- `web/src/pages/UsersPage.tsx` (~line 757): an 8-column roster; each row's Edit /
  Change-password / Enable-disable action cluster (~line 571) sits far right.
- `web/src/pages/AuditPage.tsx` (~line 322): a 6-column table.
- Both are `<Table responsive>` (`.table-responsive`, `overflow-x:auto`) — so there is
  no page-level overflow, but a many-column table is awkward on a phone and the row
  actions require sideways scrolling.
- `AuditPage.tsx` (~line 450): an expanded audit row renders raw JSON in a
  non-wrapping `<pre>{JSON.stringify(details,null,2)}</pre>` inside `<td colSpan={6}>`,
  which stretches the responsive container's scroll width.

## Requirements

- Below a small breakpoint (phones), render each Users row and each Audit row as a
  stacked "label: value" card (one record = one card) instead of a wide scrolling
  table. Keep the full table for tablet/desktop.
- On the Users cards, the row actions (Edit / Change-password / Enable-disable) must be
  a full-width, ≥44px button row on the card — reachable without any horizontal scroll.
- Preserve all existing behavior (sorting, filters, actions, RBAC) — this is a
  presentation reflow only.
- Fix the audit JSON payload: give the expanded `<pre>` its own `overflow-x:auto`
  container (or `white-space: pre-wrap; word-break: break-word`) so it scrolls/wraps on
  its own without dragging the summary columns sideways.
- If you build a reusable "responsive table → cards" helper, keep it typed (no `any`)
  and place it so other admin tables can adopt it later.

## Testing

- Add/update Vitest tests asserting the mobile card layout renders (record fields +
  action buttons) and the desktop table still renders.
- `make check` must pass. Update `docs/FRONTEND.md` if a shared component is added.