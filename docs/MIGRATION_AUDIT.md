# Migrační audit — PhotoPrism → Kukátko

- **Datum auditu:** 2026-07-17
- **Auditovaný commit:** `6e2600e` (větev `main`)
- **Rozsah:** kompletní mapování pole-po-poli, kterým import z běžící instance
  PhotoPrism plní katalog Kukátka — fotky a jejich metadata, alba, štítky,
  subjekty/lidé, markery/obličeje, místa, hodnocení a oblíbené.
- **Účel:** dát jistotu, že migrace z PhotoPrismu — jediná cesta, kterou historie
  knihovny vstupuje do nové databáze — nic tiše neztrácí. Dokument původně **pouze
  popisoval**; jeho čtyři mezery (GAP) v sekci „PhotoPrism → Kukátko“ **už jsou
  vyřešené** — tři subjektové (`Subject.Type`/`Favorite`/`Private`) jako MAPPED,
  `Album.Category` jako WAIVED — a řádky níže to odrážejí. Sekce
  „photo-sorter → Kukátko“ dále popisuje a její doporučené opravy zapsané zůstávají.

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
| **MAPPED** | 61 |
| **WAIVED** | 28 |
| **GAP** | 0 |
| **Celkem** | 89 |

**Stav mezer — všechny čtyři vyřešeny (commit tohoto úkolu):**

1. **`Subject.Private` → `subjects.private`** — **OPRAVENO (MAPPED).** Soukromý
   člověk z PhotoPrismu zůstává po importu soukromý. Byla to nejzávažnější mezera:
   měla dopad na soukromí.
2. **`Subject.Favorite` → `subjects.favorite`** — **OPRAVENO (MAPPED).** Příznak
   oblíbeného člověka se přenáší.
3. **`Subject.Type` → `subjects.type`** — **OPRAVENO (MAPPED).** Zvíře (`pet`) nebo
   jiná entita (`other`) si zachová svůj typ; už není každý subjekt natvrdo `person`.
4. **`Album.Category` → (bez sloupce)** — **WAIVED (produktové rozhodnutí).** Kukátko
   nemá koncept kategorie alba (žádný sloupec, žádné UI, žádný dotaz) — přidat
   zapisovací sloupec, který nikdo nečte, by byl mrtvý sloupec. Viz „Alba“ a
   ověřená indicie č. 5.

Mezery 1–3 měly společnou příčinu i společnou opravu — viz sekce „Subjekty“ a
ověřená indicie č. 4 níže.
Kromě těchto (nyní vyřešených) čtyř existuje několik **vědomě vynechaných** položek
s netriviálním dopadem (nepojmenované/neplatné obličejové markery, alba typu `month`
při plném běhu) — jsou popsané v „Rizika a vědomé kompromisy“.

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
private, notes, cover_photo_uid, …`; žádný sloupec `file_count`). **Dřívější
zjištění** (nyní vyřešené): `photoprism.Subject` i klient `ListSubjects` byly vůči
importu **mrtvý kód** — importní rozhraní `ppimport.PhotoPrismClient` `ListSubjects`
nedeklarovalo, takže žádné pole PP subjektu se nečetlo a subjekty vznikaly natvrdo
jako `person`. **Oprava (implementováno):** `ppimport.PhotoPrismClient` teď
`ListSubjects` deklaruje; `Service.loadSubjectIndex` přečte subjekty jednou za běh
(best-effort) do indexu podle UID i slugu jména, a `findOrCreateSubject` →
`newSubject` obohatí **nově zakládaný** subjekt o `type`/`favorite`/`private` z PP
subjektu, který marker pojmenovává (párování přes `Marker.SubjUID`, fallback slug
jména). Obohacení se děje **jen při založení** — existující (třeba v Kukátku
editovaný) subjekt zůstane beze změny, takže přeběh je idempotentní a nepřepíše
lokální úpravu (stejné chování jako `psimport`).

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Subject.Name` | `subjects.name` | **WAIVED** | Redundantní: jméno dorazí přes `Marker.Name`, samotné pole `Subject.Name` se nečte — o hodnotu se nepřichází. |
| `Subject.UID` | (index `SubjUID`→subjekt) | **MAPPED** | Neukládá se jako sloupec, ale `loadSubjectIndex` ho použije jako klíč, přes který marker (`Marker.SubjUID`) najde svůj PP subjekt pro obohacení `type`/`favorite`/`private`. Kukátkovo `subjects.uid` se dál generuje vlastní. |
| `Subject.Slug` | `subjects.slug` | **WAIVED** | Slug si Kukátko generuje z jména (`people.Slugify`); slug PP subjektu slouží jen jako fallback klíč indexu. |
| `Subject.FileCount` | — (bez sloupce) | **WAIVED** | Kukátko počítá markery živě (`ListSubjects`/`SubjectCount`), necachuje. |
| `Subject.Type` | `subjects.type` | **MAPPED** | `mapSubjectType` (`pet`/`other`/default `person`), stejně jako `psimport`. Zvíře si zachová typ; nastaveno při založení subjektu. |
| `Subject.Favorite` | `subjects.favorite` | **MAPPED** | Přeneseno při založení subjektu (`newSubject`); globální sloupec, ne per-user. |
| `Subject.Private` | `subjects.private` | **MAPPED** | Soukromá osoba z PP zůstane soukromá. Nastaveno při založení subjektu. |

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
| `Album.Category` | — (bez sloupce) | **WAIVED** | Kategorie alba (skupinování podle tématu) nemá v Kukátku kam přijít. **Produktové rozhodnutí:** Kukátko koncept kategorie alba **nemá** — žádný sloupec, žádné UI, žádný dotaz nad ním (grep celého repa: jediné „category“ jsou CLDR plurály). Přidat zapisovací `albums.category`, který nikdo nečte, by byl mrtvý sloupec, ne oprava; proto formálně WAIVED (stejné rozhodnutí jako u photo-sorter importu, kde `albums.category` zůstává GAP jen dokud se o něm nerozhodne — tady rozhodnuto: bez domova). |

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
- **Oblíbení / soukromí lidé:** `Subject.Favorite`/`Private` → **MAPPED** (globální
  sloupce, import je nyní plní při založení subjektu; viz „Subjekty“).

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

### 4. `photoprism.Subject` a `ListSubjects` byly mrtvý kód — **potvrzeno; VYŘEŠENO**

