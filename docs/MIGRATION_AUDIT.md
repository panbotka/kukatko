# Migrační audit — PhotoPrism → Kukátko

- **Datum auditu:** 2026-07-17
- **Auditovaný commit:** `6e2600e` (větev `main`)
- **Rozsah:** kompletní mapování pole-po-poli, kterým import z běžící instance
  PhotoPrism plní katalog Kukátka — fotky a jejich metadata, alba, štítky,
  subjekty/lidé, markery/obličeje, místa, hodnocení a oblíbené.
- **Účel:** dát jistotu, že migrace z PhotoPrismu — jediná cesta, kterou historie
  knihovny vstupuje do nové databáze — nic tiše neztrácí. Tento dokument **pouze
  popisuje**, nemění chování importu; doporučené opravy jsou zapsané, ne
  implementované.

Auditovaný kód: `internal/photoprism/` (zdrojové struktury `models.go` a endpointy
`photoprism.go` + `download.go`), `internal/ppimport/` (mapery), `internal/photos/`
(cílové sloupce a INSERT/UPDATE cesty), migrace `internal/database/migrations/`.
Srovnávacím importérem je `internal/psimport/` (přímá migrace z photo-sorteru), na
který se na několika místech odkazuje asymetrie.

## Legenda verdiktů

- **MAPPED** — hodnota se přenáší do sloupce. U polí s precedencí/fallbackem je
  pravidlo v poznámce.
- **WAIVED** — vědomě nepřenášeno, vždy s důvodem.
- **GAP** — skutečná ztráta, o které nikdo nerozhodl. Poznámka říká, co se ztrácí,
  kdy to zabolí a doporučenou opravu.

## Souhrn

Auditováno **89 zdrojových polí** definovaných v `internal/photoprism/models.go`
(včetně 6 kontejnerových/relačních polí — `Photo.Files`, `PhotoDetail.Albums`,
`PhotoDetail.Labels`, `File.Markers` a odkazů na `Details`):

| Verdikt | Počet |
| --- | --- |
| **MAPPED** | 58 |
| **WAIVED** | 27 |
| **GAP** | 4 |
| **Celkem** | 89 |

**Čtyři skutečné mezery (GAP):**

1. **`Subject.Private` → `subjects.private`** — člověk, kterého PhotoPrism označil jako
   soukromého, se po importu stane veřejným (viz „Subjekty“). Nejzávažnější: má
   dopad na soukromí.
2. **`Subject.Favorite` → `subjects.favorite`** — příznak oblíbeného člověka se
   ztrácí.
3. **`Subject.Type` → `subjects.type`** — každý subjekt založený importem je
   natvrdo `person`; zvíře (`pet`) nebo jiná entita (`other`) přijde o svůj typ.
4. **`Album.Category` → (bez sloupce)** — kategorie alba (skupinování alb podle
   tématu) nemá v Kukátku kam přijít a zahazuje se.

Mezery 1–3 mají společnou příčinu i společnou opravu, viz sekce „Subjekty“ a
ověřená indicie č. 4 níže.
Kromě těchto čtyř existuje několik **vědomě vynechaných** položek s netriviálním
dopadem (nepojmenované/neplatné obličejové markery, alba typu `month` při plném
běhu) — jsou popsané v „Rizika a vědomé kompromisy“.

## Jak import plní řádek fotky (kontext k tabulkám)

PhotoPrism dělí data fotky mezi **dva endpointy**, a audit proto musí sledovat obě
poloviny:

- **Výpis** `GET /photos?merged=true` (`ListPhotos`) vrací plochou vyhledávací
  strukturu `Photo`. Nese kmenová metadata (titulek, popisek, čas, GPS, kamera),
  ale **žádný blok `Details`**, soubory s **vždy prázdným polem `Markers`** a bez
  per-file kodeku/barevného profilu.
- **Detail** `GET /photos/{uid}` (`GetPhoto`) vrací `PhotoDetail` = `Photo` + blok
  `Details` (IPTC/XMP kredity) + technická pole souboru + **obličejové markery** +
  relace **alba** a **štítky**. Vše, co je „detail-only“, se přenáší z tohoto
  jediného požadavku (`ppimport.importPhotoDetail` → `importMetadata` /
  `importMarkers` / `mapPhotoContext`).

