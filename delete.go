package medialibrary

import (
	"context"
	"errors"
	"fmt"
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

// Delete removes media files from disk, then deletes the database row.
// Disk errors are returned (not swallowed). If a disk delete fails, the DB
// row is left intact so the operation can be retried.
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
		d, err := l.diskByName(*media.ConversionsDisk)
		if err != nil {
			return err
		}
		convDisk = d
	}

	if err := deleteMediaFiles(ctx, disk, convDisk, media); err != nil {
		return err
	}

	return l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&Media{}, media.ID).Error; err != nil {
			return err
		}
		return nil
	})
}

func deleteMediaFiles(ctx context.Context, disk, convDisk Disk, media Media) error {
	var errs []error
	if err := disk.Delete(ctx, OriginalPath(media)); err != nil {
		errs = append(errs, fmt.Errorf("original: %w", err))
	}
	for name := range media.GeneratedConversionMap() {
		if err := convDisk.Delete(ctx, ConversionPath(media, name)); err != nil {
			errs = append(errs, fmt.Errorf("conversion %q: %w", name, err))
		}
	}
	for key, set := range media.ResponsiveImagesMap() {
		for _, fileName := range set.URLs {
			if err := convDisk.Delete(ctx, ResponsiveImagePath(media, fileName)); err != nil {
				errs = append(errs, fmt.Errorf("responsive %s/%s: %w", key, fileName, err))
			}
		}
	}
	if local, ok := disk.(*LocalDisk); ok {
		dir := filepath.Join(local.Root(), fmt.Sprintf("%d", media.ID))
		if err := removeAll(dir); err != nil {
			errs = append(errs, fmt.Errorf("media dir: %w", err))
		}
	}
	return errors.Join(errs...)
}
