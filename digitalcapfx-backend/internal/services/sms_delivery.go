package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/pkg/redact"
	"github.com/rachfinance/digitalfx/internal/pkg/sms"
)

// ─── SMS delivery errors ──────────────────────────────────────────────────────
//
// Three distinct situations, each with its own explicit answer to the customer:
//   - country not supported (SMS_UNSUPPORTED_CALLING_CODES)  → refused up front
//   - number refused by the SMS provider (bad / unreachable)  → this number won't work
//   - provider down / no credits / bad API key               → temporary, retry later
//     (ErrSMSDelivery, logged as an ops alert)

// ErrSMSDestinationUnsupported is the sentinel for "SMS cannot reach this
// number". Use errors.As with *SMSDestinationError for the details.
var ErrSMSDestinationUnsupported = errors.New("SMS cannot be delivered to this phone number")

// SMS destination reasons.
const (
	SMSReasonCountryUnsupported = "country_not_supported" // listed in SMS_UNSUPPORTED_CALLING_CODES
	SMSReasonRejected           = "number_rejected"       // refused by the SMS provider
)

// SMSDestinationError explains why a phone number cannot receive SMS.
type SMSDestinationError struct {
	Reason      string // SMSReasonCountryUnsupported | SMSReasonRejected
	CallingCode string // matched calling code (country_not_supported only)
}

func (e *SMSDestinationError) Error() string {
	if e.Reason == SMSReasonCountryUnsupported {
		return fmt.Sprintf("SMS is not available for phone numbers starting with +%s", e.CallingCode)
	}
	return "the SMS provider refused this phone number"
}

func (e *SMSDestinationError) Unwrap() error { return ErrSMSDestinationUnsupported }

// unsupportedCallingCode returns the configured calling code a canonical E.164
// number starts with, if SMS is known not to work for it. The longest match
// wins (e.g. "1" vs "1876").
func unsupportedCallingCode(phone string, codes []string) (string, bool) {
	digits := strings.TrimPrefix(phone, "+")
	if digits == phone { // not E.164 — cannot tell the country
		return "", false
	}
	best := ""
	for _, cc := range codes {
		if cc != "" && strings.HasPrefix(digits, cc) && len(cc) > len(best) {
			best = cc
		}
	}
	return best, best != ""
}

// checkSMSDestination refuses numbers whose country is listed as unsupported.
func (s *AuthService) checkSMSDestination(phone string) error {
	if cc, ok := unsupportedCallingCode(phone, s.cfg.Brevo.SMSUnsupportedCallingCodes); ok {
		return &SMSDestinationError{Reason: SMSReasonCountryUnsupported, CallingCode: cc}
	}
	return nil
}

// sendOTPSMS sends the code and classifies a provider failure.
func (s *AuthService) sendOTPSMS(ctx context.Context, phone, code string) error {
	if s.smsClient == nil {
		s.logger.Error("SMS ALERT: SMS client not configured (BREVO_API_KEY missing) — no customer can receive a code",
			zap.String("phone", redact.Phone(phone)))
		return ErrSMSDelivery
	}
	err := s.smsClient.SendOTP(ctx, phone, s.cfg.App.Name, code)
	if err == nil {
		return nil
	}
	var apiErr *sms.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.RecipientRejected():
		s.logger.Warn("SMS refused by provider for this number",
			zap.String("phone", redact.Phone(phone)), zap.Int("status", apiErr.Status),
			zap.String("brevo_code", apiErr.Code), zap.String("brevo_message", apiErr.Message))
		return &SMSDestinationError{Reason: SMSReasonRejected}
	case errors.As(err, &apiErr) && apiErr.PlatformProblem():
		// Affects EVERY customer: invalid key, no credits, blocked IP, rate limit.
		s.logger.Error("SMS ALERT: provider refused our account — check Brevo API key / SMS credits / authorised IPs",
			zap.Int("status", apiErr.Status), zap.String("brevo_code", apiErr.Code),
			zap.String("brevo_message", apiErr.Message))
	default:
		s.logger.Error("send OTP SMS failed", zap.String("phone", redact.Phone(phone)), zap.Error(err))
	}
	return fmt.Errorf("%w: %v", ErrSMSDelivery, err)
}
