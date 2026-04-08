package output

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/jaltamir/spotlight/internal/aggregator"
	internalconfig "github.com/jaltamir/spotlight/internal/config"
)

func TestS3WriterName(t *testing.T) {
	w := NewS3Writer(internalconfig.S3Config{})
	if w.Name() != "s3" {
		t.Errorf("expected name=s3, got %s", w.Name())
	}
}

func TestS3Upload(t *testing.T) {
	mock := &mockS3{}
	w := &S3Writer{
		cfg: internalconfig.S3Config{
			Bucket: "my-bucket",
			Prefix: "reports/",
		},
		client: mock,
	}

	report := &aggregator.Report{
		GeneratedAt: "2026-04-05T12:00:00Z",
		TimeWindow:  "24h",
		TotalErrors: 5,
	}

	if err := w.Write(context.Background(), report, "", "2026-04-05T120000Z"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.putObjects) != 1 {
		t.Fatalf("expected 1 PutObject call, got %d", len(mock.putObjects))
	}
	put := mock.putObjects[0]
	if *put.Bucket != "my-bucket" {
		t.Errorf("expected bucket=my-bucket, got %s", *put.Bucket)
	}
	if *put.Key != "reports/spotlight-report-2026-04-05T120000Z.json" {
		t.Errorf("unexpected key: %s", *put.Key)
	}
	if *put.ContentType != "application/json" {
		t.Errorf("unexpected content type: %s", *put.ContentType)
	}
}

func TestPruneOldReports(t *testing.T) {
	now := time.Now()
	// 5 existing reports, retain last 3.
	objects := make([]types.Object, 5)
	for i := range objects {
		key := fmt.Sprintf("reports/spotlight-report-%d.json", i)
		modTime := now.Add(-time.Duration(i) * time.Hour)
		objects[i] = types.Object{
			Key:          aws.String(key),
			LastModified: aws.Time(modTime),
		}
	}

	mock := &mockS3{listResult: objects}
	cfg := internalconfig.S3Config{
		Bucket:     "my-bucket",
		Prefix:     "reports/",
		RetainLast: 3,
	}

	if err := pruneOldReports(context.Background(), mock, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have deleted 2 oldest reports (indices 3 and 4 by modification time).
	if len(mock.deletedKeys) != 2 {
		t.Errorf("expected 2 deletions, got %d: %v", len(mock.deletedKeys), mock.deletedKeys)
	}
}

func TestPruneNotNeededWhenBelowRetainLimit(t *testing.T) {
	now := time.Now()
	objects := []types.Object{
		{Key: aws.String("reports/spotlight-report-1.json"), LastModified: aws.Time(now)},
		{Key: aws.String("reports/spotlight-report-2.json"), LastModified: aws.Time(now.Add(-time.Hour))},
	}

	mock := &mockS3{listResult: objects}
	cfg := internalconfig.S3Config{
		Bucket:     "my-bucket",
		Prefix:     "reports/",
		RetainLast: 5, // more than the 2 existing
	}

	if err := pruneOldReports(context.Background(), mock, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mock.deletedKeys) != 0 {
		t.Errorf("expected no deletions, got %d", len(mock.deletedKeys))
	}
}

func TestS3UploadWithRetainTriggersProne(t *testing.T) {
	now := time.Now()
	// 2 existing reports, retain last 1.
	objects := []types.Object{
		{Key: aws.String("reports/spotlight-report-old.json"), LastModified: aws.Time(now.Add(-2 * time.Hour))},
		{Key: aws.String("reports/spotlight-report-new.json"), LastModified: aws.Time(now.Add(-time.Hour))},
	}

	mock := &mockS3{listResult: objects}
	w := &S3Writer{
		cfg: internalconfig.S3Config{
			Bucket:     "my-bucket",
			Prefix:     "reports/",
			RetainLast: 1,
		},
		client: mock,
	}

	report := &aggregator.Report{TimeWindow: "24h"}
	if err := w.Write(context.Background(), report, "", "ts"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 1 PutObject + 1 deletion (the oldest).
	if len(mock.putObjects) != 1 {
		t.Errorf("expected 1 PutObject, got %d", len(mock.putObjects))
	}
	if len(mock.deletedKeys) != 1 {
		t.Errorf("expected 1 deletion, got %d: %v", len(mock.deletedKeys), mock.deletedKeys)
	}
}
