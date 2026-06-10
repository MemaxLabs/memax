package objectstore

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type PutInput struct {
	Key         string
	Body        io.Reader
	ContentType string
	SizeBytes   int64
}

type GetResult struct {
	Body        io.ReadCloser
	ContentType string
	SizeBytes   int64
}

type Store interface {
	Put(ctx context.Context, input PutInput) error
	Get(ctx context.Context, key string) (*GetResult, error)
	Delete(ctx context.Context, key string) error
	PresignPut(ctx context.Context, key string, contentType string, expires time.Duration) (string, map[string]string, error)
	// PresignPutWithSize returns a presigned PUT URL that rejects uploads
	// larger than maxBytes at the storage layer. This is defense-in-depth
	// against clients that claim a small size_bytes but then upload a much
	// larger body; without this, the server's application-layer check can
	// be bypassed. Pass 0 or a negative value for no size constraint.
	PresignPutWithSize(ctx context.Context, key string, contentType string, maxBytes int64, expires time.Duration) (string, map[string]string, error)
}

func NewFromEnv() Store {
	endpoint := strings.TrimSpace(os.Getenv("S3_ENDPOINT"))
	publicEndpoint := strings.TrimSpace(os.Getenv("S3_PUBLIC_ENDPOINT"))
	accessKey := strings.TrimSpace(os.Getenv("S3_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("S3_SECRET_KEY"))
	bucket := strings.TrimSpace(os.Getenv("S3_BUCKET"))
	region := strings.TrimSpace(os.Getenv("S3_REGION"))
	if accessKey == "" || secretKey == "" || bucket == "" {
		return nil
	}
	if region == "" {
		region = "us-east-1"
	}

	cfg, err := config.LoadDefaultConfig(
		context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil
	}

	opts := func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		if shouldUsePathStyle(endpoint) {
			o.UsePathStyle = true
		}
	}

	presignEndpoint := publicEndpoint
	if presignEndpoint == "" {
		presignEndpoint = endpoint
	}
	presignOpts := func(o *s3.Options) {
		if presignEndpoint != "" {
			o.BaseEndpoint = aws.String(presignEndpoint)
		}
		if shouldUsePathStyle(presignEndpoint) {
			o.UsePathStyle = true
		}
	}

	return &S3Store{
		client:  s3.NewFromConfig(cfg, opts),
		presign: s3.NewPresignClient(s3.NewFromConfig(cfg, presignOpts)),
		bucket:  bucket,
	}
}

func shouldUsePathStyle(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	return strings.Contains(endpoint, "localhost") ||
		strings.Contains(endpoint, "127.0.0.1") ||
		strings.Contains(endpoint, "minio")
}

type S3Store struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
}

func (s *S3Store) Put(ctx context.Context, input PutInput) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(input.Key),
		Body:        input.Body,
		ContentType: aws.String(input.ContentType),
	})
	if err != nil {
		return fmt.Errorf("put object %s: %w", input.Key, err)
	}
	return nil
}

func (s *S3Store) Get(ctx context.Context, key string) (*GetResult, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}
	return &GetResult{
		Body:        out.Body,
		ContentType: aws.ToString(out.ContentType),
		SizeBytes:   aws.ToInt64(out.ContentLength),
	}, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete object %s: %w", key, err)
	}
	return nil
}

func (s *S3Store) PresignPut(ctx context.Context, key string, contentType string, expires time.Duration) (string, map[string]string, error) {
	return s.PresignPutWithSize(ctx, key, contentType, 0, expires)
}

// PresignPutWithSize bakes the expected Content-Length into the presigned
// request's signature when expectedBytes > 0. S3/R2 then rejects any PUT
// whose body length differs from the signed value, so a client that claims
// 5 MiB at presign time but uploads 200 MiB gets a signature-mismatch
// response before the bytes ever persist.
//
// The upstream handler is responsible for validating `expectedBytes` against
// its plan cap before calling this — this method enforces the claim, not
// the cap.
func (s *S3Store) PresignPutWithSize(ctx context.Context, key string, contentType string, expectedBytes int64, expires time.Duration) (string, map[string]string, error) {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}
	if expectedBytes > 0 {
		input.ContentLength = aws.Int64(expectedBytes)
	}
	req, err := s.presign.PresignPutObject(ctx, input, func(o *s3.PresignOptions) {
		if expires > 0 {
			o.Expires = expires
		}
	})
	if err != nil {
		return "", nil, fmt.Errorf("presign put object %s: %w", key, err)
	}
	headers := map[string]string{"Content-Type": contentType}
	if expectedBytes > 0 {
		// The client must send exactly this Content-Length or the signature
		// fails. Returning it in the response means the CLI/web don't have
		// to round-trip their own size calculation.
		headers["Content-Length"] = fmt.Sprintf("%d", expectedBytes)
	}
	return req.URL, headers, nil
}
