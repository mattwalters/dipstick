package types_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mattwalters/dipstick/internal/types"
)

func TestTypes_RateWindowDuration(t *testing.T) {
	var zeroSec int64 = 0
	var sixtySec int64 = 60
	rwNil := types.RateWindow{Label: "nil"}
	rwZero := types.RateWindow{Label: "zero", WindowDurationSeconds: &zeroSec}
	rwSixty := types.RateWindow{Label: "sixty", WindowDurationSeconds: &sixtySec}

	if rwNil.Duration() != 0 {
		t.Errorf("expected 0 duration for nil seconds, got %v", rwNil.Duration())
	}
	if rwZero.Duration() != 0 {
		t.Errorf("expected 0 duration for 0 seconds, got %v", rwZero.Duration())
	}
	if rwSixty.Duration() != 60*time.Second {
		t.Errorf("expected 60s duration, got %v", rwSixty.Duration())
	}
}

func TestTypes_SourceTierString(t *testing.T) {
	tests := []struct {
		tier     types.SourceTier
		expected string
	}{
		{types.TierAPI, "api"},
		{types.TierLocalState, "local_state"},
		{types.TierLocalRPC, "local_rpc"},
		{types.TierTranscripts, "transcripts"},
		{types.TierCLIScrape, "cli_scrape"},
		{types.SourceTier(99), "tier_99"},
	}

	for _, tc := range tests {
		if tc.tier.String() != tc.expected {
			t.Errorf("expected %s, got %s", tc.expected, tc.tier.String())
		}
	}
}

func TestTypes_ReasonMethods(t *testing.T) {
	if !types.ReasonUpstreamError.Retryable() {
		t.Errorf("expected ReasonUpstreamError to be retryable")
	}
	if !types.ReasonTimeout.Retryable() {
		t.Errorf("expected ReasonTimeout to be retryable")
	}
	if types.ReasonParseFailed.Retryable() {
		t.Errorf("expected ReasonParseFailed not to be retryable")
	}

	if !errors.Is(types.ReasonParseFailed.Sentinel(), types.ErrParseFailed) {
		t.Errorf("expected Sentinel for ReasonParseFailed to be ErrParseFailed")
	}
	if types.Reason("unknown").Sentinel() != nil {
		t.Errorf("expected nil Sentinel for unknown Reason")
	}
}

func TestTypes_ReasonForError(t *testing.T) {
	if types.ReasonForError(nil) != "" {
		t.Errorf("expected empty string for nil error")
	}
	if types.ReasonForError(types.ErrParseFailed) != types.ReasonParseFailed {
		t.Errorf("expected ReasonParseFailed")
	}
	if types.ReasonForError(types.ErrUnsupportedVersion) != types.ReasonUnsupportedVersion {
		t.Errorf("expected ReasonUnsupportedVersion")
	}
	if types.ReasonForError(types.ErrCredentialExpired) != types.ReasonCredentialExpired {
		t.Errorf("expected ReasonCredentialExpired")
	}
	if types.ReasonForError(types.ErrNotAuthenticated) != types.ReasonNotAuthenticated {
		t.Errorf("expected ReasonNotAuthenticated")
	}
	if types.ReasonForError(types.ErrNotInstalled) != types.ReasonNotInstalled {
		t.Errorf("expected ReasonNotInstalled")
	}
	if types.ReasonForError(types.ErrNotSupported) != types.ReasonNotSupported {
		t.Errorf("expected ReasonNotSupported")
	}
	if types.ReasonForError(types.ErrTimeout) != types.ReasonTimeout {
		t.Errorf("expected ReasonTimeout")
	}
	if types.ReasonForError(types.ErrUpstreamError) != types.ReasonUpstreamError {
		t.Errorf("expected ReasonUpstreamError")
	}
	if types.ReasonForError(context.DeadlineExceeded) != types.ReasonTimeout {
		t.Errorf("expected ReasonTimeout for DeadlineExceeded")
	}
	if types.ReasonForError(errors.New("other unmapped error")) != "" {
		t.Errorf("expected empty reason for unmapped error")
	}
}

func TestTypes_ProviderErrorMatchingAndMasking(t *testing.T) {
	pe := types.ProviderError{
		Provider:  types.ProviderCodex,
		Reason:    types.ReasonParseFailed,
		Source:    types.SourceLocalState,
		Detail:    "Authorization: Bearer sk-secret-12345",
		Retryable: false,
	}

	if !errors.Is(pe, types.ErrParseFailed) {
		t.Errorf("expected pe to match ErrParseFailed via errors.Is")
	}
	if !errors.Is(&pe, types.ErrParseFailed) {
		t.Errorf("expected &pe to match ErrParseFailed via errors.Is")
	}

	data, err := json.Marshal(pe)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	detail := parsed["detail"].(string)
	if detail != "Authorization: [REDACTED]" {
		t.Errorf("expected redacted detail in JSON, got %q", detail)
	}

	errStr := pe.Error()
	if errStr != "codex (local_state): parse_failed: Authorization: [REDACTED]" {
		t.Errorf("unexpected Error() string: %q", errStr)
	}

	peNoSource := types.ProviderError{
		Provider: types.ProviderCodex,
		Reason:   types.ReasonNotInstalled,
		Detail:   "missing",
	}
	if peNoSource.Error() != "codex: not_installed: missing" {
		t.Errorf("unexpected Error() string: %q", peNoSource.Error())
	}
}

func TestTypes_SourcePolicyAllows(t *testing.T) {
	if (types.SourcePolicyDefault).Allows(nil) {
		t.Errorf("expected Allows(nil) to be false")
	}

	policyPin := types.PinTierPolicy(types.TierLocalState)
	if !policyPin.AllowsTierAndID(types.TierLocalState, types.SourceLocalState) {
		t.Errorf("expected pinned policy to allow TierLocalState")
	}
	if policyPin.AllowsTierAndID(types.TierAPI, types.SourceOAuthAPI) {
		t.Errorf("expected pinned policy to reject TierAPI")
	}

	policyFloor := types.TierFloorPolicy(types.TierLocalRPC)
	if policyFloor.AllowsTierAndID(types.TierLocalState, types.SourceLocalState) {
		t.Errorf("expected floor 3 policy to reject tier 2")
	}
	if !policyFloor.AllowsTierAndID(types.TierLocalRPC, types.SourceAppServer) {
		t.Errorf("expected floor 3 policy to allow tier 3")
	}
	if !policyFloor.AllowsTierAndID(types.TierTranscripts, types.SourceTranscript) {
		t.Errorf("expected floor 3 policy to allow tier 4")
	}
}

func TestTypes_Ptr(t *testing.T) {
	val := 42
	ptr := types.Ptr(val)
	if ptr == nil || *ptr != 42 {
		t.Errorf("Ptr failed")
	}
}