`ListSubjects` a `photoprism.Subject` byly definované na `photoprism.Client`, ale
importní rozhraní `ppimport.PhotoPrismClient` je nedeklarovalo, takže je import
nevolal a PP subject `Favorite`/`Private`/`Type` nedorazil do `subjects`, přestože
sloupce existují a `psimport` je plní. To byl zdroj GAPů 1–3 ze souhrnu.
**Oprava (implementováno):** `ppimport.PhotoPrismClient` `ListSubjects` deklaruje;
`loadSubjectIndex` je za běh přečte (best-effort, selhání nezmaří import — jen se
neobohatí) do indexu podle `SubjUID` i slugu jména; `newSubject` obohatí nově
zakládaný subjekt o `type`/`favorite`/`private` (párování markeru `SubjUID`,
fallback slug). Symetrické s `psimport`. `Slug`/`FileCount` cíl nadále nemají (slug
se generuje, počet se počítá živě).

### 5. `Album.Category` — **potvrzeno bez domova → WAIVED (produktové rozhodnutí)**

`findOrCreateAlbum` čte jen `Title`/`Description`/`Type`/`Private`. `Category` se
nečte a `albums` sloupec pro ni není. Grep celého repa: Kukátko nemá žádný koncept
kategorie alba (sloupec, UI ani dotaz) — jediné „category“ jsou CLDR plurály v i18n.
Přidat zapisovací sloupec, který nic nečte, by byl mrtvý sloupec, ne oprava; proto
formálně **WAIVED**, nikoli vymýšlení sloupce. Viz „Alba“.

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
4. **Subjekty se párují slugem jména** — `Marker.SubjUID` se nově čte pro obohacení
   (najít správný PP subjekt pro `type`/`favorite`/`private`), ale samotný Kukátkův
   subjekt se dál zakládá podle slugu jména, takže dva různí lidé se stejným jménem
   v PhotoPrismu splynou do jednoho Kukátkova subjektu (a naopak přejmenování v PP
   založí nového). `psimport` páruje také slugem jména, takže obě cesty se chovají
   stejně — ale je to vlastnost, ne záruka identity. Hrana obohacení: u splývajícího
   jména vyhraje ten PP subjekt, jehož marker založí Kukátkův subjekt jako první.

## Riziko pokrytí testy (stálé)

Existující testy ověřují **precedenci, ne pokrytost**: `ppimport/logic_test.go`
`TestBuildPhoto_precedence`, `details_test.go`, `ppimport_integration_test.go`
kontrolují, že PP vyhrává nad EXIF a že prázdno nemaže — ale **nic netvrdí, že každé
zdrojové pole někam padne nebo je vědomě vynecháno**. Bez completeness testu může
příští přidané pole `photoprism.models` propadnout tiše, přesně jako kdysi
`Album.Category` nebo `Subject.Private` (obě mezery jsou dnes vyřešené, ale objevil
je až tento audit, ne test).

**Doporučení:** tabulkový test, který přes reflexi projde pole `photoprism.Photo` /
`File` / `Marker` / `Album` / `Label` / `Subject` a selže, dokud každé není buď
v mapovací funkci, nebo na explicitním allow-listu „WAIVED“ s odkazem na tento
dokument. Takový test promění tichou regresi v červený `make check`.

---

# Migrační audit — photo-sorter → Kukátko

- **Datum auditu:** 2026-07-17
- **Auditovaný commit:** `3d6a51e` (větev `main`)
- **Rozsah:** kompletní mapování pole-po-poli, kterým přímá migrace z photo-sorteru
  (`internal/psimport`) plní katalog Kukátka — fotky a metadata, embeddingy,
  obličeje, subjekty/lidé, markery, alba a členství, štítky a členství,
  perceptuální hashe a nedestruktivní editace.
- **Účel:** dát jistotu, že migrace z photo-sorteru — jediná cesta, kterou tato
  knihovna vstupuje do nové databáze — nic tiše neztrácí. Tento dokument **pouze
  popisuje**, nemění chování importu; doporučené opravy jsou zapsané, ne
  implementované.

Auditovaný kód: `internal/photosorter/` (read-only pgx čtečka: `models.go`,
`photos.go`, `vectors.go`, `organize.go`, `people.go`, `extras.go`),
`internal/psimport/` (mapery: `photos.go` `buildPhoto`, `mappings.go`,
`vectors.go`, `satellites.go`, `helpers.go`), `internal/photos/`,
`internal/vectors/`, `internal/people/`, `internal/organize/` (cílové sloupce)
a migrace `internal/database/migrations/`. Skutečné schéma zdroje ověřeno proti
migracím photo-sorteru (`…/postgres/migrations/001–045`, ne proti produkční DB —
DSN se do dokumentu nepíše).

**Legenda verdiktů** je stejná jako u sekce PhotoPrism výše (MAPPED / WAIVED /
GAP).

## Souhrn

Audit má **dvě vrstvy**, protože ztráty u photo-sorteru neleží v maperech, ale
v tom, co čtečka vůbec načte:

**Vrstva A — pole modelů `internal/photosorter/models.go`** (to, co čtečka do
Kukátka přinese). Auditováno **90 polí** napříč 11 strukturami (`Photo`,
`Embedding`, `Face`, `Subject`, `Marker`, `Album`, `AlbumPhoto`, `Label`,
`PhotoLabel`, `Phash`, `Edit`):

| Verdikt | Počet |
| --- | --- |
| **MAPPED** | 82 |
| **WAIVED** | 8 |
| **GAP** | 0 |
| **Celkem** | 90 |

Na této vrstvě **nic nechybí**: každé pole, které čtečka vystaví, mapery přenesou
1:1 (embeddingy, obličeje a markery se kopírují včetně UID a subjekt se jen
přeznačí). Osm WAIVED jsou identifikátory a slugy, které si Kukátko generuje samo
(`Subject/Album/Label.UID`+`Slug`), pořadí v albu (`AlbumPhoto.SortOrder`) a
watermark (`Photo.UpdatedAt`).

**Vrstva B — sloupce a tabulky photo-sorteru, které čtečka NIKDY nečte.** Tady jsou
skutečné mezery. Čtečka `SELECT`uje jen podmnožinu sloupců a jen 12 z 28 tabulek;
data, která do modelů nevstoupí, se do Kukátka nedostanou, i když cílový sloupec
existuje. Přehled (detail v sekci „Co čtečka zahazuje na hranici DB“):

