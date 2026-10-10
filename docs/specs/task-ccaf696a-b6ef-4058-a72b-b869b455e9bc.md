# Stop telling the uploader that duplicates already in an album are in none

Found in the v0.30 prerelease walkthrough (staging 88963a6). This is pre-existing and not a regression: the text dates from v0.12.0 (b1a3630).

After a batch on `/upload` finishes, the album/label assignment step always says "Tyhle fotky zatím nejsou v žádném albu ani pod štítkem. Vyberte je a přidáme je tam." (`upload.*.noAlbum` / `noAlbumWithVideo`). It says so even when the batch was all duplicates and those photos already sit in an album.

## Reproduction
- On box staging, sign in as admin and open `/upload`.
- Pick `pr030_8.jpg` + `pr030_9.jpg`, which are already in the library and in the album "Panorama" because they came in through an upload link.
- The result reads "Všechny 2 soubory už v knihovně byly." and then "Tyhle fotky zatím nejsou v žádném albu ani pod štítkem…". `GET /photos/{uid}` shows `albums: ["Panorama"]`.

## Requirements
- The "not in any album or label yet" sentence appears only when it is true for the photos it talks about, i.e. none of the batch's resolved photos (new + duplicates) belong to an album or label. Otherwise use a neutral prompt (e.g. "Chcete je přidat do alba nebo pod štítek?"). Optionally name where the duplicates already are.
- Use membership data the upload already has, or one cheap request. No per-photo request storm for a large batch.
- Vitest for both branches (cs + en), and update `docs/FRONTEND.md` (UploadPage).
