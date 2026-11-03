package pve

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/lz-wang/pve-cli/v3/internal/output"
)

// configValueMatches compares a requested configuration value against the
// value read back from PVE. It returns (match, comparable). A comparison is
// only reported as a definite mismatch when both sides reduce to reliable
// scalars or PVE option strings; anything else is not comparable and must be
// treated as an unverified outcome instead of a false alarm.
func configValueMatches(requested string, current any) (match bool, comparable bool) {
	if current == nil {
		return false, false
	}
	switch typed := current.(type) {
	case string:
		return matchOptionOrScalar(requested, typed)
	case float64:
		return matchNumeric(requested, typed)
	case bool:
		want, err := parseConfigBool(requested)
		if err != nil {
			return false, false
		}
		return want == typed, true
	case int:
		return matchNumeric(requested, float64(typed))
	case int64:
		return matchNumeric(requested, float64(typed))
	default:
		return false, false
	}
}

func matchOptionOrScalar(requested, current string) (bool, bool) {
	if canonicalScalar(requested) == canonicalScalar(current) {
		return true, true
	}
	if !strings.Contains(requested, "=") || !strings.Contains(current, "=") {
		// Plain scalars that differ are a definite mismatch: PVE echoes
		// accepted scalar values verbatim (modulo whitespace/number form).
		return false, true
	}
	return matchOptionString(requested, current)
}

// matchOptionString compares PVE option strings ("name=eth0,bridge=vmbr0").
// The requested value matches when every requested attribute is present in
// the current value with the same normalized value; PVE appends generated
// attributes (hwaddr, digest) that the caller did not request.
func matchOptionString(requested, current string) (bool, bool) {
	requestedPairs := parseOptionPairs(requested)
	if len(requestedPairs) == 0 {
		return false, false
	}
	currentPairs := parseOptionPairs(current)
	for name, value := range requestedPairs {
		currentValue, ok := currentPairs[name]
		if !ok {
			return false, true
		}
		if canonicalScalar(value) != canonicalScalar(currentValue) {
			return false, true
		}
	}
	return true, true
}

func parseOptionPairs(value string) map[string]string {
	pairs := make(map[string]string)
	for part := range strings.SplitSeq(value, ",") {
		name, attr, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found || strings.TrimSpace(name) == "" {
			continue
		}
		pairs[strings.TrimSpace(name)] = strings.TrimSpace(attr)
	}
	return pairs
}

func matchNumeric(requested string, current float64) (bool, bool) {
	want, err := strconv.ParseFloat(strings.TrimSpace(requested), 64)
	if err != nil {
		return false, false
	}
	return want == current, true
}

func parseConfigBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("not a boolean: %q", value)
	}
}

// canonicalScalar trims surrounding whitespace and collapses numeric text to
// its minimal form so "0", "0.0" and 0 compare equal.
func canonicalScalar(value string) string {
	value = strings.TrimSpace(value)
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return strconv.FormatFloat(number, 'f', -1, 64)
	}
	return value
}

// verifyOutcome is the result of the post-write state check.
//
// verified is a tri-state: nil means the outcome could not be verified
// (missing capability, failed read, or values that cannot be compared
// reliably); false means at least one requested value definitely differs.
type verifyOutcome struct {
	verified     *bool
	mismatches   []string
	unverifiable bool
	readErr      error
}

// verifyConfigWrite re-reads the guest configuration and compares every
// requested value. It performs exactly one read and never retries. Keys the
// backend no longer reports, or values that cannot be compared reliably,
// leave the outcome unverified rather than mismatched.
func (s *GuestService) verifyConfigWrite(ctx context.Context, node string, vmid int, values map[string]string) verifyOutcome {
	backend, ok := s.backend.(NativeConfigBackend)
	if !ok {
		return verifyOutcome{unverifiable: true}
	}
	config, err := backend.GuestConfig(ctx, s.kind, node, vmid)
	if err != nil {
		return verifyOutcome{readErr: ClassifyError(err)}
	}

	anyUnverifiable := false
	var mismatches []string
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if sensitiveConfigKey(name) {
			// Credential values are transformed on store (hashed), so the
			// submitted plaintext never compares; they are unverifiable and
			// never echoed.
			anyUnverifiable = true
			continue
		}
		current, present := config[name]
		if !present {
			anyUnverifiable = true
			continue
		}
		match, comparable := configValueMatches(values[name], current)
		if !comparable {
			anyUnverifiable = true
			continue
		}
		if !match {
			mismatches = append(mismatches, mismatchLine(name, values[name], current))
		}
	}
	if len(mismatches) > 0 {
		verified := false
		return verifyOutcome{verified: &verified, mismatches: mismatches}
	}
	if anyUnverifiable {
		return verifyOutcome{unverifiable: true}
	}
	verified := true
	return verifyOutcome{verified: &verified}
}

// mismatchLine renders one mismatch detail. Values of sensitive keys are
// never echoed.
func mismatchLine(name, requested string, current any) string {
	if sensitiveConfigKey(name) {
		return fmt.Sprintf("%s: value mismatch (redacted)", name)
	}
	return fmt.Sprintf("%s: requested %q, current %q", name, requested, configValueString(current))
}

func configValueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

// applyVerification folds a verification outcome into a mutation result.
//
// Decision table (from the v3 reliability review):
//   - read shows every requested value in effect: status succeeded,
//     verified=true; an earlier API/wait anomaly is preserved in api_error
//     instead of masking a non-clean operation.
//   - read succeeds but a value definitely differs: status failed,
//     verified=false with mismatch details.
//   - read fails after a submit/wait anomaly: status unknown, the prior
//     error is returned. A cleanly accepted write keeps its status with
//     verified omitted.
//   - outcome unverifiable: prior semantics are unchanged.
//
// Mutations are never retried from here.
func applyVerification(result *output.OperationResult, priorErr error, outcome verifyOutcome) error {
	switch {
	case outcome.readErr != nil:
		if priorErr != nil {
			if result.Status != "failed" {
				result.Status = "unknown"
			}
			return priorErr
		}
		return nil
	case outcome.verified != nil && *outcome.verified:
		result.Status = "succeeded"
		result.Verified = outcome.verified
		if priorErr != nil {
			result.APIError = priorErr.Error()
		}
		return nil
	case outcome.verified != nil:
		result.Verified = outcome.verified
		result.Status = "failed"
		if len(outcome.mismatches) > 0 {
			result.Error = strings.Join(outcome.mismatches, "; ")
		}
		if priorErr != nil {
			result.APIError = priorErr.Error()
			return priorErr
		}
		return fmt.Errorf("%w: configuration verification mismatch: %s", ErrConflict, strings.Join(outcome.mismatches, "; "))
	default:
		return priorErr
	}
}
