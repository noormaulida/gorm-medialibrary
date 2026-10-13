package medialibrary

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
)

func (l *Library) performConversions(ctx context.Context, disk Disk, media Media, original io.Reader) (map[string]bool, error) {
	generated := map[string]bool{}
	conversions := l.conversionsFor(media.ModelType, media.CollectionName)
	if len(conversions) == 0 {
		return generated, nil
	}

	payload, err := io.ReadAll(original)
	if err != nil {
		return nil, err
	}
	if !isImageMIME(media.MimeType) {
		return generated, nil
	}

	img, err := imaging.Decode(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotImage, err)
	}

	for _, cfg := range conversions {
		converted := applyConversion(img, cfg)
		var buf bytes.Buffer
		if err := encodeImage(&buf, converted, media.FileName, cfg.Quality); err != nil {
			return nil, err
		}
		path := ConversionPath(media, cfg.Name)
		if _, err := disk.Put(ctx, path, &buf); err != nil {
			return nil, err
		}
		generated[cfg.Name] = true
	}
	return generated, nil
}

func applyConversion(img image.Image, cfg *ConversionConfig) image.Image {
	w, h := cfg.Width, cfg.Height
	if w <= 0 && h <= 0 {
		return img
	}
	if cfg.Crop && w > 0 && h > 0 {
		return imaging.Fill(img, w, h, imaging.Center, imaging.Lanczos)
	}
	return imaging.Fit(img, max(w, 1), max(h, 1), imaging.Lanczos)
}

func encodeImage(w io.Writer, img image.Image, fileName string, quality int) error {
	if quality <= 0 || quality > 100 {
		quality = 90
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	switch ext {
	case ".png":
		return png.Encode(w, img)
	default:
		return jpeg.Encode(w, img, &jpeg.Options{Quality: quality})
	}
}

func isImageMIME(mime *string) bool {
	if mime == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(*mime), "image/")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
