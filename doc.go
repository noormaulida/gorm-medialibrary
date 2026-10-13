// Package medialibrary associates files with GORM models using a schema
// compatible with Spatie Laravel Media Library.
//
// Basic usage:
//
//	_ = medialibrary.Migrate(db)
//	lib := medialibrary.New(db, medialibrary.WithDisk(medialibrary.NewLocalDisk("public", "./storage", "/storage")))
//	lib.Register("seo_metas", func(r *medialibrary.Registrar) {
//	    r.Collection("seo_image").SingleFile().WithResponsiveImages()
//	    r.Conversion("seo_image_large").ForCollection("seo_image").Crop(1200, 630).WithResponsiveImages()
//	})
//	media, err := lib.AddMedia(ctx, model, "seo_image", medialibrary.FromPath("photo.jpg"))
package medialibrary
