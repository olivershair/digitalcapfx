package token

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// pending2FASubject marks a short-lived token issued after a correct
// phone+PIN when 2FA is required. It stands in for a server-side "pending"
// record, so the 2FA login handoff needs no Redis/DB state.
const pending2FASubject = "2fa_pending"

// Token subjects. Every token is signed with the same JWT secret, so the
// subject is what tells them apart: only an "access" token may authenticate an
// API request, only a "refresh" token may be exchanged for a new pair, and a
// "2fa_pending" token is only accepted by ParsePending2FA.
const (
	SubjectAccess  = "access"
	SubjectRefresh = "refresh"
	// SubjectPhoneVerified marks a short-lived proof that the holder entered the
	// SMS code sent to a phone number. Required by POST /auth/register.
	SubjectPhoneVerified = "phone_verified"
)

// SignPhoneVerified issues a short-lived proof that `phone` was verified by
// SMS code. It carries no user id and is refused by the API auth middleware.
func SignPhoneVerified(phone, secret string, ttl time.Duration) (string, error) {
	return sign(uuid.Nil, phone, "", "", secret, ttl, SubjectPhoneVerified)
}

// ParsePhoneVerified validates a phone-verification proof and returns the phone.
func ParsePhoneVerified(tokenStr, secret string) (string, error) {
	claims, err := ParseWithSubject(tokenStr, secret, SubjectPhoneVerified)
	if err != nil {
		return "", err
	}
	if claims.Phone == "" {
		return "", fmt.Errorf("phone verification token has no phone")
	}
	return claims.Phone, nil
}

// SignPending2FA issues a short-lived token carrying just the user id, accepted
// only by ParsePending2FA. Replaces the old Redis "2fa:pending:" ref.
func SignPending2FA(userID uuid.UUID, secret string, ttl time.Duration) (string, error) {
	return sign(userID, "", "", "", secret, ttl, pending2FASubject)
}

// ParsePending2FA validates a pending-2FA token (signature + expiry + subject)
// and returns the user id.
func ParsePending2FA(tokenStr, secret string) (uuid.UUID, error) {
	claims, err := ParseWithSubject(tokenStr, secret, pending2FASubject)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.Parse(claims.UserID)
}

type Claims struct {
	UserID    string `json:"user_id"`
	Phone     string `json:"phone"`
	SessionID string `json:"session_id,omitempty"`
	Role      string `json:"role,omitempty"` // "user" | "admin"
	jwt.RegisteredClaims
}

type Pair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	SessionID    string `json:"session_id"`
	// AccountType ("individual"|"business") is surfaced on auth responses so the
	// frontend can branch on tier without a separate profile fetch. Omitted when
	// not populated (e.g. refresh where it isn't looked up).
	AccountType string `json:"account_type,omitempty"`
}

func NewPair(userID uuid.UUID, phone, sessionID, role, secret string, accessExp, refreshExp time.Duration) (*Pair, error) {
	access, err := sign(userID, phone, sessionID, role, secret, accessExp, SubjectAccess)
	if err != nil {
		return nil, err
	}
	refresh, err := sign(userID, phone, sessionID, role, secret, refreshExp, SubjectRefresh)
	if err != nil {
		return nil, err
	}
	return &Pair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(accessExp.Seconds()),
		SessionID:    sessionID,
	}, nil
}

// Parse validates the signature (HS256 only) and expiry of any token issued by
// this package. It does NOT check the subject — callers that need a specific
// token kind must use ParseAccess / ParseRefresh / ParsePending2FA.
func Parse(tokenStr, secret string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	return claims, err
}

// ParseWithSubject is Parse plus a check that the token carries the expected
// subject, so one kind of token can never be replayed as another.
func ParseWithSubject(tokenStr, secret, subject string) (*Claims, error) {
	claims, err := Parse(tokenStr, secret)
	if err != nil {
		return nil, err
	}
	if claims.Subject != subject {
		return nil, fmt.Errorf("token subject %q is not %q", claims.Subject, subject)
	}
	return claims, nil
}

// ParseAccess accepts only access tokens.
func ParseAccess(tokenStr, secret string) (*Claims, error) {
	return ParseWithSubject(tokenStr, secret, SubjectAccess)
}

// ParseRefresh accepts only refresh tokens.
func ParseRefresh(tokenStr, secret string) (*Claims, error) {
	return ParseWithSubject(tokenStr, secret, SubjectRefresh)
}

func sign(userID uuid.UUID, phone, sessionID, role, secret string, exp time.Duration, subject string) (string, error) {
	claims := Claims{
		UserID:    userID.String(),
		Phone:     phone,
		SessionID: sessionID,
		Role:      role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(exp)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}
