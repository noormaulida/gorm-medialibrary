package medialibrary

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// newUUID is overridden in tests to force deterministic / colliding UUIDs.
var newUUID = uuid.NewString

type addOptions struct {
	disk            string
	conversionsDisk string
}

// AddOption configures AddMedia.
type AddOption func(*addOptions)

// OnDisk stores this media on the named disk (overrides collection default).
func OnDisk(name string) AddOption {
	return func(o *addOptions) {
		if name != "" {
			o.disk = name
		}
	}
}

// OnConversionsDisk stores conversions/responsive files on the named disk.
func OnConversionsDisk(name string) AddOption {
	return func(o *addOptions) {
		if name != "" {
			o.conversionsDisk = name
		}
	}
}

// AddMedia attaches a file to model under collectionName.
//
// Disk I/O (original, conversions, responsive) runs outside DB transactions so
// row locks are not held across local/S3 uploads. On failure after any disk
// writes, uploaded files and the media row are removed (compensating cleanup).
//
// For singleFile collections, existing rows are locked during insert so concurrent
// uploads serialize when the collection is non-empty. Previous items are removed
// only after the new media is stored successfully; if clearing them fails, the new
// media is removed as well. A post-success pass also drops older stragglers
// (lower ID) left by empty-collection races.
//
// Disk selection (highest priority wins): AddOption → collection Disk → library default.
func (l *Library) AddMedia(ctx context.Context, model HasMedia, collectionName string, source *FileSource, opts ...AddOption) (*Media, error) {
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

	ao := addOptions{}
	for _, opt := range opts {
		opt(&ao)
	}

	var coll *CollectionConfig
	if cfg := l.collectionConfig(modelType, collectionName); cfg != nil {
		coll = cfg
		if len(cfg.AcceptsMimeTypes) > 0 && !mimeAllowed(source.mimeType, cfg.AcceptsMimeTypes) {
			return nil, fmt.Errorf("%w: %s", ErrMimeNotAllowed, source.mimeType)
		}
	}
	singleFile := coll != nil && coll.SingleFile

	diskName := l.defaultDisk
	if coll != nil && coll.Disk != "" {
		diskName = coll.Disk
	}
	if ao.disk != "" {
		diskName = ao.disk
	}
	disk, err := l.diskByName(diskName)
	if err != nil {
		return nil, err
	}

	convDiskName := disk.Name()
	if coll != nil && coll.ConversionsDisk != "" {
		convDiskName = coll.ConversionsDisk
	}
	if ao.conversionsDisk != "" {
		convDiskName = ao.conversionsDisk
	}
	convDisk, err := l.diskByName(convDiskName)
	if err != nil {
		return nil, err
	}

	payload, err := io.ReadAll(source.reader)
	if err != nil {
		return nil, err
	}

	uid := newUUID()
	now := time.Now()
	mime := source.mimeType
	media := &Media{
		ModelType:            modelType,
		ModelID:              modelID,
		UUID:                 &uid,
		CollectionName:       collectionName,
		Name:                 source.name,
		FileName:             source.fileName,
		MimeType:             &mime,
		Disk:                 disk.Name(),
		ConversionsDisk:      strPtr(convDisk.Name()),
		Size:                 uint64(len(payload)),
		Manipulations:        emptyJSONObject(),
		CustomProperties:     emptyJSONObject(),
		GeneratedConversions: emptyJSONObject(),
		ResponsiveImages:     emptyJSONArray(),
		CreatedAt:            &now,
		UpdatedAt:            &now,
	}

	origTracker := &trackingDisk{Disk: disk}
	convTracker := &trackingDisk{Disk: convDisk}

	// Short tx: allocate order + insert row. No disk I/O.
	// nextOrderColumn uses SELECT … FOR UPDATE so concurrent singleFile uploads
	// serialize when the collection is non-empty; enforceSingleFile prunes
	// empty-collection races after a successful store.
	err = l.db.Transaction(func(tx *gorm.DB) error {
		order, err := nextOrderColumn(tx, modelType, modelID, collectionName)
		if err != nil {
			return err
		}
		media.OrderColumn = &order
		return tx.Create(media).Error
	})
	if err != nil {
		return nil, err
	}

	cleanupFailed := func() {
		cleanupUpload(ctx, disk, media.ID, origTracker.paths)
		if convDisk.Name() != disk.Name() {
			cleanupUpload(ctx, convDisk, 0, convTracker.paths)
		} else {
			cleanupUpload(ctx, disk, 0, convTracker.paths)
		}
		_ = l.db.Delete(&Media{}, media.ID).Error
	}

	path := OriginalPath(*media)
	if _, err := origTracker.Put(ctx, path, bytes.NewReader(payload)); err != nil {
		cleanupFailed()
		return nil, err
	}
	generated, err := l.performConversions(ctx, convTracker, *media, bytes.NewReader(payload))
	if err != nil {
		cleanupFailed()
		return nil, err
	}
	media.GeneratedConversions = encodeGeneratedConversions(generated)

	responsive, err := l.performResponsiveImages(ctx, convTracker, *media, bytes.NewReader(payload))
	if err != nil {
		cleanupFailed()
		return nil, err
	}
	media.ResponsiveImages = encodeResponsiveImages(responsive)

	if err := l.db.Model(media).Updates(map[string]any{
		"generated_conversions": media.GeneratedConversions,
		"responsive_images":     media.ResponsiveImages,
	}).Error; err != nil {
		cleanupFailed()
		return nil, err
	}

	if singleFile {
		if err := l.enforceSingleFile(ctx, model, collectionName, media); err != nil {
			return nil, err
		}
	}
	return media, nil
}