Vkládací cesta: `buildPhoto` (`metadata.go`) sestaví řádek z výpisu a vlastního
EXIF staženého originálu, `videoFields.apply` (`video.go`) dolije video sloupce
z ffprobe, `photos.Store.Create` (`store.go`, `photoInsertColumns`) vloží řádek.
Detailová pole pak dopisuje `photos.Store.ApplyImportMetadata`
(`store_import.go`) s pravidlem „zdroj vlastní kredity, prázdno nikdy nemaže“.
Inkrementální přeběh mění metadata přes `metadataUpdate` → `UpdateMetadata`.

---

## Fotky — struktura `Photo` (výpis)

Cíl je tabulka `photos` (migrace `0003`, `0004`, `0024`, `0027`, `0028`, `0029`,
`0030`, `0033`). Precedence u kurátorských polí: **PhotoPrism vyhrává, když má
hodnotu; prázdno spadne na vlastní EXIF souboru** (`applyCameraMeta`,
`applyCaptureMeta`).

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Photo.UID` | `photoprism_uid` | **MAPPED** | Stabilní klíč importu a deduplikace (`GetByPhotoprismUID`). |
| `Photo.Type` | `media_type` | **MAPPED** | `mapMediaType`: `video`/`animated`→video, `live`→live, zbytek→image. Rozhoduje ale i skutečný stažený soubor (`selectMedia`): video bez streamu degraduje na image. |
| `Photo.Title` | `title` | **MAPPED** | Vklad přímo; inkrement `firstNonEmpty(pp.Title, existing.Title)` — smazaný titulek nahoře nepřepíše vyplněný. |
| `Photo.Caption` | `description` | **MAPPED** | Primární zdroj popisku; `caption() = firstNonEmpty(Caption, Description)`. |
| `Photo.Description` | `description` | **MAPPED** | Fallback v `caption()`. Na současném PhotoPrismu je pole mrtvé (`gorm:"-"`, vždy prázdné), ale modelované kvůli starší instanci; precedence je správně (živý `Caption` první) a popisek nemůže spadnout pod stůl. |
| `Photo.TakenAt` | `taken_at` (+ `taken_at_source='exif'`) | **MAPPED** | `applyCaptureMeta`; při nulovém čase fallback na EXIF souboru s jeho zdrojem. |
| `Photo.TakenAtLocal` | — | **WAIVED** | Kukátko drží jediný kanonický `taken_at` (timestamptz); lokální vykreslení je odvozené na výstupu, `TakenAt` už nese okamžik. |
| `Photo.UpdatedAt` | — (bez sloupce) | **WAIVED** | Řídí inkrementální high-watermark v `import_runs` (max `UpdatedAt` za běh), není sloupcem fotky; `photos.updated_at` je čas vlastní mutace řádku. |
| `Photo.CreatedAt` | — (bez sloupce) | **WAIVED** | `photos.created_at` = kdy byla fotka založena lokálně (DB `now()`). Čas indexace v PhotoPrismu není historií knihovny. |
| `Photo.Lat` | `lat` (+ `location_source`) | **MAPPED** | Přenos jen když `Lat != 0 || Lng != 0`; provenience `exif`. Hrana: přesně `(0,0)` „Null Island“ se bere jako bez polohy (univerzální konvence), viz Rizika. |
| `Photo.Lng` | `lng` | **MAPPED** | Viz `Lat`. |
| `Photo.Altitude` | `altitude` | **MAPPED** | Přenos jen když `Altitude != 0`, jinak EXIF. Hrana: nadmořská výška `0` = hladina moře spadne na EXIF/nil (viz Rizika, „nulová past“). |
| `Photo.Width` | `file_width` | **MAPPED** | `firstPositive(pp.Width, meta.Width)`. |
| `Photo.Height` | `file_height` | **MAPPED** | `firstPositive(pp.Height, meta.Height)`. |
| `Photo.OriginalName` | `original_name` | **MAPPED** | Přes `ImportMetadata` z detailu; zároveň řídí jméno v úložišti (`originalName()`). |
| `Photo.CameraMake` | `camera_make` | **MAPPED** | `firstNonEmpty(pp, meta)`. |
| `Photo.CameraModel` | `camera_model` | **MAPPED** | dtto. |
| `Photo.LensModel` | `lens_model` | **MAPPED** | dtto. |
| `Photo.Iso` | `iso` | **MAPPED** | `firstIntPtr`: jen kladné vyhrává, ISO `0` = neznámé → fallback na EXIF (správně, ISO 0 není reálná hodnota). |
| `Photo.FNumber` | `aperture` | **MAPPED** | `firstFloatPtr`: jen kladné; `f/0` neexistuje → fallback (správně). |
| `Photo.Exposure` | `exposure` | **MAPPED** | `firstNonEmpty`. |
| `Photo.FocalLength` | `focal_length` | **MAPPED** | `firstFloatPtr(float64(pp.FocalLength), …)`; `0` → fallback. |
| `Photo.CameraSerial` | `camera_serial` | **MAPPED** | Přes `ImportMetadata` z detailu (`detail.CameraSerial`). |
| `Photo.Scan` | `scan` | **MAPPED** | Přes `ImportMetadata` z detailu; pravidlo „true-wins“ (lze nastavit, ne zrušit). |
| `Photo.Favorite` | — | **WAIVED** | Oblíbené jsou v Kukátku **per-user** záměrně (`ppimport.go` ~ř. 18–20); import běžící jako job/CLI nemá komu příznak přiřadit. |
| `Photo.Private` | `private` | **MAPPED** | `buildPhoto` i `metadataUpdate`. |
| `Photo.Files` | → tabulka `File` | **MAPPED** | Kontejner souborů; viz sekce „Soubory“. |

**`PhotoDetail` — relace nad rámec `Photo`** (obálka detailového endpointu):

| Zdrojové pole | Cíl | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `PhotoDetail.Details` | → tabulka `Details` | **MAPPED** | IPTC/XMP kredity, viz níže. |
| `PhotoDetail.Albums` | `album_photos` + `albums` | **MAPPED** | U scoped běhu se z detailu mapuje **každé** album fotky (`mapPhotoContext`), ne jen to, které ji vybralo. |
| `PhotoDetail.Labels` | `photo_labels` + `labels` | **MAPPED** | dtto pro štítky; viz „Štítky“. |

## Fotky — blok `Details` (detail, IPTC/XMP kredity)

Zapisuje `ApplyImportMetadata`: neprázdná hodnota zdroje vyhrává nad aktuální
(stejná precedence jako kamera), prázdno nikdy nemaže. `notes` je výjimka —
Kukátkovo vlastní pole, jen **gap-fill** (nikdy nepřepíše uživatelovu poznámku).

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Details.Subject` | `subject` | **MAPPED** | Trimováno. |
| `Details.Keywords` | `keywords` | **MAPPED** | `exif.NormalizeKeywords` — čte jako nativně extrahované. |
| `Details.Notes` | `notes` | **MAPPED** | Jen gap-fill (prázdné `notes` → vyplní; jinak ponechá). |
| `Details.Artist` | `artist` | **MAPPED** | |
| `Details.Copyright` | `copyright` | **MAPPED** | |
| `Details.License` | `license` | **MAPPED** | |
| `Details.Software` | `software` | **MAPPED** | |

