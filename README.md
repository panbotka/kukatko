# Kukátko

Samostatná aplikace pro správu fotek — náhrada za PhotoPrism, která kombinuje to nejlepší
z PhotoPrismu a z [photo-sorteru](https://github.com/kozaktomas/photo-sorter), ale je
**robustnější a použitelnější**.

- **Jeden spustitelný binár** (Go) včetně embedovaného frontendu (React + Bootstrap/Superhero).
- **PostgreSQL + pgvector** jako jediný zdroj pravdy pro metadata i vektory.
- **Sémantické i fulltextové hledání**, podobné fotky, **rozpoznávání obličejů/lidí**.
- **Pi-first:** běží na Raspberry Pi, výpočet embeddingů deleguje na výkonný stroj (box s GPU).
- **Import z PhotoPrismu** přes API (+ stažení originálů) a **migrace dat z photo-sorteru**.
- Mapy ([mapy.com](https://mapy.com)), slideshow, alba, štítky, hromadná editace metadat,
  per-user oblíbené, dvojjazyčné UI (čeština default + angličtina), S3 zálohování.

> **Stav:** aktivní vývoj (milník M0 — základní kostra backendu). Architektura:
> [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md), vývojářský návod:
> [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).
>
> PhotoPrism zůstává **primární** systém až do ostrého přechodu na Kukátko; do té doby
> Kukátko běží paralelně a importuje z PhotoPrismu read-only.

## Rychlý start

Potřebuješ **Go 1.26+** a **golangci-lint v2**.

```bash
make check            # brána kvality: fmt + vet + lint + unit testy
make build            # zkompiluje statický binár do bin/kukatko (CGO_ENABLED=0)
./bin/kukatko serve   # HTTP server na :8080 (GET /healthz), graceful shutdown na SIGINT/SIGTERM
./bin/kukatko version # vypíše verzi a commit
```

Více v [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) (layout, make cíle, brána kvality).
