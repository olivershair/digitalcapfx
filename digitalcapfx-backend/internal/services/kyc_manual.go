package services

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"github.com/rachfinance/digitalfx/internal/kyc"
)

// ─── Manual KYC (KYC_PROVIDER=manual) ─────────────────────────────────────────
//
// Journey without a vendor:
//  1. the customer fills the intake form           (POST /kyc/intake)
//  2. uploads the required documents, incl. the ID  (POST /uploads → POST /kyc/documents)
//     and a selfie holding it
//  3. submits the file                              (POST /kyc/init)
//     → kyc_status "submitted", identity "in_review", file appears in
//       GET /admin/kyc/pending, ops inbox notified (ADMIN_NOTIFY_EMAIL)
//  4. staff review it                               (GET /admin/kyc/{id}: fields + documents with fresh view URLs)
//     and decide                                    (POST /admin/kyc/{id}/approve | /reject)
//     A rejection puts the file back in "resubmit" so the customer can fix and resubmit.

var (
	// ErrKYCDocumentsMissing is returned when a manual submission lacks a
	// required document. Use errors.As with *KYCDocumentsMissingError for the list.
	ErrKYCDocumentsMissing = errors.New("required KYC documents are missing")
	// ErrKYCFileLocked is returned when the customer tries to change a file that
	// is under review or already approved.
	ErrKYCFileLocked = errors.New("your verification file is under review or already approved and can no longer be changed")
	// ErrKYCDocumentNotOwned is returned when a document reference does not
	// point to a file the caller uploaded.
	ErrKYCDocumentNotOwned = errors.New("doc_url must reference a file you uploaded via POST /uploads")
)

// KYCDocumentsMissingError lists the missing document keys and labels.
type KYCDocumentsMissingError struct {
	Missing []DocumentSpec
}

func (e *KYCDocumentsMissingError) Error() string {
	labels := make([]string, 0, len(e.Missing))
	for _, d := range e.Missing {
		labels = append(labels, d.Label)
	}
	return "required KYC documents are missing: " + strings.Join(labels, ", ")
}

func (e *KYCDocumentsMissingError) Unwrap() error { return ErrKYCDocumentsMissing }

// ManualMode reports whether KYC runs without a vendor.
func (s *KYCService) ManualMode() bool {
	return s.cfg != nil && s.cfg.KYCManualMode()
}

// requiredDocsFor returns the documents that must be on file before a manual
// submission, honouring the conditional (importer / NRE) rules.
func requiredDocsFor(req IntakeRequirements, isImporter bool) []DocumentSpec {
	var out []DocumentSpec
	for _, d := range req.Documents {
		if !d.Required {
			continue
		}
		if d.AppliesTo == "importers" && !isImporter {
			continue
		}
		out = append(out, d)
	}
	return out
}

