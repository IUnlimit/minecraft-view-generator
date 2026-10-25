package resources

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/adler32"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// A pack whose PNG checksums are wrong is the shape ItemsAdder's generated resource packs arrive in:
// the generator writes the image data correctly but recomputes neither the per chunk CRC nor the zlib
// wrapper around that data, so the pixels are intact while every checksum guarding them is not.
// Java's ImageIO.read refuses such a file with "Error reading PNG image data" and Go's image/png
// refuses it just as hard, which would take a whole render — or a whole font — down over one
// unreadable swatch.
//
// The repair recomputes what the generator left wrong and nothing else: the deflate payload is kept
// byte for byte, a valid zlib header and trailer are put around it, every chunk CRC is recomputed, and
// the private chunks of the generator (iaPR) stay where they are. A file whose data cannot be inflated
// to the length its own header implies is left untouched and reported, so a repair can never invent
// pixels.

// pngSignature is the eight byte header every PNG file starts with.
var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

const (
	// maxPNGRepairBytes bounds the inflated image data one repair holds in memory: an 8192x8192 RGBA
	// texture is 256 MiB, and a file claiming more than that is reported instead of repaired.
	maxPNGRepairBytes = 256 << 20
	// zlibDeflateMax is the zlib header a repair writes: deflate, 32 KiB window, no dictionary, and a
	// check value that satisfies the "%31" rule the format requires.
	zlibDeflateMax = 0x789c
)

// pngRepair is the outcome of one repair pass over an unpacked pack.
type pngRepair struct {
	// Repaired is how many files were rewritten because their checksums were wrong.
	Repaired int
	// Skipped names the files that could not be repaired safely, relative to the pack root, with the
	// reason appended after the path: one warning each.
	Skipped []string
}

// pngChunk is one chunk of a PNG file: its type, its data and the CRC the file declares for it.
type pngChunk struct {
	kind string
	data []byte
	crc  uint32
}

// valid reports whether the CRC declared for this chunk matches the data it carries.
func (c pngChunk) valid() bool {
	return c.crc == pngChunkCRC(c.kind, c.data)
}

func pngChunkCRC(kind string, data []byte) uint32 {
	hash := crc32.NewIEEE()
	hash.Write([]byte(kind))
	hash.Write(data)
	return hash.Sum32()
}

// repairPackPNGChecksums walks an unpacked pack and fixes the checksums of every PNG it finds. It is
// idempotent: a file whose checksums already match is only read, never written, which is the path
// every vanilla and healthy pack takes.
func repairPackPNGChecksums(root string) pngRepair {
	var result pngRepair
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry == nil || entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".png") {
			return nil
		}
		repaired, err := repairPNGChecksums(path)
		if err != nil {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				relative = path
			}
			result.Skipped = append(result.Skipped, filepath.ToSlash(relative)+": "+err.Error())
			return nil
		}
		if repaired {
			result.Repaired++
		}
		return nil
	})
	return result
}

// repairPNGChecksums repairs one file in place. It answers false with a nil error when the file is
// already correct, so a pack repaired once is never rewritten again; the pixels of a repaired file
// are the pixels of the original.
func repairPNGChecksums(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if len(data) < len(pngSignature) || !bytes.Equal(data[:len(pngSignature)], pngSignature) {
		return false, errors.New("the file does not start with the PNG signature")
	}
	chunks, err := parsePNGChunks(data[len(pngSignature):])
	if err != nil {
		return false, err
	}
	intact := true
	for _, chunk := range chunks {
		if !chunk.valid() {
			intact = false
			break
		}
	}
	if intact {
		return false, nil
	}
	rebuilt, err := rebuildPNG(chunks)
	if err != nil {
		return false, err
	}
	if err = writeAtomic(path, rebuilt, 0o640); err != nil {
		return false, err
	}
	return true, nil
}

