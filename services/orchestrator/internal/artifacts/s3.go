package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
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
	payload, _, err := s.LoadObject(ctx, objectKey)
	if err != nil {
		return nil, err
	}

	if !json.Valid(payload) {
		return nil, fmt.Errorf("artifact is not valid json: %s", objectKey)
	}

	return json.RawMessage(bytes.TrimSpace(payload)), nil
}

func (s *S3Store) LoadObject(ctx context.Context, objectKey string) ([]byte, string, error) {
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	contentType := ""
	if resp.ContentType != nil {
		contentType = *resp.ContentType
	}

	return bytes.TrimSpace(payload), contentType, nil
}

func (s *S3Store) DeleteObject(ctx context.Context, objectKey string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	})
	return err
}

func (s *S3Store) EnsureLifecyclePolicy(
	ctx context.Context,
	expirationDays int,
	prefixes []string,
) error {
	if expirationDays < 1 {
		return fmt.Errorf("expirationDays must be >= 1")
	}

	normalizedPrefixes := normalizeLifecyclePrefixes(prefixes)
	rules := make([]types.LifecycleRule, 0, len(normalizedPrefixes))
	expireDays := int32(expirationDays)
	abortDays := int32(1)
	if expirationDays > 1 {
		if expirationDays < 7 {
			abortDays = int32(expirationDays)
		} else {
			abortDays = 7
		}
	}

	for index, prefix := range normalizedPrefixes {
		ruleID := fmt.Sprintf("retrospec-expire-%d", index+1)
		filter := &types.LifecycleRuleFilter{}
		if prefix != "" {
			filter.Prefix = aws.String(prefix)
		}

		rules = append(rules, types.LifecycleRule{
			ID:     aws.String(ruleID),
			Status: types.ExpirationStatusEnabled,
			Filter: filter,
			Expiration: &types.LifecycleExpiration{
				Days: aws.Int32(expireDays),
			},
			AbortIncompleteMultipartUpload: &types.AbortIncompleteMultipartUpload{
				DaysAfterInitiation: aws.Int32(abortDays),
			},
		})
	}

	_, err := s.client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(s.bucket),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{
			Rules: rules,
		},
	})
	if err != nil {
		return fmt.Errorf("put bucket lifecycle configuration: %w", err)
	}
	return nil
}

func (s *S3Store) Close() error {
	return nil
}

func normalizeLifecyclePrefixes(prefixes []string) []string {
	if len(prefixes) == 0 {
		return []string{""}
	}

	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		trimmed := strings.TrimSpace(prefix)
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	if len(normalized) == 0 {
		return []string{""}
	}
	return normalized
}
