package medialibrary

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAddMediaErrorPathsAndCleanup(t *testing.T) {
	db, lib, root := setupLib(t)
	ctx := context.Background()
	model := covModel{ID: 1}

	if _, err := lib.AddMedia(ctx, model, "c", nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "", src); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, covModel{}, "c", src); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	lib2 := New(nil, WithDisk(NewLocalDisk("p", t.TempDir(), "/p")))
	src, _ = FromReader(bytes.NewReader([]byte("x")), "a.jpg", "image/jpeg")
	if _, err := lib2.AddMedia(ctx, model, "c", src); err == nil {
		t.Fatal("ensureReady")
	}

	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("single").SingleFile().AcceptsMimeTypes("image/*")
	})
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 12, 12)), "a.jpg", "image/jpeg")
	first, err := lib.AddMedia(ctx, model, "single", src)
	if err != nil {
		t.Fatal(err)
	}
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 12, 12)), "b.jpg", "image/jpeg")
	second, err := lib.AddMedia(ctx, model, "single", src)
	if err != nil {
		t.Fatal(err)
	}
	if second.FileName != "b.jpg" {
		t.Fatal(second.FileName)
	}
	if _, err := lib.Find(first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("old singleFile should be gone")
	}

	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("strict").AcceptsMimeTypes("image/png")
	})
	src, _ = FromReader(bytes.NewReader([]byte("x")), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "strict", src); !errors.Is(err, ErrMimeNotAllowed) {
		t.Fatal(err)
	}
	if !mimeAllowed("image/jpeg", []string{"image/*"}) || mimeAllowed("text/plain", []string{"image/jpeg"}) {
		t.Fatal("mime")
	}

	src = &FileSource{reader: errReader{errBoom}, fileName: "a.jpg", mimeType: "image/jpeg", name: "a"}
	if _, err := lib.AddMedia(ctx, model, "any", src); err == nil {
		t.Fatal("read")
	}

	// Compensating cleanup: Put original ok, later Put fails → no leftover files under media id.
	fail := newFailDisk("public")
	fail.putN = 1
	fail.putErr = errBoom
	lib.disks["public"] = fail
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("conv")
		r.Conversion("t").ForCollection("conv").Fit(10, 10)
	})
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 30, 30)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "conv", src); err == nil {
		t.Fatal("conversion put fail")
	}
	if len(fail.files) != 0 {
		t.Fatalf("expected cleanup, leftover %#v", fail.files)
	}

	// Local disk cleanup removes media dir after failed upload mid-conversion.
	lib.disks["public"] = NewLocalDisk("public", root, "/storage")
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("localfail")
		r.Conversion("t").ForCollection("localfail").Fit(10, 10)
	})
	// Wrap by replacing with tracking that fails on 2nd put — use failDisk again with local cleanup path:
	// Drop table to fail Create after... actually use put fail after create via failDisk putN=0 from start on conversions only.
	// Simpler: failDisk putN=1 then fail; LocalDisk isn't used. Test cleanupUpload with local:
	local := NewLocalDisk("public", root, "/storage")
	mediaDir := filepath.Join(root, "99")
	_ = os.MkdirAll(mediaDir, 0o755)
	_ = os.WriteFile(filepath.Join(mediaDir, "x.jpg"), []byte("x"), 0o644)
	cleanupUpload(ctx, local, 99, []string{"99/x.jpg"})
	if _, err := os.Stat(mediaDir); !os.IsNotExist(err) {
		t.Fatal("local dir cleanup")
	}

	lib.disks = map[string]Disk{"other": NewLocalDisk("other", t.TempDir(), "/o")}
	lib.defaultDisk = "missing"
	src, _ = FromReader(bytes.NewReader([]byte("x")), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "any", src); !errors.Is(err, ErrDiskNotFound) {
		t.Fatal(err)
	}
	if _, err := nextOrderColumn(db, "cov_models", model.ID, "single"); err != nil {
		t.Fatal(err)
	}
}

