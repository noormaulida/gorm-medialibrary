package medialibrary

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateNilDB(t *testing.T) {
	if err := Migrate(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
}

func TestLibraryOptionsAndEnsureReady(t *testing.T) {
	if err := (*Library)(nil).ensureReady(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil lib: %v", err)
	}
	lib := New(nil)
	if err := lib.ensureReady(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil db: %v", err)
	}
	db, err := gorm.Open(sqlite.Open("file:ensure?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	lib = New(db)
	if err := lib.ensureReady(); err == nil || !strings.Contains(err.Error(), "no disk") {
		t.Fatalf("no disk: %v", err)
	}

	d := NewLocalDisk("a", t.TempDir(), "/a")
	lib = New(db, WithDisk(nil), WithDisk(d), WithDefaultDisk(""), WithDefaultDisk("a"))
	if lib.DB() != db || lib.defaultDisk != "a" {
		t.Fatal("options")
	}
	lib.Register("", func(*Registrar) {})
	lib.Register("x", nil)
	if lib.collectionConfig("missing", "c") != nil || lib.conversionsFor("missing", "c") != nil {
		t.Fatal("missing registrar")
	}
}

func TestDiskByNameAndLocalDisk(t *testing.T) {
	_, lib, root := setupLib(t)
	if _, err := lib.diskByName("missing"); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("got %v", err)
	}
	d, err := lib.diskByName("")
	if err != nil || d.Name() != "public" {
		t.Fatalf("default disk: %v", err)
	}

	disk := NewLocalDisk("local", root, "")
	ctx := context.Background()
	if _, err := disk.Put(ctx, "a/b.txt", strings.NewReader("hi")); err != nil {
		t.Fatal(err)
	}
	ok, err := disk.Exists(ctx, "a/b.txt")
	if err != nil || !ok {
		t.Fatal(err)
	}
	ok, err = disk.Exists(ctx, "nope")
	if err != nil || ok {
		t.Fatal("missing")
	}
	rc, err := disk.Open(ctx, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "hi" || disk.URL("a/b.txt") != "/a/b.txt" {
		t.Fatal("open/url")
	}
	_ = disk.Delete(ctx, "a/b.txt")
	_ = disk.Delete(ctx, "gone")

	badRoot := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(badRoot, []byte("x"), 0o644)
	if _, err := NewLocalDisk("bad", badRoot, "/x").Put(ctx, "z/y.txt", strings.NewReader("z")); err == nil {
		t.Fatal("mkdir fail")
	}
	good := NewLocalDisk("g", t.TempDir(), "/g")
	_ = os.MkdirAll(filepath.Join(good.Root(), "isdir"), 0o755)
	if _, err := good.Put(ctx, "isdir", strings.NewReader("no")); err == nil {
		t.Fatal("create fail")
	}

	locked := filepath.Join(t.TempDir(), "lock")
	_ = os.MkdirAll(filepath.Join(locked, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(locked, "sub", "f.txt"), []byte("x"), 0o644)
	ld := NewLocalDisk("l", locked, "/l")
	_ = os.Chmod(filepath.Join(locked, "sub"), 0)
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(locked, "sub"), 0o755) })
	if _, err := ld.Exists(ctx, "sub/f.txt"); err == nil {
		t.Fatal("exists perm")
	}
	if err := ld.Delete(ctx, "sub/f.txt"); err == nil {
		t.Fatal("delete perm")
	}
}

func TestFileSourceHelpers(t *testing.T) {
	if _, err := FromPath("/no/such/file"); err == nil {
		t.Fatal("open")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "photo.jpg")
	writeJPEGFile(t, p, 20, 10)
	src, err := FromPath(p)
	if err != nil {
		t.Fatal(err)
	}
	_ = src.WithName("").WithName("custom")
	if src.name != "custom" {
		t.Fatal(src.name)
	}
	src.close()

	raw := filepath.Join(dir, "noext")
	writeJPEGFile(t, raw, 10, 10)
	src, err = FromPath(raw)
	if err != nil {
		t.Fatal(err)
	}
	src.close()

	fail := &seekFail{r: bytes.NewReader([]byte("hello"))}
	if _, err := fileSourceFrom(fail, "blob"); err == nil {
		t.Fatal("seek")
	}
	if _, err := FromReader(nil, "a.jpg", "image/jpeg"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, err := FromReader(strings.NewReader("x"), "", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	src, err = FromReader(strings.NewReader("x"), "doc.bin", "")
	if err != nil {
		t.Fatal(err)
	}
	src.close()
	src, err = FromReader(io.NopCloser(strings.NewReader("y")), "a.jpg", "image/jpeg")
	if err != nil {
		t.Fatal(err)
	}
	src.close()
}

type seekFail struct{ r *bytes.Reader }

func (s *seekFail) Read(p []byte) (int, error)     { return s.r.Read(p) }
func (s *seekFail) Close() error                   { return nil }
func (s *seekFail) Seek(int64, int) (int64, error) { return 0, errBoom }

func TestHasMediaPrimaryKeyAndResolve(t *testing.T) {
	if _, _, err := resolveModel(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, _, err := resolveModel(ModelFrom(0, "x")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, _, err := resolveModel(ModelFrom(1, "")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, err := PrimaryKeyID(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	var nilPtr *covModel
	if _, err := PrimaryKeyID(nilPtr); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	if _, err := PrimaryKeyID(42); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	type noID struct{ Name string }
	if _, err := PrimaryKeyID(noID{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	type badID struct{ ID string }
	if _, err := PrimaryKeyID(badID{ID: "x"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	type uintID struct{ ID uint64 }
	if _, err := PrimaryKeyID(uintID{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	id, err := PrimaryKeyID(uintID{ID: 7})
	if err != nil || id != 7 {
		t.Fatal(err)
	}
	type intID struct{ ID int }
	if _, err := PrimaryKeyID(intID{ID: -1}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	id, err = PrimaryKeyID(&intID{ID: 3})
	if err != nil || id != 3 {
		t.Fatal(err)
	}
}

func TestModelsEncodeDecode(t *testing.T) {
	m := Media{}
	if len(m.GeneratedConversionMap()) != 0 || len(m.CustomPropertyMap()) != 0 || len(m.ResponsiveImagesMap()) != 0 {
		t.Fatal("empty")
	}
	m.ResponsiveImages = datatypes.JSON([]byte("null"))
	if len(m.ResponsiveImagesMap()) != 0 {
		t.Fatal("null")
	}
	m.CustomProperties = datatypes.JSON([]byte(`{"a":1}`))
	if m.CustomPropertyMap()["a"].(float64) != 1 {
		t.Fatal("custom")
	}
	_ = encodeGeneratedConversions(nil)
	_ = encodeCustomProperties(nil)
	_ = encodeResponsiveImages(nil)
	_ = encodeGeneratedConversions(map[string]bool{"x": true})
	_ = encodeCustomProperties(map[string]any{"k": "v"})
	_ = encodeResponsiveImages(ResponsiveImagesData{"media_library_original": {URLs: []string{"a.jpg"}}})
	if string(encodeCustomProperties(map[string]any{"bad": make(chan int)})) != "{}" {
		t.Fatal("marshal fail")
	}
}

func TestPathEdgeCases(t *testing.T) {
	m := Media{ID: 1, FileName: "noext"}
	if !strings.Contains(ConversionPath(m, "thumb"), "noext-thumb.jpg") {
		t.Fatal(ConversionPath(m, "thumb"))
	}
	if !strings.HasSuffix(ResponsiveImageFileName(m, "media_library_original", 10, 5), ".jpg") {
		t.Fatal("ext")
	}
}

func TestConversionsBranches(t *testing.T) {
	_, lib, _ := setupLib(t)
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c")
		r.Conversion("noop").ForCollection("c")
		r.Conversion("fit").ForCollection("c").Fit(10, 0).Quality(0)
		r.Conversion("crop").ForCollection("c").Crop(8, 8)
	})
	disk := newFailDisk("public")
	lib.disks["public"] = disk
	media := Media{ID: 1, ModelType: "cov_models", CollectionName: "c", FileName: "a.png", MimeType: strPtr("image/png")}
	payload := pngBytes(t, 40, 40)

	got, err := lib.performConversions(context.Background(), disk, media, bytes.NewReader(payload))
	if err != nil || !got["fit"] || !got["crop"] || !got["noop"] {
		t.Fatalf("%v %#v", err, got)
	}
	empty, err := lib.performConversions(context.Background(), disk, Media{ModelType: "none", CollectionName: "c"}, bytes.NewReader(payload))
	if err != nil || len(empty) != 0 {
		t.Fatal(err)
	}
	if _, err := lib.performConversions(context.Background(), disk, media, errReader{errBoom}); err == nil {
		t.Fatal("read")
	}
	media.MimeType = strPtr("application/pdf")
	skip, _ := lib.performConversions(context.Background(), disk, media, bytes.NewReader(payload))
	if len(skip) != 0 {
		t.Fatal("skip")
	}
	media.MimeType = strPtr("image/jpeg")
	if _, err := lib.performConversions(context.Background(), disk, media, bytes.NewReader([]byte("notimage"))); !errors.Is(err, ErrNotImage) {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{A: 255})
	if applyConversion(img, &ConversionConfig{}) != img {
		t.Fatal("noop")
	}
	_ = max(1, 2)
	_ = max(3, 1)
	if isImageMIME(nil) {
		t.Fatal("nil mime")
	}
	_ = encodeImage(io.Discard, img, "x.jpg", 0)
	_ = encodeImage(io.Discard, img, "x.jpg", 101)
	_ = encodeImage(io.Discard, img, "x.png", 90)

	disk.putErr = errBoom
	media.FileName = "a.jpg"
	if _, err := lib.performConversions(context.Background(), disk, media, bytes.NewReader(jpegBytes(t, 20, 20))); err == nil {
		t.Fatal("put")
	}
}

func TestResponsiveBranches(t *testing.T) {
	_, lib, _ := setupLib(t)
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c").WithResponsiveImages()
		r.Conversion("thumb").ForCollection("c").Fit(20, 20).WithResponsiveImages()
	})
	disk := newFailDisk("public")
	lib.disks["public"] = disk
	media := Media{ID: 9, ModelType: "cov_models", CollectionName: "c", FileName: "a.jpg", MimeType: strPtr("image/jpeg")}
	payload := jpegBytes(t, 200, 150)

	out, err := lib.performResponsiveImages(context.Background(), disk, media, bytes.NewReader(payload))
	if err != nil || out["media_library_original"].URLs == nil || out["thumb"].URLs == nil {
		t.Fatalf("%v %#v", err, out)
	}
	media.MimeType = nil
	empty, _ := lib.performResponsiveImages(context.Background(), disk, media, bytes.NewReader(payload))
	if len(empty) != 0 {
		t.Fatal("nil mime")
	}
	media.MimeType = strPtr("image/jpeg")
	if _, err := lib.performResponsiveImages(context.Background(), disk, media, errReader{errBoom}); err == nil {
		t.Fatal("read")
	}
	if _, err := lib.performResponsiveImages(context.Background(), disk, media, bytes.NewReader([]byte("bad"))); !errors.Is(err, ErrNotImage) {
		t.Fatal(err)
	}
	if calculateResponsiveWidths(0, 10, 10) != nil {
		t.Fatal("zero")
	}
	_ = ResponsiveConversionKey("x")
	if _, ok := ParseResponsiveWidth("plain.jpg"); ok {
		t.Fatal("plain")
	}
	if _, ok := ParseResponsiveWidth("a___b"); ok {
		t.Fatal("short")
	}
	if _, ok := ParseResponsiveWidth("a___b_x_y.jpg"); ok {
		t.Fatal("nonint")
	}
	if _, ok := ParseResponsiveWidth("a___b_0_1.jpg"); ok {
		t.Fatal("zerow")
	}

	img := image.NewRGBA(image.Rect(0, 0, 100, 80))
	disk.putErr = errBoom
	if _, err := generateResponsiveSet(context.Background(), disk, media, img, jpegBytes(t, 100, 80), "media_library_original", 90); err == nil {
		t.Fatal("put set")
	}
}
