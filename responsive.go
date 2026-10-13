package medialibrary

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	"strings"

	"github.com/disintegration/imaging"
)

const (
	responsiveOriginalKey = "media_library_original"
	responsiveMinWidth    = 20
	responsiveMinBytes    = 10 * 1024
)

// ResponsiveImagesData is the Spatie-compatible responsive_images JSON shape.
type ResponsiveImagesData map[string]ResponsiveImageSet

// ResponsiveImageSet holds file names and optional tiny placeholder for one conversion key.
type ResponsiveImageSet struct {
	URLs      []string `json:"urls"`
	Base64SVG string   `json:"base64svg,omitempty"`
}

func (l *Library) performResponsiveImages(ctx context.Context, disk Disk, media Media, original io.Reader) (ResponsiveImagesData, error) {
	out := ResponsiveImagesData{}
	if !isImageMIME(media.MimeType) {
		return out, nil
	}

	payload, err := io.ReadAll(original)
	if err != nil {
		return nil, err
	}
	img, err := imaging.Decode(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotImage, err)
	}

	coll := l.collectionConfig(media.ModelType, media.CollectionName)
	if coll != nil && coll.ResponsiveImages {
		set, err := generateResponsiveSet(ctx, disk, media, img, payload, responsiveOriginalKey, 90)
		if err != nil {
			return nil, err
		}
		out[responsiveOriginalKey] = set
	}

	for _, cfg := range l.conversionsFor(media.ModelType, media.CollectionName) {
		if !cfg.ResponsiveImages {
			continue
		}
		converted := applyConversion(img, cfg)
		var buf bytes.Buffer
		if err := encodeImage(&buf, converted, media.FileName, cfg.Quality); err != nil {
			return nil, err
		}
		set, err := generateResponsiveSet(ctx, disk, media, converted, buf.Bytes(), cfg.Name, cfg.Quality)
		if err != nil {
			return nil, err
		}
		out[cfg.Name] = set
	}
	return out, nil
}

func generateResponsiveSet(
	ctx context.Context,
	disk Disk,
	media Media,
	img image.Image,
	fileBytes []byte,
	conversionName string,
	quality int,
) (ResponsiveImageSet, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	widths := calculateResponsiveWidths(len(fileBytes), width, height)

	urls := make([]string, 0, len(widths))
	for _, w := range widths {
		resized := imaging.Resize(img, w, 0, imaging.Lanczos)
		h := resized.Bounds().Dy()
		var buf bytes.Buffer
		if err := encodeImage(&buf, resized, media.FileName, quality); err != nil {
			return ResponsiveImageSet{}, err
		}
		name := ResponsiveImageFileName(media, conversionName, w, h)
		if _, err := disk.Put(ctx, ResponsiveImagePath(media, name), &buf); err != nil {
			return ResponsiveImageSet{}, err
		}
		urls = append(urls, name)
	}

	svg, err := generateTinyPlaceholderSVG(img, width, height)
	if err != nil {
		return ResponsiveImageSet{}, err
	}
	return ResponsiveImageSet{URLs: urls, Base64SVG: svg}, nil
}

// calculateResponsiveWidths mirrors Spatie's FileSizeOptimizedWidthCalculator.
func calculateResponsiveWidths(fileSize, width, height int) []int {
	if width <= 0 || height <= 0 || fileSize <= 0 {
		return nil
	}
	widths := []int{width}
	ratio := float64(height) / float64(width)
	area := float64(height * width)
	predicted := float64(fileSize)
	pixelPrice := predicted / area

	for {
		predicted *= 0.7
		newWidth := int(math.Floor(math.Sqrt((predicted / pixelPrice) / ratio)))
		if newWidth < responsiveMinWidth || predicted < responsiveMinBytes {
			break
		}
		widths = append(widths, newWidth)
	}
	return widths
}

func generateTinyPlaceholderSVG(img image.Image, origW, origH int) (string, error) {
	tiny := imaging.Resize(img, 32, 0, imaging.Lanczos)
	tiny = imaging.Blur(tiny, 1)
	var buf bytes.Buffer
	if err := jpegEncode(&buf, tiny, &jpeg.Options{Quality: 50}); err != nil {
		return "", err
	}
	tinyData := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d"><filter id="b" color-interpolation-filters="sRGB"><feGaussianBlur stdDeviation=".5"/></filter><image preserveAspectRatio="none" filter="url(#b)" x="0" y="0" height="100%%" width="100%%" href="%s"/></svg>`,
		origW, origH, tinyData,
	)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg)), nil
}

// ResponsiveConversionKey returns the JSON key used for a conversion (empty → original).
func ResponsiveConversionKey(conversionName string) string {
	if conversionName == "" {
		return responsiveOriginalKey
	}
	return conversionName
}

// ParseResponsiveWidth extracts the width descriptor from a Spatie responsive file name.
func ParseResponsiveWidth(fileName string) (int, bool) {
	base := fileName
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[:i]
	}
	parts := strings.Split(base, "___")
	if len(parts) < 2 {
		return 0, false
	}
	meta := strings.Split(parts[len(parts)-1], "_")
	if len(meta) < 3 {
		return 0, false
	}
	var w int
	if _, err := fmt.Sscanf(meta[len(meta)-2], "%d", &w); err != nil || w <= 0 {
		return 0, false
	}
	return w, true
}
