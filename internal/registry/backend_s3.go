// SPDX-License-Identifier: Apache-2.0

package registry

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Backend struct {
	newClient S3ClientFactory
}

type S3ClientFactory func(uri URI, auth AuthConfig) (S3Client, error)

func NewS3Backend(factory S3ClientFactory) *S3Backend {
	if factory == nil {
		factory = DefaultS3ClientFactory
	}
	return &S3Backend{newClient: factory}
}

func (b *S3Backend) Scheme() Scheme { return SchemeS3 }

func (b *S3Backend) Bundle(ctx context.Context, srcDir string, uri URI, auth AuthConfig) (string, error) {
	if err := requireScheme(uri, SchemeS3); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("bundling s3://%s/%s: %w", uri.Bucket, uri.Path, err)
	}
	blob, digest, err := BundleBytes(srcDir)
	if err != nil {
		return "", fmt.Errorf("bundling s3://%s/%s: %w", uri.Bucket, uri.Path, err)
	}
	client, err := b.newClient(uri, auth)
	if err != nil {
		return "", fmt.Errorf("bundling s3://%s/%s: %w", uri.Bucket, uri.Path, err)
	}
	key := s3ObjectKey(uri)
	if err := client.PutObject(ctx, uri.Bucket, key, bytes.NewReader(blob), int64(len(blob))); err != nil {
		return "", fmt.Errorf("bundling s3://%s/%s: %w", uri.Bucket, key, err)
	}
	return digest, nil
}

func (b *S3Backend) Fetch(ctx context.Context, uri URI, auth AuthConfig, destDir string) error {
	if err := requireScheme(uri, SchemeS3); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("fetching s3://%s/%s: %w", uri.Bucket, uri.Path, err)
	}
	client, err := b.newClient(uri, auth)
	if err != nil {
		return fmt.Errorf("fetching s3://%s/%s: %w", uri.Bucket, uri.Path, err)
	}
	key := s3ObjectKey(uri)
	body, err := client.GetObject(ctx, uri.Bucket, key)
	if err != nil {
		return fmt.Errorf("fetching s3://%s/%s: %w", uri.Bucket, key, err)
	}
	defer func() { _ = body.Close() }()
	if err := ExtractBundle(body, destDir); err != nil {
		return fmt.Errorf("fetching s3://%s/%s: %w", uri.Bucket, key, err)
	}
	return nil
}

func s3ObjectKey(uri URI) string {
	if strings.HasSuffix(uri.Path, "/") {
		return uri.Path + "bundle.tar.gz"
	}
	return uri.Path
}

func DefaultS3ClientFactory(uri URI, auth AuthConfig) (S3Client, error) {
	endpoint := uri.Endpoint
	if endpoint == "" {
		endpoint = auth.Endpoint
	}
	if endpoint == "" {
		endpoint = "https://s3.amazonaws.com"
	}
	host, secure, err := splitEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	opts := &minio.Options{Secure: secure, Region: auth.Region}
	if username, password, ok := auth.BasicAuth(); ok && username != "" && password != "" {
		opts.Creds = credentials.NewStaticV4(username, password, "")
	}
	client, err := minio.New(host, opts)
	if err != nil {
		return nil, fmt.Errorf("creating s3 client for %q: %w", host, err)
	}
	return &minioS3Client{client: client}, nil
}

func splitEndpoint(endpoint string) (string, bool, error) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return "", false, fmt.Errorf("s3 endpoint is empty")
	}
	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Host == "" {
			return "", false, fmt.Errorf("s3 endpoint %q is not a valid URL", endpoint)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return "", false, fmt.Errorf("s3 endpoint %q must use http(s)", endpoint)
		}
		return parsed.Host, parsed.Scheme == "https", nil
	}
	return trimmed, true, nil
}

var _ S3Client = (*minioS3Client)(nil)

type minioS3Client struct {
	client *minio.Client
}

func (m *minioS3Client) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting object %s/%s: %w", bucket, key, err)
	}
	return obj, nil
}

func (m *minioS3Client) PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64) error {
	_, err := m.client.PutObject(ctx, bucket, key, body, size, minio.PutObjectOptions{ContentType: BundleMediaType})
	if err != nil {
		return fmt.Errorf("putting object %s/%s: %w", bucket, key, err)
	}
	return nil
}
