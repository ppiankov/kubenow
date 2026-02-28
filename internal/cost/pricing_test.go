package cost

import (
	"math"
	"testing"
)

// --- Pricing tests ---

func TestDefaultRates(t *testing.T) {
	r := DefaultRates()
	if r.CPUPerCoreHour <= 0 {
		t.Error("default CPU rate must be positive")
	}
	if r.MemoryPerGiBHour <= 0 {
		t.Error("default memory rate must be positive")
	}
	if r.Source != sourceDefault {
		t.Errorf("expected source 'default', got %q", r.Source)
	}
}

func TestCustomRates(t *testing.T) {
	r := CustomRates(0.05, 0.01)
	if r.CPUPerCoreHour != 0.05 {
		t.Errorf("expected CPU rate 0.05, got %f", r.CPUPerCoreHour)
	}
	if r.MemoryPerGiBHour != 0.01 {
		t.Errorf("expected memory rate 0.01, got %f", r.MemoryPerGiBHour)
	}
	if r.Source != sourceUser {
		t.Errorf("expected source 'user', got %q", r.Source)
	}
}

func TestLookupRates_Known(t *testing.T) {
	r, ok := LookupRates("c5.xlarge")
	if !ok {
		t.Fatal("expected c5.xlarge to be known")
	}
	if r.CPUPerCoreHour <= 0 {
		t.Error("expected positive CPU rate for c5.xlarge")
	}
	if r.Source != "instance-type" {
		t.Errorf("expected source 'instance-type', got %q", r.Source)
	}
}

func TestLookupRates_Unknown(t *testing.T) {
	_, ok := LookupRates("unknown-instance")
	if ok {
		t.Error("expected unknown instance type to return false")
	}
}

func TestResolveRates_Priority(t *testing.T) {
	// Custom overrides everything
	r := ResolveRates("c5.xlarge", 0.1, 0.02)
	if r.Source != sourceUser {
		t.Errorf("expected user source with custom rates, got %q", r.Source)
	}
	if r.CPUPerCoreHour != 0.1 {
		t.Errorf("expected custom CPU rate 0.1, got %f", r.CPUPerCoreHour)
	}

	// Instance type when no custom
	r = ResolveRates("c5.xlarge", 0, 0)
	if r.Source != "instance-type" {
		t.Errorf("expected instance-type source, got %q", r.Source)
	}

	// Default when no custom and unknown instance
	r = ResolveRates("unknown", 0, 0)
	if r.Source != sourceDefault {
		t.Errorf("expected default source, got %q", r.Source)
	}

	// Default when empty instance type
	r = ResolveRates("", 0, 0)
	if r.Source != sourceDefault {
		t.Errorf("expected default source for empty instance, got %q", r.Source)
	}
}

func TestResolveRates_PartialCustom(t *testing.T) {
	// Only CPU custom — memory should use default
	r := ResolveRates("", 0.1, 0)
	if r.Source != sourceUser {
		t.Errorf("expected user source, got %q", r.Source)
	}
	if r.CPUPerCoreHour != 0.1 {
		t.Errorf("expected custom CPU 0.1, got %f", r.CPUPerCoreHour)
	}
	if r.MemoryPerGiBHour != DefaultRates().MemoryPerGiBHour {
		t.Errorf("expected default memory rate, got %f", r.MemoryPerGiBHour)
	}
}

// --- Estimate tests ---

func TestEstimateWorkload_Basic(t *testing.T) {
	rates := DefaultRates() // 0.031 CPU, 0.004 mem

	// 500m requested, 150m used, 1Gi requested, 0.5Gi used
	est := EstimateWorkload(0.5, 0.15, 1.0, 0.5, rates)

	// current = (0.5 * 0.031 + 1.0 * 0.004) * 730 = (0.0155 + 0.004) * 730 = 14.235
	// optimal = (0.15 * 0.031 + 0.5 * 0.004) * 730 = (0.00465 + 0.002) * 730 = 4.855
	// wasted = 14.235 - 4.855 = 9.38
	if est.CurrentMonthlyCost <= 0 {
		t.Error("expected positive current cost")
	}
	if est.WastedMonthly <= 0 {
		t.Error("expected positive wasted cost")
	}
	if est.SavingsPercent <= 0 {
		t.Error("expected positive savings percent")
	}
	if est.WastedMonthly > est.CurrentMonthlyCost {
		t.Error("wasted should not exceed current")
	}
}