// enforceSingleFile keeps only the newest media in a singleFile collection.
// Older items (lower ID) are deleted. If a newer concurrent upload exists, the
// just-added media is removed and ErrReplaceFailed is returned.
func (l *Library) enforceSingleFile(ctx context.Context, model HasMedia, collectionName string, keep *Media) error {
	items, err := l.GetMedia(model, collectionName)
	if err != nil {
		return err
	}
	for i := range items {
		if items[i].ID == keep.ID {
			continue
		}
		if items[i].ID > keep.ID {
			if delErr := l.Delete(ctx, *keep); delErr != nil {
				return fmt.Errorf("%w: concurrent newer exists; compensate: %v", ErrReplaceFailed, delErr)
			}
			return fmt.Errorf("%w: concurrent single-file upload", ErrReplaceFailed)
		}
		if err := l.Delete(ctx, items[i]); err != nil {
			if delErr := l.Delete(ctx, *keep); delErr != nil {
				return fmt.Errorf("%w: clear previous: %v; compensate new: %v", ErrReplaceFailed, err, delErr)
			}
			return fmt.Errorf("%w: %v", ErrReplaceFailed, err)
		}
	}
	return nil
}

// replacePrevious deletes previous media after a successful singleFile add.
// Kept for tests and callers that already hold the previous snapshot.
func (l *Library) replacePrevious(ctx context.Context, previous []Media, media *Media) error {
	for i := range previous {
		if previous[i].ID == media.ID {
			continue
		}
		if err := l.Delete(ctx, previous[i]); err != nil {
			if delErr := l.Delete(ctx, *media); delErr != nil {
				return fmt.Errorf("%w: clear previous: %v; compensate new: %v", ErrReplaceFailed, err, delErr)
			}
			return fmt.Errorf("%w: %v", ErrReplaceFailed, err)
		}
	}
	return nil
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
		_ = removeAll(filepath.Join(local.Root(), fmt.Sprintf("%d", mediaID)))
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

func nextOrderColumn(tx *gorm.DB, modelType string, modelID uint64, collection string) (int, error) {
	var max *int
	q := tx.Model(&Media{}).
		Select("MAX(order_column)").
		Where("model_type = ? AND model_id = ? AND collection_name = ?", modelType, modelID, collection)
	// Row lock when the dialect supports it (MySQL/Postgres); SQLite serializes writers.
	q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	if err := q.Scan(&max).Error; err != nil {
		return 0, err
	}
	if max == nil {
		return 1, nil
	}
	return *max + 1, nil
}

func strPtr(s string) *string { return &s }