| | Počet |
| --- | --- |
| Nečtené tabulky bucketu katalogu | **1 GAP** (`photo_files`) + 1 WAIVED (`era_embeddings`) |
| Zahozené sloupce `photos` | **7 GAP** + 6 WAIVED |
| Zahozené extra sloupce `subjects`/`labels`/`albums` (migrace 037 photo-sorteru) | **10 GAP** + 5 WAIVED |

**Nejzávažnější mezery (GAP), sestupně podle dopadu:**

1. **`photo_files` — celá tabulka fyzických souborů se nečte.** photo-sorter drží
   RAW+JPEG stacky, HEIC+JPEG sidecary a editované varianty v `photo_files`
   (role `original`/`sidecar`/`edited`). Migrace zkopíruje **jen primární
   originál** (jeden `photo_files` řádek v Kukátku); sourozenecké soubory jednoho
   snímku (RAW k JPEGu, motion část live-photo, editovaná varianta) se **ztrácejí**.
   Kukátko má vlastní `photo_files` + `internal/stacks`, kam by patřily. Celá
   nečtená tabulka je horší ztráta než kterékoli jednotlivé pole (indicie č. 1).
2. **IPTC/XMP kredity fotky — 6 sloupců.** `photos.exif_artist`, `exif_copyright`,
   `exif_license`, `exif_software`, `keywords` (TEXT[]) a `scan` v photo-sorteru
   **existují** a Kukátko má pro ně sloupce (`artist`/`copyright`/`license`/
   `software`/`keywords`/`scan`, migrace `0027`). Čtečka je nečte, takže fotka
   z photo-sorteru dorazí s prázdnými kredity, i když je v původní knihovně měla
   (indicie č. 3).
3. **`photos.panorama`** (bool) → Kukátkovo `projection` — panorama se ztrácí
   (částečný GAP, nesoulad typů bool↔string).
4. **`subjects.cover_photo_uid`, `albums.cover_photo_uid`** — vybraná titulní fotka
   člověka i alba se zahazuje; cílové sloupce v Kukátku existují (vyžadovaly by
   přemapování photo-UID).
5. **`albums.category`** — kategorie alba nemá kam přijít (stejný GAP jako u
   PhotoPrism importu).
6. **Extra volný text migrace 037** — `subjects.bio`/`about`/`alias`,
   `labels.description`/`categories`, `albums.location`/`notes` — o rozhodnutí je
   zahodit nikdo explicitně nerozhodl; **GAP, pokud jsou v produkci vyplněné**.
   Doporučení u každého: buď přidat cílový sloupec a namapovat, nebo — je-li pole
   v produkční knihovně prázdné — formálně WAIVED.

Kromě GAPů je několik **vědomých kompromisů** (per-user oblíbené, zahozené pořadí,
neexistence video sloupců na straně zdroje) v sekci „Rizika a vědomé kompromisy“.

## Jak import plní řádek (kontext k tabulkám)

Na rozdíl od PhotoPrism importu (dva HTTP endpointy) je photo-sorter **přímá kopie
DB→DB**. `resolvePhoto` fotku dohledá podle `photosorter_uid` (už migrovaná) nebo
`file_hash` (už v katalogu, např. z PhotoPrism — jen doplní `photosorter_uid`),
jinak `createPhoto` zkopíruje originál z `FilePath` do úložiště pod měsícem pořízení
a `buildPhoto` sestaví řádek. **Photo-sorter i Kukátko používají SHA256**, takže
dedup přes `file_hash` funguje napříč importéry. Satelity (`transferSatellites`)
se přenášejí za fotkou: embedding a obličeje jsou jádrový 1:1 přenos (selhání
zopakuje celou fotku), hashe/editace/markery/členství jsou best-effort (chyba se
loguje). Embeddingy (CLIP 768) a obličeje (InsightFace 512) sdílejí modely s
Kukátkem, takže se kopírují **bez přepočtu**.

## Fotky — struktura `Photo`

