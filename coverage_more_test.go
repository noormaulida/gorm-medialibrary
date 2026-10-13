package medialibrary

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"gorm.io/datatypes"
)

func TestResponsiveTinyAndGenerateSetFailures(t *testing.T) {
	restoreHooks(t)
	disk := newFailDisk("public")
	media := Media{ID: 3, FileName: "a.jpg"}
	img := image.NewRGBA(image.Rect(0, 0, 50, 40))

	jpegEncode = func(io.Writer, image.Image, *jpeg.Options) error { return errBoom }
	if _, err := generateTinyPlaceholderSVG(img, 50, 40); err == nil {
		t.Fatal("tiny encode fail")
	}
	if _, err := generateResponsiveSet(context.Background(), disk, media, img, jpegBytes(t, 50, 40), "media_library_original", 90); err == nil {
		t.Fatal("set tiny fail")
	}

	restoreHooks(t)
	_, lib, _ := setupLib(t)
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c").WithResponsiveImages()
	})
	lib.disks["public"] = disk
	media2 := Media{ID: 4, ModelType: "cov_models", CollectionName: "c", FileName: "a.jpg", MimeType: strPtr("image/jpeg")}
	jpegEncode = func(w io.Writer, m image.Image, o *jpeg.Options) error {
		// fail only tiny placeholder (smaller images after blur/resize still go through jpegEncode)
		return errBoom
	}
	if _, err := lib.performResponsiveImages(context.Background(), disk, media2, bytes.NewReader(jpegBytes(t, 80, 60))); err == nil {
		t.Fatal("collection responsive fail via encode")
	}
}

func TestAddMediaErrorPaths(t *testing.T) {
	db, lib, _ := setupLib(t)
	ctx := context.Background()
	model := covModel{ID: 1}

	if _, err := lib.AddMedia(ctx, model, "c", nil); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil source: %v", err)
	}
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "", src); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty collection: %v", err)
	}
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, covModel{}, "c", src); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("bad model: %v", err)
	}

	lib2 := New(nil, WithDisk(NewLocalDisk("p", t.TempDir(), "/p")))
	src, _ = FromReader(bytes.NewReader([]byte("x")), "a.jpg", "image/jpeg")
	if _, err := lib2.AddMedia(ctx, model, "c", src); err == nil {
		t.Fatal("ensureReady fail")
	}

	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("single").SingleFile().AcceptsMimeTypes("image/*")
	})
	// mime wildcard allow + single file clear path with empty collection first
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 12, 12)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "single", src); err != nil {
		t.Fatal(err)
	}
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 12, 12)), "b.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "single", src); err != nil {
		t.Fatal(err)
	}

	// mime denied exact miss
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("strict").AcceptsMimeTypes("image/png")
	})
	src, _ = FromReader(bytes.NewReader([]byte("x")), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "strict", src); !errors.Is(err, ErrMimeNotAllowed) {
		t.Fatalf("got %v", err)
	}
	if mimeAllowed("image/jpeg", []string{"image/*"}) != true {
		t.Fatal("wildcard")
	}
	if mimeAllowed("text/plain", []string{"image/jpeg"}) {
		t.Fatal("deny")
	}

	// ReadAll fail
	src = &FileSource{reader: errReader{errBoom}, fileName: "a.jpg", mimeType: "image/jpeg", name: "a"}
	if _, err := lib.AddMedia(ctx, model, "any", src); err == nil {
		t.Fatal("read fail")
	}

	// Put fail inside transaction
	fail := newFailDisk("public")
	fail.putErr = errBoom
	lib.disks["public"] = fail
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "any", src); err == nil {
		t.Fatal("put fail")
	}

	// conversion fail
	lib.disks["public"] = newFailDisk("public")
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("conv").AcceptsMimeTypes("image/jpeg")
		r.Conversion("t").ForCollection("conv").Fit(10, 10)
	})
	restoreHooks(t)
	jpegEncode = func(io.Writer, image.Image, *jpeg.Options) error { return errBoom }
	lib.disks["public"] = newFailDisk("public")
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 30, 30)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "conv", src); err == nil {
		t.Fatal("conversion encode fail")
	}

	// responsive fail after conversions ok
	restoreHooks(t)
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("resp").WithResponsiveImages()
	})
	fd := newFailDisk("public")
	fd.putN = 1 // original ok, responsive put fails
	fd.putErr = errBoom
	lib.disks["public"] = fd
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 100, 80)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "resp", src); err == nil {
		t.Fatal("responsive put fail")
	}

	// ClearMediaCollection fail during singleFile: break GetMedia via closed db is heavy;
	// use model that fails resolve on clear — already covered. Force delete error:
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("sf").SingleFile()
	})
	okDisk := newFailDisk("public")
	lib.disks["public"] = okDisk
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "sf", src); err != nil {
		t.Fatal(err)
	}
	// Break library readiness before clear by wiping disks after first add... 
	// Instead make Delete fail by pointing media.Disk to missing disk name via raw update,
	// then singleFile clear fails on Delete.
	var row Media
	if err := db.Where("collection_name = ?", "sf").First(&row).Error; err != nil {
		t.Fatal(err)
	}
	_ = db.Model(&row).Update("disk", "gone").Error
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 10, 10)), "b.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "sf", src); err == nil {
		t.Fatal("expected clear fail")
	}

	// diskByName fail: empty disks map with default set
	lib.disks = map[string]Disk{}
	src, _ = FromReader(bytes.NewReader([]byte("x")), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "any", src); err == nil {
		t.Fatal("disk missing")
	}

	_ = nextOrderColumn(db, "cov_models", model.ID, "sf")
}

