package services

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	db "github.com/rachfinance/digitalfx/internal/db/sqlc"
	"github.com/rachfinance/digitalfx/internal/pkg/hash"
)

// ─── Brute-force protection ───────────────────────────────────────────────────
//
// PINs and 6-digit OTPs have a small key space, so every guessable secret on a
// public auth endpoint is protected by a per-identity failure counter kept in
// Redis (shared by every API instance). Once an identity reaches the limit, the
// endpoint answers ErrTooManyAttempts — WITHOUT checking the secret — until the
// window expires. A successful check clears the counter.
//
// Counters are keyed on the account (user id) or the canonical phone/email
// address, never on the client IP, because X-Forwarded-For is client-controlled
// and an IP limit is trivially bypassed.

// ErrTooManyAttempts is returned when an identity is temporarily locked out.
// Handlers map it to HTTP 429.
var ErrTooManyAttempts = errors.New("too many attempts — try again later")

const (
	// maxAuthFailures is the number of wrong guesses allowed per window.
	maxAuthFailures = 5
	// authFailureWindow is the lockout window. It is longer than the 10-minute
	// OTP lifetime, so a code that was being guessed is expired by the time the
	// lock lifts.
	authFailureWindow = 15 * time.Minute

	// maxOTPSends caps how many codes can be sent to one identity per window,
	// which bounds the number of valid codes outstanding at the same time (and
	// SMS/email bombing).
	maxOTPSends   = 5
	otpSendWindow = time.Hour
	authKeyPrefix = "auth:"
)

// Attempt scopes (used in Redis keys).
const (
	scopeLoginPIN  = "login_pin"
	scopeLoginTOTP = "login_totp"
	scopePhoneOTP  = "phone_otp"
	scopePINReset  = "pin_reset"
	scopeEmailOTP  = "verify_email"
)

func failureKey(scope, id string) string { return authKeyPrefix + "fail:" + scope + ":" + id }
func sendKey(scope, id string) string    { return authKeyPrefix + "send:" + scope + ":" + id }

// checkLocked returns ErrTooManyAttempts when the identity has already reached
// the failure limit for this scope. Redis errors fail open (logged) so a Redis
// outage does not take authentication down.
func (s *AuthService) checkLocked(ctx context.Context, scope, id string) error {
	if s.rdb == nil || id == "" {
		return nil
	}
	n, err := s.rdb.Get(ctx, failureKey(scope, id)).Int()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			s.logger.Warn("auth lockout check failed (fail-open)", zap.String("scope", scope), zap.Error(err))
		}
		return nil
	}
	if n >= maxAuthFailures {
		return ErrTooManyAttempts
	}
	return nil
}

// recordFailure increments the failure counter (fixed window starting at the
// first failure). It returns ErrTooManyAttempts when this failure reaches the limit.
func (s *AuthService) recordFailure(ctx context.Context, scope, id string) error {
	if s.rdb == nil || id == "" {
		return nil
	}
	key := failureKey(scope, id)
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		s.logger.Warn("auth failure counter failed", zap.String("scope", scope), zap.Error(err))
		return nil
	}
	if n == 1 {
		_ = s.rdb.Expire(ctx, key, authFailureWindow).Err()
	}
	if n >= maxAuthFailures {
		s.logger.Warn("auth lockout triggered", zap.String("scope", scope), zap.String("identity", id))
		return ErrTooManyAttempts
	}
	return nil
}

// clearFailures resets the counter after a successful check.
func (s *AuthService) clearFailures(ctx context.Context, scope, id string) {
	if s.rdb == nil || id == "" {
		return
	}
	_ = s.rdb.Del(ctx, failureKey(scope, id)).Err()
}

// allowSend enforces maxOTPSends per otpSendWindow for one identity.
func (s *AuthService) allowSend(ctx context.Context, scope, id string) error {
	if s.rdb == nil || id == "" {
		return nil
	}
	key := sendKey(scope, id)
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		s.logger.Warn("otp send limiter failed (fail-open)", zap.String("scope", scope), zap.Error(err))
		return nil
	}
	if n == 1 {
		_ = s.rdb.Expire(ctx, key, otpSendWindow).Err()
	}
	if n > maxOTPSends {
		return ErrTooManyAttempts
	}
	return nil
}

// secureOTP returns a uniformly random 6-digit code from crypto/rand.
// math/rand must never be used for secrets: its output is predictable.
func secureOTP() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate otp: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// ─── Step-up authentication ───────────────────────────────────────────────────

// Step-up errors (handlers map them to explicit 4xx responses).
var (
	ErrStepUpRequired    = errors.New("step-up authentication required: send your PIN")
	ErrStepUpTOTPMissing = errors.New("two-factor authentication is enabled: send your authenticator code")
	ErrStepUpUnavailable = errors.New("no PIN or 2FA on this account: set a PIN before exporting keys")
)

const scopeStepUp = "step_up"

// VerifyStepUp re-authenticates an already signed-in user before a highly
// sensitive action (seed phrase / private key export). A stolen session token
// alone is therefore not enough to drain the wallet: the caller must also know
// the PIN, and the TOTP code when 2FA is enabled. Shares the brute-force
// lockout (5 failures / 15 min).
func (s *AuthService) VerifyStepUp(ctx context.Context, userID uuid.UUID, pin, totpCode string) error {
	q := db.New(s.pool)
	user, err := q.GetUserByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}
	sec, secErr := q.GetUserSecurity(ctx, userID)
	totpOn := secErr == nil && sec.TOTPEnabled && sec.TOTPSecret != nil

	if user.PinHash == nil && !totpOn {
		return ErrStepUpUnavailable
	}
	if err := s.checkLocked(ctx, scopeStepUp, userID.String()); err != nil {
		return err
	}

	if user.PinHash != nil {
		if pin == "" {
			return ErrStepUpRequired
		}
		if !hash.CheckPIN(*user.PinHash, pin) {
			if lockErr := s.recordFailure(ctx, scopeStepUp, userID.String()); lockErr != nil {
				return lockErr
			}
			return ErrInvalidPIN
		}
	}
	if totpOn {
		if totpCode == "" {
			return ErrStepUpTOTPMissing
		}
		if !totp.Validate(totpCode, *sec.TOTPSecret) {
			if lockErr := s.recordFailure(ctx, scopeStepUp, userID.String()); lockErr != nil {
				return lockErr
			}
			return ErrInvalidOTP
		}
	}
	s.clearFailures(ctx, scopeStepUp, userID.String())
	return nil
}
