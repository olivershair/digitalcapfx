package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/rachfinance/digitalfx/internal/config"
	"github.com/rachfinance/digitalfx/internal/kyc"
)

func docKeys(docs []DocumentSpec) map[string]DocumentSpec {
	m := map[string]DocumentSpec{}
	for _, d := range docs {
		m[d.Key] = d
	}
	return m
}

func TestManualRequirements_Individual(t *testing.T) {
	req := BuildIntakeRequirements("individual", IntakeOptions{Manual: true})
	if req.VerificationMode != VerificationModeManual {
		t.Fatalf("mode = %q", req.VerificationMode)
	}
	docs := docKeys(req.Documents)
	for _, k := range []string{DocIDDocument, DocSelfieWithID} {
		if d, ok := docs[k]; !ok || !d.Required {
			t.Errorf("%s must be a required document in manual mode", k)
		}
	}
	if d, ok := docs[DocIDDocumentBack]; !ok || d.Required {
		t.Errorf("id_document_back must be present and optional")
	}

	req = BuildIntakeRequirements("individual", IntakeOptions{})
	if req.VerificationMode != VerificationModeProvider {
		t.Fatalf("provider mode = %q", req.VerificationMode)
	}
	if _, ok := docKeys(req.Documents)[DocSelfieWithID]; ok {
		t.Error("provider mode must not ask for a selfie upload")
	}
}

func TestManualRequirements_BusinessReplacesLiveness(t *testing.T) {
	docs := docKeys(BuildIntakeRequirements("business", IntakeOptions{Manual: true, NeedsNRE: true}).Documents)
	if _, ok := docs[DocIDVLiveness]; ok {
		t.Error("manual mode must not require the vendor liveness check")
	}
	if d, ok := docs[DocSelfieWithID]; !ok || !d.Required {
		t.Error("manual business KYB must require a director selfie with ID")
	}
	if _, ok := docKeys(BuildIntakeRequirements("business", IntakeOptions{}).Documents)[DocIDVLiveness]; !ok {
		t.Error("provider mode must keep the liveness check")
	}
}

func TestRequiredDocsFor_ImporterRule(t *testing.T) {
	req := BuildIntakeRequirements("business", IntakeOptions{Manual: true, IsImporter: false})
	for _, d := range requiredDocsFor(req, false) {
		if d.Key == DocProofOfImports {
			t.Error("proof of imports must not be required for a non-importer")
		}
	}
	req = BuildIntakeRequirements("business", IntakeOptions{Manual: true, IsImporter: true})
	found := false
	for _, d := range requiredDocsFor(req, true) {
		found = found || d.Key == DocProofOfImports
	}
	if !found {
		t.Error("proof of imports must be required for an importer")
	}
}

func TestDocumentReferenceOwnership(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	s := &UploadService{} // no store: bucket not enforced
	obj := "kyc/" + me.String() + "/abc-id.jpg"
	cases := map[string]bool{
		obj:                  true,
		"gs://bucket/" + obj: true,
		"https://storage.googleapis.com/bucket/" + obj + "?X-Goog-Signature=x": true,
		"https://bucket.storage.googleapis.com/" + obj:                         true,
		"kyc/" + other.String() + "/abc-id.jpg":                                false,
		"https://evil.example.com/bucket/" + obj:                               false,
		"ftp://x/" + obj:                                                       false,
		"":                                                                     false,
	}
	for ref, want := range cases {
		if _, got := s.OwnedObject(ref, me); got != want {
			t.Errorf("OwnedObject(%q) = %v, want %v", ref, got, want)
		}
	}
}

func TestManualModeConfig(t *testing.T) {
	for v, want := range map[string]bool{"manual": true, " MANUAL ": true, "didit": false, "": false} {
		c := &config.Config{KYCProvider: v}
		if got := (&KYCService{cfg: c}).ManualMode(); got != want {
			t.Errorf("KYC_PROVIDER=%q → ManualMode %v, want %v", v, got, want)
		}
	}
}

func TestManualProviderRefusesWebhooks(t *testing.T) {
	_, err := kyc.NewManualProvider().HandleWebhook(context.Background(), []byte(`{}`), nil)
	if !errors.Is(err, kyc.ErrManualModeNoWebhooks) {
		t.Fatalf("got %v", err)
	}
}

func TestKYCDocumentsMissingError(t *testing.T) {
	err := error(&KYCDocumentsMissingError{Missing: []DocumentSpec{{Key: DocSelfieWithID, Label: "Selfie holding your ID"}}})
	if !errors.Is(err, ErrKYCDocumentsMissing) {
		t.Error("must unwrap to ErrKYCDocumentsMissing")
	}
	if err.Error() == "" {
		t.Error("empty message")
	}
}