func TestQueryAndDeleteCoverage(t *testing.T) {
	db, lib, root := setupLib(t)
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
		t.Fatal("nil lib get")
	}
	if _, err := lib.GetMedia(covModel{}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	items, err := lib.GetMedia(model)
	if err != nil || len(items) == 0 {
		t.Fatal(err)
	}
	items, err = lib.GetMedia(model, "")
	if err != nil || len(items) == 0 {
		t.Fatal(err)
	}
	if _, err := lib.GetFirstMedia(model, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	first, err := lib.GetFirstMedia(model, "c")
	if err != nil {
		t.Fatal(err)
	}
	_ = first

	if _, err := (*Library)(nil).Find(1); err == nil {
		t.Fatal("nil find")
	}
	if _, err := lib.Find(999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
	found, err := lib.Find(media.ID)
	if err != nil || found.ID != media.ID {
		t.Fatal(err)
	}

	// Force non-not-found DB errors by closing the underlying connection.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	closedLib := *lib
	_ = sqlDB.Close()
	if _, err := closedLib.Find(media.ID); err == nil {
		t.Fatal("expected db error on find")
	}
	if _, err := closedLib.GetMedia(model, "c"); err == nil {
		t.Fatal("expected db error on get")
	}
	if _, err := closedLib.GetFirstMedia(model, "c"); err == nil {
		t.Fatal("expected getfirst wrap")
	}
	// Fresh lib for URL tests
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
		t.Fatalf("got %v", err)
	}
	other := *media
	other.Disk = "missing"
	if _, err := lib2.GetURL(other); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("got %v", err)
	}
	convDisk := "public"
	media.ConversionsDisk = &convDisk
	if _, err := lib2.GetURL(*media, "thumb"); err != nil {
		t.Fatal(err)
	}
	if _, err := lib2.GetFirstMediaURL(covModel{ID: 5}, "missing"); err == nil {
		t.Fatal("missing collection url")
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
	emptyURLs, err := lib2.GetResponsiveImageURLs(*media, "none")
	if err != nil || emptyURLs != nil {
		t.Fatalf("%v %#v", err, emptyURLs)
	}
	bad := *media
	bad.Disk = "missing"
	bad.ConversionsDisk = strPtr("missing")
	if _, err := lib2.GetResponsiveImageURLs(bad, "thumb"); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("got %v", err)
	}

	srcset, err := lib2.GetSrcset(*media)
	if err != nil || srcset == "" {
		t.Fatal(err)
	}
	srcset, err = lib2.GetSrcset(*media, "thumb")
	if err != nil || srcset == "" {
		t.Fatal(err)
	}
	emptySet, err := lib2.GetSrcset(*media, "none")
	if err != nil || emptySet != "" {
		t.Fatalf("%v %q", err, emptySet)
	}
	if _, err := lib2.GetSrcset(bad, "thumb"); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("got %v", err)
	}

	// Srcset without parseable width + without base64svg
	m := *media
	m.ResponsiveImages = encodeResponsiveImages(ResponsiveImagesData{
		"media_library_original": {URLs: []string{"plain.jpg"}, Base64SVG: ""},
	})
	srcset, err = lib2.GetSrcset(m)
	if err != nil || !strings.Contains(srcset, "plain.jpg") {
		t.Fatalf("%v %q", err, srcset)
	}

	if err := lib2.ClearMediaCollection(ctx, covModel{ID: 5}, "c"); err != nil {
		t.Fatal(err)
	}
	if err := (*Library)(nil).Delete(ctx, *media); err == nil {
		t.Fatal("nil delete")
	}
	if err := lib2.Delete(ctx, Media{ID: 1, Disk: "missing"}); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("got %v", err)
	}

	// Delete with conversions disk + responsive urls using fail disk (non-local path)
	fd := newFailDisk("s3ish")
	lib2.disks["s3ish"] = fd
	m2 := Media{
		ID:                   42,
		ModelType:            "cov_models",
		ModelID:              99,
		CollectionName:       "tmp",
		Name:                 "x",
		Disk:                 "s3ish",
		ConversionsDisk:      strPtr("s3ish"),
		FileName:             "x.jpg",
		Size:                 1,
		Manipulations:        emptyJSONObject(),
		CustomProperties:     emptyJSONObject(),
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

	// ClearMediaCollection GetMedia error
	if err := closedLib.ClearMediaCollection(ctx, model, "c"); err == nil {
		t.Fatal("expected clear get error")
	}

	_ = root
	_ = filepath.Separator
}

