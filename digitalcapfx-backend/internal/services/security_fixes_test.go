package services

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/config"
)

func TestObjectOwnedBy(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	cases := map[string]bool{
		"kyc/" + me.String() + "/abc-id.jpg":                  true,
		"avatar/" + me.String() + "/abc-me.png":               true,
		"kyc/" + other.String() + "/abc-id.jpg":               false, // another user's KYC doc
		"kyc/" + me.String() + "/../" + other.String() + "/x": false,
		"/kyc/" + me.String() + "/abc":                        false,
		"evil/" + me.String() + "/abc":                        false,
		"kyc/" + me.String() + "/":                            false,
		"kyc/" + me.String():                                  false,
		"":                                                    false,
	}
	for obj, want := range cases {
		if got := objectOwnedBy(obj, me); got != want {
			t.Errorf("objectOwnedBy(%q) = %v, want %v", obj, got, want)
		}
	}
}

func TestSecureOTP(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		c, err := secureOTP()
		if err != nil {
			t.Fatal(err)
		}
		if len(c) != 6 {
			t.Fatalf("otp %q is not 6 digits", c)
		}
		for _, r := range c {
			if r < '0' || r > '9' {
				t.Fatalf("otp %q has a non-digit", c)
			}
		}
		seen[c] = true
	}
	if len(seen) < 190 {
		t.Errorf("only %d distinct codes out of 200", len(seen))
	}
}

func TestVerifyMetaMapWebhook(t *testing.T) {
	body := []byte(`{"eventName":"verification_completed","resource":"https://api.getmati.com/v2/verifications/abc"}`)

	noSecret := &KYCService{cfg: &config.Config{}, logger: zap.NewNop()}
	if err := noSecret.VerifyMetaMapWebhook(body, http.Header{}); !errors.Is(err, ErrWebhookNotConfigured) {
		t.Fatalf("no secret: got %v, want ErrWebhookNotConfigured (fail closed)", err)
	}

	cfg := &config.Config{}
	cfg.MetaMap.WebhookSecret = "mm-secret"
	s := &KYCService{cfg: cfg, logger: zap.NewNop()}
	mac := hmac.New(sha256.New, []byte("mm-secret"))
	mac.Write(body)
	good := hex.EncodeToString(mac.Sum(nil))

	h := http.Header{}
	h.Set("X-Signature", good)
	if err := s.VerifyMetaMapWebhook(body, h); err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
	h.Set("X-Signature", "deadbeef")
	if err := s.VerifyMetaMapWebhook(body, h); !errors.Is(err, ErrMetaMapWebhookSignature) {
		t.Errorf("bad signature: got %v", err)
	}
	if err := s.VerifyMetaMapWebhook(body, http.Header{}); !errors.Is(err, ErrMetaMapWebhookSignature) {
		t.Errorf("missing signature: got %v", err)
	}
}