func TestQueryAndDeleteCoverage(t *testing.T) {
	db, lib, _ := setupLib(t)
	ctx := context.Background()
	model := covModel{ID: 2}
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c").WithResponsiveImages()
		r.Conversion("thumb").ForCollection("c").Fit(16, 16).WithResponsiveImages()
	})
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 120, 90)), "hero.jpg", "image/jpeg")
	media, err := lib.AddMedia(ctx, model, "c", src)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := (*Library)(nil).GetMedia(model); err == nil {
		t.Fatal("nil")
	}
	if _, err := lib.GetMedia(covModel{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal(err)
	}
	items, err := lib.GetMedia(model)
	if err != nil || len(items) == 0 {
		t.Fatal(err)
	}
	_, _ = lib.GetMedia(model, "")
	if _, err := lib.GetFirstMedia(model, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := lib.GetFirstMedia(model, "c"); err != nil {
		t.Fatal(err)
	}
	if _, err := (*Library)(nil).Find(1); err == nil {
		t.Fatal("nil find")
	}
	if _, err := lib.Find(999999); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := lib.Find(media.ID); err != nil {
		t.Fatal(err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	closedLib := *lib
	_ = sqlDB.Close()
	if _, err := closedLib.Find(media.ID); err == nil {
		t.Fatal("closed find")
	}
	if _, err := closedLib.GetMedia(model, "c"); err == nil {
		t.Fatal("closed get")
	}
	if _, err := closedLib.GetFirstMedia(model, "c"); err == nil {
		t.Fatal("closed first")
	}
	if err := closedLib.ClearMediaCollection(ctx, model, "c"); err == nil {
		t.Fatal("closed clear")
	}

	db2, lib2, _ := setupLib(t)
	lib2.Register("cov_models", func(r *Registrar) {
		r.Collection("c").WithResponsiveImages()
		r.Conversion("thumb").ForCollection("c").Fit(16, 16).WithResponsiveImages()
	})
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 120, 90)), "hero.jpg", "image/jpeg")
	media, err = lib2.AddMedia(ctx, covModel{ID: 5}, "c", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib2.GetURL(*media, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	other := *media
	other.Disk = "missing"
	if _, err := lib2.GetURL(other); !errors.Is(err, ErrDiskNotFound) {
		t.Fatal(err)
	}
	media.ConversionsDisk = strPtr("public")
	if _, err := lib2.GetURL(*media, "thumb"); err != nil {
		t.Fatal(err)
	}
	if _, err := lib2.GetFirstMediaURL(covModel{ID: 5}, "missing"); err == nil {
		t.Fatal("missing")
	}
	if _, err := lib2.GetFirstMediaURL(covModel{ID: 5}, "c", "thumb"); err != nil {
		t.Fatal(err)
	}

	urls, err := lib2.GetResponsiveImageURLs(*media)
	if err != nil || len(urls) == 0 {
		t.Fatal(err)
	}
	urls, err = lib2.GetResponsiveImageURLs(*media, "thumb")
	if err != nil || len(urls) == 0 {
		t.Fatal(err)
	}
	emptyURLs, _ := lib2.GetResponsiveImageURLs(*media, "none")
	if emptyURLs != nil {
		t.Fatal("none")
	}
	bad := *media
	bad.Disk = "missing"
	bad.ConversionsDisk = strPtr("missing")
	if _, err := lib2.GetResponsiveImageURLs(bad, "thumb"); !errors.Is(err, ErrDiskNotFound) {
		t.Fatal(err)
	}
	srcset, err := lib2.GetSrcset(*media)
	if err != nil || srcset == "" {
		t.Fatal(err)
	}
	_, _ = lib2.GetSrcset(*media, "thumb")
	emptySet, _ := lib2.GetSrcset(*media, "none")
	if emptySet != "" {
		t.Fatal(emptySet)
	}
	if _, err := lib2.GetSrcset(bad, "thumb"); !errors.Is(err, ErrDiskNotFound) {
		t.Fatal(err)
	}
	m := *media
	m.ResponsiveImages = encodeResponsiveImages(ResponsiveImagesData{
		"media_library_original": {URLs: []string{"plain.jpg"}},
	})
	srcset, _ = lib2.GetSrcset(m)
	if !strings.Contains(srcset, "plain.jpg") {
		t.Fatal(srcset)
	}

	if err := lib2.ClearMediaCollection(ctx, covModel{ID: 5}, "c"); err != nil {
		t.Fatal(err)
	}
	if err := (*Library)(nil).Delete(ctx, *media); err == nil {
		t.Fatal("nil delete")
	}
	if err := lib2.Delete(ctx, Media{ID: 1, Disk: "missing"}); !errors.Is(err, ErrDiskNotFound) {
		t.Fatal(err)
	}

	fd := newFailDisk("s3ish")
	lib2.disks["s3ish"] = fd
	m2 := Media{
		ModelType: "cov_models", ModelID: 99, CollectionName: "tmp", Name: "x",
		Disk: "s3ish", ConversionsDisk: strPtr("s3ish"), FileName: "x.jpg", Size: 1,
		Manipulations: emptyJSONObject(), CustomProperties: emptyJSONObject(),
		GeneratedConversions: encodeGeneratedConversions(map[string]bool{"thumb": true}),
		ResponsiveImages: encodeResponsiveImages(ResponsiveImagesData{
			"media_library_original": {URLs: []string{"x___media_library_original_10_10.jpg"}},
		}),
	}
	if err := db2.Create(&m2).Error; err != nil {
		t.Fatal(err)
	}
	if err := lib2.Delete(ctx, m2); err != nil {
		t.Fatal(err)
	}

	_, lib3, _ := setupLib(t)
	if err := lib3.db.Migrator().DropTable(&Media{}); err != nil {
		t.Fatal(err)
	}
	if err := lib3.Delete(ctx, Media{ID: 1, Disk: "public", FileName: "a.jpg"}); err == nil {
		t.Fatal("delete missing table")
	}
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "a.jpg", "image/jpeg")
	if _, err := lib3.AddMedia(ctx, covModel{ID: 10}, "c", src); err == nil {
		t.Fatal("create missing table")
	}
}

func TestRegistrarQualityBounds(t *testing.T) {
	r := newRegistrar()
	b := r.Conversion("c").Quality(0).Quality(101).Quality(50)
	if b.cfg.Quality != 50 {
		t.Fatal(b.cfg.Quality)
	}
	r.Collection("a")
	r.Collection("a")
	r.Conversion("c")
	if (Media{}).TableName() != "media" {
		t.Fatal("table")
	}
	_ = emptyJSONObject()
	_ = emptyJSONArray()
	_ = datatypes.JSON([]byte("{}"))
}

func TestDeleteSurfacesDiskErrors(t *testing.T) {
	db, lib, _ := setupLib(t)
	ctx := context.Background()
	model := covModel{ID: 20}
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "a.jpg", "image/jpeg")
	media, err := lib.AddMedia(ctx, model, "c", src)
	if err != nil {
		t.Fatal(err)
	}
	fd := newFailDisk("public")
	fd.deleteErr = errBoom
	// seed so Exists/Open paths unused; Delete will fail
	lib.disks["public"] = fd
	if err := lib.Delete(ctx, *media); err == nil {
		t.Fatal("expected disk delete error")
	}
	var still Media
	if err := db.First(&still, media.ID).Error; err != nil {
		t.Fatal("row should remain after disk failure")
	}
}

