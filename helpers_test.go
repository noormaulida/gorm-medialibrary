package medialibrary

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type covModel struct {
	ID uint64
}

func (m covModel) MediaModelID() uint64   { return m.ID }
func (m covModel) MediaModelType() string { return "cov_models" }

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }
func (e errReader) Close() error             { return nil }

type failDisk struct {
	name      string
	putErr    error
	deleteErr error
	existsErr error
	openErr   error
	putN      int
	files     map[string][]byte
}

func newFailDisk(name string) *failDisk {
	return &failDisk{name: name, files: map[string][]byte{}}
}

func (d *failDisk) Name() string { return d.name }

func (d *failDisk) Put(_ context.Context, path string, r io.Reader) (int64, error) {
	if d.putErr != nil {
		if d.putN > 0 {
			d.putN--
			b, _ := io.ReadAll(r)
			d.files[path] = b
			return int64(len(b)), nil
		}
		return 0, d.putErr
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	d.files[path] = b
	return int64(len(b)), nil
}

func (d *failDisk) Delete(_ context.Context, path string) error {
	if d.deleteErr != nil {
		return d.deleteErr
	}
	delete(d.files, path)
	return nil
}

func (d *failDisk) Exists(_ context.Context, path string) (bool, error) {
	if d.existsErr != nil {
		return false, d.existsErr
	}
	_, ok := d.files[path]
	return ok, nil
}

func (d *failDisk) Open(_ context.Context, path string) (io.ReadCloser, error) {
	if d.openErr != nil {
		return nil, d.openErr
	}
	b, ok := d.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (d *failDisk) URL(path string) string { return "/f/" + path }

func setupLib(t *testing.T, opts ...Option) (*gorm.DB, *Library, string) {
	t.Helper()
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	disk := NewLocalDisk("public", root, "/storage")
	all := append([]Option{WithDisk(disk)}, opts...)
	lib := New(db, all...)
	return db, lib, root
}

func jpegBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{B: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeJPEGFile(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.WriteFile(path, jpegBytes(t, w, h), 0o644); err != nil {
		t.Fatal(err)
	}
}

var errBoom = errors.New("boom")
