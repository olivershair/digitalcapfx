package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rachfinance/digitalfx/internal/pkg/response"
)

type contextKey string

const (
	ContextKeyUserID    contextKey = "user_id"
	ContextKeyUserPhone contextKey = "user_phone"
	ContextKeySessionID contextKey = "session_id"
	ContextKeyRole      contextKey = "role"
)

type Claims struct {
	UserID    string `json:"user_id"`
	Phone     string `json:"phone"`
	SessionID string `json:"session_id,omitempty"`
	Role      string `json:"role,omitempty"`
	jwt.RegisteredClaims
}

// tokenSubjectAccess mirrors token.SubjectAccess (the subject set on access tokens).
const tokenSubjectAccess = "access"

func Auth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				response.Unauthorized(w, "missing or invalid authorization header")
				return
			}

			tokenStr := strings.TrimPrefix(header, "Bearer ")

			claims := &Claims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
				return []byte(secret), nil
			}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
			if err != nil || !token.Valid {
				response.Unauthorized(w, "invalid or expired token")
				return
			}

			// SECURITY: refresh tokens and pending-2FA tokens are signed with the
			// same secret. Only an access token may authenticate a request —
			// otherwise a pending-2FA token (issued after the PIN alone) would
			// bypass the TOTP step, and a 7-day refresh token would work as a
			// long-lived access token.
			if claims.Subject != tokenSubjectAccess {
				response.Unauthorized(w, "invalid or expired token")
				return
			}

			userID, err := uuid.Parse(claims.UserID)
			if err != nil {
				response.Unauthorized(w, "invalid token claims")
				return
			}

			role := claims.Role
			if role == "" {
				role = "user"
			}

			ctx := context.WithValue(r.Context(), ContextKeyUserID, userID)
			ctx = context.WithValue(ctx, ContextKeyUserPhone, claims.Phone)
			ctx = context.WithValue(ctx, ContextKeySessionID, claims.SessionID)
			ctx = context.WithValue(ctx, ContextKeyRole, role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ActiveSession rejects a request whose session has been revoked or expired, or
// whose account has been disabled. Must be used after Auth.
//
// SECURITY: a JWT stays cryptographically valid until it expires, so without
// this check logout, "disconnect device", PIN reset (which revokes every
// session) and admin "disable user" would not cut off a stolen or still-open
// access token. One indexed lookup per request.
func ActiveSession(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFromContext(r.Context())
			if !ok {
				response.Unauthorized(w, "unauthorized")
				return
			}
			sid, ok := SessionIDFromContext(r.Context())
			if !ok {
				response.Unauthorized(w, "session missing — please sign in again")
				return
			}
			sessionID, err := uuid.Parse(sid)
			if err != nil {
				response.Unauthorized(w, "invalid session — please sign in again")
				return
			}

			var userActive, sessionActive bool
			err = pool.QueryRow(r.Context(), `
				SELECT u.is_active,
				       COALESCE(s.is_active AND s.expires_at > NOW(), false)
				FROM users u
				LEFT JOIN user_sessions s ON s.id = $2 AND s.user_id = u.id
				WHERE u.id = $1`, userID, sessionID,
			).Scan(&userActive, &sessionActive)
			if err != nil {
				response.Unauthorized(w, "session not found — please sign in again")
				return
			}
			if !userActive {
				response.Forbidden(w, "account is inactive")
				return
			}
			if !sessionActive {
				response.Unauthorized(w, "session revoked or expired — please sign in again")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AdminOnly rejects requests from non-admin users. Must be used after Auth.
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value(ContextKeyRole).(string)
		if role != "admin" {
			response.Forbidden(w, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// KYCRequired blocks access unless the user's KYC status is "approved".
// pool is used to do a lightweight status check. Must be used after Auth.
func KYCRequired(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFromContext(r.Context())
			if !ok {
				response.Unauthorized(w, "unauthorized")
				return
			}

			var kycStatus string
			err := pool.QueryRow(r.Context(),
				`SELECT kyc_status FROM users WHERE id = $1`, userID,
			).Scan(&kycStatus)
			if err != nil {
				response.InternalError(w)
				return
			}

			if kycStatus != "approved" {
				response.JSON(w, http.StatusForbidden, response.Envelope{
					Success: false,
					Error: &response.Error{
						Code:    "KYC_REQUIRED",
						Message: "your identity verification must be approved before using this feature",
					},
					Data: map[string]string{"kyc_status": kycStatus},
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(ContextKeyUserID).(uuid.UUID)
	return id, ok
}

func UserPhoneFromContext(ctx context.Context) (string, bool) {
	phone, ok := ctx.Value(ContextKeyUserPhone).(string)
	return phone, ok
}

func SessionIDFromContext(ctx context.Context) (string, bool) {
	sid, ok := ctx.Value(ContextKeySessionID).(string)
	return sid, ok && sid != ""
}

func RoleFromContext(ctx context.Context) string {
	role, _ := ctx.Value(ContextKeyRole).(string)
	if role == "" {
		return "user"
	}
	return role
}

const (
	ContextKeyBusinessUserID contextKey = "business_user_id"
	ContextKeyMerchantRole   contextKey = "merchant_role"
)

func LoadMerchantContext(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserIDFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			var businessUserID uuid.UUID
			var role string
			err := pool.QueryRow(r.Context(),
				`SELECT business_user_id, role FROM merchant_staff WHERE staff_user_id = $1 AND status = 'active' LIMIT 1`,
				userID,
			).Scan(&businessUserID, &role)

			ctx := r.Context()
			if err == nil {
				ctx = context.WithValue(ctx, ContextKeyBusinessUserID, businessUserID)
				ctx = context.WithValue(ctx, ContextKeyMerchantRole, role)
			} else {
				ctx = context.WithValue(ctx, ContextKeyBusinessUserID, userID)
				ctx = context.WithValue(ctx, ContextKeyMerchantRole, "owner")
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func BusinessUserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(ContextKeyBusinessUserID).(uuid.UUID)
	return id, ok
}

func MerchantRoleFromContext(ctx context.Context) string {
	role, _ := ctx.Value(ContextKeyMerchantRole).(string)
	if role == "" {
		return "owner"
	}
	return role
}

func RequireMerchantRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := MerchantRoleFromContext(r.Context())
			if role == "owner" {
				next.ServeHTTP(w, r)
				return
			}
			for _, allowed := range allowedRoles {
				if role == allowed {
					next.ServeHTTP(w, r)
					return
				}
			}
			response.Forbidden(w, "insufficient merchant role")
		})
	}
}