## Soubory — struktura `File`

Kukátko nemá katalog PhotoPrismích souborů 1:1; z primárního souboru bere obsah
(hash na stažení + reference), z detailu technická pole. Ostatní jsou buď interní
pro PhotoPrism, nebo redundantní s poli fotky.

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `File.Hash` | `photoprism_file_hash` | **MAPPED** | SHA1 primárního souboru; zároveň klíč stažení (`/dl/<Hash>`). |
| `File.Mime` | `file_mime` | **MAPPED** | `firstNonEmpty(primary.Mime, meta.Mime, stored.MIME)`. |
| `File.Name` | `original_name` (fallback) / jméno v úložišti | **MAPPED** | `originalName()` použije `Photo.OriginalName`, jinak `path.Base(File.Name)`; `companionName()` pojmenuje motion klip. |
| `File.Primary` | (výběr primárního souboru) | **MAPPED** | Chování `PrimaryFile()`; primární soubor je originál. |
| `File.Video` | (výběr média / `IsVideo`) | **MAPPED** | Rozliší still od motion klipu a spolurozhoduje `media_type`. |
| `File.Codec` | `image_codec` (jen stills) | **MAPPED** | `exif.CodecToken`; kodek videa (`avc1`/`hvc1`) se **záměrně nebere z PP** — `video_codec` vlastní ffprobe. |
| `File.ColorProfile` | `color_profile` | **MAPPED** | Detail-only. |
| `File.Projection` | `projection` | **MAPPED** | Detail-only (panorama). |
| `File.Markers` | → tabulka `Marker` | **MAPPED** | Jen z detailu (výpis má prázdné); viz „Markery“. |
| `File.UID` | — | **WAIVED** | Kukátkovy soubory se klíčují `photo_uid` + cesta; UID souboru z PP není potřeba. |
| `File.Root` | — | **WAIVED** | Interní tag úložného rootu PhotoPrismu. |
| `File.Width` | — | **WAIVED** | Redundantní — geometrie se bere z `Photo.Width` (viz výše). |
| `File.Height` | — | **WAIVED** | dtto. |
| `File.FileType` | — | **WAIVED** | Typ nese `file_mime` + `image_codec`; textový tag se neukládá. |

