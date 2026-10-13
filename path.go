package medialibrary

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// OriginalPath is the relative disk path for the original file (Spatie layout).
func OriginalPath(m Media) string {
	return path.Join(fmt.Sprintf("%d", m.ID), m.FileName)
}

// ConversionPath is the relative disk path for a named conversion.
func ConversionPath(m Media, conversion string) string {
	ext := filepath.Ext(m.FileName)
	base := strings.TrimSuffix(m.FileName, ext)
	if ext == "" {
		ext = ".jpg"
	}
	file := fmt.Sprintf("%s-%s%s", base, conversion, ext)
	return path.Join(fmt.Sprintf("%d", m.ID), "conversions", file)
}

// ResponsiveImagesDir is the relative directory for responsive image files (Spatie layout).
func ResponsiveImagesDir(m Media) string {
	return path.Join(fmt.Sprintf("%d", m.ID), "responsive-images")
}

// ResponsiveImagePath is the relative disk path for a responsive image file name.
func ResponsiveImagePath(m Media, fileName string) string {
	return path.Join(ResponsiveImagesDir(m), fileName)
}

// ResponsiveImageFileName builds a Spatie-compatible responsive file name.
// Example: hero___media_library_original_640_360.jpg
func ResponsiveImageFileName(m Media, conversionName string, width, height int) string {
	ext := filepath.Ext(m.FileName)
	base := strings.TrimSuffix(m.FileName, ext)
	if ext == "" {
		ext = ".jpg"
	}
	ext = strings.TrimPrefix(ext, ".")
	return fmt.Sprintf("%s___%s_%d_%d.%s", base, conversionName, width, height, ext)
}
