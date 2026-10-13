package medialibrary

import (
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"io"
)

// Package-level hooks let tests force rare I/O / encode failures.
var (
	jsonMarshal = json.Marshal
	jpegEncode  = func(w io.Writer, m image.Image, o *jpeg.Options) error {
		return jpeg.Encode(w, m, o)
	}
	pngEncode = func(w io.Writer, m image.Image) error {
		return png.Encode(w, m)
	}
	buildFileSource = fileSourceFrom
)
