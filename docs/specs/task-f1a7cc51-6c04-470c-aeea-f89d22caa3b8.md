# Barevné rozlišení stavů na stránce /system

Na `/system` je tolik číselných sloupců, že nenulové selhání nebo právě běžící práce splyne s nulami. Čísla dostanou jednotnou barevnou logiku napříč celou stránkou.

## Requirements

- Jedno sdílené pravidlo pro barvu čísla podle stavu fronty, použité ve všech panelech stránky: `JobQueuePanel`, `RemainingWorkPanel`, `VideoEncodingPanel`, `LibraryOverview`.
- Nula je vždy potlačená a nikdy barevná — smyslem je, aby oko padlo na nenulové hodnoty.
- Nenulové hodnoty podle stavu: ve frontě = info, zpracovává se = primární barva s náznakem probíhající práce, nepovedlo se / mrtvé = danger, hotovo = neutrální (je to historie, nemá přitahovat pozornost).
- Barva nesmí být jediný nosič informace: stav je pojmenovaný v hlavičce sloupce a nenulové selhání dostane navíc ikonu.
- Kontrast musí projít v tmavém motivu, který instance používá; barvy brát ze stávajících tokenů, nezavádět nové literály.
- Dnešní zvýraznění mrtvých jobů se sjednotí s novou škálou, ať nevedle sebe nestojí dvě různá pravidla.
- Řazení řádků, tlačítka pro znovuzařazení a legenda stavů zůstávají beze změny.

## Implementation Notes

- `components/system/JobQueuePanel.tsx` má dnes barvu jen pro mrtvé joby; `STATE_COLUMNS` drží pořadí sloupců.
- `components/JobStateLegend.tsx` vysvětluje stavy slovy — legenda a barvy mají sedět.
- Footerové `JobQueueBadges` už danger pro selhání používají; nová škála s nimi má být konzistentní.
- Testy ověří, že nula barevnou třídu nemá a nenulové selhání ano.