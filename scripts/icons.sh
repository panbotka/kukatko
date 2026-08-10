#!/usr/bin/env bash
#
# Render Kukátko's app identity (PWA icons, favicons, apple-touch-icon) from the
# two committed source SVGs in web/public/icons/.
#
#   ./scripts/icons.sh            # re-render every PNG + favicon.ico
#
# The outputs are COMMITTED — the build never downloads or generates assets, and
# `npm run build` just copies web/public/ into the bundle. Run this only after
# editing kukatko.svg / kukatko-maskable.svg, then commit the regenerated files.
#
# Rasterising is done by headless Chromium, the only renderer on this box that
# handles the gradients faithfully (ImageMagick 6 falls back to its own toy SVG
# renderer here). Each PNG is produced by screenshotting a minimal HTML page
# that sizes the SVG to the exact target box, so the geometry is Chromium's own
# and not a resample of a bigger raster.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ICONS_DIR="$REPO_ROOT/web/public/icons"
PUBLIC_DIR="$REPO_ROOT/web/public"
CHROMIUM="${CHROMIUM:-/usr/local/bin/chromium}"

# This Chromium clamps the headless window to a minimum width and subtracts a
# constant strip from the requested height, so `--window-size=192,192` yields a
# 500×105 layout viewport and screenshots the icon half-painted. Rendering into
# one generously sized window and cropping the top-left corner sidesteps the
# clamp entirely, and a crop resamples nothing.
WINDOW=900

if [[ ! -x "$CHROMIUM" ]]; then
  echo "chromium not found at $CHROMIUM (override with CHROMIUM=/path/to/chromium)" >&2
  exit 1
fi
if ! command -v convert >/dev/null 2>&1; then
  echo "ImageMagick 'convert' is required (crop + favicon.ico)" >&2
  exit 1
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

# render <source.svg> <size> <destination.png>
#
# Screenshots the SVG at exactly size×size device pixels on a transparent
# backdrop. The wrapper page zeroes the document margins and pins the <img> box
# to the top-left corner, which the crop below then lifts out verbatim.
render() {
  local src="$1" size="$2" dest="$3"
  # Fresh, uniquely named copies per render: Chromium caches file:// URLs across
  # runs, and a reused name serves the previous icon (or a broken image).
  local stem="icon-$size-$RANDOM"
  local page="$WORK_DIR/$stem.html"
  cp "$src" "$WORK_DIR/$stem.svg"
  cat >"$page" <<HTML
<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <style>
      html, body { margin: 0; padding: 0; background: transparent; }
      img { display: block; width: ${size}px; height: ${size}px; }
    </style>
  </head>
  <body><img src="$stem.svg" alt="" /></body>
</html>
HTML
  "$CHROMIUM" --headless --no-sandbox --disable-gpu --hide-scrollbars \
    --force-device-scale-factor=1 --default-background-color=00000000 \
    --window-size="$WINDOW,$WINDOW" --screenshot="$WORK_DIR/$stem.png" \
    "file://$page" >/dev/null 2>&1
  convert "$WORK_DIR/$stem.png" -crop "${size}x${size}+0+0" +repage "$dest"
  echo "  $(basename "$dest") (${size}×${size})"
}

echo "Rendering PWA icons from kukatko.svg:"
render "$ICONS_DIR/kukatko.svg" 192 "$ICONS_DIR/kukatko-192.png"
render "$ICONS_DIR/kukatko.svg" 512 "$ICONS_DIR/kukatko-512.png"

echo "Rendering maskable icons from kukatko-maskable.svg:"
render "$ICONS_DIR/kukatko-maskable.svg" 192 "$ICONS_DIR/kukatko-maskable-192.png"
render "$ICONS_DIR/kukatko-maskable.svg" 512 "$ICONS_DIR/kukatko-maskable-512.png"

# iOS composites the home-screen icon onto black and applies its own rounded
# mask, so the full-bleed square is the right master — transparent corners would
# come out as black notches under the mask.
echo "Rendering apple-touch-icon from kukatko-maskable.svg:"
render "$ICONS_DIR/kukatko-maskable.svg" 180 "$PUBLIC_DIR/apple-touch-icon.png"

echo "Rendering favicons from kukatko.svg:"
render "$ICONS_DIR/kukatko.svg" 32 "$PUBLIC_DIR/favicon-32.png"
render "$ICONS_DIR/kukatko.svg" 16 "$PUBLIC_DIR/favicon-16.png"

# favicon.ico is only for the handful of clients that request /favicon.ico
# blindly (feed readers, crawlers, old bookmarks); browsers take the SVG above.
convert "$PUBLIC_DIR/favicon-16.png" "$PUBLIC_DIR/favicon-32.png" "$PUBLIC_DIR/favicon.ico"
echo "  favicon.ico (16+32)"

echo "Done. Commit the regenerated files."
