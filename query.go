package medialibrary

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// GetMedia returns media for model, optionally filtered by collection.
func (l *Library) GetMedia(model HasMedia, collectionName ...string) ([]Media, error) {
	if err := l.ensureReady(); err != nil {
		return nil, err
	}
	modelID, modelType, err := resolveModel(model)
	if err != nil {
		return nil, err
	}
	q := l.db.Where("model_type = ? AND model_id = ?", modelType, modelID).
		Order("order_column ASC, id ASC")
	if len(collectionName) > 0 && collectionName[0] != "" {
		q = q.Where("collection_name = ?", collectionName[0])
	}
	var items []Media
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// GetFirstMedia returns the first media item in a collection.
func (l *Library) GetFirstMedia(model HasMedia, collectionName string) (*Media, error) {
	items, err := l.GetMedia(model, collectionName)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%w: collection %q", ErrNotFound, collectionName)
	}
	return &items[0], nil
}

// Find finds media by primary key.
func (l *Library) Find(id uint64) (*Media, error) {
	if err := l.ensureReady(); err != nil {
		return nil, err
	}
	var m Media
	err := l.db.First(&m, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: media %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetURL returns a public URL for the original file or a named conversion.
func (l *Library) GetURL(media Media, conversionName ...string) (string, error) {
	diskName := media.Disk
	rel := OriginalPath(media)
	if len(conversionName) > 0 && conversionName[0] != "" {
		name := conversionName[0]
		if generated := media.GeneratedConversionMap(); !generated[name] {
			return "", fmt.Errorf("%w: conversion %q", ErrNotFound, name)
		}
		rel = ConversionPath(media, name)
		if media.ConversionsDisk != nil && *media.ConversionsDisk != "" {
			diskName = *media.ConversionsDisk
		}
	}
	disk, err := l.diskByName(diskName)
	if err != nil {
		return "", err
	}
	return disk.URL(rel), nil
}

// GetFirstMediaURL is a convenience wrapper around GetFirstMedia + GetURL.
func (l *Library) GetFirstMediaURL(model HasMedia, collectionName string, conversionName ...string) (string, error) {
	media, err := l.GetFirstMedia(model, collectionName)
	if err != nil {
		return "", err
	}
	return l.GetURL(*media, conversionName...)
}

// GetResponsiveImageURLs returns public URLs for responsive variants of the original
// or a named conversion (Spatie getResponsiveImageUrls).
//
// All responsive files (including media_library_original) are stored on the
// conversions disk when set, matching AddMedia write paths.
func (l *Library) GetResponsiveImageURLs(media Media, conversionName ...string) ([]string, error) {
	key := ResponsiveConversionKey("")
	if len(conversionName) > 0 {
		key = ResponsiveConversionKey(conversionName[0])
	}
	set, ok := media.ResponsiveImagesMap()[key]
	if !ok || len(set.URLs) == 0 {
		return nil, nil
	}
	disk, err := l.diskByName(responsiveDiskName(media))
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(set.URLs))
	for _, name := range set.URLs {
		urls = append(urls, disk.URL(ResponsiveImagePath(media, name)))
	}
	return urls, nil
}

// GetSrcset builds an HTML srcset string for responsive images (Spatie getSrcset).
func (l *Library) GetSrcset(media Media, conversionName ...string) (string, error) {
	key := ResponsiveConversionKey("")
	if len(conversionName) > 0 {
		key = ResponsiveConversionKey(conversionName[0])
	}
	set, ok := media.ResponsiveImagesMap()[key]
	if !ok || len(set.URLs) == 0 {
		return "", nil
	}
	disk, err := l.diskByName(responsiveDiskName(media))
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(set.URLs)+1)
	for _, name := range set.URLs {
		url := disk.URL(ResponsiveImagePath(media, name))
		if w, ok := ParseResponsiveWidth(name); ok {
			parts = append(parts, fmt.Sprintf("%s %dw", url, w))
		} else {
			parts = append(parts, url)
		}
	}
	if set.Base64SVG != "" {
		parts = append(parts, set.Base64SVG+" 32w")
	}
	return strings.Join(parts, ", "), nil
}

// responsiveDiskName returns the disk that holds responsive image files.
func responsiveDiskName(media Media) string {
	if media.ConversionsDisk != nil && *media.ConversionsDisk != "" {
		return *media.ConversionsDisk
	}
	return media.Disk
}
