package medialibrary_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	medialibrary "github.com/noormaulida/gorm-medialibrary"
)

type mockS3 struct {
	mu   sync.Mutex
	data map[string][]byte
}

func newMockS3() *mockS3 {
	return &mockS3{data: map[string][]byte{}}
}

func (m *mockS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	m.data[*params.Key] = b
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.data[*params.Key]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "not found"}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(b))}, nil
}

func (m *mockS3) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, *params.Key)
	return &s3.DeleteObjectOutput{}, nil
}

func (m *mockS3) HeadObject(_ context.Context, params *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.data[*params.Key]; !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "not found"}
	}
	return &s3.HeadObjectOutput{}, nil
}

func TestS3DiskRoundTrip(t *testing.T) {
	client := newMockS3()
	disk, err := medialibrary.NewS3Disk(medialibrary.S3DiskConfig{
		Name:    "s3",
		Bucket:  "media",
		Prefix:  "uploads",
		BaseURL: "https://cdn.example.com",
		Client:  client,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	n, err := disk.Put(ctx, "1/hero.jpg", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("wrote %d bytes", n)
	}
	ok, err := disk.Exists(ctx, "1/hero.jpg")
	if err != nil || !ok {
		t.Fatalf("exists=%v err=%v", ok, err)
	}
	rc, err := disk.Open(ctx, "1/hero.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	url := disk.URL("1/hero.jpg")
	if url != "https://cdn.example.com/uploads/1/hero.jpg" {
		t.Fatalf("url=%q", url)
	}
	if err := disk.Delete(ctx, "1/hero.jpg"); err != nil {
		t.Fatal(err)
	}
	ok, err = disk.Exists(ctx, "1/hero.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected missing after delete")
	}
}

func TestNewS3DiskValidation(t *testing.T) {
	_, err := medialibrary.NewS3Disk(medialibrary.S3DiskConfig{})
	if !errors.Is(err, medialibrary.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}
