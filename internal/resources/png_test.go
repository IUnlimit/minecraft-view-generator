package resources

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testPNG encodes a small image, so a fixture of this file is a real PNG rather than a hand built one.
func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 16), G: uint8(y * 16), B: 0x40, A: 0xFF})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// assemblePNG turns a chunk list back into a file. wrongCRC flips the last byte of every chunk CRC,
// which is the damage one generator loop is responsible for.
func assemblePNG(chunks []pngChunk, wrongCRC bool) []byte {
	var out bytes.Buffer
	out.Write(pngSignature)
	for _, chunk := range chunks {
		var body bytes.Buffer
		writePNGChunk(&body, chunk.kind, chunk.data)
		data := body.Bytes()
		if wrongCRC {
			data[len(data)-1] ^= 0xFF
		}
		out.Write(data)
	}
	return out.Bytes()
}

func testChunks(t *testing.T, data []byte) []pngChunk {
	t.Helper()
	chunks, err := parsePNGChunks(data[len(pngSignature):])
	if err != nil {
		t.Fatal(err)
	}
	return chunks
}

// brokenPNG reproduces the damage ItemsAdder's generated packs arrive with: every chunk CRC is wrong,
// the adler32 of the image data is wrong, the deflate payload itself is intact, and a private iaPR
// chunk sits between the header and the image data.
func brokenPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	original := testChunks(t, testPNG(t, width, height))
	chunks := make([]pngChunk, 0, len(original)+1)
	for _, chunk := range original {
		data := append([]byte(nil), chunk.data...)
		if chunk.kind == "IDAT" {
			data[len(data)-1] ^= 0xFF
		}
		chunks = append(chunks, pngChunk{kind: chunk.kind, data: data})
		if chunk.kind == "IHDR" {
			chunks = append(chunks, pngChunk{kind: "iaPR", data: []byte{0, 0, 0, 1}})
		}
	}
	return assemblePNG(chunks, true)
}

// truncatedPNG keeps the broken checksums of brokenPNG but cuts the deflate payload short, so the
// data can no longer inflate to the length the header implies and a repair has to give up.
func truncatedPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	chunks := testChunks(t, brokenPNG(t, width, height))
	for index, chunk := range chunks {
		if chunk.kind != "IDAT" {
			continue
		}
		// Keep the four byte adler32 trailer, drop the last twelve bytes of deflate data.
		shortened := append(append([]byte(nil), chunk.data[:len(chunk.data)-16]...), chunk.data[len(chunk.data)-4:]...)
		chunks[index] = pngChunk{kind: chunk.kind, data: shortened}
	}
	return assemblePNG(chunks, true)
}

// writePackArchive writes one archive whose entries are byte slices, so a test can ship an image
// instead of a text placeholder.
func writePackArchive(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if _, writeErr := entry.Write(content); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}

func chunkKinds(chunks []pngChunk) []string {
	kinds := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		kinds = append(kinds, chunk.kind)
	}
	return kinds
}

// The repair has to keep the image and the private chunks while making the checksums truthful: a
// decoder that refuses the file before the repair has to accept it afterwards, pixel for pixel.
func TestRepairPNGChecksumsFixesBrokenMetadata(t *testing.T) {
	original := testPNG(t, 16, 16)
	broken := brokenPNG(t, 16, 16)
	if _, err := png.Decode(bytes.NewReader(broken)); err == nil {
		t.Fatal("the broken fixture still decodes, so it no longer reproduces the damage")
	}
	path := filepath.Join(t.TempDir(), "broken.png")
	if err := os.WriteFile(path, broken, 0o640); err != nil {
		t.Fatal(err)
	}
	repair := repairPackPNGChecksums(filepath.Dir(path))
	if repair.Repaired != 1 || len(repair.Skipped) != 0 {
		t.Fatalf("repair = %+v, want one repaired file and no skip", repair)
	}
	fixed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(fixed))
	if err != nil {
		t.Fatalf("the repaired PNG still does not decode: %v", err)
	}
	want, err := png.Decode(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds() != want.Bounds() {
		t.Fatalf("bounds = %v, want %v", decoded.Bounds(), want.Bounds())
	}
	for y := 0; y < want.Bounds().Dy(); y++ {
		for x := 0; x < want.Bounds().Dx(); x++ {
			if decoded.At(x, y) != want.At(x, y) {
				t.Fatalf("pixel %d,%d = %v, want %v", x, y, decoded.At(x, y), want.At(x, y))
			}
		}
	}
	chunks := testChunks(t, fixed)
	if kinds := chunkKinds(chunks); !strings.Contains(strings.Join(kinds, ","), "iaPR") {
		t.Fatalf("chunks = %v, want the private iaPR chunk preserved", kinds)
	}
	for _, chunk := range chunks {
		if !chunk.valid() {
			t.Fatalf("chunk %q still has a wrong CRC", chunk.kind)
		}
	}
}