// submitManualReview queues the customer's file for staff review.
func (s *KYCService) submitManualReview(ctx context.Context, q *db.Queries, user db.User, intake db.KycIntake) (*kyc.VerificationSession, error) {
	switch user.KycStatus {
	case "approved":
		return &kyc.VerificationSession{FlowID: "manual", Status: "approved"}, nil
	case "submitted", "under_review", "processing":
		// Already queued — idempotent.
		return &kyc.VerificationSession{FlowID: "manual", Status: "under_review"}, nil
	}

	isImporter := intake.IsImporter != nil && *intake.IsImporter
	req := BuildIntakeRequirements(user.AccountType, IntakeOptions{
		NeedsNRE:   user.AccountType == "business",
		IsImporter: isImporter,
		Manual:     true,
	})

	docs, err := q.GetKYCDocumentsByUserID(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("list kyc documents: %w", err)
	}
	have := map[string]bool{}
	for _, d := range docs {
		have[d.DocType] = true
	}
	var missing []DocumentSpec
	for _, d := range requiredDocsFor(req, isImporter) {
		if !have[d.Key] {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		return nil, &KYCDocumentsMissingError{Missing: missing}
	}

	if _, err := q.UpdateUserKYCStatus(ctx, db.UpdateUserKYCStatusParams{ID: user.ID, KycStatus: "submitted"}); err != nil {
		return nil, fmt.Errorf("queue kyc for review: %w", err)
	}
	if err := q.UpsertKYCIdentityFromWebhook(ctx, db.UpsertKYCIdentityFromWebhookParams{
		UserID: user.ID,
		Status: IdentityInReview,
	}); err != nil {
		s.logger.Error("manual kyc: set identity in_review", zap.Error(err))
	}

	s.notif.Create(ctx, CreateNotificationInput{
		UserID: user.ID,
		Type:   NotifKYCSubmitted,
		Title:  "Verification file submitted",
		Body:   "Your documents have been sent to our compliance team. We'll notify you as soon as a decision is made.",
	})
	go s.notifyOpsManualKYC(user, len(docs))

	s.logger.Info("manual kyc submitted for review",
		zap.String("user_id", user.ID.String()), zap.String("account_type", user.AccountType), zap.Int("documents", len(docs)))
	return &kyc.VerificationSession{FlowID: "manual", Status: "under_review"}, nil
}

// notifyOpsManualKYC emails the ops inbox (ADMIN_NOTIFY_EMAIL) that a file
// awaits review. Best-effort.
func (s *KYCService) notifyOpsManualKYC(user db.User, docCount int) {
	to := s.cfg.App.AdminNotifyEmail
	if to == "" || s.emailClient == nil {
		return
	}
	name := strings.TrimSpace(user.FirstName + " " + user.LastName)
	subject := "KYC file awaiting review: " + name
	body := fmt.Sprintf(`<div style="font-family:Arial,sans-serif;max-width:520px;margin:0 auto;color:#111">
  <h2 style="margin:0 0 12px">KYC file awaiting manual review</h2>
  <table style="font-size:14px;line-height:1.9;border-collapse:collapse">
    <tr><td style="color:#666;padding-right:16px">Customer</td><td><strong>%s</strong></td></tr>
    <tr><td style="color:#666;padding-right:16px">User ID</td><td>%s</td></tr>
    <tr><td style="color:#666;padding-right:16px">Account type</td><td>%s</td></tr>
    <tr><td style="color:#666;padding-right:16px">Documents</td><td>%d</td></tr>
    <tr><td style="color:#666;padding-right:16px">Submitted</td><td>%s</td></tr>
  </table>
  <p style="font-size:13px;color:#444">Review it in the admin console (KYC → pending).</p>
</div>`, html.EscapeString(name), user.ID.String(), html.EscapeString(user.AccountType), docCount,
		time.Now().UTC().Format("02 Jan 2006 15:04 UTC"))
	if err := s.emailClient.Send(to, subject, body); err != nil {
		s.logger.Error("manual kyc: notify ops", zap.Error(err))
	}
}

// AdminKYCDocument is a stored document plus a fresh, short-lived URL that
// staff can open. OwnedByUser is false when the reference does not point to a
// file this customer uploaded (legacy or tampered data) — no URL is issued then.
type AdminKYCDocument struct {
	db.KycDocument
	ViewURL     string `json:"view_url,omitempty"`
	OwnedByUser bool   `json:"owned_by_user"`
}

// staffDocumentViewTTL keeps staff links short-lived (identity documents).
const staffDocumentViewTTL = 15 * time.Minute

func (s *KYCService) adminDocuments(userID uuid.UUID, docs []db.KycDocument) []AdminKYCDocument {
	out := make([]AdminKYCDocument, 0, len(docs))
	for _, d := range docs {
		item := AdminKYCDocument{KycDocument: d}
		if s.uploads != nil {
			if obj, ok := s.uploads.OwnedObject(d.DocURL, userID); ok {
				item.OwnedByUser = true
				if u, err := s.uploads.ReadURL(obj, staffDocumentViewTTL); err == nil {
					item.ViewURL = u
				}
			}
		}
		out = append(out, item)
	}
	return out
}

// markManualDecision mirrors a staff decision on the identity sub-state so the
// customer's journey shows the right stage. A rejection becomes "resubmit" in
// manual mode, which unlocks the form for a corrected submission.
func (s *KYCService) markManualDecision(ctx context.Context, q *db.Queries, userID uuid.UUID, approved bool, reason string) {
	if !s.ManualMode() {
		return
	}
	params := db.UpsertKYCIdentityFromWebhookParams{UserID: userID, Status: IdentityApproved}
	if !approved {
		params.Status = IdentityResubmit
		if reason != "" {
			params.ModerationComment = &reason
		}
	}
	if err := q.UpsertKYCIdentityFromWebhook(ctx, params); err != nil {
		s.logger.Error("manual kyc: record decision on identity", zap.Error(err))
	}
}