## Markery / obličeje — struktura `Marker`

Cíl je tabulka `markers` (migrace `0008`). `importMarkers` seje **pouze
pojmenované, platné obličejové** markery (`isNamedFaceMarker`: `Type=="face"` &&
`!Invalid` && `Name != ""`); z jména se najde/založí subjekt (`findOrCreateSubject`)
a marker se k němu přiřadí. Marker si **ponechá PhotoPrism UID** → import je
idempotentní a identita markerů je sdílená s `psimport` (jehož markery JSOU
PhotoPrismovy).

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Marker.UID` | `markers.uid` | **MAPPED** | Idempotence (`GetMarkerByUID`) a sdílená identita napříč importéry. |
| `Marker.Name` | `subjects.name` (nepřímo) | **MAPPED** | Seje subjekt jménem (`findOrCreateSubject`). Marker sám bez jména se nepřenese. |
| `Marker.X` / `Y` / `W` / `H` | `markers.x/y/w/h` | **MAPPED** | Normalizovaný bbox (0..1). |
| `Marker.Score` | `markers.score` | **MAPPED** | Pozn.: `score` je import-provenience, ne kvalita (0 = nezaznamenáno); nikdy podle něj neřadit obličeje. |
| `Marker.Review` | `markers.reviewed` | **MAPPED** | `Reviewed = !Review`. |
| `Marker.Type` | (filtr: jen `face`) | **WAIVED** | Přenáší se jen `face`; **label markery se zahazují** — Kukátkovy štítky pocházejí z relace `Labels`, ne z label markerů. |
| `Marker.Invalid` | `markers.invalid` (nikdy nenastaveno) | **WAIVED** | `isNamedFaceMarker` neplatné markery vyfiltruje; sloupec zůstane `false`. **Asymetrie:** `psimport` `Invalid` zachovává. Vědomé rozhodnutí (Kukátkovo `face_detect` regiony znovu objeví), ale má cenu — viz Rizika. |
| `Marker.FileUID` | — | **WAIVED** | Kukátkovy markery odkazují `photo_uid`, ne soubor. |
| `Marker.FileHash` | — | **WAIVED** | dtto. |
| `Marker.SubjUID` | — | **WAIVED** | Vazba na subjekt se **znovu odvozuje ze jména**, ne z PP subject UID; PP subjekty se vůbec nečtou (viz „Subjekty“). |
| `Marker.SubjSrc` | — | **WAIVED** | dtto. |

## Subjekty / lidé — struktura `Subject`

Cíl je tabulka `subjects` (migrace `0008`: `uid, slug, name, type, favorite,
private, notes, cover_photo_uid, …`; žádný sloupec `file_count`). **Klíčové
zjištění:** `photoprism.Subject` i klient `ListSubjects` jsou **mrtvý kód** vůči
importu — importní rozhraní `ppimport.PhotoPrismClient` `ListSubjects` vůbec
nedeklaruje (viz ověřená indicie č. 4 níže),
takže **žádné pole PP subjektu se nečte**. Subjekty vznikají výhradně ze jmen
markerů (`findOrCreateSubject` → `people.Subject{Name, Type: person}`).

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Subject.Name` | `subjects.name` | **WAIVED** | Redundantní: jméno dorazí přes `Marker.Name`, samotné pole `Subject.Name` se nečte — o hodnotu se nepřichází. |
| `Subject.UID` | — | **WAIVED** | PP subject UID se neukládá; subjekt se páruje slugem jména. |
| `Subject.Slug` | `subjects.slug` | **WAIVED** | Slug si Kukátko generuje z jména (`people.Slugify`). |
| `Subject.FileCount` | — (bez sloupce) | **WAIVED** | Kukátko počítá markery živě (`ListSubjects`/`SubjectCount`), necachuje. |
| `Subject.Type` | `subjects.type` | **GAP** | Každý importem založený subjekt je natvrdo `person`; PP `pet`/`other` přijde o typ. `psimport` typ mapuje (`mapSubjectType`). Bije při zvířatech: pes se v Kukátku ukáže jako člověk. **Oprava:** viz níže. |
| `Subject.Favorite` | `subjects.favorite` | **GAP** | Cílový sloupec existuje (default `false`) a `psimport` ho plní; ppimport ho nechá `false`. Oblíbený člověk ztrácí příznak. |
| `Subject.Private` | `subjects.private` | **GAP** | **Nejzávažnější.** Soukromá osoba z PP se stane veřejnou (sloupec je globální, ne per-user). `psimport` `Private` zachovává. |

