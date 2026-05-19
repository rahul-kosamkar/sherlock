package objectstore

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Store struct {
	client *s3.Client
	bucket string
}

func New(_ context.Context, endpoint, bucket, region, accessKey, secretKey string, usePathStyle bool) (*Store, error) {
	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = usePathStyle
	})
	return &Store{client: client, bucket: bucket}, nil
}

func (s *Store) PutRawPayload(ctx context.Context, key string, data []byte) error {
	return s.put(ctx, key, data)
}

func (s *Store) PutEvidenceBlob(ctx context.Context, key string, data []byte) error {
	return s.put(ctx, key, data)
}

func (s *Store) GetBlob(ctx context.Context, key string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %s: %w", key, err)
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (s *Store) put(ctx context.Context, key string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}
