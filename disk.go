package medialibrary

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Disk stores and serves media bytes. Implement this for S3, GCS, etc.
type Disk interface {
	Name() string
	Put(ctx context.Context, path string, r io.Reader) (int64, error)
	Delete(ctx context.Context, path string) error
	Exists(ctx context.Context, path string) (bool, error)
	Open(ctx context.Context, path string) (io.ReadCloser, error)
	// URL returns a publicly reachable URL for path (may be relative).
	URL(path string) string
}

// LocalDisk stores files under Root and exposes them under URLPrefix.
type LocalDisk struct {
	name      string
	root      string
	urlPrefix string
}

// NewLocalDisk creates a filesystem disk.
//
//	NewLocalDisk("public", "./storage/app/public", "/storage")
func NewLocalDisk(name, root, urlPrefix string) *LocalDisk {
	return &LocalDisk{
		name:      name,
		root:      root,
		urlPrefix: strings.TrimRight(urlPrefix, "/"),
	}
}

// Name implements Disk.
func (d *LocalDisk) Name() string { return d.name }

func (d *LocalDisk) abs(path string) string {
	return filepath.Join(d.root, filepath.FromSlash(path))
}

// Put implements Disk.
func (d *LocalDisk) Put(_ context.Context, path string, r io.Reader) (int64, error) {
	abs := d.abs(path)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(abs)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return io.Copy(f, r)
}

// Delete implements Disk.
func (d *LocalDisk) Delete(_ context.Context, path string) error {
	err := os.Remove(d.abs(path))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Exists implements Disk.
func (d *LocalDisk) Exists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(d.abs(path))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Open implements Disk.
func (d *LocalDisk) Open(_ context.Context, path string) (io.ReadCloser, error) {
	return os.Open(d.abs(path))
}

// URL implements Disk.
func (d *LocalDisk) URL(path string) string {
	path = strings.TrimLeft(filepath.ToSlash(path), "/")
	if d.urlPrefix == "" {
		return "/" + path
	}
	return d.urlPrefix + "/" + path
}

// Root returns the filesystem root (useful in tests).
func (d *LocalDisk) Root() string { return d.root }

func (l *Library) diskByName(name string) (Disk, error) {
	if name == "" {
		name = l.defaultDisk
	}
	d, ok := l.disks[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrDiskNotFound, name)
	}
	return d, nil
}
