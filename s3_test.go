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
	mu       sync.Mutex
	data     map[string][]byte
	ctype    map[string]string
	failPut  bool
	failGet  bool
	notFound bool
	apiCode  string
}

func newMockS3() *mockS3 {
	return &mockS3{data: map[string][]byte{}, ctype: map[string]string{}}
}

func (m *mockS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.failPut {
		return nil, errors.New("boom")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	b, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	m.data[*params.Key] = b
	if params.ContentType != nil {
		m.ctype[*params.Key] = *params.ContentType
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.failGet {
		return nil, errors.New("boom")
	}
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
	if m.notFound {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "missing"}
	}
	if m.apiCode != "" {
		return nil, &smithy.GenericAPIError{Code: m.apiCode, Message: "x"}
	}
	if _, ok := m.data[*params.Key]; !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "missing"}
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
		t.Fatalf("wrote %d", n)
	}
	if ct := client.ctype["uploads/1/hero.jpg"]; !strings.Contains(ct, "jpeg") {
		t.Fatalf("content-type=%q", ct)
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
	if disk.URL("1/hero.jpg") != "https://cdn.example.com/uploads/1/hero.jpg" {
		t.Fatalf("url=%q", disk.URL("1/hero.jpg"))
	}
	if disk.Name() != "s3" {
		t.Fatal(disk.Name())
	}
	if err := disk.Delete(ctx, "1/hero.jpg"); err != nil {
		t.Fatal(err)
	}
}

func TestNewS3DiskValidation(t *testing.T) {
	_, err := medialibrary.NewS3Disk(medialibrary.S3DiskConfig{})
	if !errors.Is(err, medialibrary.ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	_, err = medialibrary.NewS3Disk(medialibrary.S3DiskConfig{Name: "s3", Client: newMockS3()})
	if !errors.Is(err, medialibrary.ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
	_, err = medialibrary.NewS3Disk(medialibrary.S3DiskConfig{Name: "s3", Bucket: "b"})
	if !errors.Is(err, medialibrary.ErrInvalidArgument) {
		t.Fatalf("got %v", err)
	}
}

func TestS3DiskEdges(t *testing.T) {
	client := newMockS3()
	disk, err := medialibrary.NewS3Disk(medialibrary.S3DiskConfig{Name: "s3", Bucket: "b", Client: client})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if disk.URL("x") != "/x" {
		t.Fatalf("%q", disk.URL("x"))
	}
	client.failPut = true
	if _, err := disk.Put(ctx, "a.jpg", strings.NewReader("x")); err == nil {
		t.Fatal("put fail")
	}
	client.failPut = false
	_, _ = disk.Put(ctx, "a.jpg", strings.NewReader("x"))
	client.apiCode = "Boom"
	if _, err := disk.Exists(ctx, "a.jpg"); err == nil {
		t.Fatal("exists err")
	}
	client.apiCode = "NoSuchKey"
	ok, err := disk.Exists(ctx, "missing")
	if err != nil || ok {
		t.Fatalf("%v %v", ok, err)
	}
	client.apiCode = ""
	client.failGet = true
	if _, err := disk.Open(ctx, "a.jpg"); err == nil {
		t.Fatal("open fail")
	}
}