Cíl je tabulka `photos` (migrace `0003`, `0004`, `0027`). `buildPhoto` mapuje
kmenová i kurátorská pole **1:1 — stejné názvy sloupců na obou stranách**.
Obsahová identita (`file_hash`/`file_path`/`file_size`) pochází z **čerstvě
uloženého** souboru (`stored`), takže popisuje bajty skutečně na disku; hodnoty
jsou totožné s photo-sorterem (stejný obsah, stejný SHA256).

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Photo.UID` | `photosorter_uid` | **MAPPED** | Klíč idempotence (`GetByPhotosorterUID`) a dedup; uloženo přes ukazatel. |
| `Photo.FileHash` | `file_hash` | **MAPPED** | SHA256; dedup klíč (`resolvePhoto`→`GetByFileHash`). `file_hash` = `stored.Hash` týchž bajtů. |
| `Photo.FilePath` | (zdroj kopie) → `file_path` | **MAPPED** | Cesta k originálu, ze které se čtou bajty (`copyOriginal`); do `file_path` jde nový layout úložiště. Zároveň fallback pro jméno (`originalName`). |
| `Photo.FileName` | `file_name` | **MAPPED** | `originalName(ps)`; při prázdném jménu `path.Base(FilePath)`, jinak UID. |
| `Photo.FileSize` | `file_size` | **MAPPED** | `file_size` = `stored.Size` zkopírovaných bajtů (týž obsah); pole `ps.FileSize` samo se nečte, ale je rovné. |
| `Photo.FileMime` | `file_mime` | **MAPPED** | `photoMime`: preferuje `ps.FileMime`, jinak MIME nasniffovaný z uložených bajtů. |
| `Photo.FileWidth` | `file_width` | **MAPPED** | |
| `Photo.FileHeight` | `file_height` | **MAPPED** | |
| `Photo.FileOrientation` | `file_orientation` | **MAPPED** | |
| `Photo.TakenAt` | `taken_at` | **MAPPED** | Zároveň řídí měsíc v úložišti (`Store(..., takenAt, ...)`). |
| `Photo.TakenAtSource` | `taken_at_source` | **MAPPED** | Přenáší se **přímo** ze zdroje (asymetrie: ppimport `taken_at_source` natvrdo razítkuje `exif`). |
| `Photo.Title` | `title` | **MAPPED** | |
| `Photo.Description` | `description` | **MAPPED** | Přímý přenos (photo-sorter má jediné pole `description`). |
| `Photo.Notes` | `notes` | **MAPPED** | Vlastní poznámka; přenáší se přímo. |
| `Photo.Lat` | `lat` | **MAPPED** | Přenos bez „nulové pasti“ — GPS jde přímo (i `(0,0)`), viz indicie č. 3. `location_source` se ale nerazítkuje (viz cílová strana). |
| `Photo.Lng` | `lng` | **MAPPED** | |
| `Photo.Altitude` | `altitude` | **MAPPED** | Ukazatel — `nil` = neznámo, `0` = hladina moře se zachová (na rozdíl od ppimport, kde `0` spadne na EXIF). |
| `Photo.CameraMake` | `camera_make` | **MAPPED** | |
| `Photo.CameraModel` | `camera_model` | **MAPPED** | |
| `Photo.LensModel` | `lens_model` | **MAPPED** | |
| `Photo.ISO` | `iso` | **MAPPED** | Ukazatel; `nil` = neznámo (bez „nulové pasti“). |
| `Photo.Aperture` | `aperture` | **MAPPED** | Ukazatel. |
| `Photo.Exposure` | `exposure` | **MAPPED** | |
| `Photo.FocalLength` | `focal_length` | **MAPPED** | Ukazatel. |
| `Photo.Exif` | `exif` | **MAPPED** | Syrový JSON EXIF se kopíruje **beze změny** (asymetrie: ppimport EXIF znovu extrahuje ze staženého souboru). |
| `Photo.Private` | `private` | **MAPPED** | |
| `Photo.ArchivedAt` | `archived_at` | **MAPPED** | Archivační stav se **zachová** (asymetrie: ppimport `archived_at` nepřenáší). |
| `Photo.UpdatedAt` | — (bez sloupce) | **WAIVED** | Řídí inkrementální watermark (paging `ORDER BY updated_at`, resume), není sloupcem fotky; `photos.updated_at` je čas vlastní mutace. |

**27 MAPPED, 1 WAIVED, 0 GAP** na úrovni modelu. Skutečné ztráty fotky leží ve
sloupcích, které čtečka nečte — viz „Co čtečka zahazuje na hranici DB“.

## Embeddingy — struktura `Embedding`

Cíl je tabulka `embeddings` (migrace `0006`, `halfvec` + HNSW). `transferEmbedding`
kopíruje vektor 1:1; když photo-sorter embedding nemá, zařadí se Kukátkův
`image_embed` job.

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Embedding.PhotoUID` | `embeddings.photo_uid` (kk) | **MAPPED** | Přeznačeno na Kukátkovo UID fotky. |
| `Embedding.Vector` | `embeddings.embedding` | **MAPPED** | CLIP 768, 1:1 bez přepočtu (stejné modely). |
| `Embedding.Model` | `embeddings.model` | **MAPPED** | |
| `Embedding.Pretrained` | `embeddings.pretrained` | **MAPPED** | |

## Obličeje — struktura `Face`

Cíl je tabulka `faces` (migrace `0009`/`0010`). `transferFaces` + `convertFace`
kopírují každý obličej 1:1, přeznačí `SubjectUID` a **zachovají `MarkerUID`**
(marker migruje se stejným UID). `RecordFaceDetection` zapíše detekci i pro nulový
počet obličejů, aby se fotka znovu nedetekovala; fotku, kterou photo-sorter nikdy
nezpracoval (`faces_processed` chybí), předá Kukátkovu `face_detect`.

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Face.PhotoUID` | `faces.photo_uid` (kk) | **MAPPED** | Přeznačeno na Kukátkovo UID. |
| `Face.FaceIndex` | `faces.face_index` | **MAPPED** | |
| `Face.Vector` | `faces.embedding` | **MAPPED** | InsightFace 512, 1:1. |
| `Face.BBox` | `faces.bbox` | **MAPPED** | Normalizovaný `[x,y,w,h]` (0..1). |
| `Face.DetScore` | `faces.det_score` | **MAPPED** | |
| `Face.Model` | `faces.model` | **MAPPED** | Zároveň `faceModel()` určí model detekce. |
| `Face.MarkerUID` | `faces.marker_uid` | **MAPPED** | **Zachováno** — markery migrují se stejným UID, cache zůstane platná. |
| `Face.SubjectUID` | `faces.subject_uid` | **MAPPED** | `remapSubject`: přeznačeno na Kukátkův subjekt; neznámý subjekt → `nil`. |
| `Face.SubjectName` | `faces.subject_name` | **MAPPED** | Denormalizovaný render-hint. |
| `Face.PhotoWidth` | `faces.photo_width` | **MAPPED** | Referenční rámec bboxu — viz indicie č. 7. |
| `Face.PhotoHeight` | `faces.photo_height` | **MAPPED** | dtto. |
| `Face.Orientation` | `faces.orientation` | **MAPPED** | dtto; přenáší se s bboxem, takže re-orientace box neposune. |

Vše MAPPED. `faces_processed.face_count` čtečka čte, ale `transferFaces` ho
**zahazuje** (bere `len(faces)`) — viz indicie č. 8.

## Subjekty / lidé — struktura `Subject`

Cíl je tabulka `subjects` (migrace `0008`: `uid, slug, name, type, favorite,
private, notes, cover_photo_uid`). `findOrCreateSubject` páruje existující subjekt
**podle slugu jména** (`people.Slugify`), jinak zakládá nový a **zachová typ i
příznaky**.

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Subject.Name` | `subjects.name` | **MAPPED** | Klíč find-or-create. |
| `Subject.Type` | `subjects.type` | **MAPPED** | `mapSubjectType` (`pet`/`other`/default `person`). **Zvíře si typ zachová** (na rozdíl od ppimport, kde je vše `person`). |
| `Subject.Favorite` | `subjects.favorite` | **MAPPED** | `subjects.favorite` je **skutečný sloupec subjektu (globální)**, ne per-user starost jako u fotek — proto se plní (viz indicie č. 6). |
| `Subject.Private` | `subjects.private` | **MAPPED** | Soukromá osoba zůstane soukromá (na rozdíl od ppimport, kde se ztrácí). |
| `Subject.Notes` | `subjects.notes` | **MAPPED** | |
| `Subject.UID` | — | **WAIVED** | Kukátko generuje vlastní `uid`; subjekt se páruje slugem. |
| `Subject.Slug` | `subjects.slug` | **WAIVED** | Přegeneruje se z jména (`people.Slugify`). |

