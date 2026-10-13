package medialibrary

import (
	"fmt"

	"gorm.io/gorm"
)

// Library is the entry point for media operations.
type Library struct {
	db          *gorm.DB
	disks       map[string]Disk
	defaultDisk string
	registrars  map[string]*Registrar
}

// Option configures a Library.
type Option func(*Library)

// WithDisk registers a storage disk.
func WithDisk(disk Disk) Option {
	return func(l *Library) {
		if disk == nil {
			return
		}
		l.disks[disk.Name()] = disk
		if l.defaultDisk == "" {
			l.defaultDisk = disk.Name()
		}
	}
}

// WithDefaultDisk selects the disk name used when media.Disk is empty.
func WithDefaultDisk(name string) Option {
	return func(l *Library) {
		if name != "" {
			l.defaultDisk = name
		}
	}
}

// New creates a Library bound to db.
func New(db *gorm.DB, opts ...Option) *Library {
	l := &Library{
		db:         db,
		disks:      map[string]Disk{},
		registrars: map[string]*Registrar{},
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// DB exposes the underlying GORM handle.
func (l *Library) DB() *gorm.DB { return l.db }

// Register attaches collection/conversion definitions for a morph model type.
func (l *Library) Register(modelType string, fn func(*Registrar)) {
	if modelType == "" || fn == nil {
		return
	}
	r := newRegistrar()
	fn(r)
	l.registrars[modelType] = r
}

func (l *Library) registrarFor(modelType string) *Registrar {
	return l.registrars[modelType]
}

func (l *Library) collectionConfig(modelType, collection string) *CollectionConfig {
	r := l.registrarFor(modelType)
	if r == nil {
		return nil
	}
	return r.collections[collection]
}

func (l *Library) conversionsFor(modelType, collection string) []*ConversionConfig {
	r := l.registrarFor(modelType)
	if r == nil {
		return nil
	}
	var out []*ConversionConfig
	for _, c := range r.conversions {
		if c.Collection == "" || c.Collection == collection {
			out = append(out, c)
		}
	}
	return out
}

func (l *Library) ensureReady() error {
	if l == nil || l.db == nil {
		return ErrInvalidArgument
	}
	if l.defaultDisk == "" || len(l.disks) == 0 {
		return fmt.Errorf("%w: no disk configured", ErrInvalidArgument)
	}
	return nil
}
