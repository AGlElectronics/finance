package docstore

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// The "GET Object" example of the AWS Signature Version 4 documentation.
func TestSignMatchesAWSExample(t *testing.T) {
	s, err := NewS3(S3Config{
		Endpoint:        "https://s3.amazonaws.com",
		Bucket:          "examplebucket",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	})
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) }
	req, _ := http.NewRequest(http.MethodGet, s.objectURL("test.txt").String(), nil)
	req.Header.Set("Range", "bytes=0-9")
	s.sign(req, nil)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization =\n%s\nwant\n%s", got, want)
	}
	if req.URL.Host != "examplebucket.s3.amazonaws.com" {
		t.Fatalf("virtual-hosted URL host = %s", req.URL.Host)
	}
}

func TestPathStyleURL(t *testing.T) {
	s, _ := NewS3(S3Config{Endpoint: "http://localhost:9000/", Bucket: "finance-data-dev", PathStyle: true})
	if got := s.objectURL("invoices/a b.pdf").String(); got != "http://localhost:9000/finance-data-dev/invoices/a%20b.pdf" {
		t.Fatalf("url = %s", got)
	}
}

// TestS3RoundTrip runs against a real S3 endpoint (the dev SeaweedFS) when
// FINANCE_TEST_S3_ENDPOINT is set, e.g. http://localhost:9000 with
// FINANCE_TEST_S3_ACCESS_KEY_ID=conservices FINANCE_TEST_S3_SECRET_ACCESS_KEY=conservicesdev.
func TestS3RoundTrip(t *testing.T) {
	endpoint := os.Getenv("FINANCE_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("FINANCE_TEST_S3_ENDPOINT not set")
	}
	bucket := os.Getenv("FINANCE_TEST_S3_BUCKET")
	if bucket == "" {
		bucket = "finance-data-dev"
	}
	s, err := NewS3(S3Config{
		Endpoint: endpoint, Bucket: bucket, PathStyle: true,
		AccessKeyID:     os.Getenv("FINANCE_TEST_S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("FINANCE_TEST_S3_SECRET_ACCESS_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "test/roundtrip-" + time.Now().Format("20060102T150405.000000000") + ".pdf"
	body := []byte("%PDF-1.4 test " + strings.Repeat("x", 100))
	if err := s.Put(ctx, key, body, "application/pdf"); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("got %q", got)
	}
	if _, err := s.Get(ctx, key+".missing"); err != ErrNotFound {
		t.Fatalf("missing object: %v", err)
	}
}
