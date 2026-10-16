# Account page uses a narrow centred column, other pages the full container

UI/UX review finding #31 (2026-10-11). `/account` (Můj účet) renders its cards in a narrow centred column (~640px) while every other page, including `/settings` and `/help` which are also text-heavy, uses the full-width container. The inconsistency is noticeable when switching between them.

## Requirements
- Decide one convention for text/form pages and apply it: a shared "narrow page" layout (max-width around 720–800px, centred) used by Můj účet, Nastavení instance, Nápověda, Žebříček and the empty-state tool pages — or the full container everywhere. Preferred: the shared narrow layout for reading/form pages, because long Markdown lines and forms read better at ~70 characters per line.
- Grid/list pages (library, albums, people, tasks, admin tables) keep the full container.
- Implement as one layout component/class, not per-page magic numbers; document it in docs/FRONTEND.md.
- Tests: the pages that use the narrow layout render its wrapper.

## Scope and verification
- UI only.
- Reference screenshots: `<path>/kukatko/uiux-review-2026-10-11/shots/d-account.png`, `d-settings.png`, `d-help.png`.
- Check at 1440px and 1024px.
- Finish per CLAUDE.md Definition of Done (docs/FRONTEND.md, `make check-box`, commit, push).