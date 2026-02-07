package cli

import (
	"fmt"
	"os"

	"github.com/ppiankov/kubenow/internal/policy"
	"github.com/spf13/cobra"
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

func runValidatePolicy(cmd *cobra.Command, args []string) error {
	result := policy.Load(policyPath)

	if result.Absent {
		fmt.Fprintf(os.Stderr, "No policy file found at %s\n", result.Path)
		fmt.Fprintf(os.Stderr, "Pro-monitor will operate in observe-only mode (no apply).\n")
		fmt.Fprintf(os.Stderr, "\nTo create a policy file, see: examples/policy.yaml\n")
		return nil
	}

	if result.ErrorMsg != "" {
		return fmt.Errorf("policy file %s: %s", result.Path, result.ErrorMsg)
	}

	fmt.Fprintf(os.Stdout, "Policy file: %s\n", result.Path)

	vr := policy.Validate(result.Policy)
	if !vr.Valid {
		fmt.Fprintf(os.Stdout, "Validation: FAILED\n\n")
		for _, e := range vr.Errors {
			fmt.Fprintf(os.Stdout, "  ✗ %s\n", e.String())
		}
		return fmt.Errorf("policy validation failed with %d error(s)", len(vr.Errors))
	}

	fmt.Fprintf(os.Stdout, "Validation: OK\n")

	// Summary
	p := result.Policy
	fmt.Fprintf(os.Stdout, "\nPolicy summary:\n")
	fmt.Fprintf(os.Stdout, "  Global enabled:      %v\n", p.Global.Enabled)
	fmt.Fprintf(os.Stdout, "  Apply enabled:       %v\n", p.Apply.Enabled)
	fmt.Fprintf(os.Stdout, "  Audit backend:       %s\n", p.Audit.Backend)
	fmt.Fprintf(os.Stdout, "  Denied namespaces:   %v\n", p.Namespaces.Deny)
	fmt.Fprintf(os.Stdout, "  Max request delta:   %d%%\n", p.Apply.MaxRequestDeltaPct)
	fmt.Fprintf(os.Stdout, "  Max limit delta:     %d%%\n", p.Apply.MaxLimitDeltaPct)
	fmt.Fprintf(os.Stdout, "  Min safety rating:   %s\n", p.Apply.MinSafetyRating)
	fmt.Fprintf(os.Stdout, "  Rate limit:          %d applies/hour\n", p.RateLimits.MaxAppliesPerHour)

	if checkPaths && p.Audit.Path != "" {
		fmt.Fprintf(os.Stdout, "\nPath checks:\n")
		if err := policy.CheckAuditPath(p.Audit.Path); err != nil {
			fmt.Fprintf(os.Stdout, "  ✗ audit.path: %v\n", err)
			return fmt.Errorf("path check failed")
		}
		fmt.Fprintf(os.Stdout, "  ✓ audit.path: %s (writable)\n", p.Audit.Path)
	}

	return nil
}