// A pack that is already correct must not be written at all: the modification time is the evidence,
// because a rewrite that happened to reproduce the same bytes would still be a write.
func TestRepairPNGChecksumsLeavesAValidPNGAlone(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "valid.png")
	valid := testPNG(t, 16, 16)
	if err := os.WriteFile(path, valid, 0o640); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	repair := repairPackPNGChecksums(directory)
	if repair.Repaired != 0 || len(repair.Skipped) != 0 {
		t.Fatalf("repair = %+v, want a valid pack untouched", repair)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, valid) {
		t.Fatal("a valid PNG was rewritten")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(stamp) {
		t.Fatalf("the valid PNG was written at %v, want the untouched %v", info.ModTime(), stamp)
	}
}

// Data that no longer matches its own header is reported instead of repaired: a repair must never
// invent pixels, and a half written file is worse than the broken one.
func TestRepairPNGChecksumsSkipsUndecodableData(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "truncated.png")
	broken := truncatedPNG(t, 16, 16)
	if err := os.WriteFile(path, broken, 0o640); err != nil {
		t.Fatal(err)
	}
	repair := repairPackPNGChecksums(directory)
	if repair.Repaired != 0 {
		t.Fatalf("repair = %+v, want nothing repaired", repair)
	}
	if len(repair.Skipped) != 1 || !strings.Contains(repair.Skipped[0], "truncated.png") {
		t.Fatalf("skipped = %v, want one entry naming the file", repair.Skipped)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, broken) {
		t.Fatal("a file that cannot be repaired was rewritten")
	}
}

// The repair is wired into the pack loader, so an unpacked pack renders even though the archive it
// came from carries the broken checksums; the archive itself is never modified and a second scan
// finds nothing left to rewrite.
func TestPackStoreRepairsPackPNGChecksums(t *testing.T) {
	cfg := testConfig(t)
	directory := packDir(t, cfg)
	broken := brokenPNG(t, 16, 16)
	path := filepath.Join(directory, "ia.zip")
	writePackArchive(t, path, map[string][]byte{
		"pack.mcmeta":                        []byte(`{"pack":{"pack_format":46}}`),
		"assets/ia/textures/item/broken.png": broken,
	})
	archiveBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store := newPackStore(cfg)
	if _, err := store.Reload(true); err != nil {
		t.Fatal(err)
	}
	report := store.Report()
	if len(report.Packs) != 1 || report.Packs[0].Status != PackLoaded {
		t.Fatalf("packs = %+v, want one loaded pack", report.Packs)
	}
	if report.Packs[0].RepairedImages != 1 {
		t.Fatalf("repairedImages = %d, want 1", report.Packs[0].RepairedImages)
	}
	unpacked, err := os.ReadFile(filepath.Join(report.Packs[0].Root, "assets", "ia", "textures", "item", "broken.png"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = png.Decode(bytes.NewReader(unpacked)); err != nil {
		t.Fatalf("the unpacked PNG is still unreadable: %v", err)
	}
	archiveAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archiveAfter, archiveBefore) {
		t.Fatal("the archive was modified, only the unpacked cache may be repaired")
	}
	if _, err = store.Reload(true); err != nil {
		t.Fatal(err)
	}
	if again := store.Report().Packs[0].RepairedImages; again != 0 {
		t.Fatalf("repairedImages = %d on the second scan, want 0 rewrites", again)
	}
}