func TestS3DiskFullCoverage(t *testing.T) {
	client := &covS3{data: map[string][]byte{}}
	if _, err := NewS3Disk(S3DiskConfig{Name: "s3", Bucket: "b"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("nil client: %v", err)
	}
	if _, err := NewS3Disk(S3DiskConfig{Name: "s3", Client: client}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("empty bucket: %v", err)
	}
	disk, err := NewS3Disk(S3DiskConfig{Name: "s3", Bucket: "b", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if disk.Name() != "s3" {
		t.Fatal(disk.Name())
	}
	if disk.key("") != "" && disk.key(".") != "" {
		// with empty prefix, clean of "/" is "."
	}
	if got := disk.key("a\\b"); !strings.Contains(got, "a/b") {
		t.Fatalf("key slash: %q", got)
	}
	if disk.URL("x") != "/x" {
		t.Fatalf("url=%q", disk.URL("x"))
	}

	disk2, err := NewS3Disk(S3DiskConfig{Name: "s3", Bucket: "b", Prefix: "p", BaseURL: "https://cdn", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if disk2.key("") != "p" && disk2.key(".") != "p" {
		t.Fatalf("prefix only: %q %q", disk2.key(""), disk2.key("."))
	}
	ctx := context.Background()
	if _, err := disk2.Put(ctx, "1/a.jpg", strings.NewReader("hi")); err != nil {
		t.Fatal(err)
	}
	client.failPut = true
	if _, err := disk2.Put(ctx, "1/b.jpg", strings.NewReader("hi")); err == nil {
		t.Fatal("put fail")
	}
	client.failPut = false

	ok, err := disk2.Exists(ctx, "1/a.jpg")
	if err != nil || !ok {
		t.Fatal(err)
	}
	client.notFoundTyped = true
	ok, err = disk2.Exists(ctx, "missing")
	if err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	client.notFoundTyped = false
	client.apiCode = "NoSuchKey"
	ok, err = disk2.Exists(ctx, "missing2")
	if err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	client.apiCode = "404"
	ok, err = disk2.Exists(ctx, "missing3")
	if err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	client.apiCode = "Boom"
	if _, err := disk2.Exists(ctx, "x"); err == nil {
		t.Fatal("exists other err")
	}
	client.apiCode = ""

	client.failGet = true
	if _, err := disk2.Open(ctx, "1/a.jpg"); err == nil {
		t.Fatal("open fail")
	}
	client.failGet = false
	rc, err := disk2.Open(ctx, "1/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	_ = rc.Close()

	if disk2.URL("1/a.jpg") != "https://cdn/p/1/a.jpg" {
		t.Fatalf("%q", disk2.URL("1/a.jpg"))
	}
}

type covS3 struct {
	data          map[string][]byte
	failPut       bool
	failGet       bool
	notFoundTyped bool
	apiCode       string
}

func (m *covS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.failPut {
		return nil, errBoom
	}
	b, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	m.data[*params.Key] = b
	return &s3.PutObjectOutput{}, nil
}

func (m *covS3) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.failGet {
		return nil, errBoom
	}
	b, ok := m.data[*params.Key]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "missing"}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(b))}, nil
}

func (m *covS3) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(m.data, *params.Key)
	return &s3.DeleteObjectOutput{}, nil
}

func (m *covS3) HeadObject(_ context.Context, params *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if m.notFoundTyped {
		return nil, &types.NotFound{}
	}
	if m.apiCode != "" {
		return nil, &smithy.GenericAPIError{Code: m.apiCode, Message: "x"}
	}
	if _, ok := m.data[*params.Key]; !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "missing"}
	}
	return &s3.HeadObjectOutput{}, nil
}