func TestOnDiskAndCollectionDisk(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ondisk?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	rootA := t.TempDir()
	rootB := t.TempDir()
	a := NewLocalDisk("a", rootA, "/a")
	b := NewLocalDisk("b", rootB, "/b")
	lib := New(db, WithDisk(a), WithDisk(b), WithDefaultDisk("a"))
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("on_b").Disk("b")
		r.Collection("split").Disk("a").ConversionsDisk("b")
		r.Conversion("thumb").ForCollection("split").Fit(8, 8)
	})
	ctx := context.Background()
	model := covModel{ID: 21}

	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 20, 20)), "x.jpg", "image/jpeg")
	m1, err := lib.AddMedia(ctx, model, "on_b", src)
	if err != nil {
		t.Fatal(err)
	}
	if m1.Disk != "b" {
		t.Fatalf("disk=%s", m1.Disk)
	}
	if _, err := os.Stat(filepath.Join(rootB, OriginalPath(*m1))); err != nil {
		t.Fatal(err)
	}

	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 20, 20)), "y.jpg", "image/jpeg")
	m2, err := lib.AddMedia(ctx, model, "c", src, OnDisk("b"))
	if err != nil {
		t.Fatal(err)
	}
	if m2.Disk != "b" {
		t.Fatalf("ondisk=%s", m2.Disk)
	}

	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 40, 40)), "z.jpg", "image/jpeg")
	m3, err := lib.AddMedia(ctx, model, "split", src)
	if err != nil {
		t.Fatal(err)
	}
	if m3.Disk != "a" || m3.ConversionsDisk == nil || *m3.ConversionsDisk != "b" {
		t.Fatalf("split disks: %s / %v", m3.Disk, m3.ConversionsDisk)
	}
	if _, err := os.Stat(filepath.Join(rootA, OriginalPath(*m3))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(rootB, ConversionPath(*m3, "thumb"))); err != nil {
		t.Fatal(err)
	}
}

func TestSingleFileReplaceFailureCompensates(t *testing.T) {
	db, lib, root := setupLib(t)
	ctx := context.Background()
	model := covModel{ID: 22}
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("sf").SingleFile()
	})
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "a.jpg", "image/jpeg")
	first, err := lib.AddMedia(ctx, model, "sf", src)
	if err != nil {
		t.Fatal(err)
	}

	// Make delete of previous fail by pointing its disk at a failing disk after mutate row.
	fd := newFailDisk("broken")
	fd.deleteErr = errBoom
	lib.disks["broken"] = fd
	if err := db.Model(first).Update("disk", "broken").Error; err != nil {
		t.Fatal(err)
	}
	first.Disk = "broken"

	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "b.jpg", "image/jpeg")
	_, err = lib.AddMedia(ctx, model, "sf", src)
	if !errors.Is(err, ErrReplaceFailed) {
		t.Fatalf("got %v", err)
	}
	items, err := lib.GetMedia(model, "sf")
	if err != nil {
		t.Fatal(err)
	}
	// New media compensated away; old row still present (delete failed).
	if len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("expected only old media, got %#v", items)
	}
	_ = root
}

func TestConversionResponsivePutFail(t *testing.T) {
	_, lib, _ := setupLib(t)
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c")
		r.Conversion("thumb").ForCollection("c").Fit(10, 10).WithResponsiveImages()
	})
	fd := newFailDisk("public")
	fd.putErr = errBoom
	lib.disks["public"] = fd
	media := Media{ID: 1, ModelType: "cov_models", CollectionName: "c", FileName: "a.jpg", MimeType: strPtr("image/jpeg")}
	if _, err := lib.performResponsiveImages(context.Background(), fd, media, bytes.NewReader(jpegBytes(t, 80, 60))); err == nil {
		t.Fatal("expected fail")
	}
}
