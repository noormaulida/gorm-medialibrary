package medialibrary_test

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"

	medialibrary "github.com/noormaulida/gorm-medialibrary"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type examplePage struct {
	ID uint64
}

func (p examplePage) MediaModelID() uint64   { return p.ID }
func (p examplePage) MediaModelType() string { return "pages" }

func ExampleLibrary_AddMedia() {
	db, _ := gorm.Open(sqlite.Open("file:example_media?mode=memory&cache=shared"), &gorm.Config{})
	_ = db.AutoMigrate(&examplePage{})
	_ = medialibrary.Migrate(db)

	root, _ := os.MkdirTemp("", "medialib-ex-")
	defer os.RemoveAll(root)
	disk := medialibrary.NewLocalDisk("public", root, "/storage")
	lib := medialibrary.New(db, medialibrary.WithDisk(disk))
	lib.Register("pages", func(r *medialibrary.Registrar) {
		r.Collection("cover").SingleFile()
		r.Conversion("thumb").ForCollection("cover").Fit(40, 40)
	})

	page := examplePage{ID: 1}
	_ = db.Create(&page)

	imgPath := filepath.Join(root, "in.jpg")
	f, _ := os.Create(imgPath)
	img := image.NewRGBA(image.Rect(0, 0, 80, 60))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	_ = jpeg.Encode(f, img, nil)
	_ = f.Close()

	src, _ := medialibrary.FromPath(imgPath)
	media, _ := lib.AddMedia(context.Background(), page, "cover", src)
	url, _ := lib.GetURL(*media, "thumb")
	fmt.Println(media.CollectionName)
	fmt.Println(url != "")
	// Output:
	// cover
	// true
}
