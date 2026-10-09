# Stop telling the curator an expired upload link still works

Found in the v0.29 prerelease walkthrough (box staging, commit c2f7eb7).

On `/upload-links`, a link created before migration 0091 (no readable code) shows `uploadLinks.codeUnknown`: "Odkaz vznikl dřív, než si Kukátko adresy odkazů pamatovalo, proto ji nelze zobrazit. **Odkaz dál funguje** — zadejte jeho původní kód…". `web/src/pages/UploadLinksPage.tsx:362` renders it for every state except `revoked`, so an **expired** link also claims it still works — it does not, uploads are refused until it is extended.

## Reproduce
Staging `/upload-links`, cards "Format matrix" and "Expiry probe" (both `expired`). Screenshot: `/tmp/claude-1000/-home-pi-projects-kukatko/e1859691-8352-48a1-a30b-9400f72ae1fe/scratchpad/22-links.png`.

## Requirements
- For an `active` link keep the current text (cs + en).
- For an `expired` link, the text must not claim it works: say the address cannot be shown, that the link has expired (extending it with "Prodloužit" revives the same address), and that the original code can still be restored. Restore-code and new-code actions stay available as now.
- Both locales; update `UploadLinksPage.test.tsx` with an expired-link case asserting the expired wording and the absence of the "still works" claim.