**Společná oprava mezer u subjektů:** buď (a) přidat `ListSubjects` do
`ppimport.PhotoPrismClient` a při seji subjektu obohatit `type`/`favorite`/`private`
z PP subjektu, nebo (b) z markeru přečíst `SubjUID` a subjekt dohledat. Varianta
(a) je čistší a odstraní všechny tři GAPy naráz. Do vyřešení je bezpečnější
importovat lidi z `psimport` (photo-sorter), který tato pole nese.

## Alba — struktura `Album`

Cíl je tabulka `albums` (migrace `0011`; `0022` odstranilo `order_by`/`sort_order`).
`findOrCreateAlbum` páruje/zakládá album **podle titulku**. Plný běh mapuje typy
`album/folder/moment/state` (default `DefaultAlbumTypes`, `month` vynechán); scoped
běh mapuje jakýkoli typ alba fotky (`mapPhotoContext`, `requireAlbum` prochází
všechny typy).

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Album.Title` | `albums.title` | **MAPPED** | Klíč find-or-create; prázdný titulek = album se přeskočí. |
| `Album.Description` | `albums.description` | **MAPPED** | |
| `Album.Type` | `albums.type` | **MAPPED** | `mapAlbumType`; neznámé/prázdné → `album`. Pozn.: `month` alba (560 auto-generovaných) plný běh nemapuje — viz Rizika. |
| `Album.Private` | `albums.private` | **MAPPED** | |
| `Album.UID` | — | **WAIVED** | Slouží jen k výpisu členů; Kukátko generuje vlastní `uid`. |
| `Album.Slug` | `albums.slug` | **WAIVED** | Regeneruje se z titulku. |
| `Album.Favorite` | — (bez sloupce) | **WAIVED** | `albums` nemá `favorite` — koncept oblíbeného alba v Kukátku není. |
| `Album.CreatedAt` | — | **WAIVED** | `albums.created_at` je DB `now()`. |
| `Album.UpdatedAt` | — | **WAIVED** | dtto. |
| `Album.Category` | — (bez sloupce) | **GAP** | Kategorie alba (skupinování podle tématu) nemá v Kukátku kam přijít a zahazuje se. Bije uživatele, kteří alba do kategorií třídili. **Oprava:** přidat nullable `albums.category` a namapovat, nebo — je-li produkční knihovna nepoužívá — formálně WAIVED. |

**Členství v albu** (`album_photos`): PhotoPrismí pořadí ve výpisu se nepřenáší —
Kukátko alba řadí **chronologicky** (`0022` zahodilo `sort_order`). `AddPhoto` je
idempotentní. Členy, které ještě nejsou importované, přeskočí.

## Štítky — struktury `Label` a `PhotoLabel`

Cíl jsou tabulky `labels` (`uid, slug, name, priority`) a připojení `photo_labels`
(`source`, `uncertainty`), obě migrace `0011`. `findOrCreateLabel` páruje/zakládá
**podle jména**.

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Label.Name` | `labels.name` | **MAPPED** | Klíč find-or-create. |
| `Label.Priority` | `labels.priority` | **MAPPED** | |
| `Label.UID` | — | **WAIVED** | Vlastní `uid`. |
| `Label.Slug` | `labels.slug` | **WAIVED** | Slouží k dotazu na členy (`label:"<slug>"`); ukládaný slug se regeneruje. |
| `Label.Favorite` | — (bez sloupce) | **WAIVED** | `labels` nemá `favorite`. |
| `PhotoLabel.LabelSrc` | `photo_labels.source` | **MAPPED** | `mapLabelSource`: `manual`→manual, `image`→ai, ostatní (`batch`/`keyword`/`location`/`meta`…)→import. |
| `PhotoLabel.Uncertainty` | `photo_labels.uncertainty` | **MAPPED** | `clampUncertainty` na 0–100. |
| `PhotoLabel.Label` | `labels` | **MAPPED** | Vnořený štítek, viz výše. |

