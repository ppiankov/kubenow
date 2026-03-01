package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ppiankov/kubenow/internal/policy"
)

var validatePolicyCmd = &cobra.Command{
	Use:   "validate-policy",
	Short: "Validate an admin policy file",
	Long: `Validate a kubenow admin policy file for correctness.

Checks:
  - YAML syntax
  - apiVersion and kind
  - Field value ranges (delta percents, durations, safety ratings)
  - Required fields when apply is enabled (audit backend + path)
  - Audit path writability (when --check-paths is set)

Examples:
  # Validate the default policy location
  kubenow pro-monitor validate-policy

  # Validate a specific file
  kubenow pro-monitor validate-policy --policy /path/to/policy.yaml

  # Also check that audit paths are writable
  kubenow pro-monitor validate-policy --check-paths`,
	RunE: runValidatePolicy,
}

var checkPaths bool

func init() {
	proMonitorCmd.AddCommand(validatePolicyCmd)
	validatePolicyCmd.Flags().BoolVar(&checkPaths, "check-paths", false, "verify audit path exists and is writable")
}

func runValidatePolicy(_ *cobra.Command, _ []string) error {
	result := policy.Load(policyPath)

	if result.Absent {
		stderrf("No policy file found at %s\n", result.Path)
		stderrf("Pro-monitor will operate in observe-only mode (no apply).\n")
		stderrf("\nTo create a policy file, see: examples/policy.yaml\n")
		return nil
	}

	if result.ErrorMsg != "" {
		return fmt.Errorf("policy file %s: %s", result.Path, result.ErrorMsg)
	}

	stdoutf("Policy file: %s\n", result.Path)

	vr := policy.Validate(result.Policy)
	if !vr.Valid {
		stdoutf("Validation: FAILED\n\n")
		for _, e := range vr.Errors {
			stdoutf("  ✗ %s\n", e.String())
		}
		return fmt.Errorf("policy validation failed with %d error(s)", len(vr.Errors))
	}

	stdoutf("Validation: OK\n")

	// Summary
	p := result.Policy
	stdoutf("\nPolicy summary:\n")
	stdoutf("  Global enabled:      %v\n", p.Global.Enabled)
	stdoutf("  Apply enabled:       %v\n", p.Apply.Enabled)
	stdoutf("  Audit backend:       %s\n", p.Audit.Backend)
	stdoutf("  Denied namespaces:   %v\n", p.Namespaces.Deny)
	stdoutf("  Max request delta:   %d%%\n", p.Apply.MaxRequestDeltaPct)
	stdoutf("  Max limit delta:     %d%%\n", p.Apply.MaxLimitDeltaPct)
	stdoutf("  Min safety rating:   %s\n", p.Apply.MinSafetyRating)
	stdoutf("  Rate limit:          %d applies/hour\n", p.RateLimits.MaxAppliesPerHour)

	if checkPaths && p.Audit.Path != "" {
		stdoutf("\nPath checks:\n")
		if err := policy.CheckAuditPath(p.Audit.Path); err != nil {
			stdoutf("  ✗ audit.path: %v\n", err)
			return fmt.Errorf("path check failed")
		}
		stdoutf("  ✓ audit.path: %s (writable)\n", p.Audit.Path)
	}

	return nil
}
