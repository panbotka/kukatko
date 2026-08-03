## Požadavek (od uživatele)

„Bylo by super, kdyby si mohl uživatel vybrat mezi štítky a lidmi. Nebo oboje."

Dnes fronta vždycky prokládá oba zdroje. Uživatel chce zvolit, na co se ho to bude ptát.

## Kde

- Backend: `internal/review` — `collect` volá `faceQuestions` i `labelQuestions` a
  `interleave` je slučuje. Volba zdroje musí projít až sem, aby se nepočítal sken, jehož
  výsledek se stejně zahodí (to je zároveň úspora paměti i času, viz task na OOM).
- API: `GET /api/v1/review/queue` — parametr zdroje. Zdokumentuj do `docs/API.md`.
- Frontend: stránka třídění ve `web/`. Přepínač se třemi stavy (štítky / lidé / oboje).

## Na co si dát pozor

- **Stav ve URL.** Projektové pravidlo „zpátky vždycky funguje" — volba zdroje patří do
  query parametrů + History API, ne jen do state komponenty.
- **i18n**: čeština default + angličtina, texty přes i18next.
- Session fronty je cachovaná per uživatel; změna zdroje musí frontu přestavět, ne servírovat
  starou dávku z cache.
- Prázdný výsledek musí dát srozumitelný důvod (dnes na to je `Reason`) — např. „ve zvoleném
  zdroji už není co třídit", ať to uživatel nečte jako rozbité.

## Povinnosti

Platí `CLAUDE.md`: testy povinné (unit + integrační na endpoint, Vitest na frontend),
`make check` musí projít, `docs/API.md` + `docs/FRONTEND.md`, commit + push.