## Místa

PhotoPrism v importovaných strukturách **žádná místní pole nemá** (`Country`,
`PlaceLabel` apod. se do `photoprism.models` nečtou). Kukátkova tabulka
`photo_places` (migrace `0018`: `country, region, city, place_name, lat, lng,
geocoded_at`) je **cache reverzní geokódace** plněná jobem `places` z GPS fotky, ne
z PhotoPrismu. Souřadnice (`lat`/`lng`) se tedy migrují (viz „Fotky“), názvy míst se
**dopočítají znovu**. Pro tento audit: bez zdrojového pole, odvozeno v Kukátku
(**WAIVED**, žádná ztráta — vzniká z migrované polohy).

## Hodnocení a oblíbené

- **Hvězdičkové hodnocení:** PhotoPrism ho nemá, není co migrovat. Kukátkovo
  `user_ratings` (`rating` 0–5, `flag` `none/pick/reject/eye`; migrace `0016`,
  `0025`) je **per-user** a po importu startuje na nule.
- **Oblíbené fotky:** `Photo.Favorite` → **WAIVED** (per-user `user_favorites`, viz
  „Fotky“).
- **Oblíbená alba / štítky:** cílové sloupce neexistují → **WAIVED** (viz „Alba“,
  „Štítky“).
- **Oblíbení / soukromí lidé:** `Subject.Favorite`/`Private` → **GAP** (globální
  sloupce existují, import je neplní; viz „Subjekty“).

## Cílová strana — sloupce `photos`, které import neplní z PP

Aby byla pokrytost prokazatelná oběma směry: každý z 56 vkládaných sloupců
`photos` (`photoInsertColumns`) je buď namapovaný z pole PP (výše), nebo **odvozený Kukátkem** ze staženého
originálu, nebo **výhradně Kukátkův** (import je nechává na defaultu). Odvozené a
vlastní sloupce, které tedy *nemají* zdrojové PP pole:

| Sloupec | Původ | Pozn. |
| --- | --- | --- |
| `uid`, `created_at`, `updated_at` | DB | Generováno při vložení. |
| `file_hash`, `file_path`, `file_name`, `file_size` | Úložiště | SHA256 a layout staženého originálu. |
| `file_orientation`, `exif` | EXIF souboru | PhotoPrism orientaci nevrací. |
| `taken_at_source`, `location_source` | Odvozeno | `exif`/`unknown`/`""` podle původu času a GPS. |
| `duration_ms`, `video_codec`, `audio_codec`, `has_audio`, `fps` | ffprobe | Video sloupce z `videoFields`, ne z PP. |
| `taken_at_estimated`, `taken_at_note` | Kukátko-only | PhotoPrism přibližné datum nezná; inkrement je nese beze změny. |
| `ai_note` | Kukátko-only | Externí AI průchod. |
| `archived_at`, `uploaded_by` | Kukátko-only | Import nenastavuje (job nemá uživatele). |
| `stack_uid`, `stack_primary` | Kukátko-only | Detekce stacků (`internal/stacks`). |
| `metadata_extracted_at` | Kukátko-only | Import nechá `nil` (mapuje ze zdroje, ne ze souboru) → naplánuje ho `metadata` backfill. |
| `photosorter_uid` | jiný import | Plní jen `psimport`. |

---

## Ověření konkrétních indicií ze zadání

### 1. Úplnost `metadataUpdate` vs. 19 přepisovaných sloupců — **potvrzeno správné (ne bug)**

