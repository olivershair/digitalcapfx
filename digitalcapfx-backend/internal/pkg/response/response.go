package response

import (
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Meta struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type Paginated struct {
	Success bool `json:"success"`
	Data    any  `json:"data"`
	Meta    Meta `json:"meta"`
}

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func OK(w http.ResponseWriter, data any) {
	JSON(w, http.StatusOK, Envelope{Success: true, Data: data})
}

func Created(w http.ResponseWriter, data any) {
	JSON(w, http.StatusCreated, Envelope{Success: true, Data: data})
}

func OKWithMessage(w http.ResponseWriter, message string, data any) {
	JSON(w, http.StatusOK, Envelope{Success: true, Message: message, Data: data})
}

func OKPaginated(w http.ResponseWriter, data any, meta Meta) {
	JSON(w, http.StatusOK, Paginated{Success: true, Data: data, Meta: meta})
}

func BadRequest(w http.ResponseWriter, code, message string) {
	JSON(w, http.StatusBadRequest, Envelope{
		Success: false,
		Error:   &Error{Code: code, Message: message},
	})
}

func Unauthorized(w http.ResponseWriter, message string) {
	JSON(w, http.StatusUnauthorized, Envelope{
		Success: false,
		Error:   &Error{Code: "UNAUTHORIZED", Message: message},
	})
}

func Forbidden(w http.ResponseWriter, message string) {
	JSON(w, http.StatusForbidden, Envelope{
		Success: false,
		Error:   &Error{Code: "FORBIDDEN", Message: message},
	})
}

func NotFound(w http.ResponseWriter, message string) {
	JSON(w, http.StatusNotFound, Envelope{
		Success: false,
		Error:   &Error{Code: "NOT_FOUND", Message: message},
	})
}

func Conflict(w http.ResponseWriter, code, message string) {
	JSON(w, http.StatusConflict, Envelope{
		Success: false,
		Error:   &Error{Code: code, Message: message},
	})
}

func InternalError(w http.ResponseWriter) {
	JSON(w, http.StatusInternalServerError, Envelope{
		Success: false,
		Error:   &Error{Code: "INTERNAL_ERROR", Message: "an unexpected error occurred"},
	})
}

func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func ServiceUnavailable(w http.ResponseWriter, message string) {
	JSON(w, http.StatusServiceUnavailable, Envelope{
		Success: false,
		Error:   &Error{Code: "SERVICE_UNAVAILABLE", Message: message},
	})
}

// TooManyRequests answers 429 — used when an identity is temporarily locked
// out after too many failed PIN/OTP attempts or too many codes requested.
func TooManyRequests(w http.ResponseWriter, message string) {
	JSON(w, http.StatusTooManyRequests, Envelope{
		Success: false,
		Error:   &Error{Code: "TOO_MANY_ATTEMPTS", Message: message},
	})
}

// WebhookSecretMissing answers 503 with an explicit error naming the server
// environment variable that must be configured. The webhook processed nothing;
// 503 makes the provider retry, so no delivery is lost once the secret is set.
// Only the variable NAME is returned — never a secret value.
func WebhookSecretMissing(w http.ResponseWriter, webhook, envVar string) {
	JSON(w, http.StatusServiceUnavailable, Envelope{
		Success: false,
		Error: &Error{
			Code: "WEBHOOK_SECRET_MISSING",
			Message: "the " + webhook + " webhook is disabled because the server secret " + envVar +
				" is not configured — set " + envVar + " in the backend environment (Secret Manager) and redeploy; " +
				"no delivery is processed until then",
		},
		Data: map[string]string{"webhook": webhook, "missing_secret": envVar},
	})
}

// WebhookSignatureInvalid answers 401 with an explicit error naming the header
// that was checked and the secret it is verified against.
func WebhookSignatureInvalid(w http.ResponseWriter, webhook, header, envVar string) {
	JSON(w, http.StatusUnauthorized, Envelope{
		Success: false,
		Error: &Error{
			Code: "WEBHOOK_SIGNATURE_INVALID",
			Message: "the " + webhook + " webhook signature is missing or invalid: header " + header +
				" must be the HMAC-SHA256 of the raw body keyed with " + envVar +
				" — check that the secret configured on the provider matches the server's " + envVar,
		},
		Data: map[string]string{"webhook": webhook, "signature_header": header, "secret": envVar},
	})
}