**5 MAPPED, 2 WAIVED.** Pozor: `type`/`favorite`/`private`/`notes` se nastaví jen
při **založení**. Existující subjekt (spárovaný slugem — např. dřív zaseto ppimportem
jako holý `person`) se převezme **beze změny**; photo-sorterův bohatší typ/příznak
ho nepřepíše (viz Rizika). Extra pole `subjects.bio`/`about`/`alias`/`cover_photo_uid`
čtečka nečte — viz „Co čtečka zahazuje“.

## Markery — struktura `Marker`

Cíl je tabulka `markers` (migrace `0008`). `transferMarkers` migruje **každý**
marker (idempotentně přes zachované UID); `mapMarkerType` mapuje typ, subjekt se
přeznačí. **Klíčová asymetrie vůči ppimport:** protože jde o kopii DB, přenáší se
markery *všechny* — pojmenované i nepojmenované, platné i neplatné, `face` i
`label` — ne jen pojmenované platné obličeje.

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Marker.UID` | `markers.uid` | **MAPPED** | **Zachováno** — idempotence (`GetMarkerByUID`) a sdílená identita s `faces.marker_uid`. |
| `Marker.PhotoUID` | `markers.photo_uid` (kk) | **MAPPED** | Přeznačeno. |
| `Marker.SubjectUID` | `markers.subject_uid` | **MAPPED** | `remapSubject`. |
| `Marker.Type` | `markers.type` | **MAPPED** | `mapMarkerType` (`label`/default `face`); **label markery se zachovají** (ppimport je zahazuje). |
| `Marker.X` / `Y` / `W` / `H` | `markers.x/y/w/h` | **MAPPED** | Normalizovaný bbox (0..1). |
| `Marker.Score` | `markers.score` | **MAPPED** | Import-provenience, ne kvalita (0 = nezaznamenáno); neřadit podle něj obličeje. |
| `Marker.Invalid` | `markers.invalid` | **MAPPED** | **Zachováno** — lidské „to není obličej“ přežije (asymetrie: ppimport `Invalid` filtruje pryč). |
| `Marker.Reviewed` | `markers.reviewed` | **MAPPED** | Přímý přenos (ppimport odvozuje `Reviewed = !Review`). |

Vše MAPPED (11 polí).

## Alba — struktury `Album` a `AlbumPhoto`

Cíl je tabulka `albums` (migrace `0011`; `0022` odstranilo `order_by`/`sort_order`)
a připojení `album_photos`. `findOrCreateAlbum` páruje/zakládá album **podle
titulku**; prázdný titulek → album se přeskočí. `mapAlbumType` mapuje **všechny**
typy včetně `month` (na rozdíl od ppimportova `DefaultAlbumTypes`, kde `month`
vypadává).

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Album.Title` | `albums.title` | **MAPPED** | Klíč find-or-create. |
| `Album.Description` | `albums.description` | **MAPPED** | |
| `Album.Type` | `albums.type` | **MAPPED** | `mapAlbumType`; neznámé/prázdné → `album` (manual). Včetně `month`. |
| `Album.Private` | `albums.private` | **MAPPED** | |
| `Album.UID` | — | **WAIVED** | Kukátko generuje vlastní `uid`; album se páruje titulkem. |
| `Album.Slug` | `albums.slug` | **WAIVED** | Přegeneruje se z titulku. |
| `AlbumPhoto.AlbumUID` | (klíč, přemapován) | **MAPPED** | Přes `maps.albums`. |
| `AlbumPhoto.PhotoUID` | (klíč → kk UID) | **MAPPED** | `AddPhoto` idempotentní; nenaimportovaný člen se přeskočí. |
| `AlbumPhoto.SortOrder` | — | **WAIVED** | Kukátko řadí alba **chronologicky** (`0022` zahodilo `album_photos.sort_order`). |

**Album: 4 MAPPED, 2 WAIVED. AlbumPhoto: 2 MAPPED, 1 WAIVED.** Extra sloupce alba
(`category`, `cover_photo_uid`, `location`, `notes`, `filter`, `favorite`,
`album_order`/`order_by`) čtečka nečte — viz „Co čtečka zahazuje“.

## Štítky — struktury `Label` a `PhotoLabel`

Cíl jsou tabulky `labels` (`uid, slug, name, priority`) a `photo_labels`
(`source, uncertainty`), migrace `0011`. `findOrCreateLabel` páruje **podle jména**.

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Label.Name` | `labels.name` | **MAPPED** | Klíč find-or-create. |
| `Label.Priority` | `labels.priority` | **MAPPED** | |
| `Label.UID` | — | **WAIVED** | Vlastní `uid`. |
| `Label.Slug` | `labels.slug` | **WAIVED** | Přegeneruje se z jména. |
| `PhotoLabel.PhotoUID` | (klíč → kk UID) | **MAPPED** | |
| `PhotoLabel.LabelUID` | (klíč, přemapován) | **MAPPED** | Přes `maps.labels`; `AttachLabel` idempotentní. |
| `PhotoLabel.Source` | `photo_labels.source` | **MAPPED** | `mapLabelSource`: `manual`→manual, `ai`→ai, ostatní→import. |
| `PhotoLabel.Uncertainty` | `photo_labels.uncertainty` | **MAPPED** | Přímý přenos. |

**Label: 2 MAPPED, 2 WAIVED. PhotoLabel: 4 MAPPED.** Extra `labels.description`/
`categories`/`favorite` čtečka nečte — viz „Co čtečka zahazuje“.

## Perceptuální hashe — struktura `Phash`

Cíl je `photo_phashes`. `transferPhash` je idempotentní upsert (best-effort).

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Phash.PhotoUID` | `photo_phashes.photo_uid` (kk) | **MAPPED** | |
| `Phash.Phash` | `photo_phashes.phash` | **MAPPED** | pHash (DCT). |
| `Phash.Dhash` | `photo_phashes.dhash` | **MAPPED** | dHash (gradient). |

## Editace — struktura `Edit`

Cíl je `photo_edits`. `transferEdit` je idempotentní upsert (best-effort).
Nedestruktivní crop/rotace/tón se přenášejí 1:1.

| Zdrojové pole | Cílový sloupec | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `Edit.PhotoUID` | `photo_edits.photo_uid` (kk) | **MAPPED** | |
| `Edit.CropX` / `CropY` / `CropW` / `CropH` | `photo_edits.crop_x/y/w/h` | **MAPPED** | Ukazatele — `nil` = bez ořezu. |
| `Edit.Rotation` | `photo_edits.rotation` | **MAPPED** | 0/90/180/270. |
| `Edit.Brightness` | `photo_edits.brightness` | **MAPPED** | |
| `Edit.Contrast` | `photo_edits.contrast` | **MAPPED** | |