`UpdateMetadata` (`store.go` ~ř. 268–274) přepisuje celý řádek — 19 sloupců:
`title, description, notes, ai_note, taken_at, taken_at_source, lat, lng, altitude,
private, subject, keywords, artist, copyright, license, scan, taken_at_estimated,
taken_at_note, location_source`. `metadataUpdate` (`metadata.go` ~ř. 129) **nese
všechny**: mapovaná pole z PP (`Title`, `Description`, `Private`, podmíněně
`TakenAt`/GPS/`Altitude`), zbytek **prokazatelně přenáší z `existing`** (`Notes`,
`AiNote`, `Subject`, `Keywords`, `Artist`, `Copyright`, `License`, `Scan`,
`TakenAtEstimated`, `TakenAtNote`, `LocationSource`). Žádný editovatelný sloupec se
inkrementálním během tiše nevynuluje. Kredity mění samostatně `ApplyImportMetadata`
z detailu; `metadataUpdate` je jen „přenese beze změny“, aby je hromadný přepis
nesmazal. **Indicie o tichém mazání je vyvrácená.**

### 2. Symetrie `metadataUnchanged` — **potvrzeno symetrické**

`metadataUnchanged` (`captionsUnchanged` + `creditsUnchanged` + `placementUnchanged`)
porovnává přesně těchže 19 polí, která `UpdateMetadata` zapisuje. Žádné pole
z porovnání nevypadává, takže skutečný přepis se nemůže tvářit jako no-op.

### 3. Nulová past (`firstPositive`/`firstFloatPtr`, GPS) — **z větší části neškodná, jedna reálná hrana**

- `Iso`, `FNumber`, `FocalLength`: `firstIntPtr`/`firstFloatPtr` berou jen **striktně
  kladné**. Nula u těchto veličin je sentinel „neznámo“ (ISO 0, `f/0`, ohnisko 0
  reálně neexistují), takže fallback na EXIF je **správný**, ne ztráta.
- `Altitude`: `if pp.Altitude != 0`. Nadmořská výška **0 = hladina moře je legitimní**
  a spadne na EXIF/nil. Reálná (byť okrajová) ztráta věrnosti pro fotky přesně na
  hladině moře, kde PP mělo 0 a EXIF ne. Nízký dopad.
- GPS `if pp.Lat != 0 || pp.Lng != 0`: rovník či nultý poledník samotné projdou (OR),
  problém je **jen přesně `(0,0)` „Null Island“** — bere se jako bez polohy. To je
  univerzální konvence pro „negeotagováno“ a reálná fotka tam prakticky nevznikne;
  přijatelné.

### 4. `photoprism.Subject` a `ListSubjects` jsou mrtvý kód — **potvrzeno, včetně asymetrie s `psimport`**

`ListSubjects` a `photoprism.Subject` jsou definované na `photoprism.Client` a
implementované, ale **mimo vlastní testy je nikdo nevolá** (grep: jediné výskyty
`photoprism.Subject`/`ListSubjects` v importní vrstvě jsou v `photoprism_test.go`).
Importní rozhraní `ppimport.PhotoPrismClient` metodu vůbec neuvádí. Subjekty tedy
vznikají jen nepřímo ze jmen markerů, a PP subject `Favorite`/`Private`/`Type`
nedorazí do `subjects`, přestože sloupce existují a `psimport` je plní
(`people.Subject{Favorite: ps.Favorite, Private: ps.Private, Type: mapSubjectType(...)}`).
`Slug`/`FileCount` cíl nemají (slug se generuje, počet se počítá živě). Tato
asymetrie je zdrojem GAPů 1–3 ze souhrnu.

### 5. `Album.Category` — **potvrzeno bez domova → GAP**

`findOrCreateAlbum` čte jen `Title`/`Description`/`Type`/`Private`. `Category` se
nečte a `albums` sloupec nemá. Viz „Alba“.

### 6. `markers.invalid` — **vědomé rozhodnutí (WAIVED) s asymetrií vůči `psimport`**

`ppimport` neplatné markery vyfiltruje (`isNamedFaceMarker`) a sloupec `invalid`
nikdy nenastaví; `psimport` `Invalid` zachovává (`people.Marker{Invalid: m.Invalid,
…}`). Navíc `ppimport` zahazuje **i nepojmenované** obličeje (`Name == ""`) a **label
markery**. Není to omyl — komentář i architektura říkají, že nepojmenované/neplatné
regiony si znovu najde Kukátkovo `face_detect` (párování přes IoU). Cena rozhodnutí
je ale reálná, viz Rizika.

### 7. Klasifikace vyjmenovaných polí

