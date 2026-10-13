package medialibrary

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AddMedia attaches a file to model under collectionName.
//
// On failure after any disk writes, uploaded files are deleted (compensating
// cleanup). For singleFile collections, existing items are removed only after
// the new media is stored successfully.
func (l *Library) AddMedia(ctx context.Context, model HasMedia, collectionName string, source *FileSource) (*Media, error) {
	if err := l.ensureReady(); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fmt.Errorf("%w: file source is nil", ErrInvalidArgument)
	}
	defer source.close()

	if collectionName == "" {
		return nil, fmt.Errorf("%w: collection name is empty", ErrInvalidArgument)
	}

	modelID, modelType, err := resolveModel(model)
	if err != nil {
		return nil, err
	}

	var previous []Media
	if cfg := l.collectionConfig(modelType, collectionName); cfg != nil {
		if len(cfg.AcceptsMimeTypes) > 0 && !mimeAllowed(source.mimeType, cfg.AcceptsMimeTypes) {
			return nil, fmt.Errorf("%w: %s", ErrMimeNotAllowed, source.mimeType)
		}
		if cfg.SingleFile {
			previous, err = l.GetMedia(model, collectionName)
			if err != nil {
				return nil, err
			}
		}
	}

	disk, err := l.diskByName(l.defaultDisk)
	if err != nil {
		return nil, err
	}

	payload, err := io.ReadAll(source.reader)
	if err != nil {
		return nil, err
	}

	uid := uuid.NewString()
	now := time.Now()
	mime := source.mimeType
	order := nextOrderColumn(l.db, modelType, modelID, collectionName)
	media := &Media{
		ModelType:            modelType,
		ModelID:              modelID,
		UUID:                 &uid,
		CollectionName:       collectionName,
		Name:                 source.name,
		FileName:             source.fileName,
		MimeType:             &mime,
		Disk:                 disk.Name(),
		ConversionsDisk:      strPtr(disk.Name()),
		Size:                 uint64(len(payload)),
		Manipulations:        emptyJSONObject(),
		CustomProperties:     emptyJSONObject(),
		GeneratedConversions: emptyJSONObject(),
		ResponsiveImages:     emptyJSONArray(),
		OrderColumn:          &order,
		CreatedAt:            &now,
		UpdatedAt:            &now,
	}

	tracker := &trackingDisk{Disk: disk}
	err = l.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(media).Error; err != nil {
			return err
		}
		path := OriginalPath(*media)
		if _, err := tracker.Put(ctx, path, bytes.NewReader(payload)); err != nil {
			return err
		}
		generated, err := l.performConversions(ctx, tracker, *media, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		media.GeneratedConversions = encodeGeneratedConversions(generated)

		responsive, err := l.performResponsiveImages(ctx, tracker, *media, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		media.ResponsiveImages = encodeResponsiveImages(responsive)

		return tx.Model(media).Updates(map[string]any{
			"generated_conversions": media.GeneratedConversions,
			"responsive_images":     media.ResponsiveImages,
		}).Error
	})
	if err != nil {
		cleanupUpload(ctx, disk, media.ID, tracker.paths)
		return nil, err
	}

	for i := range previous {
		if previous[i].ID == media.ID {
			continue
		}
		_ = l.Delete(ctx, previous[i])
	}
	return media, nil
}

// trackingDisk records successful Put paths for compensating cleanup.
type trackingDisk struct {
	Disk
	paths []string
}

func (t *trackingDisk) Put(ctx context.Context, path string, r io.Reader) (int64, error) {
	n, err := t.Disk.Put(ctx, path, r)
	if err == nil {
		t.paths = append(t.paths, path)
	}
	return n, err
}

func cleanupUpload(ctx context.Context, disk Disk, mediaID uint64, paths []string) {
	for _, p := range paths {
		_ = disk.Delete(ctx, p)
	}
	if mediaID == 0 {
		return
	}
	if local, ok := disk.(*LocalDisk); ok {
		_ = os.RemoveAll(filepath.Join(local.Root(), fmt.Sprintf("%d", mediaID)))
	}
}

func mimeAllowed(got string, allowed []string) bool {
	got = strings.ToLower(got)
	for _, a := range allowed {
		a = strings.ToLower(a)
		if a == got {
			return true
		}
		if strings.HasSuffix(a, "/*") {
			prefix := strings.TrimSuffix(a, "*")
			if strings.HasPrefix(got, prefix) {
				return true
			}
		}
	}
	return false
}

func nextOrderColumn(db *gorm.DB, modelType string, modelID uint64, collection string) int {
	var max *int
	_ = db.Model(&Media{}).
		Select("MAX(order_column)").
		Where("model_type = ? AND model_id = ? AND collection_name = ?", modelType, modelID, collection).
		Scan(&max).Error
	if max == nil {
		return 1
	}
	return *max + 1
}

func strPtr(s string) *string { return &s }