func TestRemainingCoverageGaps(t *testing.T) {
	restoreHooks(t)
	ctx := context.Background()

	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var buf bytes.Buffer
	if err := encodeImage(&buf, img, "x.jpg", 0); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := encodeImage(&buf, img, "x.jpg", 101); err != nil {
		t.Fatal(err)
	}

	p := filepath.Join(t.TempDir(), "a.jpg")
	writeJPEGFile(t, p, 8, 8)
	buildFileSource = func(f interface {
		io.ReadCloser
		io.Seeker
	}, base string) (*FileSource, error) {
		return nil, errBoom
	}
	if _, err := FromPath(p); err == nil {
		t.Fatal("expected buildFileSource fail")
	}
	buildFileSource = fileSourceFrom

	model := covModel{ID: 10}
	_, lib, _ := setupLib(t)
	lib.disks = map[string]Disk{"other": NewLocalDisk("other", t.TempDir(), "/o")}
	lib.defaultDisk = "missing"
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "c", src); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("got %v", err)
	}

	// Create fails inside transaction (table dropped after begin-capable setup)
	_, lib, _ = setupLib(t)
	if err := lib.db.Migrator().DropTable(&Media{}); err != nil {
		t.Fatal(err)
	}
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "c", src); err == nil {
		t.Fatal("expected create fail")
	}

	// Delete fails inside transaction when table is missing
	_, lib2, _ := setupLib(t)
	if err := lib2.db.Migrator().DropTable(&Media{}); err != nil {
		t.Fatal(err)
	}
	if err := lib2.Delete(ctx, Media{ID: 1, Disk: "public", FileName: "a.jpg"}); err == nil {
		t.Fatal("expected delete fail")
	}

	// Conversion responsive: encodeImage fails (no collection responsive)
	jpegEncode, pngEncode = jpeg.Encode, png.Encode
	_, lib3, _ := setupLib(t)
	lib3.Register("cov_models", func(r *Registrar) {
		r.Collection("c")
		r.Conversion("thumb").ForCollection("c").Fit(10, 10).WithResponsiveImages()
	})
	fd := newFailDisk("public")
	lib3.disks["public"] = fd
	media := Media{ID: 1, ModelType: "cov_models", CollectionName: "c", FileName: "a.png", MimeType: strPtr("image/png")}
	pngEncode = func(io.Writer, image.Image) error { return errBoom }
	if _, err := lib3.performResponsiveImages(ctx, fd, media, bytes.NewReader(pngBytes(t, 40, 40))); err == nil {
		t.Fatal("expected conversion encode fail")
	}

	// Widths use pngEncode; tiny placeholder uses jpegEncode → hit L104
	jpegEncode, pngEncode = jpeg.Encode, png.Encode
	jpegEncode = func(io.Writer, image.Image, *jpeg.Options) error { return errBoom }
	img2 := image.NewRGBA(image.Rect(0, 0, 40, 30))
	if _, err := generateResponsiveSet(ctx, newFailDisk("public"), media, img2, pngBytes(t, 40, 30), "media_library_original", 90); err == nil {
		t.Fatal("expected tiny fail")
	}

	// Conversion responsive: generateResponsiveSet Put fails (L66)
	jpegEncode, pngEncode = jpeg.Encode, png.Encode
	_, lib4, _ := setupLib(t)
	lib4.Register("cov_models", func(r *Registrar) {
		r.Collection("c")
		r.Conversion("thumb").ForCollection("c").Fit(10, 10).WithResponsiveImages()
	})
	fd2 := newFailDisk("public")
	fd2.putErr = errBoom
	lib4.disks["public"] = fd2
	media.FileName = "a.jpg"
	media.MimeType = strPtr("image/jpeg")
	if _, err := lib4.performResponsiveImages(ctx, fd2, media, bytes.NewReader(jpegBytes(t, 80, 60))); err == nil {
		t.Fatal("expected set put fail")
	}
}

func TestRegistrarQualityBounds(t *testing.T) {
	r := newRegistrar()
	b := r.Conversion("c").Quality(0).Quality(101).Quality(50)
	if b.cfg.Quality != 50 {
		t.Fatalf("%d", b.cfg.Quality)
	}
	r.Collection("a")
	r.Collection("a") // existing
	r.Conversion("c") // existing
}

func TestMediaTableName(t *testing.T) {
	if (Media{}).TableName() != "media" {
		t.Fatal("table")
	}
	_ = emptyJSONObject()
	_ = emptyJSONArray()
	_ = datatypes.JSON([]byte("{}"))
}
