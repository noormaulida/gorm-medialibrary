package medialibrary

import "errors"

var (
	// ErrInvalidArgument is returned for nil inputs or empty required fields.
	ErrInvalidArgument = errors.New("medialibrary: invalid argument")

	// ErrNotFound is returned when media cannot be located.
	ErrNotFound = errors.New("medialibrary: not found")

	// ErrDiskNotFound is returned when a configured disk name is missing.
	ErrDiskNotFound = errors.New("medialibrary: disk not found")

	// ErrMimeNotAllowed is returned when a file MIME type is rejected by the collection.
	ErrMimeNotAllowed = errors.New("medialibrary: mime type not allowed")

	// ErrNotImage is returned when an image conversion is requested for a non-image.
	ErrNotImage = errors.New("medialibrary: not an image")

	// ErrReplaceFailed is returned when singleFile cannot remove previous media
	// after a successful upload (the new media is rolled back when possible).
	ErrReplaceFailed = errors.New("medialibrary: failed to replace previous media")
)
