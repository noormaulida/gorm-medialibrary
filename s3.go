package medialibrary

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3API is the subset of the AWS S3 client used by S3Disk.
// *s3.Client from aws-sdk-go-v2 satisfies this interface.
type S3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
}

// S3DiskConfig configures a built-in S3 (or S3-compatible) disk.
type S3DiskConfig struct {
	// Name is the disk name stored on media rows (e.g. "s3").
	Name string
	// Bucket is the S3 bucket name.
	Bucket string
	// Prefix is an optional key prefix (without leading/trailing slashes).
	Prefix string
	// BaseURL is the public URL prefix for URL(), e.g.
	// "https://cdn.example.com" or "https://my-bucket.s3.amazonaws.com".
	BaseURL string
	// Client is an aws-sdk-go-v2 S3 client (or compatible mock).
	Client S3API
}

// S3Disk stores media objects in Amazon S3 (or compatible APIs).
type S3Disk struct {
	name    string
	bucket  string
	prefix  string
	baseURL string
	client  S3API
}

// NewS3Disk creates an S3-backed Disk.
//
//	cfg, _ := config.LoadDefaultConfig(ctx)
//	client := s3.NewFromConfig(cfg)
//	disk := medialibrary.NewS3Disk(medialibrary.S3DiskConfig{
//	    Name: "s3", Bucket: "my-bucket", BaseURL: "https://cdn.example.com", Client: client,
//	})
func NewS3Disk(cfg S3DiskConfig) (*S3Disk, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("%w: s3 disk name is empty", ErrInvalidArgument)
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("%w: s3 bucket is empty", ErrInvalidArgument)
	}
	if cfg.Client == nil {
		return nil, fmt.Errorf("%w: s3 client is nil", ErrInvalidArgument)
	}
	return &S3Disk{
		name:    cfg.Name,
		bucket:  cfg.Bucket,
		prefix:  strings.Trim(cfg.Prefix, "/"),
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		client:  cfg.Client,
	}, nil
}

// Name implements Disk.
func (d *S3Disk) Name() string { return d.name }

func (d *S3Disk) key(rel string) string {
	rel = strings.TrimLeft(path.Clean("/"+filepathToSlash(rel)), "/")
	if d.prefix == "" {
		return rel
	}
	if rel == "" || rel == "." {
		return d.prefix
	}
	return d.prefix + "/" + rel
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// Put implements Disk.
func (d *S3Disk) Put(ctx context.Context, rel string, r io.Reader) (int64, error) {
	counting := &countReader{r: r}
	_, err := d.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(d.key(rel)),
		Body:   counting,
	})
	if err != nil {
		return counting.n, err
	}
	return counting.n, nil
}

type countReader struct {
	r io.Reader
	n int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// Delete implements Disk.
func (d *S3Disk) Delete(ctx context.Context, rel string) error {
	_, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(d.key(rel)),
	})
	return err
}

// Exists implements Disk.
func (d *S3Disk) Exists(ctx context.Context, rel string) (bool, error) {
	_, err := d.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(d.key(rel)),
	})
	if err == nil {
		return true, nil
	}
	var nsk *types.NotFound
	if errors.As(err, &nsk) {
		return false, nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NotFound", "NoSuchKey", "404":
			return false, nil
		}
	}
	return false, err
}

// Open implements Disk.
func (d *S3Disk) Open(ctx context.Context, rel string) (io.ReadCloser, error) {
	out, err := d.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(d.bucket),
		Key:    aws.String(d.key(rel)),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

// URL implements Disk.
func (d *S3Disk) URL(rel string) string {
	rel = strings.TrimLeft(filepathToSlash(rel), "/")
	key := rel
	if d.prefix != "" {
		key = d.prefix + "/" + rel
	}
	if d.baseURL == "" {
		return "/" + key
	}
	return d.baseURL + "/" + key
}
