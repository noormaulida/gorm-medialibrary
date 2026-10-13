package medialibrary

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOnConversionsDiskAndEmptyOpts(t *testing.T) {
	restoreHooks(t)
	db, err := gorm.Open(sqlite.Open("file:ocd?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = Migrate(db)
	a := NewLocalDisk("a", t.TempDir(), "/a")
	b := NewLocalDisk("b", t.TempDir(), "/b")
	lib := New(db, WithDisk(a), WithDisk(b), WithDefaultDisk("a"))
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c")
		r.Conversion("thumb").ForCollection("c").Fit(8, 8)
	})
	OnDisk("")(&addOptions{})
	OnConversionsDisk("")(&addOptions{})

	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 20, 20)), "z.jpg", "image/jpeg")
	m, err := lib.AddMedia(context.Background(), covModel{ID: 1}, "c", src, OnConversionsDisk("b"))
	if err != nil {
		t.Fatal(err)
	}
	if m.ConversionsDisk == nil || *m.ConversionsDisk != "b" {
		t.Fatalf("%v", m.ConversionsDisk)
	}
}

func TestAddMediaMissingConvDiskAndSingleFileGetFail(t *testing.T) {
	_, lib, _ := setupLib(t)
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("bad").ConversionsDisk("nope")
		r.Collection("sf").SingleFile()
	})
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(context.Background(), covModel{ID: 2}, "bad", src); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("got %v", err)
	}

	sqlDB, _ := lib.db.DB()
	_ = sqlDB.Close()
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(context.Background(), covModel{ID: 2}, "sf", src); err == nil {
		t.Fatal("expected getmedia fail")
	}
}

func TestAddMediaPutAndResponsiveFailCleanupSplitDisks(t *testing.T) {
	restoreHooks(t)
	db, err := gorm.Open(sqlite.Open("file:splitfail?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	_ = Migrate(db)
	orig := newFailDisk("a")
	conv := newFailDisk("b")
	lib := New(db, WithDisk(orig), WithDisk(conv), WithDefaultDisk("a"))
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c").Disk("a").ConversionsDisk("b").WithResponsiveImages()
	})
	conv.putErr = errBoom
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 100, 80)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(context.Background(), covModel{ID: 3}, "c", src); err == nil {
		t.Fatal("expected fail")
	}
	if len(orig.files) != 0 || len(conv.files) != 0 {
		t.Fatalf("cleanup leftover orig=%d conv=%d", len(orig.files), len(conv.files))
	}

	orig2 := newFailDisk("a")
	orig2.putErr = errBoom
	lib.disks["a"] = orig2
	lib.Register("cov_models", func(r *Registrar) { r.Collection("c2") })
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "a.jpg", "image/jpeg")
	if _, err := lib.AddMedia(context.Background(), covModel{ID: 4}, "c2", src); err == nil {
		t.Fatal("put fail")
	}
}

func TestAddMediaCreateFailUUIDConflict(t *testing.T) {
	restoreHooks(t)
	db, lib, _ := setupLib(t)
	fixed := "11111111-1111-1111-1111-111111111111"
	dup := Media{
		ModelType: "cov_models", ModelID: 1, UUID: &fixed, CollectionName: "c", Name: "n",
		FileName: "a.jpg", Disk: "public", Size: 1,
		Manipulations: emptyJSONObject(), CustomProperties: emptyJSONObject(),
		GeneratedConversions: emptyJSONObject(), ResponsiveImages: emptyJSONArray(),
	}
	if err := db.Create(&dup).Error; err != nil {
		t.Fatal(err)
	}
	newUUID = func() string { return fixed }
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "b.jpg", "image/jpeg")
	if _, err := lib.AddMedia(context.Background(), covModel{ID: 9}, "c", src); err == nil {
		t.Fatal("expected uuid conflict")
	}
}