| Pole | Verdikt | Kde v dokumentu |
| --- | --- | --- |
| `Photo.TakenAtLocal` | **WAIVED** | Fotky (jediný kanonický `taken_at`). |
| `Photo.CreatedAt` | **WAIVED** | Fotky (lokální čas založení). |
| `Photo.UpdatedAt` | **WAIVED** | Fotky (řídí high-watermark, ne sloupec). |
| `File.UID` | **WAIVED** | Soubory. |
| `File.Root` | **WAIVED** | Soubory (interní PP). |
| `File.FileType` | **WAIVED** | Soubory (nese mime + `image_codec`). |
| `File.Width` / `File.Height` | **WAIVED** | Soubory (redundantní s `Photo.Width/Height`). |
| `Marker.FileUID` | **WAIVED** | Markery (odkaz na `photo_uid`). |
| `Marker.SubjUID` | **WAIVED** | Markery (vazba znovu ze jména). |
| `Marker.SubjSrc` | **WAIVED** | Markery. |
| `Marker.Type == label` | **WAIVED** | Markery (jen `face`; štítky z relace). |
| `Album.Slug` | **WAIVED** | Alba (regenerováno). |
| `Album.Favorite` | **WAIVED** | Alba (cíl neexistuje). |
| `Album.CreatedAt` / `UpdatedAt` | **WAIVED** | Alba (DB časy). |
| `Label.Favorite` | **WAIVED** | Štítky (cíl neexistuje). |

### 8. `Photo.Caption` vs. `Photo.Description` — **potvrzeno správné**

`caption() = firstNonEmpty(pp.Caption, pp.Description)`. `Caption` je živé pole
(PhotoPrism přejmenoval `photo_description` → `photo_caption`); `Description` je
mrtvý předchůdce (`gorm:"-"`, ze současné instance vždy prázdný). Precedence je
správná: `Caption` první, `Description` jen fallback pro starou instanci. Reálný
popisek nemůže spadnout pod stůl.

---

## Rizika a vědomé kompromisy

Položky, které nejsou „GAP“ (buď je o nich rozhodnuto, nebo je dopad okrajový), ale
majitel o nich má vědět:

1. **Nepojmenované a neplatné obličejové markery se nepřenášejí** (`isNamedFaceMarker`).
   Největší behaviorální rozdíl na straně lidí. Přenesou se jen **pojmenované platné**
   obličeje; zbytek si má znovu najít `face_detect`. Dvě ceny: (a) dokud je box
   offline, tyto obličeje v knihovně **nejsou** (job čeká ve frontě); (b) obličej,
   který člověk v PhotoPrismu ručně označil jako **neplatný** (`Invalid`), může
   `face_detect` znovu vytáhnout — lidské „ne“ se ztrácí. `psimport` tento problém
   nemá (markery kopíruje včetně `Invalid`). Zvážit, zda pro PP import nezachovat
   aspoň `Invalid` regiony jako neplatné markery.
2. **Alba typu `month` se při plném běhu nemapují** (`DefaultAlbumTypes` je vynechává).
   Vědomé (560 auto-generovaných měsíčních alb, pokrývá je časová osa). Scoped běh je
   ale namapuje, takže výsledek závisí na způsobu importu — konzistentní zdokumentovat.
3. **Nadmořská výška 0 a „Null Island“** — okrajové hrany nulové pasti (indicie 3).
4. **PP subject UID se zahazuje** — lidé se páruji slugem jména, takže dva různí lidé
   se stejným jménem v PhotoPrismu splynou do jednoho Kukátkova subjektu (a naopak
   přejmenování v PP založí nového). `psimport` páruje také slugem jména, takže obě
   cesty se chovají stejně — ale je to vlastnost, ne záruka identity.

## Riziko pokrytí testy (stálé)

Existující testy ověřují **precedenci, ne pokrytost**: `ppimport/logic_test.go`
`TestBuildPhoto_precedence`, `details_test.go`, `ppimport_integration_test.go`
kontrolují, že PP vyhrává nad EXIF a že prázdno nemaže — ale **nic netvrdí, že každé
zdrojové pole někam padne nebo je vědomě vynecháno**. Bez completeness testu může
příští přidané pole `photoprism.models` propadnout tiše, přesně jako dnes
`Album.Category` nebo `Subject.Private`.

**Doporučení:** tabulkový test, který přes reflexi projde pole `photoprism.Photo` /
`File` / `Marker` / `Album` / `Label` / `Subject` a selže, dokud každé není buď
v mapovací funkci, nebo na explicitním allow-listu „WAIVED“ s odkazem na tento
dokument. Takový test promění tichou regresi v červený `make check`.
