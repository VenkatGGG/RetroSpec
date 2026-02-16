package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Store struct {
	bucket string
	client *s3.Client
}

func NewS3Store(
	ctx context.Context,
	region, endpoint, accessKey, secretKey, bucket string,
) (*S3Store, error) {
	loadOpts := []func(*awsConfig.LoadOptions) error{
		awsConfig.WithRegion(region),
		awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	}

	cfg, err := awsConfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		}
	})

	return &S3Store{bucket: bucket, client: client}, nil
}

func (s *S3Store) StoreJSON(ctx context.Context, objectKey string, payload json.RawMessage) error {
	if !json.Valid(payload) {
		return fmt.Errorf("artifact payload is not valid json: %s", objectKey)
	}

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(bytes.TrimSpace(payload)),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *S3Store) LoadJSON(ctx context.Context, objectKey string) (json.RawMessage, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if !json.Valid(payload) {
		return nil, fmt.Errorf("artifact is not valid json: %s", objectKey)
	}

	return json.RawMessage(bytes.TrimSpace(payload)), nil
}

func (s *S3Store) Close() error {
	return nil
}
