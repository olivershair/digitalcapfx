package services

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/pkg/storage"
)

// ErrUploadsDisabled is returned when no uploads bucket is configured.
var ErrUploadsDisabled = fmt.Errorf("uploads are not configured on this environment")

// UploadService stores user files (KYC documents, avatars, …) in GCS and
// returns time-limited signed URLs to read them back.
type UploadService struct {
	store  *storage.GCS
	logger *zap.Logger
}

// NewUploadService opens the GCS client from config. When no bucket is set
// (e.g. local dev) it returns a service whose calls report ErrUploadsDisabled,
// so the rest of the app still boots.
func NewUploadService(cfg *config.Config, logger *zap.Logger) *UploadService {
	if cfg.GCP.UploadsBucket == "" {
		logger.Warn("uploads disabled: no UPLOADS_BUCKET / KYC_BUCKET configured")
		return &UploadService{logger: logger}
	}
	store, err := storage.New(context.Background(), cfg.GCP.UploadsBucket, cfg.GCP.SignerSA)
	if err != nil {
		logger.Error("uploads disabled: failed to open GCS client", zap.Error(err))
		return &UploadService{logger: logger}
	}
	logger.Info("uploads enabled", zap.String("bucket", cfg.GCP.UploadsBucket))
	return &UploadService{store: store, logger: logger}
}

// UploadResult describes a stored object.
type UploadResult struct {
	Object   string `json:"object"`   // path within the bucket
	URI      string `json:"uri"`      // gs://bucket/object (stable reference)
	ReadURL  string `json:"read_url"` // signed HTTPS URL, time-limited
	Bucket   string `json:"bucket"`
	Purpose  string `json:"purpose"`
	MimeType string `json:"mime_type"`
}

var safeName = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// maxSignedTTL stays just under GCS's hard V4 limit of 604800s (7 days) to
// avoid the boundary being rejected once request latency/clock skew is added.
const maxSignedTTL = 7*24*time.Hour - 5*time.Minute

// validPurposes are the allowed logical buckets/prefixes for an upload.
var validPurposes = map[string]bool{
	"kyc": true, "avatar": true, "document": true, "business": true, "misc": true,
}

// Upload stores a file for a user under a purpose prefix and returns its
// object path plus a 7-day signed read URL.
func (s *UploadService) Upload(ctx context.Context, userID uuid.UUID, purpose, filename, contentType string, r io.Reader) (*UploadResult, error) {
	if s.store == nil {
		return nil, ErrUploadsDisabled
	}
	purpose = strings.ToLower(strings.TrimSpace(purpose))
	if purpose == "" {
		purpose = "misc"
	}
	if !validPurposes[purpose] {
		return nil, fmt.Errorf("invalid purpose %q (allowed: kyc, avatar, document, business, misc)", purpose)
	}

	clean := safeName.ReplaceAllString(path.Base(filename), "_")
	if clean == "" || clean == "_" {
		clean = "file"
	}
	object := fmt.Sprintf("%s/%s/%s-%s", purpose, userID.String(), uuid.NewString(), clean)

	uri, err := s.store.Upload(ctx, object, r, contentType)
	if err != nil {
		return nil, err
	}

	res := &UploadResult{
		Object:   object,
		URI:      uri,
		Bucket:   s.store.Bucket(),
		Purpose:  purpose,
		MimeType: contentType,
	}
	// Best-effort signed URL — upload still succeeds if signing is unavailable.
	if url, err := s.store.SignedReadURL(object, maxSignedTTL); err == nil {
		res.ReadURL = url
	} else {
		s.logger.Warn("could not sign read url", zap.String("object", object), zap.Error(err))
	}
	return res, nil
}

// ErrUploadForbidden is returned when a caller asks for an object that does
// not belong to them.
var ErrUploadForbidden = fmt.Errorf("object not found")

// ReadURLForUser returns a signed read URL only if the object belongs to
// userID. Objects are stored as "<purpose>/<user id>/<uuid>-<name>" (see
// Upload), so ownership is the second path segment. Any other shape — "..",
// absolute paths, another user's id, unknown prefix — is refused, so a
// customer can never obtain another customer's KYC documents.
func (s *UploadService) ReadURLForUser(userID uuid.UUID, object string, ttl time.Duration) (string, error) {
	if s.store == nil {
		return "", ErrUploadsDisabled
	}
	if !objectOwnedBy(object, userID) {
		return "", ErrUploadForbidden
	}
	return s.ReadURL(object, ttl)
}

// objectOwnedBy reports whether object is a well-formed upload path owned by userID.
func objectOwnedBy(object string, userID uuid.UUID) bool {
	if object == "" || strings.Contains(object, "..") || strings.HasPrefix(object, "/") ||
		strings.Contains(object, "\\") || path.Clean(object) != object {
		return false
	}
	parts := strings.SplitN(object, "/", 3)
	if len(parts) != 3 || parts[2] == "" {
		return false
	}
	return validPurposes[parts[0]] && parts[1] == userID.String()
}

// ReadURL returns a fresh signed read URL for an already-stored object. It
// performs NO ownership check — only call it from trusted code paths (e.g.
// staff-only handlers); customer-facing handlers must use ReadURLForUser.
func (s *UploadService) ReadURL(object string, ttl time.Duration) (string, error) {
	if s.store == nil {
		return "", ErrUploadsDisabled
	}
	if ttl <= 0 || ttl > maxSignedTTL {
		ttl = maxSignedTTL
	}
	return s.store.SignedReadURL(object, ttl)
}

// objectFromReference extracts the bucket object path from a stored document
// reference. Accepted forms: a bare object path ("kyc/<user>/<file>"),
// "gs://<bucket>/<object>", or a (signed) HTTPS URL on storage.googleapis.com
// in path style (/<bucket>/<object>) or virtual-host style
// (<bucket>.storage.googleapis.com/<object>). When the service knows its
// bucket, references to any other bucket are refused.
func (s *UploadService) objectFromReference(ref string) (string, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false
	}
	bucket := ""
	if s.store != nil {
		bucket = s.store.Bucket()
	}
	sameBucket := func(b string) bool { return bucket == "" || b == bucket }

	switch {
	case strings.HasPrefix(ref, "gs://"):
		rest := strings.TrimPrefix(ref, "gs://")
		b, obj, ok := strings.Cut(rest, "/")
		if !ok || !sameBucket(b) {
			return "", false
		}
		return obj, obj != ""
	case strings.HasPrefix(ref, "https://"):
		u, err := url.Parse(ref)
		if err != nil {
			return "", false
		}
		p := strings.TrimPrefix(u.Path, "/")
		switch {
		case u.Host == "storage.googleapis.com":
			b, obj, ok := strings.Cut(p, "/")
			if !ok || !sameBucket(b) {
				return "", false
			}
			return obj, obj != ""
		case strings.HasSuffix(u.Host, ".storage.googleapis.com"):
			if !sameBucket(strings.TrimSuffix(u.Host, ".storage.googleapis.com")) {
				return "", false
			}
			return p, p != ""
		}
		return "", false
	case strings.Contains(ref, "://"):
		return "", false
	default:
		return ref, true
	}
}

// OwnedObject returns the object path a document reference points to, and
// whether that object was uploaded by userID (see objectOwnedBy).
func (s *UploadService) OwnedObject(ref string, userID uuid.UUID) (string, bool) {
	obj, ok := s.objectFromReference(ref)
	if !ok || !objectOwnedBy(obj, userID) {
		return "", false
	}
	return obj, true
}
