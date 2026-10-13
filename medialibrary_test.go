package medialibrary_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	medialibrary "github.com/noormaulida/gorm-medialibrary"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type SeoMeta struct {
	ID    uint64
	Title string
}

func (s SeoMeta) MediaModelID() uint64   { return s.ID }
func (s SeoMeta) MediaModelType() string { return "seo_metas" }

func setup(t *testing.T) (*gorm.DB, *medialibrary.Library, string) {
	t.Helper()
	root := t.TempDir()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("sqlite: %v", err)
	}
	if err := db.AutoMigrate(&SeoMeta{}); err != nil {
		t.Fatalf("migrate model: %v", err)
	}
	if err := medialibrary.Migrate(db); err != nil {
		t.Fatalf("migrate media: %v", err)
	}
	disk := medialibrary.NewLocalDisk("public", root, "/storage")
	lib := medialibrary.New(db, medialibrary.WithDisk(disk))
	lib.Register("seo_metas", func(r *medialibrary.Registrar) {
		r.Collection("seo_image").
			SingleFile().
			AcceptsMimeTypes("image/jpeg", "image/png").
			WithResponsiveImages()
		r.Conversion("seo_image_large").ForCollection("seo_image").Crop(120, 63).Quality(85).WithResponsiveImages()
		r.Conversion("seo_image_small").ForCollection("seo_image").Crop(24, 12)
	})
	return db, lib, root
}

func writeJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 80, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
}

