package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/api/handlers"
	"github.com/rachfinance/digitalfx/internal/pkg/token"
)

// TestAuth_OnlyAccessTokensAccepted guards against the 2FA bypass: a
// pending-2FA token (issued after the PIN alone) and a refresh token are signed
// with the same secret but must never authenticate an API request.
func TestAuth_OnlyAccessTokensAccepted(t *testing.T) {
	h := buildTestRouter()
	uid := uuid.New()

	pending, err := token.SignPending2FA(uid, routeTestSecret, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := token.NewPair(uid, "+237612345678", uuid.NewString(), "user", routeTestSecret, time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	phoneProof, err := token.SignPhoneVerified("+237612345678", routeTestSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		tok  string
		want int
	}{
		"phone-verified proof": {phoneProof, http.StatusUnauthorized},
		"pending-2fa token":    {pending, http.StatusUnauthorized},
		"refresh token":        {pair.RefreshToken, http.StatusUnauthorized},
		"access token":         {pair.AccessToken, http.StatusOK},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := do(t, h, "GET", "/api/v1/profile", c.tok); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestToken_ParseRefreshRejectsOtherKinds(t *testing.T) {
	uid := uuid.New()
	pair, _ := token.NewPair(uid, "", uuid.NewString(), "user", routeTestSecret, time.Hour, time.Hour)
	pending, _ := token.SignPending2FA(uid, routeTestSecret, time.Minute)

	if _, err := token.ParseRefresh(pair.RefreshToken, routeTestSecret); err != nil {
		t.Errorf("refresh token rejected: %v", err)
	}
	for name, tok := range map[string]string{"access": pair.AccessToken, "pending": pending} {
		if _, err := token.ParseRefresh(tok, routeTestSecret); err == nil {
			t.Errorf("%s token accepted as refresh token", name)
		}
	}
	if _, err := token.ParsePending2FA(pair.AccessToken, routeTestSecret); err == nil {
		t.Error("access token accepted as pending-2FA token")
	}
}

// TestWebhooks_MissingSecretIsExplicit: with no secret configured, money-moving
// webhooks must refuse every delivery with an explicit 503 naming the variable.
func TestWebhooks_MissingSecretIsExplicit(t *testing.T) {
	cases := map[string]struct {
		h      http.HandlerFunc
		envVar string
	}{
		"payments": {handlers.NewPaymentsWebhookHandler(nil, "", zap.NewNop()).Receive, "PAYMENTS_WEBHOOK_SECRET"},
		"caas":     {handlers.NewCaasWebhookHandler(nil, "", zap.NewNop()).Receive, "CAAS_WEBHOOK_SECRET"},
	}
	body := `{"event":"wallet.deposit.confirmed","data":{"customer_id":"x","deposit_id":1,"address":"0xabc","amount":"1000","currency":"USDT","safe_to_credit":true}}`
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c.h(rec, httptest.NewRequest("POST", "/webhooks/"+name, strings.NewReader(body)))
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status %d, want 503", rec.Code)
			}
			out := rec.Body.String()
			if !strings.Contains(out, "WEBHOOK_SECRET_MISSING") || !strings.Contains(out, c.envVar) {
				t.Errorf("error is not explicit: %s", out)
			}
		})
	}
}

func TestToken_PhoneVerifiedProof(t *testing.T) {
	proof, err := token.SignPhoneVerified("+237690001122", routeTestSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	phone, err := token.ParsePhoneVerified(proof, routeTestSecret)
	if err != nil || phone != "+237690001122" {
		t.Fatalf("got %q, %v", phone, err)
	}
	if _, err := token.ParsePhoneVerified(proof, "other-secret"); err == nil {
		t.Error("proof accepted with the wrong secret")
	}
	pair, _ := token.NewPair(uuid.New(), "+237690001122", uuid.NewString(), "user", routeTestSecret, time.Hour, time.Hour)
	if _, err := token.ParsePhoneVerified(pair.AccessToken, routeTestSecret); err == nil {
		t.Error("an access token must not count as a phone-verification proof")
	}
	expired, _ := token.SignPhoneVerified("+237690001122", routeTestSecret, -time.Minute)
	if _, err := token.ParsePhoneVerified(expired, routeTestSecret); err == nil {
		t.Error("expired proof accepted")
	}
}
