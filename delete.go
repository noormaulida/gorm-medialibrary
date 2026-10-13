package medialibrary

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"
)

// ClearMediaCollection deletes all media in a collection for model.
func (l *Library) ClearMediaCollection(ctx context.Context, model HasMedia, collectionName string) error {
	items, err := l.GetMedia(model, collectionName)
	if err != nil {
		return err
	}
	for i := range items {
		if err := l.Delete(ctx, items[i]); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes a media row and its files (original + conversions).
func (l *Library) Delete(ctx context.Context, media Media) error {
	if err := l.ensureReady(); err != nil {
		return err
	}
	disk, err := l.diskByName(media.Disk)
	if err != nil {
		return err
	}
	convDisk := disk
	if media.ConversionsDisk != nil && *media.ConversionsDisk != "" {
		if d, err := l.diskByName(*media.ConversionsDisk); err == nil {
			convDisk = d
		}
	}

	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&Media{}, media.ID).Error; err != nil {
			return err
		}
		_ = disk.Delete(ctx, OriginalPath(media))
		for name := range media.GeneratedConversionMap() {
			_ = convDisk.Delete(ctx, ConversionPath(media, name))
		}
		for _, set := range media.ResponsiveImagesMap() {
			for _, fileName := range set.URLs {
				_ = convDisk.Delete(ctx, ResponsiveImagePath(media, fileName))
			}
		}
		if local, ok := disk.(*LocalDisk); ok {
			dir := filepath.Join(local.Root(), fmt.Sprintf("%d", media.ID))
			_ = os.RemoveAll(dir)
		}
		return nil
	})
}