Vše MAPPED (8 polí).

---

## Co čtečka zahazuje na hranici DB (indicie č. 1 a 3)

Toto je jádro auditu. `internal/photosorter` je jediná brána mezi photo-sorterem a
Kukátkem; co její `SELECT`y nenačtou, do modelů nevstoupí a mapery nemají co
přenést. Skutečné schéma photo-sorteru (migrace `001–045`) obsahuje **28 tabulek**;
čtečka `SELECT`uje z **12** a i z nich bere jen podmnožinu sloupců.

### Tabulky, které nikdo nečte

Bucket katalogu (tabulky s daty knihovny) má **14 tabulek**; čtečka čte 12. Zbývají:

| Tabulka | Verdikt | Co se ztrácí / proč ne |
| --- | --- | --- |
| `photo_files` | **GAP** | **Nejzávažnější.** Fyzické soubory snímku — RAW+JPEG stacky, HEIC+JPEG sidecary, editované varianty (`role` `original`/`sidecar`/`edited`). Migrace zkopíruje jen primární originál a založí **jeden** `photo_files` řádek v Kukátku; sourozenecké soubory se ztrácejí. **Bije** uživatele se stacky (RAW vedle JPEGu, live-photo klip). Kukátko má vlastní `photo_files` + `internal/stacks`. **Oprava:** ve čtečce přidat listování `photo_files` a v `psimport` zkopírovat i sekundární soubory (role `sidecar`/`edited`) jako další `photo_files` řádky a nechat `internal/stacks` je seskupit. |
| `era_embeddings` | **WAIVED** | Referenční CLIP centroidy „ér“ pro odhad období — odvozená/přepočítatelná data, ne obsah knihovny; Kukátko funkci „ér“ nemá. |

Ostatní nečtené tabulky (`users`, `sessions`, `photo_books`, `book_sections`,
`section_photos`, `book_pages`, `page_slots`, `book_chapters`, `text_versions`,
`text_check_results`, `album_share_links`, `smart_albums`, `audit_log`,
`api_tokens`) jsou **mimo rozsah** (fotokniha, sdílení, účty, audit) — vědomě
nepřenášené, žádná ztráta knihovních dat. Tím je indicie č. 1 potvrzena: jediná
nečtená tabulka s daty knihovny je `photo_files`.

### Sloupce `photos`, které čtečka nečte (indicie č. 3)

`photoColumns` (čtečka) bere 28 sloupců. Schéma `photos` (po migracích `032`, `035`,
`036`) jich má víc. Zahozené:

| Sloupec photo-sorteru | Cíl v Kukátku | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `exif_artist` | `photos.artist` | **GAP** | Cílový sloupec existuje (`0027`); autor fotky dorazí prázdný. |
| `exif_copyright` | `photos.copyright` | **GAP** | dtto. |
| `exif_license` | `photos.license` | **GAP** | dtto. |
| `exif_software` | `photos.software` | **GAP** | dtto. |
| `keywords` (TEXT[]) | `photos.keywords` | **GAP** | Cílový sloupec existuje; klíčová slova se ztrácejí. |
| `scan` (bool) | `photos.scan` | **GAP** | Cílový sloupec existuje; příznak „sken“ se ztrácí. |
| `panorama` (bool) | `photos.projection` | **GAP** | Částečný — bool by mohl nastavit `projection` (např. `equirectangular`), teď se zahodí. |
| `favorite` (bool) | (per-user `user_favorites`) | **WAIVED** | Oblíbené jsou v Kukátku **per-user** — migrace `0011` explicitně: „per-user favorites that **replace** photo-sorter's global `photos.favorite` flag“. Job nemá komu příznak přiřadit (stejné jako `Photo.Favorite` u PhotoPrism). |
| `quality` (smallint) | — | **WAIVED** | Přepočítatelné skóre kvality; Kukátko ho nemodeluje. |
| `time_zone` / `taken_at_offset` | — | **WAIVED** | Kukátko drží kanonický `taken_at` (timestamptz, absolutní okamžik); jako `TakenAtLocal` u PhotoPrism. Drobná ztráta věrnosti při rekonstrukci lokálního času (viz Rizika). |
| `uploaded_by` / `created_at` | — | **WAIVED** | Interní/DB-spravované, ne obsah knihovny. |

**Oprava GAPů kreditů:** rozšířit `photoColumns` o `exif_artist/copyright/license/
software`, `keywords`, `scan`, `panorama` a v `buildPhoto` je namapovat na
existující sloupce (kredity by měly ctít precedenci „prázdno nemaže“, jako
`ppimport.ApplyImportMetadata`). Půjde o čistě aditivní změnu čtečky + `buildPhoto`.

**Video sloupce** (`media_type`, `duration_ms`, `video_codec`, `audio_codec`,
`has_audio`, `fps`; indicie č. 4): photo-sorterův `photos` je **jen obrazový** —
žádný video sloupec neexistuje. `psimport` je tedy neplní a **nemá z čeho** (ani
nespouští ffprobe). `media_type` defaultuje na `image`. Není to ztráta (zdroj video
nemodeluje), ale je to omezení: pronikne-li do photo-sorteru přece jen video, dorazí
jako obrázek bez trvání/kodeku (viz Rizika).

### Extra sloupce `subjects` / `labels` / `albums` (migrace 037 photo-sorteru)

Migrace `037` přidala photo-sorteru bohatší metadata subjektů, štítků a alb; čtečka
(`people.go`, `organize.go`) z nich čte jen jádro.

