package output

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/jaltamir/spotlight/internal/aggregator"
	internalconfig "github.com/jaltamir/spotlight/internal/config"
)

// s3API is the subset of the S3 client API used by S3Writer.
// It allows injecting a mock in tests.
type s3API interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	DeleteObject(ctx context.Context, params *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// S3Writer uploads the report to an S3 bucket.
type S3Writer struct {
	cfg    internalconfig.S3Config
	client s3API // nil means create from config at Write time
}

func NewS3Writer(cfg internalconfig.S3Config) *S3Writer {
	return &S3Writer{cfg: cfg}
}

func (w *S3Writer) Name() string { return "s3" }

func (w *S3Writer) Write(ctx context.Context, report *aggregator.Report, _ string, timestamp string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	client := w.client
	if client == nil {
		c, err := newS3Client(ctx, w.cfg)
		if err != nil {
			return fmt.Errorf("creating s3 client: %w", err)
		}
		client = c
	}

	key := w.cfg.Prefix + fmt.Sprintf("spotlight-report-%s.json", timestamp)
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(w.cfg.Bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("uploading to s3: %w", err)
	}

	if w.cfg.RetainLast > 0 {
		if err := pruneOldReports(ctx, client, w.cfg); err != nil {
			return fmt.Errorf("pruning old reports: %w", err)
		}
	}

	return nil
}

func newS3Client(ctx context.Context, cfg internalconfig.S3Config) (*s3.Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, err
	}

	return s3.NewFromConfig(awsCfg), nil
}

func pruneOldReports(ctx context.Context, client s3API, cfg internalconfig.S3Config) error {
	listOutput, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(cfg.Bucket),
		Prefix: aws.String(cfg.Prefix + "spotlight-report-"),
	})
	if err != nil {
		return err
	}

	type obj struct {
		Key          string
		LastModified time.Time
	}

	var reports []obj
	for _, o := range listOutput.Contents {
		if strings.HasSuffix(*o.Key, ".json") {
			reports = append(reports, obj{Key: *o.Key, LastModified: *o.LastModified})
		}
	}

	if len(reports) <= cfg.RetainLast {
		return nil
	}

	sort.Slice(reports, func(i, j int) bool {
		return reports[i].LastModified.After(reports[j].LastModified)
	})

	for _, r := range reports[cfg.RetainLast:] {
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(cfg.Bucket),
			Key:    aws.String(r.Key),
		})
		if err != nil {
			return fmt.Errorf("deleting %s: %w", r.Key, err)
		}
	}

	return nil
}

// mockS3 is a test double for the S3 API.
type mockS3 struct {
	putObjects    []s3.PutObjectInput
	listResult    []types.Object
	deletedKeys   []string
	putErr        error
	listErr       error
	deleteErr     error
}

func (m *mockS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if m.putErr != nil {
		return nil, m.putErr
	}
	m.putObjects = append(m.putObjects, *params)
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return &s3.ListObjectsV2Output{Contents: m.listResult}, nil
}

func (m *mockS3) DeleteObject(_ context.Context, params *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	m.deletedKeys = append(m.deletedKeys, *params.Key)
	return &s3.DeleteObjectOutput{}, nil
}