func TestAddMediaReplaceCompensateBothFail(t *testing.T) {
	_, lib, _ := setupLib(t)
	ctx := context.Background()
	model := covModel{ID: 5}
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("sf").SingleFile()
	})
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "a.jpg", "image/jpeg")
	first, err := lib.AddMedia(ctx, model, "sf", src)
	if err != nil {
		t.Fatal(err)
	}

	fd := newFailDisk("broken")
	fd.deleteErr = errBoom
	lib.disks["broken"] = fd
	if err := lib.db.Model(first).Update("disk", "broken").Error; err != nil {
		t.Fatal(err)
	}
	lib.disks["public"] = fd
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "b.jpg", "image/jpeg")
	_, err = lib.AddMedia(ctx, model, "sf", src)
	if !errors.Is(err, ErrReplaceFailed) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "compensate") {
		t.Fatalf("want compensate msg, got %v", err)
	}
}

func TestDeleteConversionResponsiveAndRemoveAllErrors(t *testing.T) {
	restoreHooks(t)
	_, lib, root := setupLib(t)
	ctx := context.Background()
	model := covModel{ID: 7}
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c").WithResponsiveImages()
		r.Conversion("thumb").ForCollection("c").Fit(10, 10)
	})
	src, _ := FromReader(bytes.NewReader(jpegBytes(t, 60, 40)), "a.jpg", "image/jpeg")
	media, err := lib.AddMedia(ctx, model, "c", src)
	if err != nil {
		t.Fatal(err)
	}

	m := *media
	m.ConversionsDisk = strPtr("missing")
	if err := lib.Delete(ctx, m); !errors.Is(err, ErrDiskNotFound) {
		t.Fatalf("got %v", err)
	}

	fd := newFailDisk("public")
	fd.deleteErr = errBoom
	lib.disks["public"] = fd
	if err := lib.Delete(ctx, *media); err == nil {
		t.Fatal("expected delete errors")
	}

	lib.disks["public"] = NewLocalDisk("public", root, "/storage")
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "c.jpg", "image/jpeg")
	if _, err := lib.AddMedia(ctx, model, "c", src); err != nil {
		t.Fatal(err)
	}
	lib.disks["public"] = fd
	if err := lib.ClearMediaCollection(ctx, model, "c"); err == nil {
		t.Fatal("clear fail")
	}

	restoreHooks(t)
	lib.Register("cov_models", func(r *Registrar) { r.Collection("plain") })
	lib.disks["public"] = NewLocalDisk("public", root, "/storage")
	src, _ = FromReader(bytes.NewReader(jpegBytes(t, 8, 8)), "d.jpg", "image/jpeg")
	media2, err := lib.AddMedia(ctx, covModel{ID: 8}, "plain", src)
	if err != nil {
		t.Fatal(err)
	}
	removeAll = func(string) error { return errBoom }
	if err := lib.Delete(ctx, *media2); err == nil {
		t.Fatal("removeAll fail")
	}
}

