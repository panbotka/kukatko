# Frontend

Popisný referenční přehled frontendu (`web/`). **Nejsou to pravidla** — pravidla
jsou v [`CLAUDE.md`](../CLAUDE.md). Novou komponentu, hook, stránku nebo službu
zapiš sem.

<!-- BODY BEGIN -->
- **Frontend layout:** `web/` (Vite + React 19 + TS): `web/src/` s `components/`
  (`Layout` = navbar shell s user-menu/logout + role-gated nav, **vyvážená kolem toho, jak se
  knihovna reálně prochází — po albu, po štítku, po roce**: **Knihovna** `/` (= úvodní stránka,
  dostupná i přes brand link; `NavLink` má `end`, jinak by se rozsvítila na každé routě),
  **Alba** `/albums` a **Štítky** `/labels` jsou vždy viditelné top-level položky
  (registr `PRIMARY_ITEMS`); zbylé browse cíle sdružuje dropdown **Procházet** (`nav.browse`,
  `BROWSE_GROUP`): **Oblíbené** `/favorites`, **Lidé** `/people`, **Místa** `/places`, **Mapa**
  `/map`; **Nahrát** `/upload` je top-level (gate `canWrite`); editorský dropdown **Nástroje**
  (`nav.tools`, `TOOLS_GROUP`, celý gate `canWrite`) sdružuje **Duplikáty** `/duplicates` + **Koš**
  `/trash`; adminský dropdown **Správa** (`nav.admin`, `ADMIN_GROUP`, celý gate `isAdmin`) sdružuje
  **Import** `/import` + **Údržba** `/maintenance` + **Systém** `/system`. **V navbaru není hledání**
  (ani odkaz, ani živé pole ani uložená hledání) — hledá se z knihovny a ze stránky `/search`.
  Každá položka i každý dropdown toggle nese **ikonu** (`Icon`) a **`title` popisující akci**, ne
  podstatné jméno („Zobrazit alba", ne „Alba"; klíče `nav.titles.*`); ikony jsou dekorativní
  (`aria-hidden`) vedle viditelného textového labelu. Dropdown se skryje celý, když má uživatel
  skryté všechny jeho položky (Tools/Admin u viewera); rodičovské menu má **active stav** (`active`
  prop), když je aktuální route některé z jeho dětí (`pathMatches` ctí i detail sub-cesty jako
  `/albums/{uid}`) — skládá se z `Dropdown`+`Dropdown.Toggle as={NavLink}` (ne `NavDropdown`, ten
  spotřebuje prop `title` na obsah toggle, takže by nezbyl na tooltip); položky v mobilním burger
  menu expandují inline s tap-targety (`kukatko-tap-target`),
  `Icon` (**jediná ikonová sada** aplikace: bootstrap-icons glyf jako `<i class="bi bi-{name}">`,
  font se importuje globálně v `main.tsx`; union `IconName` drží slovník použitých ikon, takže překlep
  je chyba překladu; vždy `aria-hidden` vedle viditelného labelu),
  `LanguageSwitcher` (button group cs/en, `aria-pressed` na aktivní; **nesedí v navbaru** —
  bydlí v sekci Jazyk na `AccountPage`, protože tuhle instanci používají jen Češi a trvalé
  místo v liště by bylo plýtvání. Volbu persistuje i18next language detector do localStorage),
  `KeyboardShortcutsHelp` (v navbaru: ikonka klávesnice + **modal nápovědy zkratek** — otevře se
  `?` (Shift+/) kdekoli nebo klikem, vypíše všechny zkratky seskupené dle kontextu (Mřížka / Detail)
  ze `lib/shortcuts.ts` `SHORTCUT_GROUPS`, zavře Escapem/křížkem),
  `EmptyState` (**sdílený placeholder prázdné kolekce**: ikona v kulaté jámě, krátký titulek,
  jednořádkový hint a volitelné akční tlačítko, vycentrované v prostoru, který by kolekce zabrala.
  Props `title` (povinné), `hint?`, `icon?` (default = obrys prázdného rámečku, `aria-hidden`),
  `action?` (obvykle stejné tlačítko, které nabízí naplněný pohled), `size?` `'md' | 'sm'`
  (kompaktní varianta pro dlaždici/úzký panel), `className?`. Titulky/hinty si **překládá volající**
  (každá stránka má vlastní i18n klíč, aby copy byla konkrétní). Nahradil holé jednořádky
  „Bez štítků." / „Bez náhledu" i všechny ručně skládané `text-center py-5` bloky napříč
  stránkami (`LibraryPage`, `SearchPage`, `AlbumsPage`, `AlbumDetailPage`, `LabelsPage`,
  `LabelDetailPage`, `PeoplePage`, `SubjectPage`, `PlacesPage`, `MapPage`, `FavoritesPage`,
  `SavedSearchesPage`, `ClustersPage`, `DuplicatesPage`, `TrashPage`, `SlideshowPage` (s akcí
  „Zpět"), `ImportPage`) i v komponentách (`AlbumTile`/`SubjectTile` cover placeholder,
  `OrganizePanel` bez štítků, `Outliers`). Bloky se objeví přes `.kk-appear`, které
  `prefers-reduced-motion` vypne. Testy: `EmptyState.test.tsx`);
  `components/upload/` = `DropZone` (drag-and-drop zóna + file input `multiple`
  `accept="image/*,video/*"` → mobilní galerie + tlačítko **Vyfotit** `capture="environment"`),
  `UploadItem` (řádek fronty: jméno+velikost, progress-bar, status badge, near-duplicate
  varování, remove/retry akce); `components/library/` = `PhotoTile`
  (čtvercová lazy-load dlaždice → `/photos/{uid}`, badge soukromé, **play badge + délka** u
  videa/live fotky (`▶` + `formatDuration`), placeholder bez
  layout-shiftu; volitelný **favorite heart** overlay `favoritable` → `FavoriteButton`;
  volitelný **rating overlay** `ratable` → kompaktní `RatingStars`+`FlagControl` (per-user
  hvězdy 0–5 + pick/reject) nad `useRating`, plus **hotkeys na fokusnuté dlaždici** `0`–`5`
  nastaví hodnocení a `p`/`r` pick/reject (`ratingHotkey`/`isTypingElement`, nefungují při psaní
  do inputu); **zamítnutá fotka** je ztlumená + má reject badge; heart i rating overlay se
  v selection módu skryjí; `src` bere **`photo.thumb_url` z payloadu** přes `useThumbSrc` a
  **nikdy** ho neskládá z UID),
  `PhotoGrid` (virtualizovaný **`react-virtuoso` `VirtuosoGrid`**,
  window-scroll, `endReached` → další stránka, footer spinner/retry; props `favoritable`/`ratable`
  prosáknou srdíčko a hvězdy/flag na dlaždice; volitelný `gridRef` (imperativní `scrollToIndex`
  handle) + `onRangeChanged` (viditelný rozsah) pro časovou osu),
  `TimelineScrubber` (**časová osa** — tenká fixní svislá datová lišta u mřížky: fetchne měsíční
  histogram přes `useTimeline(params)` (refetch při změně filtrů), každý měsíc = klikací tick
  umístěný proporčně dle `cumulative/total`, měsíční popisky přes `lib/format` `formatMonth`;
  klik/tažení skočí na měsíc přes `onJump(bucket.cumulative)`, aktivní měsíc se zvýrazní dle
  `activeIndex` (start viditelného rozsahu); overlay `position: fixed`, takže loading/prázdný
  timeline nerendruje nic a neposouvá layout, na malých šířkách se skryje přes `styles/app.css`
  `.kukatko-timeline*`; jen pro výchozí newest řazení), `FilterBar`
  (**redesign pro klidný výchozí stav + progresivní odhalení**: v hlavičce jen prominentní
  vyhledávací pole (vizuální kotva, největší prvek), řazení (vč. **dle hodnocení**) a tlačítko
  **Filtry** s odznakem počtu aktivních filtrů; pokročilé filtry (datum od/do, poloha, soukromé,
  fotoaparát, archiv, **min. hodnocení ≥1…≥5**, **flag vybrané/zamítnuté**) žijí v rozbalovacím
  panelu — na desktopu inline `Collapse`, na mobilu `Offcanvas` dle `matchMedia` (`useIsNarrow`,
  defenzivní k jsdom, kde `matchMedia` vrací `undefined`); každý aktivní filtr = odebíratelný
  **chip** (`buildChips`, `text-bg-primary` pill s křížkem, zruší jen ten filtr — dotaz `q` chip
  nemá, má vlastní pole) + jedno **„zrušit filtry"** + počet fotek; **beze změny chování** — vše
  jede přes `viewToParams`/`useUrlState`/`LibraryView`, dotaz replacuje historii, ostatní pushují;
  generický nad `LibraryView`+supersetem, props `showSearch`/`showSort` skryjí dotaz/řazení
  na search stránce (chipy/panel/zrušit fungují dál); tap-targety ~44 px přes `styles/app.css`
  `.kukatko-filter-*`;
  **tři facety, kterými se fotky reálně hledají** (prop `facets` z `useLibraryFacets`) jsou
  **vždy viditelné** pod hlavičkou, ne schované v panelu: **Rok** = prostý `<select>`
  („Libovolný rok" + `{{year}} ({{n}})` z `GET /photos/years`, katalog má vždy jen hrstku let),
  **Album** a **Štítek** = `SearchableSelect` (obě kolekce rostou bez omezení).
  Inline pole **„filtrovat dle názvu/popisu"** (`q`) zůstává rychlým zúžením mřížky; vedle něj
  **zřetelný odkaz na `/search`** pro skutečný fulltext + sémantické hledání (`searchHref` nese
  aktuální `q`) — režimy hledání se tu **nezdvojují**), `SearchableSelect`
  (`components/library/`, jednovýběrový facet, do kterého se dá psát: v klidu ukazuje volbu,
  focus otevře celý seznam, psaní ho zúží **case- i diakritika-insensitive** přes `lib/text`
  `foldedIncludes` (`namesti` najde `Náměstí`, stejně jako backendový `immutable_unaccent`);
  vedoucí řádek „libovolné" facet zruší, klávesnice Up/Down/Enter/Esc, combobox/listbox ARIA,
  strop `MAX_SUGGESTIONS` (50) rendrovaných návrhů; nikdy nevytváří položky —
  zrcadlí `AddAutocomplete`), `filterChips.ts` (pure `buildChips(view, t, {facets?, includeQuery?})`
  → `FilterChip{key,label,clear}` pro každý aktivní filtr; `facets` pojmenují album/štítek titulkem
  místo UID (chybějící → raw UID, chip nikdy není prázdný), `includeQuery` zapíná chip pro `q`
  — filter bar ho vypíná (má vlastní pole), **prázdný stav zapíná** (čtenář u nuly výsledků musí
  vidět všechny filtry, které ho tam dostaly); délka pole = počet aktivních filtrů na odznaku),
  `SimilarPhotos` (znovupoužitelný horizontálně scrollovatelný pruh
  podobných fotek nad `GET /photos/{uid}/similar` přes `fetchSimilar`, odkazy na detail,
  empty-friendly + loading/error, refetch při změně `uid`),
  `FavoriteButton` (heart toggle nad `useFavorite` — **optimistický** per-user favorite
  s rollbackem; bez role-gate, smí každý přihlášený; jako overlay na dlaždici je sibling
  linku, takže klik nenaviguje), `RatingStars` (pure controlled 0–5 hvězd; klik na aktuální
  hodnocení maže na 0; bez `onRate` read-only display) + `FlagControl` (pure controlled pick/
  reject toggle, klik na aktivní flag maže na `none`; oba sibling linku → klik nenaviguje),
  `GridSkeleton` (placeholder mřížka při prvním načtení); `PhotoTile`+`PhotoGrid` podporují
  volitelný **selection mód** (props `selectable`/`selected`/`onToggleSelect`, resp. `selection`;
  heart i rating overlay se v selection módu skryjí),
  `components/organize/` = `AlbumTile` (karta alba: **efektivní obálka** `cover_uid`
  (ručně zvolená, jinak nejnovější fotka alba — počítá backend) / název / **rozsah let**
  přes `formatCaptureRange` (jen když album má datované fotky) / počet → `/albums/{uid}`;
  `EmptyState` až pro album bez fotek),
  `AlbumEditModal` (create/rename alba: název/popis/soukromé), `LabelEditModal` (create/rename
  štítku: jméno/priorita), `ReorderableGrid` (ne-virtualizovaná drag-and-drop mřížka + šipky pro
  přeřazení alba, controlled přes `onReorder`), `SelectionBar` (sticky toolbar výběru: počet +
  akce + zrušit), `BulkEditModal` (**hromadná úprava** výběru přes `POST /photos/bulk`: add/remove
  alba, add/remove štítku, set/clear popisu, set/clear polohy, soukromé, archiv, oblíbené — set/clear
  páry jako samostatné módy; klientská validace souřadnic + „aspoň jedna změna"; po aplikaci
  **per-foto result summary** z odpovědi),
  `pages/` (`LoginPage`, `AccountPage` = identita/role, **sekce Jazyk** (`LanguageSwitcher` +
  hint, `account.language*`) a změna vlastního hesla, **plus technický stav aplikace**
  (`GET /healthz` badge + verze, bez commit hashe) v malém ztlumeném řádku dole — status i jazyk
  sem přišly odjinud (z úvodní stránky, resp. z navbaru): patří tam, kde je uživatel hledá, ne
  před fotky ani do prime místa v liště,
  `LibraryPage` = hlavní foto-knihovna **a zároveň úvodní stránka aplikace** (routa `/`):
  `FilterBar` nad virtualizovanou nekonečně-scrollující
  mřížkou, loading/empty/error stavy, celý pohled (filtry+řazení) v URL, srdíčka **i hvězdy/flag**
  na dlaždicích (favoritable+ratable, rating hotkeys na fokusnuté dlaždici), tlačítko **Promítání**
  (`slideshowHref` → `/slideshow` s aktuálními filtry/řazením),
  **dva různé prázdné stavy** — s aktivními filtry „Nenalezeny žádné fotky", jehož hint
  **vyjmenuje aktivní filtry** (`buildChips(..., {facets, includeQuery: true})` spojené ` · `,
  album/štítek titulkem, ne UID) a nabídne je jedním tlačítkem zrušit,
  bez filtrů „Zatím tu nejsou žádné fotky" s CTA na `/upload` (editor/admin; viewer dostane jen
  vysvětlující větu), rozlišené přes `hasActiveFilters(view)`,
  `LibraryRedirect` = shim pro vysloužilou routu `/library`: `<Navigate replace>` na `/` s doslova
  zachovaným `search`+`hash` (staré záložky a odkazy fungují, `replace` zabrání odskočení Zpět),
  plus **časová osa** (`TimelineScrubber`) vedle mřížky pro rychlé skoky na měsíc — mřížka
  vystaví `gridRef`+`onRangeChanged`, skok jede přes `useGridJump` (donačte stránky, když měsíc
  leží za načtenou částí), zobrazí se jen pro výchozí newest řazení a mimo režim výběru,
  plus pro editory **režim výběru** (`Vybrat`/`Vybrat vše`) → `BulkEditModal`
  (hromadná úprava metadat přes bulk API), plus tlačítko **Uložit pohled** (`SaveSearchModal` →
  `createSavedSearch` s aktuálním view objektem jako `params`),
  `SavedSearchesPage` = `/saved` (jakýkoli přihlášený) „Moje uložená hledání": seznam uložených
  pohledů aktuálního uživatele, každý odkaz otevírá přesně obnovený pohled (`savedSearchHref`), plus
  přejmenování (`SaveSearchModal`) a **optimistické mazání** + empty state,
  `FavoritesPage` = `/favorites` oblíbené aktuálního uživatele: stejná mřížka/filtry jako knihovna
  scopnutá `favorite=true`, srdíčka pro odebrání z oblíbených + hvězdy/flag na místě (ratable),
  `AlbumsPage` = `/albums` mřížka karet alb + `Nové album` (editor/admin),
  `AlbumDetailPage` = `/albums/:uid` hlavička + tlačítko **Promítání** (všem) + editorské akce
  (upravit/smazat/vybrat/přeřadit) nad
  fotomřížkou scopnutou na album (`useScopedPhotos` + `FilterBar` + URL stav); přeřazení přes
  `ReorderableGrid`→`PATCH /albums/{uid}/order`, výběr → odebrat z alba / nastavit cover,
  `LabelsPage` = `/labels` seznam štítků s počty + create/rename/delete (editor/admin),
  `LabelDetailPage` = `/labels/:uid` fotomřížka scopnutá na štítek (`useScopedPhotos` + `FilterBar` + URL)
  + tlačítko **Promítání**,
  `SearchPage` = sémantické/hybridní/fulltext hledání: prominentní debouncované (350 ms)
  vyhledávací pole + přepínač režimu (`q`+`mode` v URL), stejná virtualizovaná mřížka jako
  knihovna + sdílený `FilterBar` (bez dotazu/řazení), `degraded` → neblokující upozornění
  (sidecar offline), idle/loading/empty/error stavy, plus nad mřížkou **cross-entity sekce**
  (`GlobalSearchSections`) s chipy shodných alb/lidí/štítků (grouped `GET /search/global`), aby
  textový dotaz vynesl i nefotkové entity, plus v hlavičce **jediný vstupní bod uložených hledání**
  (`SavedSearchesDropdown` — vypsat, otevřít, „Spravovat" → `/saved`) vedle tlačítka **Uložit pohled**
  (`SaveSearchModal` — `params` nese i `mode`, takže obnova míří na `/search`),
  `UploadPage` = multiupload (drag-and-drop + galerie/fotoaparát na mobilu): `DropZone`
  nad frontou `UploadItem`, per-file progress/status, souhrn počtů, start/clear/retry-failed,
  po dokončení odkaz na nově nahrané fotky (`/?sort=added`, přes `LIBRARY_PATH`),
  `ImportPage` = `/import` (jen admin) admin konzole importu/migrace: dvě sekce (PhotoPrism,
  photo-sorter) s tlačítkem **Spustit import** (gate na `sources` flagy), živý průběh běžícího běhu
  (spinner + counts imported/updated/skipped/failed) a stav fronty na pozadí (`GET /jobs/stats`),
  plus tabulka **historie běhů** (`import_runs`: zdroj/začátek/konec/stav/počty/chyba); polluje
  `GET /import/runs` + `GET /jobs/stats` po 3 s, 409 → „už běží", confirm před prvním (velkým) během
  zdroje, sebe-gate na `isAdmin`,
  `MaintenancePage` = `/maintenance` (jen admin) konzole údržby knihovny: tlačítko **Spustit kontrolu**
  (`GET /maintenance/scan`) → souhrn totálů + tabulka nálezů (počet + vzorky per třída, nebo „knihovna
  konzistentní"), checkboxy oprav (náhledy/embeddingy/obličeje/hashe/import osiřelých — anotované
  zbývajícím počtem z poslední kontroly) → **Spustit opravy** (`POST /maintenance/repair`) s výsledným
  souhrnem, plus stav fronty na pozadí (`GET /jobs/stats` polluje po 3 s) jako progress; sebe-gate na
  `isAdmin`,
  `SystemStatusPage` = `/system` (jen admin) **system-status dashboard**: auto-refresh (polling 5 s)
  `GET /system/status` → kartová mřížka (DB, embeddingy, fronta jobů, záloha, importy, úložiště,
  verze) s **rychlými akcemi** — *znovu zařadit mrtvé úlohy* (`requeueDeadLetterJobs`: list dead →
  per-job `POST /jobs/{id}/requeue`), *spustit zálohu* (`POST /backup`), odkazy na flow importu
  (`/import`) a kontroly údržby (`/maintenance`); **box offline** + čekající embeddingy → zvýrazněná
  hláška „doženou se po návratu"; loading/error/notice stavy, sebe-gate na `isAdmin`,
  `PhotoDetailPage` = `/photos/:uid` **bohatý detail fotky**: velký náhled (`fit_1920`)
  reflektující uložený nedestruktivní edit (CSS) — u **videa** místo obrázku `VideoPlayer`
  (`components/photo/`, HTML5 `<video controls>` nad range endpointem `…/video`, poster `fit_1920`,
  klávesy/fullscreen/touch zdarma, fallback na stažení když codec prohlížeč neumí), u **live fotky**
  `LivePhoto` (still + „Live" badge, motion klip se přehraje při hover/podržení/focusu); **klik na
  still náhled otevře fullscreen lightbox** (`Lightbox` v `components/photo/` + `lightbox.css`):
  fotka na celou obrazovku (contain) na tmavém pozadí s uloženým editem, **velké šipky vlevo/vpravo**
  listující stejné pořadí/scope jako detail (vlastní `usePhotoNeighbors` nad `neighborParams`, stop
  na koncích), klávesy ←/→ + Esc, swipe na mobilu, close křížkem (44px tap-target) i klikem na pozadí,
  přednačtení sousedů (`new Image()` na `fit_1920`), fetch title+editu zobrazené fotky při navigaci;
  lightbox si listuje **interně bez změny URL** a při zavření předá aktuální uid zpět → detail obnoví
  URL (`navigate` replace), takže Zpět vždy funguje; video/live neotevírá image-lightbox (mají vlastní
  nativní fullscreen), a **detailové klávesové zkratky (←/→/Esc/rating hotkeys) jsou při otevřeném
  lightboxu vypnuté** (`useKeyboardShortcuts({enabled:!lightboxOpen})`), aby je ovládal lightbox;
  **prev/next navigace** respektující pořadí
  zdrojového výpisu (`usePhotoNeighbors` pageuje stejný `GET /photos` se scope+filtry z URL),
  deep-linkovatelný + **Zpět** na zdrojový pohled (`lib/detailView` `backHref`/`detailToParams`/
  `detailQueryString`), v hlavičce `RatingStars`+`FlagControl` (per-user hvězdy 0–5 + pick/reject
  nad `useRating`) a `FavoriteButton`, plus **rating hotkeys** `0`–`5`/`p`/`r` na document (mimo
  psaní do inputu), tlačítka **Stáhnout originál** /
  **Stáhnout upravenou** (`downloadUrl`), interaktivní `FaceOverlay` (pojmenování obličejů),
  pruh `SimilarPhotos` a pravý panel se záložkami (`components/photo/`): **Informace**
  (`MetadataPanel` = view/edit title/description/notes/taken_at + camera/lens/EXIF + **vizuální
  location picker** (nahradil holá lat/lng pole): jedno tolerantní pole souřadnic parsované
  pure helperem `lib/coordinates` (`parseCoordinates`→`{lat,lng}`|error / `formatCoordinates`;
  **desetinné stupně** `49.1234, 16.5678` (komma/mezera, ±), **DMS** `49°7'24.2"N 16°34'12.5"E`,
  **stupně-desetinné-minuty** `49°7.4'N, 16°34.2'E`, tolerantní k mezerám/unicode primám/'',
  hemisféry N/S/E/W i znaménka, axis reorder dle hemisfér, range check) nad **`LeafletMap` picker
  módem** (nová prop `picker={position,onPick}`: draggable marker + click-to-place nad mapy.com
  tile proxy, panTo jen u parse-driven změny, ne u klik/drag); **obousměrný sync** (text→marker,
  marker→kanonický text desetinných stupňů), **neplatný text = inline chyba + `disabled` Save**
  (nikdy nePATCHne smetí), tlačítko vymazat polohu (lat/lng null), bez souřadnic mapa nad ČR;
  PATCH přes `updatePhoto`; `OrganizePanel` = inline add/remove alb a štítků přes organize API,
  přidání jede přes **`AddAutocomplete`** (`components/photo/`, type-to-filter combobox nad
  react-bootstrap primitivy, bez nové závislosti — nahradil dřívější `Form.Select` dropdown;
  filtruje klientsky **case/accent-insensitive** přes `lib/text` `foldText`/`foldedIncludes`,
  klávesnice ↑/↓/Enter/Esc + klik, „nic neodpovídá" stav, ~44px tap-targety, ARIA combobox/listbox;
  volitelná prop `onCreate` přidá poslední řádek „Vytvořit «dotaz»", když dotaz nesedí na žádnou
  existující položku — vrací `Promise<boolean>`, `true` vyčistí input, `false` **ponechá napsaný
  text** k opakování; **štítek jde založit rovnou z fotky** — pole se štítky se renderuje i nad
  prázdným seznamem, jinak by první štítek v katalogu nešel vytvořit, a `createAndAttachLabel`
  udělá `createLabel` + `attachLabel` v jedné akci; shodu jména hledá `foldedEquals` nad načteným
  seznamem, takže existující štítek jen připojí místo kolize na unikátním slugu; alba se odsud
  nezakládají, nesou typ/obálku/privátnost — ta patří na stránku Alba)),
  **Poloha** (`PhotoLocation` = Leaflet mini-mapa nad mapy.com proxy + on-demand reverse-geocode
  `reverseGeocode` + clear location) a **Úpravy** (editor/admin: `EditPanel` = rotace/jas/kontrast/
  crop s živým CSS preview, `PUT /photos/{uid}/edit` přes `saveEdit`); viewer vidí read-only
  (žádná záložka Úpravy, žádné edit akce, `FaceOverlay` readOnly); `lib/photoEdit` = pure helpery
  edit→CSS (`editPreviewStyle`/`editFilter`/`editTransform`/`cropClipPath`/`isIdentityEdit`/
  `rotateRight`/`hasCrop`/`NEUTRAL_EDIT`),
  `PeoplePage` = `/people` index osob: responzivní mřížka `SubjectTile` (cover/jméno/počet
  fotek), editorům odkaz na review shluků,
  `SubjectPage` = `/people/:uid` stránka osoby: hlavička (jméno/typ + edit přes
  `SubjectEditModal`), paginovaná galerie (`useSubjectPhotos` + `SubjectPhotoTile` se
  „set as cover" akcí editorům), a sekce `Outliers` (jen editor/admin),
  `ClustersPage` = `/people/clusters` (editor/admin) review fronta nepojmenovaných shluků:
  `ClusterCard` (reprezentant + ukázky + odebrání zatoulaného obličeje + jednorázové pojmenování
  celého shluku) v `Row`/`Col` mřížce, optimistické odebrání po pojmenování,
  `MapPage` = `/map` mapový pohled: geotagované fotky jako shlukované markery nad mapy.com
  dlaždicemi (Leaflet), přepínač podkladu + filtry (datum/archiv/soukromé) v `MapFilterBar`,
  stav (mapset/viewport/filtry) v URL — posun/zoom zapisuje viewport bez refetche, změna filtru
  dotáhne GeoJSON; klik na marker → detail fotky; loading/empty/error stavy,
  `PlacesPage` = `/places` procházení knihovny dle lokality: jedním fetchem `fetchPlaces()` natáhne
  hierarchii zemí→měst s počty; **drill v URL** (`?country=&city=` přes `useUrlState` nad
  `PlacesView` = `LibraryView`+`country`/`city`, takže Zpět prochází úrovně) — úroveň 1 seznam zemí
  (`ListGroup`), úroveň 2 města vybrané země (z nested dat, bez refetche), úroveň 3 fotomřížka
  scopnutá na `{country,city}` přes `useScopedPhotos` (enabled až po výběru města) + sdílený
  `FilterBar` + breadcrumb Místa/země/město; loading/empty/error stavy,
  `SlideshowPage` = `/slideshow` fullscreen promítání (mimo `Layout`, bez navbaru): čte scope
  (`?album=`/`?label=`/žádné) + filtry/řazení z URL (stejný stav jako mřížka), pageuje přes
  `usePaginatedPhotos` (`fetchPhotos`, velké sady se nenačítají najednou), řídí `useSlideshow` +
  `useSlideshowSettings`, renderuje loading/empty/error stavy nebo `Slideshow`; exit → `navigate(-1)`
  (fallback na zdrojový pohled), takže Zpět funguje,
  `TrashPage` = `/trash` (editor/admin) koš: archivované fotky (`useScopedPhotos`-style listing přes
  `usePaginatedPhotos` scopnutý `archived=only`) v mřížce `TrashCard` s `FilterBar`, **obnova**
  (`unarchivePhoto`) a **trvalé mazání** (`purgePhoto`) jednotlivě i hromadně (`useSelection`
  `SelectionBar`), **Vyprázdnit koš** (`emptyTrash`), každá trvalá akce přes potvrzovací `Modal`;
  `fetchTrashInfo` dotáhne retenci pro odpočet na kartách,
  `DuplicatesPage` = `/duplicates` (editor/admin) kontrola duplikátů: stránkovaný seznam skupin
  (`fetchDuplicates`, „načíst další" přes `next_offset`) v `DuplicateGroupCard`; per skupina uživatel
  vybere keeper a **archivuje zbytek** přes `bulkUpdatePhotos(archiveUids,{archive:true})` (zbytek do
  koše, vratné) → skupina zmizí + success alert s počtem, nebo skupinu **odmítne** („není duplikát",
  jen lokálně skryje); 503 → „nedostupné", loading přes `GridSkeleton`, error s retry,
  `NotFoundPage`),
  `components/savedsearch/` = `SaveSearchModal` (modal pro pojmenování při uložení nového pohledu
  i přejmenování existujícího uloženého hledání) + `SavedSearchesDropdown` (dropdown v hlavičce
  `SearchPage` — **ne v navbaru**; lazy fetch při otevření, položky otevírají uložený pohled přes
  `savedSearchHref`, „Spravovat" → `/saved`, loading/empty/error stavy uvnitř menu);
  `components/search/` = `GlobalSearchSections` (kompaktní cross-entity sekce nad photo mřížkou
  search stránky: přes `useGlobalSearch(query)` natáhne grouped `GET /search/global` a vyrenderuje
  chipy shodných **alb/lidí/štítků** odkazující na entitu; nezávislé na photo fulltext/semantic
  hledání pod ním, nerendruje nic dokud nepřijde aspoň jedna nefotková shoda — prázdný dotaz /
  probíhající hledání / jen-fotky shoda nepřidá žádné chrome);
  `components/trash/` = `TrashCard` (dlaždice archivované fotky: náhled + odpočet do auto-purge přes
  `trashCountdown` + restore/delete akce + výběr v selection módu);
  `components/duplicates/` = `DuplicateGroupCard` (karta skupiny: členové vedle sebe s náhledem/
  rozměry/velikostí/`taken_at`/vzdálenostmi, radio výběr keepera (default navržený), badge `reason`,
  akce **Archivovat ostatní** / **Není duplikát**, busy stav);
  `components/slideshow/` = `Slideshow` (prezentační fullscreen stage: aktuální fotka v preview
  velikosti `fit_1920` s CSS přechodem dle `settings.effect`, přednačítání sousedních snímků přes
  `new Image()`, ovládání předchozí/play-pause/další/fullscreen/nastavení/zavřít + titulek + pozice
  `n/total`; klávesy ←/→ / mezerník / Esc / F a dotykový swipe; Fullscreen API feature-detected;
  panel nastavení = výběr efektu + rychlosti) + `slideshow.css` (keyframes `slideshow-fade`/
  `slideshow-slide`, fullscreen layout);
  `components/map/` = `LeafletMap` (imperativní Leaflet most: dlaždicová vrstva na **backend
  proxy** `/api/v1/map/tiles/{mapset}/{z}/{x}/{y}{r}` (klíč server-side, `{r}`→`@2x` na retině),
  **povinné mapy.com prvky** — attribution „© Seznam.cz a.s. a další" → `/copyright` a klikatelné
  **logo** vlevo dole → `mapy.com`; `leaflet.markercluster` shluky (klik přibližuje), markery
  z GeoJSON, popup s náhledem → detail fotky; jednorázový setup, výměna URL dlaždic při změně
  mapsetu, přestavba markerů při změně fotek, fit-bounds na markery), `MapFilterBar` (přepínač
  podkladu basic/outdoor/aerial + datum od/do, archiv, soukromé, počet, zrušit filtry);
  `components/people/` = `SubjectTile`/`SubjectPhotoTile`/`SubjectEditModal`, `FaceThumb`
  (čtvercový výřez obličeje z thumbnailu fotky dle normalized bbox přes `faceCropStyle`),
  `FaceOverlay`+`FaceAssignPanel` (boxy přes obrázek z normalized bbox přes `faceBoxStyle`,
  klik → panel s návrhy (one-tap accept) + free-text jméno; optimistický update + refetch),
  `ClusterCard`, `Outliers` (žebříček podezřelých obličejů s one-tap unassign);
  `auth/` (`AuthContext`/`useAuth` + `AuthProvider` = boot `GET /auth/me`,
  vystavuje `user`/`role`/`login`/`logout`/`refresh`/`canWrite`/`isAdmin`; `ProtectedRoute` =
  `RequireAuth` + `RequireRole` route guardy), `hooks/` (`usePaginatedPhotos` = sdílený
  paginovaný infinite-scroll loader nad libovolným `PageFetcher`: akumuluje stránky,
  `loadMore`/`retry`, reset+refetch při změně dotazu/`key`/`enabled`, ruší in-flight requesty
  a ignoruje stale odpovědi, vystavuje i `mode`/`degraded`; `enabled:false` → `idle` stav bez
  requestu; `usePhotoLibrary` = tenká obálka nad ním nad `fetchPhotos`; `usePhotoSearch` =
  obálka nad `searchPhotos` s injektovaným `mode`, vypnutá při prázdném `q` (idle);
  `useUploadQueue` = fronta uploadu: `addFiles` (dedup jméno+velikost+mtime)/`removeItem`/
  `start`/`retry`/`retryFailed`/`clear`, konkurenční strop `MAX_CONCURRENT_UPLOADS` (3),
  per-file status+progress, souhrn počtů, `createdUids` pro odkaz do knihovny; auto-drainuje
  frontu efektem po `start`/retry, ruší běžící uploady při unmountu;
  `useSubjectPhotos` = obálka nad `usePaginatedPhotos` nad `GET /subjects/{uid}/photos`
  (galerie osoby, reset+reload při změně `uid`); `useScopedPhotos` = obálka nad `usePaginatedPhotos`
  nad `GET /photos` scopnutým na album/štítek/**lokalitu** (`PhotoScope` `{album?,label?,country?,city?}`
  + filtry/sort z URL, options `{reloadKey?,enabled?}` — `reloadKey` pro refetch po mutaci, `enabled:false`
  → idle bez fetche, např. Places před výběrem města); `useMapPhotos` = jednorázový (nestránkovaný) loader
  GeoJSON feedu geotagovaných fotek nad `fetchMapPhotos` (`status` loading/ready/error, `retry`,
  ruší in-flight + ignoruje stale při změně filtrů);
  `useLibraryFacets(params)` = loader nabídek tří facetů knihovny → `LibraryFacets{years,albums,labels}`:
  roky přes `fetchPhotoYears` **refetchuje při změně filtrů** (rok drží méně fotek, jakmile přibude
  štítek), ale **`year` z requestu strhává** (backend ho stejně ignoruje — facet nesmí zúžit vlastní
  nabídku — a bez něj zůstane request identický, takže přepínání let nerefetchuje); alba a štítky
  jsou katalogové, načtou se **jednou**. Neúspěch nechá ten seznam **prázdný** místo chyby (facet,
  který nemá co nabídnout, je degradovaný bar, ne rozbitá stránka — chyby načtení hlásí mřížka);
  in-flight requesty ruší `AbortController` při změně `params`/unmountu, takže pomalá odpověď
  nepřepíše novější (`params` si volající memoizuje z URL stavu); `useTimeline(params)` = jednorázový loader
  měsíčního date-histogramu nad `fetchTimeline` (`buckets`/`total`/`status`, refetch při změně
  filtrů, ruší in-flight + ignoruje stale — podklad `TimelineScrubber`); `useGlobalSearch(query,
  debounceMs?)` = debouncovaný (default 250 ms) grouped global-search loader nad `globalSearch`
  (`status` idle/loading/ready/error + `result`, prázdný dotaz → idle bez requestu, ruší in-flight +
  ignoruje stale — podklad `GlobalSearchSections`); `useGridJump({gridRef,
  loadedCount,hasMore,loadingMore,loadMore})` = vrátí `jumpTo(index)`, který skočí mřížkou na foto
  index přes `VirtuosoGridHandle.scrollToIndex` a **nejdřív donačte stránky**, když cíl leží za
  infinite-scroll kurzorem (nebo clampne na poslední načtené, když už další stránky nejsou) —
  podklad skoku časové osy na měsíc před načtenou částí; `useSelection` = multi-výběr fotek v mřížce
  (`active`/`selected`/`count`/`enable`/`disable`/`toggle`/`selectMany` (select-all-in-view)/`clear`);
  `useKeyboardShortcuts(handlers,{enabled?})` = sdílené plumbing všech klávesových zkratek: jeden
  document-level `keydown` listener dispatchuje dle normalizovaného `shortcutToken(event.key)` na
  `handlers` (přes refy, bind jednou a vždy vidí aktuální closury), matched key `preventDefault`;
  **nikdy nevystřelí** při držení Ctrl/Meta/Alt, při psaní (`isTypingElement`) ani při otevřeném
  form-modalu (`isFormModalOpen`); `useGridKeyboardNavigation({count,enabled,resetKey,getColumns,
  scrollToIndex,onOpen,onToggleSelect,onToggleFavorite,hasSelection,onClearSelection})` = navigace
  mřížky nad `useKeyboardShortcuts`: drží `focusedIndex` (zvýraznění), šipky + `j`/`k`/`h`/`l` posouvají
  (vlevo/vpravo o 1, nahoru/dolů o řádek dle živého počtu sloupců) a dorolují dlaždici do view, `Enter`
  otevře, `x` vybere (zapne selection mód), `f` přepne oblíbenou, `Escape` zruší nejdřív výběr, pak
  fokus; fokus se resetuje na `resetKey` (nová filtr/sort/scope);
  `useFavorite(uid,initial)` = **optimistický** per-user favorite toggle nad `favoritePhoto`
  (`PUT`/`DELETE …/favorite`), rollback při chybě, ignoruje souběžný toggle, resync na změnu
  `uid`/server stavu; `useRating(uid,initialRating,initialFlag)` = **optimistické** per-user
  hodnocení (hvězdy) + pick/reject flag nad `ratePhoto` (`PUT …/rating` jen s měněným polem),
  `setRating`/`setFlag` s per-poli rollbackem při chybě, no-op na shodnou hodnotu, `pending` přes
  in-flight counter, resync na změnu `uid`/server stavu (mirror `useFavorite`);
  `useThumbSrc(uid,thumbUrl)` → `{src,failed,onError}` = **odolnost vůči expirované podepsané URL**:
  `thumb_url` v payloadu může být krátkodobě podepsaná adresa media Workeru (default 1 h), takže
  payload držený ve virtualizovaném seznamu nebo přečkaný přes delší nečinnost dá `<img>` adresu,
  kterou Worker odmítne. První `onError` proto **jednou** refetchne fotku (`fetchPhoto`) a zkusí to
  s čerstvě podepsanou URL; druhý pád, selhaný refetch, prázdná nebo **nezměněná** adresa (to dělá
  filesystem backend — jeho URL jsou routy a nestárnou, takže pád = fakt chybějící náhled) → `failed`
  a volající vykreslí placeholder. Nová `thumbUrl` prop (nová stránka výsledků) resetuje retry budget.
  Řeší se to takhle, **ne dlouhým TTL** — krátká životnost je celý smysl podpisu. Používá
  `PhotoTile` a `TrashCard`;
  `useSlideshow({length,hasMore,intervalMs,autoPlay?,onLoadMore?})` = řízení promítání: vlastní
  `index`+`playing`, `next`/`prev`/`play`/`pause`/`toggle`/`goTo`, auto-advance na interval
  (setTimeout, manuální nav resetuje odpočet), wrap-around, prefetch `PRELOAD_AHEAD` snímků dopředu
  přes `onLoadMore` (na konci s další stránkou počká místo zacyklení), prázdná sada = no-op, clamp
  indexu při zmenšení sady; `useSlideshowSettings` = persistentní efekt+rychlost přes
  `lib/slideshowSettings` (read once on mount, setteri zapisují do localStorage, sanitizace))),
  `lib/` (`urlState.ts` = hook `useUrlState` +
  pure `readUrlState`/`writeUrlState`: stav pohledu ↔ URL query přes History API, „Zpět vždy
  funguje"; `libraryView.ts` = typ `LibraryView` (vč. `min_rating`/`flag` a facetů `year`/`album`/`label`) +
  `LIBRARY_DEFAULTS` +
  `LIBRARY_PATH` (= `/`, kanonická routa knihovny — **knihovna je úvodní stránka**; všechny odkazy
  v appce míří sem, `/library` je jen redirect pro staré odkazy) +
  `viewToParams` (sanitizuje sort/archived/**year** — `toYear` propustí jen čtyřciferný rok, ručně
  psaná/zastaralá URL spadne na „bez filtru" místo backendové 400 —, prosákne `min_rating`/`flag`
  a UID facetů `album`/`label` (neznámé UID prostě nic nenamatchuje); `sort` union navíc
  `rating`) + `hasActiveFilters` (`{ignoreQuery}` na search stránce, zahrnuje rating/flag i facety) —
  mapování URL stavu na API params; `ratingHotkeys.ts` = pure `ratingHotkey(key)` (`0`–`5` →
  rating, `p`/`r` → pick/reject, jinak null) + `isTypingElement(target)` (input/textarea/select/
  contenteditable → hotkey se přeskočí) — sdíleno detailem fotky i fokusnutou dlaždicí;
  `shortcuts.ts` = registr klávesových zkratek + pure helpery: `shortcutToken(key)` (normalizace
  `KeyboardEvent.key` — single-char lower-case, named keys passthrough, `?` zůstává), `isFormModalOpen`
  (je otevřený `.modal.show` s form controlem? → suppress zkratek za dialogem), `HELP_SHORTCUT_KEY`
  (`?`) a `SHORTCUT_GROUPS` (grouped Grid/Detail zdroj pravdy pro nápovědu, `titleKey`/`descriptionKey`
  typované jako i18next `ParseKeys`, takže neexistující klíč je compile error);
  `searchView.ts` = typ `SearchView` (= `LibraryView` + `mode`)
  + `SEARCH_DEFAULTS` (mode `hybrid`) + `toMode` sanitizér;
  `savedSearchView.ts` = pure `isSearchParams(params)` (přítomnost `mode` rozlišuje search od library
  pohledu) + `savedSearchHref(params)` (složí `pathname?query` na `LIBRARY_PATH` nebo `/search`, minimálně
  zakóduje uložené params proti defaultům přes `writeUrlState`, ignoruje neznámé/zastaralé klíče) —
  obnova uloženého hledání na přesnou URL;
  `mapView.ts` = typ `MapView` (mapset + viewport `lat`/`lng`/`z` + filtry) + `MAP_DEFAULTS` +
  `mapViewToParams` (sanitizuje archived) + `viewportFromView`/`mapsetFromView`/`hasActiveMapFilters`
  — mapování URL stavu mapy na feed params; `mapPopup.ts` = pure `buildPopupElement` (náhled +
  odkaz na detail fotky jako popup element, plain klik → SPA navigace, modifikovaný klik projde);
  `faceGeometry.ts` = pure `faceBoxStyle` (normalized bbox → absolutní `left/top/width/height`
  v %, pro overlay) + `faceCropStyle` (čtvercový výřez obličeje z thumbnailu přes
  background-position/-size, pro `FaceThumb`);
  `coordinates.ts` = pure tolerantní parser souřadnic pro location picker: `parseCoordinates(input)`
  → `{ok:true,value:{lat,lng}}` | `{ok:false,error:'empty'|'format'|'range'}` (desetinné stupně /
  DMS / stupně-desetinné-minuty, komma/mezera oddělovač, ±/hemisféry N/S/E/W, unicode primy/`''`,
  axis reorder dle hemisfér, range check ±90/±180) + `formatCoordinates({lat,lng},precision=6)` →
  kanonický `"49.123400, 16.567800"` (round-tripuje parserem) — sdílí `MetadataPanel` picker;
  `slideshowSettings.ts` = typ `SlideshowSettings{effect,intervalMs}` + `SlideshowEffect`
  (`fade`/`slide`/`none`) + nabídky `SLIDESHOW_EFFECTS`/`SLIDESHOW_INTERVALS_MS` + `SLIDESHOW_DEFAULTS`
  + pure `readSettings`/`writeSettings`/`sanitizeSettings` (localStorage `kukatko.slideshow.settings`,
  sanitizace efektu + clamp intervalu, fallback na defaulty při chybě/nedostupném storage);
  `slideshowView.ts` = pure `slideshowHref(scope,view)` (staví `/slideshow?…` z `LibraryView` přes
  `writeUrlState` + scope `album`/`label`, default filtry vynechá — launch link promítání);
  `trashCountdown.ts` = pure `purgeCountdown(archivedAt,retentionDays,now?)` (zbývající dny do
  auto-purge z `archived_at` + retence → `{daysLeft,due}` nebo `null` když odpočet neplatí
  (nearchivovaná / retence ≤ 0 / neparsovatelné), odpočet na kartách koše);
  `format.ts` = pure `formatBytes(bytes)` (byte count → human-readable binární jednotky, např.
  `1536`→`"1.5 KB"`, neplatné→`"0 B"`) pro velikost souboru na duplicate-group kartách +
  `formatDuration(ms)` (ms → `M:SS`/`H:MM:SS`, neplatné→`"0:00"`) pro délku videa na dlaždicích +
  `formatMonth(year,month,locale)` (1-based rok/měsíc → locale-aware krátký měsíc + rok, např.
  `2026,1,'en'`→`"Jan 2026"`, mimo 1–12 → `""`) pro popisky ticků časové osy +
  `formatCaptureRange(from?,to?)` (rozsah `taken_at` alba → nejužší tvar: jeden měsíc
  `"6/2007"`, jeden rok `"2006"`, jinak `"1998–1999"` s en-dash; chybějící/neplatná mez →
  `""`, tj. album bez datovaných fotek nekreslí řádek) pro `AlbumTile` +
  **locale-aware** `formatDate(value,locale)`/`formatDateTime(value,locale)` (ISO/epoch/`Date` →
  `toLocaleDateString`/`toLocaleString` s **aktivním jazykem UI** `i18n.language`, ne výchozím
  jazykem prohlížeče; neparseovatelný vstup → původní string; používá PhotoTile/DuplicateGroupCard/
  MetadataPanel/Import/System pro datumy v cs/en formátu))),
  `services/` (`health.ts`, `auth.ts` = login/logout/me/changePassword, typy
  `User`/`Role`/`AuthSession`, `ApiError` se statusem, `canWrite`/`roleAtLeast`,
  `MIN_PASSWORD_LENGTH`; `photos.ts` = `fetchPhotos(params,signal)` nad `GET /api/v1/photos`
  (filtry/řazení/stránkování → `PhotoListResponse{photos,total,limit,offset,next_offset}`),
  `searchPhotos(params,mode?,signal)` nad `GET /api/v1/search` (mód
  `fulltext`/`semantic`/`hybrid`, odpověď navíc `mode`+`degraded`),
  `fetchSimilar(uid,limit?,signal)` nad `GET /api/v1/photos/{uid}/similar` → `SimilarPhoto[]`
  (`Photo`+`distance`; empty-friendly), typy `SimilarPhoto`/`SimilarResponse`,
  `fetchTimeline(params,signal)` nad `GET /api/v1/photos/timeline` → `Timeline{buckets,total}`
  (měsíční date-histogram, stejné filtry jako list; sort/stránkování backend ignoruje), typy
  `Timeline`/`TimelineBucket{year,month,count,cumulative}` — podklad `TimelineScrubber`,
  `fetchPhotoYears(params,signal)` nad `GET /api/v1/photos/years` → `YearsResponse{years,total}`
  (rok-histogram, stejné filtry jako list; backend ignoruje `year` sám, sort/stránkování taky),
  typy `YearsResponse`/`YearBucket{year,count}` — podklad year facetu (`useLibraryFacets`);
  `PhotoListParams` navíc `year?: string` (čtyřciferný rok), `buildPhotoQuery` ho serializuje,
  `favoritePhoto(uid,favorite,signal)` nad `PUT`/`DELETE /api/v1/photos/{uid}/favorite` (per-user
  toggle, 204, podklad optimistického `useFavorite`),
  `ratePhoto(uid,{rating?,flag?},signal)` nad `PUT /api/v1/photos/{uid}/rating` +
  `clearRating(uid,signal)` nad `DELETE …/rating` (per-user hvězdy 0–5 + pick/reject flag, 204,
  podklad `useRating`), typy `RatingUpdate`/`RatingFlag`,
  **koš** `unarchivePhoto(uid)` (`POST …/unarchive` obnova), `purgePhoto(uid)` (`POST …/purge?confirm=true`
  trvalé mazání), `emptyTrash()` (`POST /trash/empty?confirm=true` → `PurgeResult{purged,failed}`),
  `fetchTrashInfo()` (`GET /trash/info` → `TrashInfo{retention_days}`),
  `buildPhotoQuery`, `thumbUrl(uid,size,token?)`, `videoUrl(uid,token?)` (range stream pro
  `<video>`; při R2 backendu routa **302** redirectne na Workera, `<video>` redirect následuje
  při každém requestu, takže seek jede vždy proti čerstvému podpisu), `GRID_THUMB_SIZE`,
  typy `Photo` (vč. `is_favorite` + per-user `rating`/`flag` + video pole
  `duration_ms`/`video_codec`/`audio_codec`/`has_audio`/`fps` + **`thumb_url`/`download_url`**)/`PhotoListParams`
  (vč. `album`/`label` scope + **`country`/`city` place scope** + `favorite` filtr + `min_rating`/`flag` filtry)/`PhotoSort`
  (vč. `rating`)/`RatingFlag`/`ArchivedFilter`/`SearchMode`, `ApiError`.
  **Adresy médií se neskládají z UID.** Grid dlaždice i download odkaz berou `photo.thumb_url` /
  `photo.download_url` z payloadu — jen server umí URL podepsat. `thumbUrl(uid,size)` zůstává pro
  velikost, kterou payload nenese (lightbox, canvas editoru, cover podle UID) a `downloadUrl(uid,…)`
  pro **rendering nedestruktivního editu**, který umí jen aplikace;
  `organize.ts` = Albums/Labels klient: alba `fetchAlbums`/`fetchAlbum`/`createAlbum`/`updateAlbum`/
  `deleteAlbum`/`addAlbumPhotos`/`removeAlbumPhotos`/`reorderAlbumPhotos`, štítky `fetchLabels`/
  `fetchLabel`/`createLabel`/`updateLabel`/`deleteLabel`/`attachLabel`/`detachLabel`; typy
  `Album`/`AlbumCount`/`AlbumInput`/`AlbumType`/`Label`/`LabelCount`/`LabelInput`;
  `savedSearches.ts` = uložená hledání klient: `fetchSavedSearches`/`createSavedSearch(name,params)`/
  `updateSavedSearch(uid,{name?,params?})`/`deleteSavedSearch(uid)` nad `/api/v1/saved-searches`, typy
  `SavedSearch`/`SavedSearchParams` (= verbatim URL view-state `Record<string,string>`)/
  `SavedSearchUpdate`; `search.ts` = grouped **global search** klient: `globalSearch(q,signal)` nad
  `GET /api/v1/search/global` → `GlobalSearchResult{query,albums,labels,people,photos}` (top-N per
  skupina, každá vždy pole) + pure helpery `hasEntityMatches`/`isEmptyResult`, typy
  `GlobalSearchAlbum`/`GlobalSearchLabel`/`GlobalSearchPerson`/`GlobalSearchResult`; oddělené od
  photo `searchPhotos` (fulltext/semantic/hybrid), podklad `GlobalSearchSections`; `bulk.ts` =
  `bulkUpdatePhotos(uids,ops)` nad `POST /photos/bulk` (hromadná úprava výběru), typy
  `BulkOperations` (add/remove alba+štítku, set/clear caption+popisu+polohy, set_private,
  archive/unarchive, set_favorite per-user)/`BulkLocation`/`BulkResult`; `duplicates.ts` =
  `fetchDuplicates(params,signal)` nad `GET /api/v1/duplicates` (skupiny duplikátů →
  `DuplicatesResponse{groups,total,limit,offset,next_offset}`), typy `DuplicateReason`/
  `DuplicateMember`/`DuplicateGroup`/`DuplicatesParams`; úklid jde přes `bulk.ts`
  `bulkUpdatePhotos`; `upload.ts` =
  `uploadFile(file,{onProgress,signal})`
  nad **`XMLHttpRequest`** (jeden soubor/request kvůli upload-progress eventům, FormData se
  streamuje), `isAbortError`, typy `UploadFileResult`/`UploadResponse`/`UploadWarning`/
  `UploadOutcome`; `photos.ts` navíc `fetchPhoto(uid)` (detail `GET /photos/{uid}` →
  `PhotoDetail` = `Photo`+`files`+`albums`+`labels` inline chipy), `updatePhoto(uid,patch)`
  (`PATCH …` částečná editace metadat → `PhotoMetadataUpdate`, null maže nullable),
  `fetchEdit(uid)`/`saveEdit(uid,edit)` (`GET`/`PUT …/edit` nedestruktivní edit → `PhotoEdit`
  crop/rotation/brightness/contrast), `downloadUrl(uid,{original?,token?})` (URL downloadu,
  default honoruje edit, `original:true` pro originál); typy `PhotoDetail`/`PhotoAlbumRef`/
  `PhotoLabelRef`/`PhotoMetadataUpdate`/`PhotoEdit`; `people.ts` = People/face klient: subjekty
  `fetchSubjects`/`fetchSubject`/`createSubject`/`updateSubject`/`deleteSubject`/
  `fetchSubjectPhotos`, obličeje `fetchFaces`/`assignFace`, shluky `fetchClusters`/
  `assignCluster`/`removeClusterFace`, outlier `fetchOutliers`; typy `Subject`/`SubjectCount`/
  `SubjectInput`/`SubjectType`/`Bbox`/`FaceView`/`FacesResponse`/`AssignRequest`/`Suggestion`/
  `ClusterView`/`ExampleFace`/`ClusterAssignRequest`/`RemoveFaceRequest`/`OutlierResult`/
  `OutlierFace`; sdílí `ApiError`+`buildPhotoQuery` z `auth.ts`/`photos.ts`);
  `map.ts` = mapový klient: `fetchMapPhotos(params,signal)` nad `GET /api/v1/map/photos`
  (GeoJSON FeatureCollection geotagovaných fotek + `buildMapQuery`), `tileLayerUrl(mapset)` (Leaflet
  URL template na backend proxy, **bez API klíče**), `reverseGeocode(lat,lng,signal?)` nad
  `GET /api/v1/map/rgeocode` (on-demand reverse geocode pro detail fotky → `GeocodeResult`),
  `toMapset`/`MAPSETS`; typy
  `MapFeature`/`MapFeatureCollection`/`MapFeatureProperties`/`MapPhotoParams`/`Mapset`/
  `GeocodeResult`/`RegionalItem`);
  `places.ts` = klient hierarchie míst: `fetchPlaces(country?,signal)` nad `GET /api/v1/places`
  → `PlaceCountry[]` (země s počty + nested `cities`, volitelné `country` drillne do měst jedné
  země); typy `PlaceCountry`/`PlaceCity`; procházení fotek lokality jde přes sdílené
  `fetchPhotos({country,city})`;
  `import.ts` = admin import klient: `fetchImportRuns(signal)` nad `GET /api/v1/import/runs`
  (`{runs,limit,offset,sources}`), `fetchJobStats(signal)` nad `GET /api/v1/jobs/stats`,
  `startImport(source,signal)` nad `POST /api/v1/import/{photoprism|photosorter}` (409 → ApiError);
  typy `ImportSource`/`RunStatus`/`ImportCounts`/`ImportRun`/`ImportSources`/`ImportRunsResponse`/
  `StartImportResult`/`JobStats`),
  `maintenance.ts` = admin maintenance klient: `fetchMaintenanceScan(signal)` nad
  `GET /api/v1/maintenance/scan` → `ScanReport`, `runMaintenanceRepair(options,signal)` nad
  `POST /api/v1/maintenance/repair` → `RepairResult`; typy `Finding`/`ScanReport`/`RepairOptions`/
  `RepairResult`; sdílí `ApiError` z `auth.ts` a `fetchJobStats` z `import.ts` pro progress,
  `system.ts` = admin system-status klient: `fetchSystemStatus(signal)` nad `GET /api/v1/system/status`
  → `SystemStatus`, `triggerBackup(signal)` nad `POST /api/v1/backup` (409/503 → ApiError),
  `requeueDeadLetterJobs(signal)` (vylistuje `GET /jobs?state=dead` → per-job `POST /jobs/{id}/requeue`,
  vrací počet, 404/409 skip); typy `SystemStatus`/`DatabaseStatus`/`EmbeddingsStatus`/`JobsStatus`/
  `BackupStatus`/`ImportsStatus`/`StorageStatus`/`VersionInfo`; sdílí `ApiError` z `auth.ts` a `ImportRun`
  z `import.ts`,
  `i18n/` (i18next init — options jsou exportované jako `initOptions`, ať si je test může nabootit
  do vlastní instance — + `locales/{cs,en}/common.json`;
  typované klíče přes `types/i18next.d.ts` — nové stringy přidávej do **obou** locale souborů;
  **čeština default**, žádné natvrdo zapsané UI texty — vše přes `t()`. Jediný detektor je
  `localStorage` (kam píše `LanguageSwitcher` z `AccountPage`); `navigator`/`htmlTag` **záměrně
  nejsou** v `detection.order`, jinak by anglicky nastavený prohlížeč dostal při první návštěvě
  anglické UI — bez uložené volby rozhoduje `fallbackLng: 'cs'`. **Pluralizace** přes
  i18next CLDR plural sufixy: count-vázané řetězce kde se podstatné jméno shoduje s číslem mají
  formy `key_one/_few/_many/_other` (čeština) a `key_one/_other` (angličtina) — caller jen předá
  `{ count }` (např. `albums.photoCount`, `clusters.size`, `bulkEdit.title`, `duplicates.memberCount`/
  `archived`, `trash.confirm.bulk`); label-tvary s dvojtečkou/závorkou (`library.count`, `selection.count`)
  zůstávají bez plurálu. **Datumy/čísla respektují jazyk** přes `lib/format` `formatDate`/`formatDateTime`
  (`i18n.language`). **Drift-guard testy** `i18n.test.ts` (cs/en mají identické *logické* klíče po
  odstranění plural sufixu, žádné prázdné hodnoty, každý jazyk má všechny své CLDR plural kategorie,
  interpolační `{{var}}` proměnné se shodují napříč jazyky; navíc **default-language testy** nad
  čerstvou instancí z `initOptions`: prázdný localStorage → `cs` i pod anglickým prohlížečem,
  uložená volba vyhrává, změna jazyka se uloží) + `screens.test.tsx` (reprezentativní
  obrazovky — navbar + dlaždice — se vykreslí bez missing-key warningů v cs i en přes
  `cloneInstance({saveMissing})`, plural rendering 1/3/5, language-switch přepíše viditelný text)),
  `styles/tokens.css` (**design token vrstva** — jediný zdroj pravdy pro odstupy, rádiusy, elevaci,
  motion a typografickou škálu; importovaná **jednou** v `main.tsx` hned za Bootswatch CSS a **před**
  `app.css`, které tokeny konzumuje. Bootswatch Superhero zůstává základní téma — tohle je vrstva
  **nad** ním, nepřepisuje `--bs-*` proměnné globálně. Obsah: **spacing** `--kk-space-1..7` (4px
  škála), **rádiusy** `--kk-radius-sm/md/lg/pill`, **elevace** `--kk-shadow-0..3` (na tmavém tématu
  vždy dvojice: drop shadow + `inset 0 1px 0` horní highlight, jinak by stín na navy pozadí zanikl),
  **povrchy** `--kk-surface-raised` (odvozený z `--bs-body-bg`; **záměrně není** Superhero
  `--bs-card-bg` `#4e5d6c` — ta barva je zároveň `secondary`, takže `outline-secondary` tlačítko
  na ní zmizí) a `--kk-surface-sunken` (jáma pod náhledem), **motion** `--kk-duration-fast/
  base/slow` + `--kk-ease-standard`, **focus ring** `--kk-focus-ring-*`, **typografie**
  `--kk-font-size-*`/`--kk-line-height-*`/`--kk-tracking-*`.
  Sémantické třídy: **typografická škála** `.kk-page-title` (jedna na route, na `<h1>`),
  `.kk-section-title` (nadpis panelu/sekce, `<h2>`/`<h3>`), `.kk-text-body`, `.kk-text-caption`,
  `.kk-text-eyebrow` — komponenty **nenastavují vlastní `font-size`** (žádné `h3`/`h5`/`fs-5`
  utility na nadpisech, žádné inline `fontSize`); **povrchy** `.card` (ztrácí těžký okraj přes
  `--bs-card-border-color: transparent`, dostává `--kk-shadow-1`; `.border-primary` apod. stále
  fungují) a `.kk-surface`; **dlaždice** `.kk-tile` + `.kk-tile__media` (bez okraje, elevace,
  hover/focus lift na `--kk-shadow-3` — používají `AlbumTile`, `SubjectTile`, `PhotoTile`;
  `:focus-within` pokrývá `PhotoTile`, kde je fokusovatelný až vnitřní odkaz) a `.kk-tile-row`
  (řádková varianta pro seznam štítků — místo liftu se zvýrazní pozadím, protože řádek v sloupci
  nemá kam vyskočit); `.kk-tile__placeholder`; **appear** `.kk-appear` (jednorázový fade-up).
  **Focus outline se nikdy neodstraňuje** — `.kk-tile:focus-visible`/`.kk-tile__media:focus-visible`
  kreslí `outline` (přežije `overflow: hidden` náhledu). **`prefers-reduced-motion`**: token
  durations spadnou na `1ms`, lift (`transform`) a `.kk-appear` se vypnou úplně; spinnery
  a progress bary animují dál, protože nesou význam),
  `styles/app.css` (**global responzivní polish vrstva** importovaná v `main.tsx` hned za
  `tokens.css` — jen cross-cutting mobil/touch věci, které Bootstrap utility neumí: **safe-area
  insety** přes `env(safe-area-inset-*)` (fungují díky `viewport-fit=cover` v `index.html`) na
  navbaru (`.kukatko-navbar`) a hlavním kontejneru (`.kukatko-main`); guard proti vodorovnému
  scrollu/overscroll bounce (`body overflow-x:hidden`, `html overscroll-behavior-y:none`); sdílený
  **sticky-toolbar offset** `.kukatko-sticky-toolbar` (`top: navbar height + safe-area-inset-top`,
  z-index pod navbarem — in-page sticky bary jako `SelectionBar` dosednou pod navbar, ne pod něj);
  **min. tap-target** `.kukatko-tap-target` (2.75rem/44px) pro icon-only ovládání jako
  `FavoriteButton`; **app-wide touch-target floor** — `@media (pointer: coarse)` blok, který na
  dotykových zařízeních (telefon/tablet) vynutí min. 44px na `.btn`/`.form-control`/`.form-select`/
  `.nav-link`/`.dropdown-item`/`.list-group-item-action`/`.page-link` + větší `.form-check-input`,
  bez zásahu do desktop (fine-pointer) layoutu a bez per-komponentových změn (systémová oprava
  všudypřítomných `size="sm"` ovládání);
  **časová osa** `.kukatko-timeline*` (fixní svislá datová lišta u pravého
  okraje pod navbarem, absolutně umístěné ticky, floating popisek aktivního měsíce, `touch-action:
  none` pro tažení, na šířkách ≤ 575.98px skrytá); **filtr-bar** `.kukatko-filter-*`
  (`.kukatko-filter-search` = search pole roste a plní řádek hlavičky, `.kukatko-filter-sort`
  min. šířka, `.kukatko-filter-panel` = 44px tap-targety na prvcích panelu, `.kukatko-filter-chip`
  = tappable pill chip s křížkem); CSS proměnná `--kukatko-navbar-height`),
  `test/setup.ts` (jsdom **`window.matchMedia` stub** — non-matching default, jednotlivé testy ho
  můžou přepsat pro simulaci telefonu).
  Routing v `App.tsx`: tabulka rout žije v exportované `AppRoutes` (aby ji šlo v testech mountnout
  do `MemoryRouter` a ověřit samotné drátování — `App.test.tsx`), `App` ji jen obalí
  `BrowserRouter`+`AuthProvider`. `/login` veřejné, zbytek pod `RequireAuth`; `/slideshow` je pod
  `RequireAuth` ale **mimo `Layout`** (fullscreen bez navbaru), zbytek pod `Layout`
  (**`/` = `LibraryPage`** — knihovna je úvodní stránka; `/library` → `LibraryRedirect`
  (`replace` redirect na `/` se zachovaným query stringem),
  `/favorites`, `/albums`, `/albums/:uid`, `/labels`, `/labels/:uid`, `/search`, `/saved`, `/map`,
  `/places`, `/photos/:uid`, `/people`,
  `/people/:uid`, `/account`; `/upload`, `/people/clusters`, `/trash` a `/duplicates`
  navíc pod `RequireRole role="editor"` = write-only, `/import`, `/maintenance` a `/system` pod
  `RequireRole role="admin"` = admin-only). Konfig:
  `vite.config.ts` (build → `../internal/web/static/dist`, vitest jsdom, dev proxy
  `/healthz`+`/api` → `:8080`), `eslint.config.js` (strict typed), `.prettierrc.json`,
  `tsconfig*.json`.
