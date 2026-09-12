# Slideshow: první snímek po studeném načtení nedrží „Video se zpracovává"

Nalezeno při pre-release walkthroughu commitu `62914e4` (rozsah v0.19.0..HEAD) na box staging.

## Co je špatně

`SlideshowVideo` rozhoduje o zadržení klipu v **inicializátoru `useState`**:

```ts
const [state, setState] = useState<SlideState>(
  streamPending(photo, videoStreaming) ? 'pending' : 'starting',
)
```

`videoStreaming` pochází z `useCapabilities()`, jehož `CAPABILITIES_DEFAULT` je až do odpovědi
`GET /api/v1/capabilities` celý `false` — `CapabilitiesProvider` vykreslí děti okamžitě a flagy
doplní asynchronně. Při studeném načtení stránky se tedy první snímek může připojit s
`video_streaming: false`, `streamPending` vrátí `false`, a protože je rozhodnutí zmrazené při
mountu, už se nikdy nepřepočítá.

Důsledek pro klip, který ještě nemá streamovací rendition: místo náhledového snímku s přesýpacími
hodinami a hláškou „Video se zpracovává" se vykreslí
`<video src="/api/v1/photos/{uid}/video">` s odznakem „▶ 7:00", jako by se chystalo hrát.
Prohlížeč stáhne originál (2× HTTP 206 proti ~250MB souboru), neumí ho dekódovat a snímek po
5s grace period spadne do `failed`.

To je v rozporu s tím, co release slibuje — README: „a clip whose smooth version is still being
made shown as a picture with a note rather than one that never starts"; spec
`docs/specs/task-38551be5-eed4-4970-99ab-deb0c1391898.md`: „Stejně se chová mřížka i slideshow".

**Týká se to jen slideshow a jen jeho prvního snímku.** `VideoPlayer` (prohlížeč fotky) i dlaždice
v mřížce počítají totéž **při renderu**, takže se samy opraví, jakmile flagy dorazí — ověřeno.

## Jak to reprodukovat

Na box staging (`http://100.127.79.1:6490`, admin/adminadmin):

1. Mít video bez HLS renditionu — buď nahrát dlouhý klip, nebo dočasně smazat jeho řádek
   v `photo_hls_renditions`.
2. `agent-browser open "http://100.127.79.1:6490/slideshow?q=filename%3A<jmeno>"` — studené
   načtení, tedy vložený odkaz, reload nebo obnovená záložka.
3. Snímek ukáže „▶ 7:00" nad náhledem a `document.querySelector('video')` není null;
   `agent-browser network requests --filter /video` ukáže dva 206 proti originálu.
   Očekáváno: `<img>` s náhledem a odznakem „Video se zpracovává", a žádný request na `/video`.
4. Kontrast: studeně načíst `/slideshow?q=filename%3Ahold&sort=added` a šipkou `→` dojít
   ke stejnému klipu jako k pozdějšímu snímku — tam se vykreslí správně náhled s hláškou.

Reprodukováno 3/3 při studeném načtení na desktopu a 1/1 na mobilu (iPhone 12 emulace);
správné chování 1/1 při teplém mountu.

## Závažnost

WARN, ne blocker. Promítání se nezastaví (stav `failed` podrží snímek jeden interval a jde dál)
a **není to regrese** — před tímto releasem se každý klip prohlížeči předával tak jako tak.
Ale nová funkce tiše neplatí na té vstupní cestě, která se nejspíš sdílí a otevírá odkazem,
a na mobilu to stojí zbytečné stažení velkého souboru po datech.

## Návrh opravy

Počítat zadržení při renderu, jak to dělá `VideoPlayer` přes `videoHold`, nebo ho dosynchronizovat
v efektu při změně `videoStreaming` / `photo.hls` — místo zmrazení v inicializátoru `useState`.
Pozor na to, že `SlideshowVideo` je klíčovaný přes `photo.uid`, takže přechod na jiný snímek
remountuje; opravit se musí právě jen ten první.

Test: rozšířit `SlideshowVideo.test.tsx` o případ, kdy se komponenta připojí s
`video_streaming: false` a flag se doplní až potom — snímek musí přejít do `pending`
a nesmí sáhnout na `/video`.

## Screenshoty z walkthroughu

`/tmp/claude-1000/-home-pi-projects-kukatko/ed228130-3290-47a1-9d72-db060e00b5e6/scratchpad/23-slideshow-coldload-bug.png`
`/tmp/claude-1000/-home-pi-projects-kukatko/ed228130-3290-47a1-9d72-db060e00b5e6/scratchpad/m11-slideshow-coldload.png`
(scratchpad je dočasný — screenshoty ber jako doplněk, reprodukce výše stojí sama o sobě)