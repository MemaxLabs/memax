package export

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"
	"time"
)

// ZipMediaType is the archive's media type.
const ZipMediaType = "application/zip"

// ZipSink writes the export as a zip archive, every file under the
// space's folder (its slug), deflated, dated at the export's as-of time,
// so the same record gives the same archive. The archive is written as it
// goes (the zip format's data descriptors), so it can stream to a client.
type ZipSink struct {
	zw       *zip.Writer
	root     string
	modified time.Time
}

// NewZipSink writes to w. Call Start (from Options.OnSpace) before the
// first file and Close after Finish.
func NewZipSink(w io.Writer) *ZipSink { return &ZipSink{zw: zip.NewWriter(w)} }

// Start names the folder every file goes in and the time they're dated.
func (z *ZipSink) Start(root string, modified time.Time) {
	z.root = strings.Trim(root, "/")
	if modified.IsZero() {
		modified = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	z.modified = modified.UTC().Truncate(time.Second)
}

// Create implements Sink.
func (z *ZipSink) Create(name string) (io.Writer, error) {
	full := name
	if z.root != "" {
		full = z.root + "/" + name
	}
	return z.zw.CreateHeader(&zip.FileHeader{Name: full, Method: zip.Deflate, Modified: z.modified})
}

// Close ends the archive.
func (z *ZipSink) Close() error { return z.zw.Close() }

// MapSink keeps the files in memory, by path (tests, and reading an
// archive back).
type MapSink struct {
	Files map[string][]byte
	// Order is the order the files were written in.
	Order []string
}

// NewMapSink is an empty MapSink.
func NewMapSink() *MapSink { return &MapSink{Files: map[string][]byte{}} }

// Create implements Sink.
func (m *MapSink) Create(name string) (io.Writer, error) {
	if _, dup := m.Files[name]; dup {
		return nil, fmt.Errorf("export: %s written twice", name)
	}
	m.Files[name] = nil
	m.Order = append(m.Order, name)
	return &mapFile{m: m, name: name}, nil
}

type mapFile struct {
	m    *MapSink
	name string
}

func (f *mapFile) Write(p []byte) (int, error) {
	f.m.Files[f.name] = append(f.m.Files[f.name], p...)
	return len(p), nil
}

// ReadZip reads an export archive into its files, by path below the
// space's folder, and returns the folder's name. Every entry must be in
// one folder, with a clean relative path.
func ReadZip(raw []byte) (root string, files map[string][]byte, err error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", nil, fmt.Errorf("export: read archive: %w", err)
	}
	files = map[string][]byte{}
	for _, f := range zr.File {
		if f.Name != path.Clean(f.Name) || strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "..") {
			return "", nil, fmt.Errorf("export: archive entry %q isn't a clean relative path", f.Name)
		}
		top, rest, ok := strings.Cut(f.Name, "/")
		if !ok || rest == "" {
			return "", nil, fmt.Errorf("export: archive entry %q isn't inside the space's folder", f.Name)
		}
		if root == "" {
			root = top
		} else if top != root {
			return "", nil, fmt.Errorf("export: archive holds two folders, %s and %s", root, top)
		}
		rc, err := f.Open()
		if err != nil {
			return "", nil, fmt.Errorf("export: open %s: %w", f.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return "", nil, fmt.Errorf("export: read %s: %w", f.Name, err)
		}
		files[rest] = data
	}
	return root, files, nil
}
