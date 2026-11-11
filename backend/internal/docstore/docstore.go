// Package docstore keeps the PDFs of issued invoices and credit notes in
// S3-compatible object storage (S3_* settings). Objects are written once,
// when the document is issued, and never rewritten; Finance stores their
// SHA-256 and checks it on every download.
package docstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Store puts and gets documents by key.
type Store interface {
	Put(ctx context.Context, key string, body []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
}

// ErrNotConfigured is returned by Unconfigured.
var ErrNotConfigured = errors.New("document storage is not configured (set the S3_* settings)")

// ErrNotFound is returned when the object does not exist.
var ErrNotFound = errors.New("document not found in storage")

// Unconfigured is the Store used when S3 is not set up: every call fails.
type Unconfigured struct{}

func (Unconfigured) Put(context.Context, string, []byte, string) error { return ErrNotConfigured }
func (Unconfigured) Get(context.Context, string) ([]byte, error)       { return nil, ErrNotConfigured }

// S3Config wires an S3 store.
type S3Config struct {
	Endpoint        string
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	// PathStyle puts the bucket in the path (http://host/bucket/key) rather
	// than the host name; SeaweedFS, Garage and MinIO need it.
	PathStyle bool
}

// S3 is a minimal S3 client (PUT and GET object, AWS Signature Version 4).
type S3 struct {
	cfg  S3Config
	base *url.URL
	http *http.Client
	now  func() time.Time
}

// NewS3 validates cfg and returns a store.
func NewS3(cfg S3Config) (*S3, error) {
	u, err := url.Parse(strings.TrimRight(cfg.Endpoint, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("S3_ENDPOINT must be a URL such as https://s3.example.org, got %q", cfg.Endpoint)
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	return &S3{cfg: cfg, base: u, http: &http.Client{Timeout: 30 * time.Second}, now: time.Now}, nil
}

func (s *S3) objectURL(key string) *url.URL {
	u := *s.base
	if s.cfg.PathStyle {
		u.Path = s.base.Path + "/" + s.cfg.Bucket + "/" + key
	} else {
		u.Host = s.cfg.Bucket + "." + s.base.Host
		u.Path = s.base.Path + "/" + key
	}
	u.RawPath = ""
	return &u
}

// Put stores body under key.
func (s *S3) Put(ctx context.Context, key string, body []byte, contentType string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key).String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	s.sign(req, body)
	res, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return fmt.Errorf("s3 put %s: %s: %s", key, res.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// Get returns the object stored under key.
func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key).String(), nil)
	if err != nil {
		return nil, err
	}
	s.sign(req, nil)
	res, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3 get %s: %w", key, err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if res.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return nil, fmt.Errorf("s3 get %s: %s: %s", key, res.Status, strings.TrimSpace(string(msg)))
	}
	return io.ReadAll(io.LimitReader(res.Body, 64<<20))
}

// sign adds the AWS Signature Version 4 headers to req. Every header already
// set on req (except Content-Type and Content-Length) is signed.
func (s *S3) sign(req *http.Request, body []byte) {
	now := s.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	payloadHash := sha256Hex(body)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	headers := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		lk := strings.ToLower(k)
		if lk == "content-type" || lk == "content-length" || lk == "authorization" {
			continue
		}
		headers[lk] = strings.TrimSpace(strings.Join(v, ","))
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		uriEncodePath(req.URL.Path),
		canonicalQuery(req.URL.Query()),
		canonHeaders.String(),
		signed,
		payloadHash,
	}, "\n")
	scope := day + "/" + s.cfg.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonical))

	k := hmacSHA256([]byte("AWS4"+s.cfg.SecretAccessKey), day)
	k = hmacSHA256(k, s.cfg.Region)
	k = hmacSHA256(k, "s3")
	k = hmacSHA256(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(k, toSign))

	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.cfg.AccessKeyID, scope, signed, sig))
}

func canonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		vs := append([]string{}, q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			parts = append(parts, uriEncode(k)+"="+uriEncode(v))
		}
	}
	return strings.Join(parts, "&")
}

func uriEncodePath(p string) string {
	if p == "" {
		return "/"
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = uriEncode(s)
	}
	return strings.Join(segs, "/")
}

func uriEncode(s string) string {
	var sb strings.Builder
	for _, b := range []byte(s) {
		if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') ||
			b == '-' || b == '_' || b == '.' || b == '~' {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "%%%02X", b)
		}
	}
	return sb.String()
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

// Memory is an in-process Store for tests.
type Memory struct {
	Objects map[string][]byte
	// FailPut makes Put fail, to test that issuing rolls back.
	FailPut bool
}

// NewMemory returns an empty Memory store.
func NewMemory() *Memory { return &Memory{Objects: map[string][]byte{}} }

func (m *Memory) Put(_ context.Context, key string, body []byte, _ string) error {
	if m.FailPut {
		return errors.New("memory store: put refused")
	}
	m.Objects[key] = append([]byte{}, body...)
	return nil
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, error) {
	b, ok := m.Objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte{}, b...), nil
}