// rebuildPNG produces a PNG whose chunks carry correct CRCs and whose image data is wrapped in a
// valid zlib stream. Chunk order is preserved so that a private chunk such as the iaPR one ItemsAdder
// writes keeps its position, and only the checksum bytes of the file change.
func rebuildPNG(chunks []pngChunk) ([]byte, error) {
	header := chunks[0]
	if header.kind != "IHDR" || len(header.data) != 13 {
		return nil, errors.New("the first chunk is not an IHDR")
	}
	width := binary.BigEndian.Uint32(header.data[0:])
	height := binary.BigEndian.Uint32(header.data[4:])
	bitDepth := header.data[8]
	colorType := header.data[9]
	if width == 0 || height == 0 {
		return nil, errors.New("the header declares an empty image")
	}
	if header.data[10] != 0 || header.data[11] != 0 {
		return nil, errors.New("the header uses an unknown compression or filter method")
	}
	if header.data[12] != 0 {
		// Adam7 splits the image into seven passes, so the inflated length is not the plain scanline
		// count this repair checks against.
		return nil, fmt.Errorf("interlace method %d is not supported", header.data[12])
	}
	channels, known := pngChannels(colorType)
	if !known {
		return nil, fmt.Errorf("colour type %d is unknown", colorType)
	}
	if !pngBitDepth(bitDepth) {
		return nil, fmt.Errorf("bit depth %d is unknown", bitDepth)
	}
	compressed := make([]byte, 0, 4096)
	firstData := -1
	for index, chunk := range chunks {
		if chunk.kind != "IDAT" {
			continue
		}
		if firstData < 0 {
			firstData = index
		}
		compressed = append(compressed, chunk.data...)
	}
	if firstData < 0 {
		return nil, errors.New("the image data is missing")
	}
	if len(compressed) < 6 {
		return nil, errors.New("the image data is too short to be a zlib stream")
	}
	// A zlib stream is a two byte header, the deflate payload and a four byte adler32 of the inflated
	// data. The payload is the part the generator got right, so it is reused as it is.
	payload := compressed[2 : len(compressed)-4]
	expected := int64(height) * (1 + (int64(width)*int64(channels)*int64(bitDepth)+7)/8)
	if expected > maxPNGRepairBytes {
		return nil, fmt.Errorf("the header implies %d bytes of image data, over the %d byte limit", expected, maxPNGRepairBytes)
	}
	inflated, err := inflatePNGData(payload, expected)
	if err != nil {
		return nil, err
	}
	var trailer [4]byte
	binary.BigEndian.PutUint32(trailer[:], adler32.Checksum(inflated))
	stream := make([]byte, 0, len(payload)+6)
	stream = append(stream, byte(zlibDeflateMax>>8), byte(zlibDeflateMax&0xFF))
	stream = append(stream, payload...)
	stream = append(stream, trailer[:]...)

	var out bytes.Buffer
	out.Grow(len(pngSignature) + len(chunks)*12 + len(stream))
	out.Write(pngSignature)
	replaced := false
	for _, chunk := range chunks {
		if chunk.kind == "IDAT" {
			// Several IDAT chunks become one: the split is a streaming artefact of the encoder, and
			// carrying it over would only spread the same bytes across more headers.
			if replaced {
				continue
			}
			replaced = true
			chunk.data = stream
		}
		writePNGChunk(&out, chunk.kind, chunk.data)
	}
	return out.Bytes(), nil
}

// inflatePNGData inflates a bare deflate stream, bounded by the length the IHDR implies: data that
// inflates to anything else is not the image its own header describes, so it is refused instead of
// repaired. The bound also keeps a hostile stream from inflating past what a texture could be.
func inflatePNGData(payload []byte, expected int64) ([]byte, error) {
	reader := flate.NewReader(bytes.NewReader(payload))
	defer reader.Close()
	inflated, err := io.ReadAll(io.LimitReader(reader, expected+1))
	if err != nil {
		return nil, fmt.Errorf("the image data cannot be inflated: %w", err)
	}
	if int64(len(inflated)) != expected {
		return nil, fmt.Errorf("the image data inflates to %d bytes, the header implies %d", len(inflated), expected)
	}
	return inflated, nil
}

// parsePNGChunks splits the body of a PNG file into chunks, keeping the CRCs as declared so the
// caller can tell an intact file from a broken one without inflating anything.
func parsePNGChunks(body []byte) ([]pngChunk, error) {
	chunks := make([]pngChunk, 0, 8)
	for offset := 0; offset < len(body); {
		if len(body)-offset < 12 {
			return nil, fmt.Errorf("a chunk header at offset %d is truncated", offset)
		}
		length := int(binary.BigEndian.Uint32(body[offset:]))
		if length < 0 || length > len(body)-offset-12 {
			return nil, fmt.Errorf("the chunk at offset %d runs past the end of the file", offset)
		}
		kind := string(body[offset+4 : offset+8])
		start := offset + 8
		end := start + length
		chunks = append(chunks, pngChunk{kind: kind, data: body[start:end], crc: binary.BigEndian.Uint32(body[end:])})
		if kind == "IEND" {
			break
		}
		offset = end + 4
	}
	if len(chunks) == 0 {
		return nil, errors.New("the file has no chunks")
	}
	return chunks, nil
}

// writePNGChunk appends one chunk with a correct CRC.
func writePNGChunk(out *bytes.Buffer, kind string, data []byte) {
	var header [8]byte
	binary.BigEndian.PutUint32(header[0:], uint32(len(data)))
	copy(header[4:], kind)
	out.Write(header[:])
	out.Write(data)
	var trailer [4]byte
	binary.BigEndian.PutUint32(trailer[:], pngChunkCRC(kind, data))
	out.Write(trailer[:])
}

// pngChannels is the number of samples per pixel of a PNG colour type: grey, truecolour, indexed,
// grey with alpha and truecolour with alpha.
func pngChannels(colorType byte) (int, bool) {
	switch colorType {
	case 0, 3:
		return 1, true
	case 2:
		return 3, true
	case 4:
		return 2, true
	case 6:
		return 4, true
	}
	return 0, false
}

func pngBitDepth(depth byte) bool {
	switch depth {
	case 1, 2, 4, 8, 16:
		return true
	}
	return false
}
