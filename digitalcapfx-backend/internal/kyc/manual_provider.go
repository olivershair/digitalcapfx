package kyc

import (
	"context"
	"errors"
	"net/http"
)

// ErrManualModeNoWebhooks is returned when a provider webhook reaches the API
// while KYC runs in manual mode: no automated vendor is configured, so no
// delivery can be trusted or attributed.
var ErrManualModeNoWebhooks = errors.New("kyc manual mode: provider webhooks are disabled")

// ManualProvider is the "no vendor" KYC mode (KYC_PROVIDER=manual). The
// customer uploads an ID document and a selfie holding it, then submits the
// file; a staff member with the kyc:approve / kyc:reject permission decides
// via the admin endpoints. There is no SDK session and no webhook.
type ManualProvider struct{}

// NewManualProvider returns the manual KYC provider.
func NewManualProvider() *ManualProvider { return &ManualProvider{} }

// Name identifies the mode.
func (p *ManualProvider) Name() string { return "manual" }

// Initiate is never used to open a vendor SDK in manual mode; KYCService
// handles the submission itself. It reports the review state for safety.
func (p *ManualProvider) Initiate(_ context.Context, _, _, _ string) (*VerificationSession, error) {
	return &VerificationSession{FlowID: "manual", Status: "under_review"}, nil
}

// HandleWebhook always refuses: there is no vendor in manual mode.
func (p *ManualProvider) HandleWebhook(_ context.Context, _ []byte, _ http.Header) (*VerificationEvent, error) {
	return nil, ErrManualModeNoWebhooks
}
