# Našeptávač dotazovacího jazyka v paletě hledání

Vyhledávací pole rozumí jazyku `klíč:hodnota`, ale uživatel musí klíče znát zpaměti. Paleta má při psaní napovídat klíče i jejich hodnoty.

## Requirements

- Našeptávání se týká jen palety hledání (`/` nebo Cmd-K), ne filtru v knihovně.
- Rozepsaný token bez dvojtečky nabídne klíče dotazovacího jazyka s vysvětlením, co dělají, filtrované podle napsaného prefixu.
- Po dvojtečce se nabízejí hodnoty podle klíče: výčtové klíče nabídnou své hodnoty, ano/ne klíče nabídnou ano/ne, a `album:`, `label:` a `person:` našeptají skutečné názvy z knihovny.
- Enter nebo Tab na nabídce doplní token a rovnou spustí hledání s celým dotazem.
- Šipky nahoru/dolů chodí po nabídce, Esc ji zavře a napsaný text nechá být.
- Našeptávač doplňuje jen právě rozepsaný token; ostatní už napsané filtry zůstanou beze změny.
- Stávající obsah palety — historie hledání a přímé výsledky alb, štítků, lidí a fotek — nezmizí; nabídka klíčů se řadí nad ně, dokud se píše filtr.
- Aliasy se nabízet nemusí, ale napsané fungují dál.
- Seznam klíčů nesmí být ručně opsaný text, který se časem rozejde s parserem: buď ho vydává backend, nebo test selže, jakmile se seznamy liší.

## Implementation Notes

- Klíče jsou konstanty v `internal/query/query.go`, hodnoty výčtů v `internal/query/parse.go`.
- Paleta je `web/src/components/search/SearchCommand.tsx`.
- Názvy alb, štítků a lidí umí `GET /search/global` — nevymýšlet nový endpoint, pokud tenhle stačí.