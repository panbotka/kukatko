# Photo Comments — Frontend

Comment threads in the photo detail so users can discuss photos. The goal is a light social-network feel inside the family archive.

## Requirements
- The backend exposes photo comment endpoints (list/create/edit/delete under the photo API, plus a comment count on the photo detail payload — see docs/API.md). If these endpoints do not exist in the codebase, stop and report instead of building against a guess.
- Add a "Comments" section to the photo detail info panel: chronological list with author name, relative time ("před 2 h"), and body; an input with a submit button at the bottom. Enter submits, Shift+Enter inserts a newline.
- Users can edit and delete their own comments (inline affordances, confirmation on delete); admins see delete on all comments.
- A comment-count badge on the info toggle (or equivalent place in the detail chrome) makes discussions discoverable while browsing; the count updates after posting without a full reload.
- Read-only viewers CAN comment — this is a deliberate product decision; do not hide the input for them.
- Empty state invites the first comment (Czech default, e.g. "Napiš, co o téhle fotce víš…").
- Author identity is visualized with an initial-circle avatar colored deterministically from the name, using existing theme tokens — no external assets.
- Mobile: usable inside the existing bottom-sheet info layout; the input must not be covered by the on-screen keyboard.
- i18n cs/en including pluralized comment counts (1 komentář / 2 komentáře / 5 komentářů).
- Component tests: render thread, post, edit own, delete with confirm, viewer can post, badge count updates, empty state.
- Update docs/FRONTEND.md.