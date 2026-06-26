package thumb

// resizeMode selects how a source image is fitted into a named size.
const (
	// modeFit scales the image down so its longest side is at most Max,
	// preserving aspect ratio; it never upscales.
	modeFit = "fit"
	// modeCropSquare center-crops to a square then resizes to Max × Max.
	modeCropSquare = "crop-square"
)

// sizeSpec describes a single named thumbnail size in the registry.
type sizeSpec struct {
	// Max is the longest side for modeFit, or the square side for modeCropSquare.
	Max int
	// Quality is the JPEG encoder quality (1-100).
	Quality int
	// Mode is modeFit or modeCropSquare.
	Mode string
}

// sizes is the read-only registry of thumbnail sizes. Callers reference a size
// by its string name (e.g. "fit_1920"). The set is intentionally small and
// easy to extend: add an entry here and its slot in sizeOrder and every part of
// the pipeline (cache layout, generation, API) picks it up automatically.
var sizes = map[string]sizeSpec{
	"fit_720":  {Max: 720, Quality: 90, Mode: modeFit},
	"fit_1280": {Max: 1280, Quality: 90, Mode: modeFit},
	"fit_1920": {Max: 1920, Quality: 90, Mode: modeFit},
	"fit_2560": {Max: 2560, Quality: 88, Mode: modeFit},
	"fit_3840": {Max: 3840, Quality: 88, Mode: modeFit},
	"tile_100": {Max: 100, Quality: 85, Mode: modeCropSquare},
	"tile_224": {Max: 224, Quality: 85, Mode: modeCropSquare},
	"tile_500": {Max: 500, Quality: 90, Mode: modeCropSquare},
}

// sizeOrder is the deterministic iteration order for GenerateAll and SizeNames,
// from the largest fit thumbnail down to the smallest tile so observers see big
// previews complete first.
var sizeOrder = []string{
	"fit_3840",
	"fit_2560",
	"fit_1920",
	"fit_1280",
	"fit_720",
	"tile_500",
	"tile_224",
	"tile_100",
}

// SizeNames returns every registered size name in canonical order. The slice is
// a fresh copy, so callers may sort or filter it without mutating the registry.
func SizeNames() []string {
	out := make([]string, len(sizeOrder))
	copy(out, sizeOrder)
	return out
}

// IsValidSize reports whether name is a registered thumbnail size.
func IsValidSize(name string) bool {
	_, ok := sizes[name]
	return ok
}
