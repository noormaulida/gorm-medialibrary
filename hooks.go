package medialibrary

import (
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
)

// Testable seams for rare failure paths (overridden in *_test.go only).
var (
	jsonMarshal = json.Marshal
	jpegEncode  = func(w io.Writer, m image.Image, o *jpeg.Options) error {
		return jpeg.Encode(w, m, o)
	}
	pngEncode = func(w io.Writer, m image.Image) error {
		return png.Encode(w, m)
	}
	buildFileSource = fileSourceFrom
	removeAll       = os.RemoveAll
)

