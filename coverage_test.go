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
	if lib.DB() != db {
		t.Fatal("DB()")
	}
	if lib.defaultDisk != "a" {
		t.Fatalf("defaultDisk=%q", lib.defaultDisk)
	}
	lib.Register("", func(*Registrar) {})
	lib.Register("x", nil)
	if lib.collectionConfig("missing", "c") != nil {
		t.Fatal("expected nil collection")
	}
	if lib.conversionsFor("missing", "c") != nil {
		t.Fatal("expected nil conversions")
	}
}

func TestDiskByNameAndLocalDisk(t *testing.T) {
	_, lib, root := setupLib(t)
	if _, err := lib.diskByName("missing"); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("got %v", err)
	}
	d, err := lib.diskByName("")
	if err != nil || d.Name() != "public" {
		t.Fatalf("default disk: %v %#v", err, d)
	}

	disk := NewLocalDisk("local", root, "")
	ctx := context.Background()
	if _, err := disk.Put(ctx, "a/b.txt", strings.NewReader("hi")); err != nil {
		t.Fatal(err)
	}
	ok, err := disk.Exists(ctx, "a/b.txt")
	if err != nil || !ok {
		t.Fatalf("exists: %v %v", ok, err)
	}
	ok, err = disk.Exists(ctx, "nope")
	if err != nil || ok {
		t.Fatalf("missing: %v %v", ok, err)
	}
	rc, err := disk.Open(ctx, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "hi" {
		t.Fatalf("got %q", b)
	}
	if disk.URL("a/b.txt") != "/a/b.txt" {
		t.Fatalf("url=%q", disk.URL("a/b.txt"))
	}
	if err := disk.Delete(ctx, "a/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := disk.Delete(ctx, "gone"); err != nil {
		t.Fatal(err)
	}

	badRoot := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(badRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := NewLocalDisk("bad", badRoot, "/x")
	if _, err := bad.Put(ctx, "z/y.txt", strings.NewReader("z")); err == nil {
		t.Fatal("expected mkdir fail")
	}

	good := NewLocalDisk("g", t.TempDir(), "/g")
	dirPath := filepath.Join(good.Root(), "isdir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := good.Put(ctx, "isdir", strings.NewReader("no")); err == nil {
		t.Fatal("expected create fail")
	}

	locked := filepath.Join(t.TempDir(), "lock")
	if err := os.MkdirAll(filepath.Join(locked, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "sub", "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ld := NewLocalDisk("l", locked, "/l")
	if err := os.Chmod(filepath.Join(locked, "sub"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(locked, "sub"), 0o755) })
	if _, err := ld.Exists(ctx, "sub/f.txt"); err == nil {
		t.Fatal("expected exists permission error")
	}
	if err := ld.Delete(ctx, "sub/f.txt"); err == nil {
		t.Fatal("expected delete permission error")
	}
}

func TestFileSourceHelpers(t *testing.T) {
	if _, err := FromPath("/no/such/file"); err == nil {
		t.Fatal("expected open error")
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
		t.Fatalf("name=%q", src.name)
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
		t.Fatal("expected seek error")
	}

	if _, err := FromReader(nil, "a.jpg", "image/jpeg"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	if _, err := FromReader(strings.NewReader("x"), "", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
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

func (s *seekFail) Read(p []byte) (int, error)         { return s.r.Read(p) }
func (s *seekFail) Close() error                       { return nil }
func (s *seekFail) Seek(int64, int) (int64, error)     { return 0, errBoom }

func TestHasMediaPrimaryKeyAndResolve(t *testing.T) {
	if _, _, err := resolveModel(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	if _, _, err := resolveModel(ModelFrom(0, "x")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	if _, _, err := resolveModel(ModelFrom(1, "")); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}

	if _, err := PrimaryKeyID(nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	var nilPtr *covModel
	if _, err := PrimaryKeyID(nilPtr); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	if _, err := PrimaryKeyID(42); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	type noID struct{ Name string }
	if _, err := PrimaryKeyID(noID{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	type badID struct{ ID string }
	if _, err := PrimaryKeyID(badID{ID: "x"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	type uintID struct{ ID uint64 }
	if _, err := PrimaryKeyID(uintID{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	id, err := PrimaryKeyID(uintID{ID: 7})
	if err != nil || id != 7 {
		t.Fatalf("got %d %v", id, err)
	}
	type intID struct{ ID int }
	if _, err := PrimaryKeyID(intID{ID: -1}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	id, err = PrimaryKeyID(&intID{ID: 3})
	if err != nil || id != 3 {
		t.Fatalf("got %d %v", id, err)
	}
}

func TestModelsEncodeDecode(t *testing.T) {
	restoreHooks(t)
	m := Media{}
	if len(m.GeneratedConversionMap()) != 0 || len(m.CustomPropertyMap()) != 0 || len(m.ResponsiveImagesMap()) != 0 {
		t.Fatal("expected empty maps")
	}
	m.ResponsiveImages = datatypes.JSON([]byte("null"))
	if len(m.ResponsiveImagesMap()) != 0 {
		t.Fatal("null responsive")
	}
	m.CustomProperties = datatypes.JSON([]byte(`{"a":1}`))
	if m.CustomPropertyMap()["a"].(float64) != 1 {
		t.Fatal("custom decode")
	}
	if string(encodeGeneratedConversions(nil)) != "{}" || string(encodeCustomProperties(nil)) != "{}" || string(encodeResponsiveImages(nil)) != "[]" {
		t.Fatal("nil encoders")
	}
	_ = encodeGeneratedConversions(map[string]bool{"x": true})
	_ = encodeCustomProperties(map[string]any{"k": "v"})
	_ = encodeResponsiveImages(ResponsiveImagesData{"media_library_original": {URLs: []string{"a.jpg"}}})

	jsonMarshal = func(any) ([]byte, error) { return nil, errBoom }
	if string(encodeGeneratedConversions(map[string]bool{"a": true})) != "{}" {
		t.Fatal("marshal fail generated")
	}
	if string(encodeCustomProperties(map[string]any{"a": 1})) != "{}" {
		t.Fatal("marshal fail custom")
	}
	if string(encodeResponsiveImages(ResponsiveImagesData{"k": {}})) != "[]" {
		t.Fatal("marshal fail responsive")
	}
}

func TestPathEdgeCases(t *testing.T) {
	m := Media{ID: 1, FileName: "noext"}
	if !strings.Contains(ConversionPath(m, "thumb"), "noext-thumb.jpg") {
		t.Fatal(ConversionPath(m, "thumb"))
	}
	if !strings.HasSuffix(ResponsiveImageFileName(m, "media_library_original", 10, 5), ".jpg") {
		t.Fatal(ResponsiveImageFileName(m, "media_library_original", 10, 5))
	}
}

func TestConversionsBranches(t *testing.T) {
	restoreHooks(t)
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
		t.Fatalf("%v %#v", err, empty)
	}
	if _, err := lib.performConversions(context.Background(), disk, media, errReader{errBoom}); err == nil {
		t.Fatal("read fail")
	}
	media.MimeType = strPtr("application/pdf")
	skip, err := lib.performConversions(context.Background(), disk, media, bytes.NewReader(payload))
	if err != nil || len(skip) != 0 {
		t.Fatalf("%v %#v", err, skip)
	}
	media.MimeType = strPtr("image/jpeg")
	if _, err := lib.performConversions(context.Background(), disk, media, bytes.NewReader([]byte("notimage"))); !errors.Is(err, ErrNotImage) {
		t.Fatalf("got %v", err)
	}

	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.RGBA{A: 255})
	if applyConversion(img, &ConversionConfig{}) != img {
		t.Fatal("noop convert")
	}
	if max(1, 2) != 2 || max(3, 1) != 3 {
		t.Fatal("max")
	}
	if isImageMIME(nil) {
		t.Fatal("nil mime")
	}

	pngEncode = func(io.Writer, image.Image) error { return errBoom }
	media.MimeType = strPtr("image/png")
	media.FileName = "a.png"
	if _, err := lib.performConversions(context.Background(), disk, media, bytes.NewReader(payload)); err == nil {
		t.Fatal("encode fail")
	}

	restoreHooks(t)
	disk.putErr = errBoom
	media.FileName = "a.jpg"
	media.MimeType = strPtr("image/jpeg")
	if _, err := lib.performConversions(context.Background(), disk, media, bytes.NewReader(jpegBytes(t, 20, 20))); err == nil {
		t.Fatal("put fail")
	}
}

func TestResponsiveBranches(t *testing.T) {
	restoreHooks(t)
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
	empty, err := lib.performResponsiveImages(context.Background(), disk, media, bytes.NewReader(payload))
	if err != nil || len(empty) != 0 {
		t.Fatalf("%v %#v", err, empty)
	}
	media.MimeType = strPtr("image/jpeg")
	if _, err := lib.performResponsiveImages(context.Background(), disk, media, errReader{errBoom}); err == nil {
		t.Fatal("read fail")
	}
	if _, err := lib.performResponsiveImages(context.Background(), disk, media, bytes.NewReader([]byte("bad"))); !errors.Is(err, ErrNotImage) {
		t.Fatalf("got %v", err)
	}

	if calculateResponsiveWidths(0, 10, 10) != nil {
		t.Fatal("zero size")
	}
	if ResponsiveConversionKey("x") != "x" {
		t.Fatal("key")
	}
	if _, ok := ParseResponsiveWidth("plain.jpg"); ok {
		t.Fatal("no ___")
	}
	if _, ok := ParseResponsiveWidth("a___b"); ok {
		t.Fatal("short meta")
	}
	if _, ok := ParseResponsiveWidth("a___b_x_y.jpg"); ok {
		t.Fatal("non-int width")
	}
	if _, ok := ParseResponsiveWidth("a___b_0_1.jpg"); ok {
		t.Fatal("zero width")
	}

	img := image.NewRGBA(image.Rect(0, 0, 100, 80))
	disk.putErr = errBoom
	if _, err := generateResponsiveSet(context.Background(), disk, media, img, jpegBytes(t, 100, 80), "media_library_original", 90); err == nil {
		t.Fatal("put fail in set")
	}
}

func TestResponsiveEncodeFailures(t *testing.T) {
	restoreHooks(t)
	_, lib, _ := setupLib(t)
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c").WithResponsiveImages()
		r.Conversion("thumb").ForCollection("c").Fit(20, 20).WithResponsiveImages()
	})
	disk := newFailDisk("public")
	lib.disks["public"] = disk
	media := Media{ID: 3, ModelType: "cov_models", CollectionName: "c", FileName: "a.png", MimeType: strPtr("image/png")}
	payload := pngBytes(t, 120, 90)

	pngEncode = func(io.Writer, image.Image) error { return errBoom }
	if _, err := lib.performResponsiveImages(context.Background(), disk, media, bytes.NewReader(payload)); err == nil {
		t.Fatal("encode conversion fail")
	}

	restoreHooks(t)
	img := image.NewRGBA(image.Rect(0, 0, 50, 40))
	pngEncode = func(io.Writer, image.Image) error { return errBoom }
	if _, err := generateResponsiveSet(context.Background(), disk, media, img, payload, "media_library_original", 90); err == nil {
		t.Fatal("encode in set fail")
	}
}
