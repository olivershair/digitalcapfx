package sms

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSend_ClassifiesBrevoErrors(t *testing.T) {
	cases := []struct {
		status            int
		body              string
		rejected, platfrm bool
	}{
		{400, `{"code":"invalid_parameter","message":"Invalid phone number"}`, true, false},
		{401, `{"code":"unauthorized","message":"Key not found"}`, false, true},
		{402, `{"code":"not_enough_credits","message":"Not enough credits"}`, false, true},
		{500, `oops`, false, false},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte(c.body))
		}))
		err := New("k", "DigitalFX").WithBaseURL(srv.URL).SendOTP(context.Background(), "+237690001122", "DigitalFX", "123456")
		srv.Close()
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("status %d: got %v, want *APIError", c.status, err)
		}
		if apiErr.RecipientRejected() != c.rejected || apiErr.PlatformProblem() != c.platfrm {
			t.Errorf("status %d: rejected=%v platform=%v", c.status, apiErr.RecipientRejected(), apiErr.PlatformProblem())
		}
	}
}

func TestSend_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "k" {
			t.Error("api-key header missing")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"reference":"r","messageId":1}`))
	}))
	defer srv.Close()
	if err := New("k", "DigitalFX").WithBaseURL(srv.URL).SendOTP(context.Background(), "+237690001122", "DigitalFX", "123456"); err != nil {
		t.Fatal(err)
	}
}
