//go:build pixelbomb

// The pixel-bomb memory harness (SEC-018). It is not part of `make check`: it
// allocates gigabytes on purpose. Run it by hand, and only on a machine with the
// RAM to spare:
//
//	go test -tags pixelbomb -run TestPixelBombMemory -v -timeout 30m ./internal/ingest/
//
// Each scenario runs in a fresh child process (this test binary re-executed),
// so the VmHWM it reports is that scenario's own peak and not the high-water
// mark of whatever ran before it. A child that passes ~3 GiB of resident memory
// stops itself and reports "aborted" instead of driving the machine into swap.
package ingest

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/panbotka/kukatko/internal/blurhash"
	"github.com/panbotka/kukatko/internal/imgconvert"
	"github.com/panbotka/kukatko/internal/phash"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/storage"
	"github.com/panbotka/kukatko/internal/thumb"
)

const (
	// bombSide is the side of a square image just under the 200 MP default cap
	// (14142² = 199,996,164 pixels).
	bombSide = 14142
	// bombMaxPixels is the thumb.max_pixels default the lead is about.
	bombMaxPixels = 200_000_000
	// bombAbortRSS is where a child stops itself rather than keep allocating.
	bombAbortRSS = 3 << 30
	// envChild selects the scenario a re-executed child runs.
	envChild = "KUKATKO_PIXELBOMB_CHILD"
	// envBudget sets the decode budget (bytes) a child runs with; unset = none.
	envBudget = "KUKATKO_PIXELBOMB_BUDGET"
)

// TestPixelBombMemory measures the peak resident memory of the upload path's
// in-process decodes — verifyPixels, the pHash and blurhash over its image, then
// the thumbnailer's own decode of the stored original — for one upload and for
// three concurrent ones, over an 8-bit and a 16-bit single-colour PNG just under
// the pixel cap. It logs a table; it asserts nothing, it is a measurement.
func TestPixelBombMemory(t *testing.T) {
	if spec := os.Getenv(envChild); spec != "" {
		runBombChild(t, spec)
		return
	}
	dir := t.TempDir()
	for name, depth := range map[string]int{"rgba8": 8, "rgba16": 16} {
		path := filepath.Join(dir, name+".png")
		if err := writeUniformPNG(path, bombSide, bombSide, depth); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		t.Logf("%s: %dx%d, %d-bit RGBA PNG, %.2f MB on disk", name, bombSide, bombSide, depth, fileMB(t, path))
	}
	// The control: an ordinary 24 MP camera JPEG, so the bound's cost to the
	// everyday upload is measured alongside the bomb.
	photo := filepath.Join(dir, "photo24.jpg")
	if err := writeNoiseJPEG(photo, 6000, 4000); err != nil {
		t.Fatalf("writing photo24: %v", err)
	}
	t.Logf("photo24: 6000x4000 JPEG, %.2f MB on disk", fileMB(t, photo))
	budgets := []string{""}
	if extra := os.Getenv(envBudget); extra != "" {
		budgets = append(budgets, extra)
	}
	for _, budget := range budgets {
		for _, name := range []string{"photo24", "rgba8", "rgba16"} {
			for _, n := range []int{1, 3} {
				out := runBombScenario(t, scenarioPath(dir, name), n, budget)
				t.Logf("budget=%-10s %-6s x%d: %s", orNone(budget), name, n, out)
			}
		}
	}
}

// fileMB returns the size of the file at path in MB.
func fileMB(t *testing.T, path string) float64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return float64(info.Size()) / 1e6
}