func TestAddMediaWithConversions(t *testing.T) {
	db, lib, root := setup(t)
	meta := SeoMeta{Title: "Home"}
	if err := db.Create(&meta).Error; err != nil {
		t.Fatal(err)
	}

	srcPath := filepath.Join(t.TempDir(), "hero.jpg")
	writeJPEG(t, srcPath, 400, 300)

	src, err := medialibrary.FromPath(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	media, err := lib.AddMedia(context.Background(), meta, "seo_image", src)
	if err != nil {
		t.Fatalf("AddMedia: %v", err)
	}
	if media.ID == 0 || media.CollectionName != "seo_image" {
		t.Fatalf("unexpected media: %#v", media)
	}
	generated := media.GeneratedConversionMap()
	if !generated["seo_image_large"] || !generated["seo_image_small"] {
		t.Fatalf("conversions not generated: %#v", generated)
	}

	orig := filepath.Join(root, medialibrary.OriginalPath(*media))
	if _, err := os.Stat(orig); err != nil {
		t.Fatalf("original missing: %v", err)
	}
	large := filepath.Join(root, medialibrary.ConversionPath(*media, "seo_image_large"))
	if _, err := os.Stat(large); err != nil {
		t.Fatalf("large conversion missing: %v", err)
	}

	url, err := lib.GetURL(*media, "seo_image_large")
	if err != nil {
		t.Fatal(err)
	}
	want := "/storage/" + medialibrary.ConversionPath(*media, "seo_image_large")
	if url != want {
		t.Fatalf("url=%q want=%q", url, want)
	}
}

func TestSingleFileReplacesPrevious(t *testing.T) {
	db, lib, _ := setup(t)
	meta := SeoMeta{Title: "X"}
	if err := db.Create(&meta).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, name := range []string{"a.jpg", "b.jpg"} {
		p := filepath.Join(t.TempDir(), name)
		writeJPEG(t, p, 50+i, 50+i)
		src, err := medialibrary.FromPath(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := lib.AddMedia(ctx, meta, "seo_image", src); err != nil {
			t.Fatal(err)
		}
	}
	items, err := lib.GetMedia(meta, "seo_image")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 media after singleFile, got %d", len(items))
	}
	if items[0].FileName != "b.jpg" {
		t.Fatalf("expected b.jpg, got %s", items[0].FileName)
	}
}

func TestMimeWhitelist(t *testing.T) {
	db, lib, _ := setup(t)
	meta := SeoMeta{Title: "Y"}
	_ = db.Create(&meta)
	src, err := medialibrary.FromReader(bytes.NewReader([]byte("%PDF-1.4")), "doc.pdf", "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, err = lib.AddMedia(context.Background(), meta, "seo_image", src)
	if !errors.Is(err, medialibrary.ErrMimeNotAllowed) {
		t.Fatalf("expected ErrMimeNotAllowed, got %v", err)
	}
}

func TestDeleteRemovesFiles(t *testing.T) {
	db, lib, root := setup(t)
	meta := SeoMeta{Title: "Z"}
	_ = db.Create(&meta)
	p := filepath.Join(t.TempDir(), "z.jpg")
	writeJPEG(t, p, 80, 60)
	src, _ := medialibrary.FromPath(p)
	media, err := lib.AddMedia(context.Background(), meta, "seo_image", src)
	if err != nil {
		t.Fatal(err)
	}
	mediaDir := filepath.Join(root, filepath.Dir(medialibrary.OriginalPath(*media)))
	if err := lib.Delete(context.Background(), *media); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mediaDir); !os.IsNotExist(err) {
		t.Fatalf("expected media dir removed, err=%v", err)
	}
	_, err = lib.Find(media.ID)
	if !errors.Is(err, medialibrary.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetFirstMediaURL(t *testing.T) {
	db, lib, _ := setup(t)
	meta := SeoMeta{Title: "U"}
	_ = db.Create(&meta)
	p := filepath.Join(t.TempDir(), "u.jpg")
	writeJPEG(t, p, 100, 80)
	src, _ := medialibrary.FromPath(p)
	_, err := lib.AddMedia(context.Background(), meta, "seo_image", src)
	if err != nil {
		t.Fatal(err)
	}
	url, err := lib.GetFirstMediaURL(meta, "seo_image")
	if err != nil {
		t.Fatal(err)
	}
	if url == "" {
		t.Fatal("empty url")
	}
}

func TestModelFrom(t *testing.T) {
	_, lib, _ := setup(t)
	model := medialibrary.ModelFrom(99, "seo_metas")
	items, err := lib.GetMedia(model, "seo_image")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty, got %d", len(items))
	}
}

func TestResponsiveImagesGeneration(t *testing.T) {
	db, lib, root := setup(t)
	meta := SeoMeta{Title: "R"}
	if err := db.Create(&meta).Error; err != nil {
		t.Fatal(err)
	}
	srcPath := filepath.Join(t.TempDir(), "wide.jpg")
	writeJPEG(t, srcPath, 800, 600)

	src, err := medialibrary.FromPath(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	media, err := lib.AddMedia(context.Background(), meta, "seo_image", src)
	if err != nil {
		t.Fatalf("AddMedia: %v", err)
	}

	ri := media.ResponsiveImagesMap()
	orig, ok := ri["media_library_original"]
	if !ok || len(orig.URLs) == 0 {
		t.Fatalf("missing original responsive images: %#v", ri)
	}
	if orig.Base64SVG == "" {
		t.Fatal("expected base64svg placeholder")
	}
	large, ok := ri["seo_image_large"]
	if !ok || len(large.URLs) == 0 {
		t.Fatalf("missing conversion responsive images: %#v", ri)
	}
	if _, ok := ri["seo_image_small"]; ok {
		t.Fatal("seo_image_small should not have responsive images")
	}

	for _, name := range orig.URLs {
		p := filepath.Join(root, medialibrary.ResponsiveImagePath(*media, name))
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("responsive file missing %s: %v", name, err)
		}
	}

	urls, err := lib.GetResponsiveImageURLs(*media)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != len(orig.URLs) {
		t.Fatalf("urls=%d want=%d", len(urls), len(orig.URLs))
	}
	srcset, err := lib.GetSrcset(*media)
	if err != nil {
		t.Fatal(err)
	}
	if srcset == "" || !bytes.Contains([]byte(srcset), []byte("w")) {
		t.Fatalf("unexpected srcset: %q", srcset)
	}
}