func TestEncodeAndResponsiveHookFailures(t *testing.T) {
	restoreHooks(t)
	resetEncoders := func() {
		jpegEncode = func(w io.Writer, m image.Image, o *jpeg.Options) error {
			return jpeg.Encode(w, m, o)
		}
		pngEncode = func(w io.Writer, m image.Image) error {
			return png.Encode(w, m)
		}
		jsonMarshal = json.Marshal
	}

	_, lib, _ := setupLib(t)
	disk := newFailDisk("public")
	lib.disks["public"] = disk
	media := Media{ID: 1, ModelType: "cov_models", CollectionName: "c", FileName: "a.png", MimeType: strPtr("image/png")}
	payload := pngBytes(t, 80, 60)

	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c")
		r.Conversion("thumb").ForCollection("c").Fit(10, 10)
	})
	pngEncode = func(io.Writer, image.Image) error { return errBoom }
	if _, err := lib.performConversions(context.Background(), disk, media, bytes.NewReader(payload)); err == nil {
		t.Fatal("conv encode")
	}

	resetEncoders()
	lib.Register("cov_models", func(r *Registrar) {
		r.Collection("c")
		r.Conversion("thumb").ForCollection("c").Fit(10, 10).WithResponsiveImages()
	})
	pngEncode = func(io.Writer, image.Image) error { return errBoom }
	if _, err := lib.performResponsiveImages(context.Background(), disk, media, bytes.NewReader(payload)); err == nil {
		t.Fatal("resp conversion encode")
	}

	resetEncoders()
	img := image.NewRGBA(image.Rect(0, 0, 80, 60))
	pngEncode = func(io.Writer, image.Image) error { return errBoom }
	if _, err := generateResponsiveSet(context.Background(), disk, media, img, payload, "media_library_original", 90); err == nil {
		t.Fatal("set encode")
	}

	resetEncoders()
	jpegEncode = func(io.Writer, image.Image, *jpeg.Options) error { return errBoom }
	if _, err := generateResponsiveSet(context.Background(), disk, media, img, payload, "media_library_original", 90); err == nil {
		t.Fatal("tiny after widths")
	}

	resetEncoders()
	jpegEncode = func(io.Writer, image.Image, *jpeg.Options) error { return errBoom }
	if _, err := generateTinyPlaceholderSVG(img, 40, 30); err == nil {
		t.Fatal("tiny")
	}

	resetEncoders()
	jsonMarshal = func(any) ([]byte, error) { return nil, errBoom }
	if string(encodeGeneratedConversions(map[string]bool{"a": true})) != "{}" {
		t.Fatal("gen")
	}
	if string(encodeResponsiveImages(ResponsiveImagesData{"k": {}})) != "[]" {
		t.Fatal("ri")
	}
}

func TestFromPathBuildFileSourceFail(t *testing.T) {
	restoreHooks(t)
	p := filepath.Join(t.TempDir(), "a.jpg")
	writeJPEGFile(t, p, 8, 8)
	buildFileSource = func(f interface {
		io.ReadCloser
		io.Seeker
	}, base string) (*FileSource, error) {
		return nil, errBoom
	}
	if _, err := FromPath(p); err == nil {
		t.Fatal("expected fail")
	}
}

func TestS3KeyPrefixOnlyAndTypedNotFound(t *testing.T) {
	client := &headMock{data: map[string][]byte{}}
	disk, err := NewS3Disk(S3DiskConfig{Name: "s3", Bucket: "b", Prefix: "p", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	if got := disk.key("."); got != "p" {
		t.Fatalf("key .= %q", got)
	}
	if got := disk.key(""); got != "p" {
		t.Fatalf("key empty = %q", got)
	}
	client.typedNF = true
	ok, err := disk.Exists(context.Background(), "x")
	if err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
}

type headMock struct {
	data    map[string][]byte
	typedNF bool
}

func (m *headMock) PutObject(ctx context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	b, _ := io.ReadAll(params.Body)
	m.data[*params.Key] = b
	return &s3.PutObjectOutput{}, nil
}

func (m *headMock) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	b, ok := m.data[*params.Key]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "x"}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(b))}, nil
}

func (m *headMock) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(m.data, *params.Key)
	return &s3.DeleteObjectOutput{}, nil
}

func (m *headMock) HeadObject(_ context.Context, params *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if m.typedNF {
		return nil, &types.NotFound{}
	}
	if _, ok := m.data[*params.Key]; !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "x"}
	}
	return &s3.HeadObjectOutput{}, nil
}

func TestReplacePreviousSameIDSkipped(t *testing.T) {
	_, lib, _ := setupLib(t)
	media := &Media{ID: 42, Disk: "public", FileName: "a.jpg"}
	if err := lib.replacePrevious(context.Background(), []Media{{ID: 42, Disk: "public", FileName: "a.jpg"}}, media); err != nil {
		t.Fatal(err)
	}
	cleanupUpload(context.Background(), newFailDisk("x"), 0, []string{"a"})
	_ = os.ErrNotExist
}