| Sloupec photo-sorteru | Cíl v Kukátku | Verdikt | Poznámka |
| --- | --- | --- | --- |
| `subjects.cover_photo_uid` | `subjects.cover_photo_uid` | **GAP** | Cílový sloupec existuje; titulní fotka člověka se ztrácí (vyžaduje přemapování photo-UID). |
| `subjects.bio` / `about` / `alias` | — | **GAP** | Volný text o osobě bez cíle v Kukátku (které má jen `notes`, to je mapované ze `Subject.Notes`). **GAP, pokud vyplněno.** Oprava: přidat sloupce a namapovat, nebo je-li produkce nepoužívá, formálně WAIVED. |
| `albums.cover_photo_uid` | `albums.cover_photo_uid` | **GAP** | Cílový sloupec existuje; titulní fotka alba se ztrácí (přemapování photo-UID). |
| `albums.category` | — (bez sloupce) | **GAP** | Kategorie alba nemá v Kukátku kam přijít (stejný GAP jako `Album.Category` u PhotoPrism). Oprava: přidat nullable `albums.category`, nebo WAIVED je-li nepoužito. |
| `albums.location` / `notes` | — | **GAP** | Volný text/lokalita alba bez cíle. **GAP, pokud vyplněno.** |
| `labels.description` / `categories` | — | **GAP** | Popis/kategorie štítku bez cíle v Kukátku (`labels` má jen `slug, name, priority`). **GAP, pokud vyplněno.** |
| `albums.filter` | (`internal/savedsearch`) | **WAIVED** | photo-sorterův „smart album“ jako filtr; Kukátko má chytrá alba samostatně (`saved_searches`), album se migruje jako statické. |
| `albums.album_order` / `order_by` | — | **WAIVED** | Pořadí/řazení alba — Kukátko řadí chronologicky (`0022`). |
| `albums.favorite` / `labels.favorite` | — | **WAIVED** | Kukátkova `albums`/`labels` sloupec `favorite` nemají (koncept oblíbeného alba/štítku není). |

Interní sloupce (`faces.id`/`dim`/`file_uid`, `embeddings.dim`/`created_at`, časy
`created_at`/`updated_at`, `album_photos.added_at`, `photo_labels.created_at`) jsou
DB-interní a přenášet se nemají — **WAIVED** (neuvádím jako samostatné řádky).

---

## Cílová strana — sloupce `photos`, které `psimport` neplní

Pro prokazatelnost oběma směry: každý z 55 vkládaných sloupců `photos`
(`photoInsertColumns`) je buď namapovaný z pole photo-sorteru (výše), nebo
Kukátkem-generovaný/vlastní. `buildPhoto` nastaví 27 sloupců + `uid` (DB) a
`media_type` (default `image` v `Create`). Neplněné (default/prázdno):

| Sloupec | Původ | Pozn. |
| --- | --- | --- |
| `duration_ms`, `video_codec`, `audio_codec`, `has_audio`, `fps` | — | Video; zdroj je jen obrazový, `psimport` ffprobe nespouští. |
| `subject`, `color_profile`, `image_codec`, `camera_serial`, `original_name`, `projection` | — | Zdrojový sloupec neexistuje (`subject`/`color_profile`/`image_codec`/`camera_serial`/`original_name`), nebo je nečtený (`projection`←`panorama`, GAP). `file_name` nese jméno souboru. |
| `artist`, `copyright`, `license`, `software`, `keywords`, `scan` | (dropnuto) | Zdroj **existuje**, ale čtečka ho nečte → **GAP** (viz výše). |
| `location_source` | Nerazítkováno | `psimport` plní `lat`/`lng`, ale provenienci polohy nechá prázdnou (asymetrie: ppimport razítkuje `exif`). Ne ztráta dat, jen prázdná provenience. |
| `taken_at_estimated`, `taken_at_note`, `ai_note` | Kukátko-only | Zdroj nemá; default. |
| `uploaded_by` | — | Job nemá uživatele. |
| `photoprism_uid`, `photoprism_file_hash` | jiný import | Plní jen `ppimport`. |
| `metadata_extracted_at` | Kukátko-only | `nil` → naplánuje `metadata` backfill. |
| `stack_uid`, `stack_primary` | Kukátko-only | Detekce stacků (`internal/stacks`). |
| `uid`, `created_at`, `updated_at` | DB | Generováno při vložení. |

---

## Ověření konkrétních indicií ze zadání

### 1. Tabulky, které nikdo nečte — **potvrzeno: jediná je `photo_files` (GAP)**

Schéma photo-sorteru má 28 tabulek; 14 je mimo rozsah (fotokniha/sdílení/účty/
audit/smart-alba), vědomě nepřenášených. Z 14 katalogových tabulek čtečka čte 12.
Nečtené: **`photo_files`** (fyzické soubory / stacky — reálná ztráta, GAP) a
`era_embeddings` (referenční, WAIVED). `photo_files` je největší jednotlivá mezera
celého auditu — celá tabulka s daty knihovny nevstoupí do modelů.

### 2. `TestBuildPhoto` je tenký — **potvrzeno, 20 z 27 mapovaných polí bez testu**

`TestBuildPhoto` (`helpers_test.go`) ověřuje jen **7** polí: `FileHash`, `FilePath`,
`FileSize`, `Title`, `Private`, `FileOrientation`, `PhotosorterUID`. `buildPhoto`
mapuje **27** sloupců, takže **20 nemá test** prokazující, že přežijí:

> `FileName`, `FileMime`, `FileWidth`, `FileHeight`, `TakenAt`, `TakenAtSource`,
> `Description`, `Notes`, `Lat`, `Lng`, `Altitude`, `CameraMake`, `CameraModel`,
> `LensModel`, `ISO`, `Aperture`, `Exposure`, `FocalLength`, `Exif`, `ArchivedAt`.

Stálé riziko: přejmenování/vynechání kteréhokoli z těchto 20 při refaktoru projde
zeleně. **Doporučení** je stejné jako u PhotoPrism sekce — completeness test přes
reflexi (viz „Riziko pokrytí testy“).

### 3. IPTC/XMP sloupce — **potvrzeno GAP (6 kreditů + `scan`)**

photo-sorter data **drží** (`exif_artist`/`copyright`/`license`/`software`,
`keywords`, `scan`), Kukátko má cílové sloupce (`0027`), ale čtečka je nečte →
fotka dorazí s prázdnými kredity. GAP, viz „Co čtečka zahazuje“. (`subject`,
`color_profile`, `image_codec`, `camera_serial`, `original_name` v photo-sorteru
neexistují — tam není co ztratit; `projection`←`panorama` je částečný GAP.)

### 4. Video sloupce — **potvrzeno: zdroj video nemodeluje, `psimport` neplní**

photo-sorterův `photos` nemá žádný video sloupec (`media_type`/`duration_ms`/
`video_codec`/`audio_codec`/`has_audio`/`fps`). `psimport` je nemá z čeho plnit a
ffprobe nespouští; `media_type` defaultuje na `image`. Není ztráta (jen obrazový
zdroj), viz Rizika ohledně případného videa v photo-sorteru.

