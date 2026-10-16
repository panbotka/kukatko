Found in the v0.31 pre-release walkthrough on box staging (ba313619). Screenshot (scratchpad, may be gone): <scratchpad>/03-scrolled.png

Repro: library `/`, scroll to the end — the tiles whose thumbnail job died (staging fixtures, e.g. `<photo uid>`) show the fallback text "Náhled není dostupný" underlined like a link, and the favourite heart disc sits on top of the text's last word.

## Requirements
- The no-preview fallback keeps its text clear of the tile's overlay controls (heart, select circle) at every tile size the stepper allows.
- The text should not look like a link unless it is one (drop the underline or make it a real action).
- Visual check via a harness or screenshot; unit test if the layout is class-driven.