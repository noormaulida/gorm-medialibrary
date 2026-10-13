package medialibrary

import (
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// FileSource is a pending upload before it is attached to a model.
type FileSource struct {
	reader   io.ReadCloser
	fileName string
	mimeType string
	name     string
}

// FromPath opens a local file as a FileSource.
func FromPath(path string) (*FileSource, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	src, err := buildFileSource(f, filepath.Base(path))
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return src, nil
}

func fileSourceFrom(f interface {
	io.ReadCloser
	io.Seeker
}, base string) (*FileSource, error) {
	mimeType := mime.TypeByExtension(filepath.Ext(base))
	if mimeType == "" {
		buf := make([]byte, 512)
		n, _ := f.Read(buf)
		mimeType = http.DetectContentType(buf[:n])
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	return &FileSource{
		reader:   f,
		fileName: sanitizeFileName(base),
		mimeType: mimeType,
		name:     strings.TrimSuffix(base, filepath.Ext(base)),
	}, nil
}

// FromReader wraps an in-memory or streamed upload.
func FromReader(r io.Reader, fileName, mimeType string) (*FileSource, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: reader is nil", ErrInvalidArgument)
	}
	if fileName == "" {
		return nil, fmt.Errorf("%w: file name is empty", ErrInvalidArgument)
	}
	rc, ok := r.(io.ReadCloser)
	if !ok {
		rc = io.NopCloser(r)
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(fileName))
	}
	return &FileSource{
		reader:   rc,
		fileName: sanitizeFileName(fileName),
		mimeType: mimeType,
		name:     strings.TrimSuffix(fileName, filepath.Ext(fileName)),
	}, nil
}

// WithName overrides the media "name" column (defaults to file basename without extension).
func (f *FileSource) WithName(name string) *FileSource {
	if name != "" {
		f.name = name
	}
	return f
}

func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, " ", "-")
	return name
}

func (f *FileSource) close() {
	if f != nil && f.reader != nil {
		_ = f.reader.Close()
	}
}