### 5. `Marker.Invalid` a `Marker.Reviewed` — **potvrzeno: `psimport` je zachovává**

`transferOneMarker` mapuje `Invalid: m.Invalid` i `Reviewed: m.Reviewed` přímo.
Navíc `psimport` přenáší **všechny** markery (i nepojmenované a `label`), protože
kopíruje DB. **Asymetrie vůči ppimport**, který neplatné/nepojmenované/`label`
markery filtruje (`isNamedFaceMarker`) a `Invalid` nikdy nenastaví. Pro migraci lidí
je tedy `psimport` věrnější — lidské „to není obličej“ (`Invalid`) i ruční regiony
přežijí.

### 6. `Subject.Favorite` — **skutečný sloupec subjektu, ne per-user (MAPPED)**

`subjects.favorite` je globální BOOLEAN sloupec tabulky `subjects` (migrace `0008`),
ne per-user starost jako `Photo.Favorite` (to má `user_favorites`). `findOrCreateSubject`
ho plní (`people.Subject{Favorite: ps.Favorite, ...}`), stejně `Private`, `Type`,
`Notes`. Rozdíl proti fotkám: oblíbenost **osoby** je vlastnost subjektu, oblíbenost
**fotky** je vztah uživatel↔fotka. Proto `Subject.Favorite` = MAPPED, kdežto
`Photo.Favorite`/`photos.favorite` = WAIVED.

### 7. Geometrie obličeje (`photo_width`/`height`/`orientation`) — **potvrzeno bezpečné**

`convertFace` kopíruje `BBox` (normalizovaný 0..1) **spolu s** `PhotoWidth`,
`PhotoHeight` a `Orientation` — celý referenční rámec boxu. Protože se přenáší
normalizovaný box i jeho rámec 1:1 (žádné přepočítání rozměrů ani re-orientace),
box nemůže tiše „ujet“. Kukátkovo `vectors.Face` má odpovídající sloupce
(`bbox`, `photo_width`, `photo_height`, `orientation`). Bez ztráty.

### 8. Klasifikace vyjmenovaných polí

| Pole | Verdikt | Kde v dokumentu |
| --- | --- | --- |
| `AlbumPhoto.SortOrder` | **WAIVED** | Alba (chronologické řazení, `0022`). |
| `Album.Slug` | **WAIVED** | Alba (přegenerováno). |
| `Label.Slug` | **WAIVED** | Štítky (přegenerováno). |
| `Subject.Slug` | **WAIVED** | Subjekty (přegenerováno). |
| `ps.UpdatedAt` (`Photo.UpdatedAt`) | **WAIVED** | Fotky (watermark, ne sloupec). |
| `faces_processed.face_count` | **WAIVED** | Obličeje — čtečka ho čte (`FacesProcessed`), ale `transferFaces` ho zahodí a použije `len(faces)`; detekce se zapíše ze skutečných obličejů. |
| `PhotoLabel.Source` / `Uncertainty` | **MAPPED** | Štítky (`mapLabelSource` / přímý přenos). |

---

## Riziko pokrytí testy (stálé)

`psimport` má integrační testy přenosu (embeddingy, obličeje, markery, členství) a
`TestBuildPhoto`, ale **žádný completeness test**: nic netvrdí, že každé pole
`photosorter.models` někam padne nebo je vědomě vynecháno, ani že čtečka čte každý
relevantní sloupec zdroje. Právě proto zůstaly nezachyceny mezery vrstvy B — čtečka
tiše nečte IPTC sloupce, `photo_files` i extra metadata `037`.

**Doporučení (dvě úrovně):**

1. **Reflexní tabulkový test nad modely** (jako u PhotoPrism sekce): projde pole
   `photosorter.Photo`/`Face`/`Marker`/`Subject`/`Album`/`Label`/`PhotoLabel`/
   `Phash`/`Edit` a selže, dokud každé není buď v maperu, nebo na explicitním
   allow-listu „WAIVED“ s odkazem na tento dokument. Chrání vrstvu A.
2. **Test „čtečka vs. schéma zdroje“**: porovná sloupce, které čtečka `SELECT`uje,
   s aktuálním schématem photo-sorteru (nebo aspoň allow-list vědomě nečtených
   sloupců/tabulek). Chrání vrstvu B — právě tam dnes ztráty jsou.

## Rizika a vědomé kompromisy

1. **`photo_files` se nemigruje** — snímek se stackem (RAW+JPEG, sidecar, editovaná
   varianta) dorazí jako osamocený originál; sekundární soubory se ztrácejí.
   Největší strukturální ztráta, viz GAP č. 1.
2. **IPTC kredity + `keywords`/`scan`/`panorama` se zahazují na hranici čtečky** —
   data v photo-sorteru jsou, cílové sloupce v Kukátku jsou, jen se nečtou. Aditivní
   oprava, viz GAP č. 2–3.
3. **Extra metadata `037`** (bio/alias/kategorie/lokalita/popis subjektů, štítků a
   alb, titulní fotky) — GAP jen pokud jsou v produkci vyplněné; jinak formálně
   WAIVED. Doporučeno ověřit využití v produkční knihovně a rozhodnout.
4. **Existující subjekt se nepřepisuje** — `findOrCreateSubject` nastaví
   `type`/`favorite`/`private`/`notes` jen při založení. Byl-li subjekt zaseto dřív
   (ppimportem jako holý `person`, nebo předchozím během), photo-sorterův bohatší
   typ/příznak ho nedožene. Doporučení: při shodě slugu doplnit chybějící pole.
5. **`location_source` se nerazítkuje** — migrované fotky mají `lat`/`lng`, ale
   prázdnou provenienci polohy (ppimport razítkuje `exif`). Kosmetické, ale
   nekonzistentní.
6. **`time_zone`/`taken_at_offset` se ztrácí** — Kukátko drží absolutní `taken_at`;
   rekonstrukce původního lokálního času (offsetu pořízení) není možná. Nízký dopad.
7. **Video v obrazovém zdroji** — pronikne-li do photo-sorteru video, `psimport` ho
   uloží jako `image` bez trvání/kodeku (ffprobe se nespouští). Zdroj video
   nemodeluje, takže v praxi okrajové.
8. **Párování subjektů/alb/štítků podle jména/titulku** — dva různí lidé téhož jména
   splynou do jednoho subjektu (a naopak). Stejné chování jako ppimport; vlastnost,
   ne záruka identity.
