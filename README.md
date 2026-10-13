<p align="center">
  <img src="https://skillicons.dev/icons?i=go" alt="Go" width="80" />
</p>

<p align="center">
  <img src=".github/gorm-medialibrary.svg" alt="gorm-medialibrary" width="520" />
</p>

<p align="center">
  Spatie Laravel Media Library–compatible media attachments for GORM.
</p>

<p align="center">
  <a href="https://github.com/noormaulida/gorm-medialibrary/actions/workflows/go.yml">
    <img src="https://github.com/noormaulida/gorm-medialibrary/actions/workflows/go.yml/badge.svg" alt="Go Tests" />
  </a>
  <a href="https://codecov.io/gh/noormaulida/gorm-medialibrary">
    <img src="https://codecov.io/gh/noormaulida/gorm-medialibrary/graph/badge.svg" alt="codecov" />
  </a>
  <a href="https://pkg.go.dev/github.com/noormaulida/gorm-medialibrary">
    <img src="https://pkg.go.dev/badge/github.com/noormaulida/gorm-medialibrary.svg" alt="Go Reference" />
  </a>
  <a href="https://github.com/noormaulida/gorm-medialibrary/blob/master/LICENSE">
    <img src="https://img.shields.io/github/license/noormaulida/gorm-medialibrary" alt="License" />
  </a>
</p>

## Features

- Spatie-compatible `media` table (collections, conversions, responsive images, UUID, order)
- Attach from path or `io.Reader`
- Named collections (`SingleFile`, MIME allow-list, disk selection)
- Image conversions (fit / crop) via `disintegration/imaging`
- Responsive images (width variants + tiny SVG placeholder)
- Built-in `LocalDisk` and `S3Disk` (AWS SDK v2 / S3-compatible)
- Safe uploads: compensating disk cleanup; `singleFile` replaces only after success
- Path layout: `{id}/{file}`, `{id}/conversions/`, `{id}/responsive-images/`
- Go **1.24+**

## Install

```bash
go get github.com/noormaulida/gorm-medialibrary@latest
```

## Quick start

```go
disk := medialibrary.NewLocalDisk("public", "./storage/app/public", "/storage")

lib := medialibrary.New(db,
    medialibrary.WithDisk(disk),
    medialibrary.WithDefaultDisk("public"),
)
_ = medialibrary.Migrate(db) // skip if Laravel/Spatie already migrated
```

### Model

```go
type SeoMeta struct {
    ID    uint64
    Title string
}

func (s SeoMeta) MediaModelID() uint64   { return s.ID }
func (s SeoMeta) MediaModelType() string { return "seo_metas" } // morph alias shared with Laravel
```

### Register

```go
lib.Register("seo_metas", func(r *medialibrary.Registrar) {
    r.Collection("seo_image").
        SingleFile().
        AcceptsMimeTypes("image/jpeg", "image/png").
        Disk("public").
        WithResponsiveImages()

    r.Conversion("seo_image_large").
        ForCollection("seo_image").
        Crop(1200, 630).
        Quality(85).
        WithResponsiveImages()

    r.Conversion("seo_image_small").
        ForCollection("seo_image").
        Crop(240, 126)
})
```

### Add & read

```go
src, err := medialibrary.FromPath("./uploads/hero.jpg")
if err != nil {
    return err
}

media, err := lib.AddMedia(ctx, seoMeta, "seo_image", src)
if err != nil {
    return err
}

// Optional per-upload disk override:
// lib.AddMedia(ctx, seoMeta, "seo_image", src, medialibrary.OnDisk("s3"))

url, _ := lib.GetURL(*media)
large, _ := lib.GetURL(*media, "seo_image_large")
srcset, _ := lib.GetSrcset(*media)
```

Also: `GetMedia`, `GetFirstMedia`, `GetFirstMediaURL`, `GetResponsiveImageURLs`, `ClearMediaCollection`, `Delete`.

### HTTP upload

```go
file, header, _ := c.Request.FormFile("image")
defer file.Close()
src, err := medialibrary.FromReader(file, header.Filename, header.Header.Get("Content-Type"))
```

## S3 disk

```go
import (
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/s3"
)

cfg, err := config.LoadDefaultConfig(ctx)
if err != nil {
    return err
}
client := s3.NewFromConfig(cfg)

disk, err := medialibrary.NewS3Disk(medialibrary.S3DiskConfig{
    Name:    "s3",
    Bucket:  "my-bucket",
    Prefix:  "media",
    BaseURL: "https://cdn.example.com",
    Client:  client,
})
if err != nil {
    return err
}
lib := medialibrary.New(db, medialibrary.WithDisk(disk), medialibrary.WithDefaultDisk("s3"))
```

Works with MinIO, R2, and other S3-compatible APIs via the AWS SDK endpoint config.
For GCS or custom storage, implement `medialibrary.Disk`.

## Disk selection

Priority (highest first): `OnDisk` / `OnConversionsDisk` → collection `Disk` / `ConversionsDisk` → library default.

```go
r.Collection("images").Disk("s3").ConversionsDisk("s3")

lib.AddMedia(ctx, model, "images", src, medialibrary.OnDisk("s3"))
```

## Spatie notes

| Concern | Guidance |
|---------|----------|
| Table / columns | Spatie v9+ style (e.g. SuitCMS) |
| `model_type` | Prefer morph alias shared with Laravel |
| Paths | `{id}/{file}`, `conversions/`, `responsive-images/` |
| Responsive JSON | Keys `media_library_original` / conversion name |

## License

MIT