// writeNoiseJPEG writes a w×h JPEG of a gradient with pseudo-random noise — a
// stand-in for a camera photo, compressing to a realistic few MB.
func writeNoiseJPEG(path string, w, h int) error {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	seed := uint32(1)
	for i := 0; i < len(img.Pix); i += 4 {
		seed = seed*1664525 + 1013904223
		px := i / 4
		img.Pix[i] = uint8(px % w * 255 / w)
		img.Pix[i+1] = uint8(px / w * 255 / h)
		img.Pix[i+2] = uint8(seed >> 24)
		img.Pix[i+3] = 0xff
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

// scenarioPath returns the generated file of the named scenario in dir.
func scenarioPath(dir, name string) string {
	if name == "photo24" {
		return filepath.Join(dir, name+".jpg")
	}
	return filepath.Join(dir, name+".png")
}

// orNone prints an unset budget as "none".
func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// runBombScenario re-executes this test binary as a child running one scenario
// and returns the line it reports.
func runBombScenario(t *testing.T, path string, n int, budget string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run", "^TestPixelBombMemory$", "-test.v")
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%s:%d", envChild, path, n), envBudget+"="+budget)
	out, _ := cmd.CombinedOutput()
	for line := range strings.SplitSeq(string(out), "\n") {
		if _, result, ok := strings.Cut(line, "RESULT "); ok {
			return strings.TrimSpace(result)
		}
	}
	return "no result:\n" + string(out)
}

// runBombChild is the child side: n concurrent "uploads" of the file named in
// spec, each running the same decode sequence ingest runs, under a watchdog.
func runBombChild(t *testing.T, spec string) {
	sep := strings.LastIndex(spec, ":")
	path := spec[:sep]
	n, err := strconv.Atoi(spec[sep+1:])
	if err != nil {
		t.Fatalf("bad child spec %q", spec)
	}
	var budget *imgconvert.DecodeBudget
	if raw := os.Getenv(envBudget); raw != "" {
		bytesBudget, perr := strconv.ParseInt(raw, 10, 64)
		if perr != nil {
			t.Fatalf("bad budget %q", raw)
		}
		budget = imgconvert.NewDecodeBudget(bytesBudget)
	}
	peak := watchRSS()

	root := t.TempDir()
	fs, err := storage.NewFS(root)
	if err != nil {
		t.Fatalf("fs: %v", err)
	}
	svc := &Service{
		maxPixels: bombMaxPixels,
		budget:    budget,
		thumbs: thumb.New(fs, t.TempDir(), thumb.WithMaxPixels(bombMaxPixels),
			thumb.WithDecodeBudget(budget)),
	}
	start := time.Now()
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		rel := fmt.Sprintf("2026/10/bomb-%d%s", i, filepath.Ext(path))
		if err := copyFile(path, filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("copy: %v", err)
		}
		wg.Go(func() { errs[i] = bombUpload(svc, path, rel, i) })
	}
	wg.Wait()
	elapsed := time.Since(start)
	fmt.Printf("RESULT VmHWM=%s sampled-peak-RSS=%s elapsed=%s errors=%v\n",
		mib(readStatus("VmHWM")), mib(peak()), elapsed.Round(time.Millisecond), errs)
}

// bombUpload runs ingest's decode sequence for one upload: the pre-store decode
// (verifyPixels), the pHash and the oriented blurhash over its image, then — with
// that image released, as postProcess releases it — the thumbnailer's decode of
// the stored original at rel.
func bombUpload(svc *Service, staged, rel string, i int) error {
	ctx := context.Background()
	pixels, err := svc.verifyPixels(ctx, staged, "upload"+filepath.Ext(staged))
	if err != nil {
		return fmt.Errorf("verify: %w", err)
	}
	if pixels.img != nil {
		phash.Compute(pixels.img)
		if _, err := blurhash.EncodeOriented(pixels.img, nil); err != nil {
			return fmt.Errorf("blurhash: %w", err)
		}
	}
	pixels.done()
	_, err = svc.thumbs.GenerateAll(ctx, photos.Photo{
		FilePath: rel, FileHash: fmt.Sprintf("%064d", i), FileOrientation: 1,
	})
	return err
}

// watchRSS starts sampling VmRSS every 5 ms and returns a func reporting the
// highest value seen. Past bombAbortRSS it prints an "aborted" result and exits.
func watchRSS() func() int64 {
	var mu sync.Mutex
	var peak int64
	go func() {
		for {
			rss := readStatus("VmRSS")
			mu.Lock()
			peak = max(peak, rss)
			mu.Unlock()
			if rss > bombAbortRSS {
				fmt.Printf("RESULT aborted: VmRSS=%s passed the %s safety stop\n", mib(rss), mib(bombAbortRSS))
				os.Exit(3)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return func() int64 {
		mu.Lock()
		defer mu.Unlock()
		return peak
	}
}

// readStatus returns a "kB" field of /proc/self/status in bytes (0 if absent).
func readStatus(field string) int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, field+":"); ok {
			kb, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "kB")), 10, 64)
			return kb << 10
		}
	}
	return 0
}

// mib formats bytes as MiB.
func mib(b int64) string { return fmt.Sprintf("%dMiB", b>>20) }

// copyFile copies src to dst, creating dst's directory.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o600)
}

// writeUniformPNG streams a w×h single-colour RGBA PNG of the given bit depth
// (8 or 16) to path without ever holding its bitmap: one row, compressed h
// times. That is exactly why such a file is a bomb — a few MB on disk, w×h×4
// (or ×8) bytes once decoded.
func writeUniformPNG(path string, w, h, depth int) error {
	var idat bytes.Buffer
	zw, err := zlib.NewWriterLevel(&idat, zlib.BestCompression)
	if err != nil {
		return err
	}
	row := make([]byte, 1+w*4*depth/8) // filter byte 0 + pixels
	for i := 1; i < len(row); i++ {
		row[i] = 0x7f
	}
	for range h {
		if _, err := zw.Write(row); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	ihdr[8] = byte(depth)
	ihdr[9] = 6 // truecolour with alpha
	writeChunk(&out, "IHDR", ihdr)
	writeChunk(&out, "IDAT", idat.Bytes())
	writeChunk(&out, "IEND", nil)
	return os.WriteFile(path, out.Bytes(), 0o600)
}

// writeChunk appends one PNG chunk (length, type, data, CRC) to out.
func writeChunk(out *bytes.Buffer, kind string, data []byte) {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(data)))
	out.Write(hdr[:])
	crc := crc32.NewIEEE()
	crc.Write([]byte(kind))
	crc.Write(data)
	out.WriteString(kind)
	out.Write(data)
	binary.BigEndian.PutUint32(hdr[:], crc.Sum32())
	out.Write(hdr[:])
}