func TestEstimateWorkload_ZeroUsage(t *testing.T) {
	rates := DefaultRates()
	est := EstimateWorkload(1.0, 0, 2.0, 0, rates)

	if est.SavingsPercent != 100 {
		t.Errorf("expected 100%% savings for zero usage, got %.1f%%", est.SavingsPercent)
	}
	if est.WastedMonthly != est.CurrentMonthlyCost {
		t.Errorf("wasted should equal current when usage is zero")
	}
}

func TestEstimateWorkload_EqualRequestAndUsage(t *testing.T) {
	rates := DefaultRates()
	est := EstimateWorkload(1.0, 1.0, 2.0, 2.0, rates)

	if est.WastedMonthly != 0 {
		t.Errorf("expected $0 wasted for equal request/usage, got $%.2f", est.WastedMonthly)
	}
	if est.SavingsPercent != 0 {
		t.Errorf("expected 0%% savings, got %.1f%%", est.SavingsPercent)
	}
}

func TestEstimateWorkload_UnderProvisioned(t *testing.T) {
	rates := DefaultRates()
	// Usage exceeds request
	est := EstimateWorkload(0.5, 1.0, 1.0, 3.0, rates)

	if est.WastedMonthly != 0 {
		t.Errorf("expected $0 wasted for under-provisioned, got $%.2f", est.WastedMonthly)
	}
}

func TestEstimateWorkload_ZeroRequested(t *testing.T) {
	rates := DefaultRates()
	est := EstimateWorkload(0, 0, 0, 0, rates)

	if est.SavingsPercent != 0 {
		t.Errorf("expected 0%% savings for zero requests, got %.1f%%", est.SavingsPercent)
	}
}

func TestEstimateWorkload_ExactDollarAmount(t *testing.T) {
	// Use known rates for exact calculation
	rates := Rates{CPUPerCoreHour: 0.01, MemoryPerGiBHour: 0.001, Source: "test"}

	// 2 cores requested, 1 core used, 4Gi requested, 2Gi used
	est := EstimateWorkload(2.0, 1.0, 4.0, 2.0, rates)

	// current = (2.0 * 0.01 + 4.0 * 0.001) * 730 = (0.02 + 0.004) * 730 = 17.52
	// optimal = (1.0 * 0.01 + 2.0 * 0.001) * 730 = (0.01 + 0.002) * 730 = 8.76
	// wasted = 17.52 - 8.76 = 8.76
	expectedCurrent := 17.52
	expectedWasted := 8.76

	if math.Abs(est.CurrentMonthlyCost-expectedCurrent) > 0.01 {
		t.Errorf("expected current $%.2f, got $%.2f", expectedCurrent, est.CurrentMonthlyCost)
	}
	if math.Abs(est.WastedMonthly-expectedWasted) > 0.01 {
		t.Errorf("expected wasted $%.2f, got $%.2f", expectedWasted, est.WastedMonthly)
	}
}

func TestEstimateSummary(t *testing.T) {
	rates := Rates{CPUPerCoreHour: 0.01, MemoryPerGiBHour: 0.001, Source: "test"}

	// 5 cores wasted, 10Gi wasted, 10 cores total, 20Gi total
	summary := EstimateSummary(5.0, 10.0, 10.0, 20.0, rates)

	if summary.TotalWastedMonthly <= 0 {
		t.Error("expected positive wasted total")
	}
	if summary.TotalCurrentMonthly <= 0 {
		t.Error("expected positive current total")
	}
	if summary.SavingsPercent <= 0 || summary.SavingsPercent > 100 {
		t.Errorf("unexpected savings percent: %.1f%%", summary.SavingsPercent)
	}
	if summary.Rates.Source != "test" {
		t.Errorf("expected rates source 'test', got %q", summary.Rates.Source)
	}
}

func TestEstimateSummary_NegativeWaste(t *testing.T) {
	rates := DefaultRates()
	// More used than requested (cluster under-provisioned)
	summary := EstimateSummary(-2.0, -4.0, 5.0, 10.0, rates)

	if summary.TotalWastedMonthly != 0 {
		t.Errorf("expected $0 wasted for negative waste, got $%.2f", summary.TotalWastedMonthly)
	}
}
