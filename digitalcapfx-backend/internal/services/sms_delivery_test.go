package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/pkg/sms"
)

func TestUnsupportedCallingCode(t *testing.T) {
	codes := []string{"1", "1876", "91"}
	cases := map[string]string{
		"+14155550123":  "1",
		"+18765550123":  "1876", // longest match wins
		"+919876543210": "91",
		"+237690001122": "",
		"0690001122":    "", // not E.164: unknown country, never refused
	}
	for phone, want := range cases {
		got, ok := unsupportedCallingCode(phone, codes)
		if got != want || ok != (want != "") {
			t.Errorf("%s → %q,%v want %q", phone, got, ok, want)
		}
	}
}

func TestSendOTPSMS_Classification(t *testing.T) {
	for status, want := range map[int]string{400: "dest", 402: "unavailable", 503: "unavailable"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"code":"x","message":"y"}`))
		}))
		s := &AuthService{cfg: &config.Config{}, logger: zap.NewNop(),
			smsClient: sms.New("k", "DigitalFX").WithBaseURL(srv.URL)}
		err := s.sendOTPSMS(context.Background(), "+237690001122", "123456")
		srv.Close()
		var dest *SMSDestinationError
		switch want {
		case "dest":
			if !errors.As(err, &dest) || dest.Reason != SMSReasonRejected {
				t.Errorf("status %d: got %v, want number_rejected", status, err)
			}
		case "unavailable":
			if !errors.Is(err, ErrSMSDelivery) || errors.As(err, &dest) {
				t.Errorf("status %d: got %v, want ErrSMSDelivery", status, err)
			}
		}
	}
	// No SMS client configured → unavailable (never "your country").
	s := &AuthService{cfg: &config.Config{}, logger: zap.NewNop()}
	if err := s.sendOTPSMS(context.Background(), "+237690001122", "1"); !errors.Is(err, ErrSMSDelivery) {
		t.Errorf("nil client: got %v", err)
	}
}

func TestCheckSMSDestination(t *testing.T) {
	cfg := &config.Config{}
	cfg.Brevo.SMSUnsupportedCallingCodes = []string{"1"}
	s := &AuthService{cfg: cfg}
	var dest *SMSDestinationError
	if err := s.checkSMSDestination("+14155550123"); !errors.As(err, &dest) || dest.Reason != SMSReasonCountryUnsupported || dest.CallingCode != "1" {
		t.Errorf("got %v", err)
	}
	if err := s.checkSMSDestination("+237690001122"); err != nil {
		t.Errorf("Cameroon refused: %v", err)
	}
}
