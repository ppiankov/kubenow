package cost

import "math"

// WorkloadCostEstimate holds per-workload cost impact.
type WorkloadCostEstimate struct {
	CurrentMonthlyCost float64 `json:"current_monthly_cost"`
	OptimalMonthlyCost float64 `json:"optimal_monthly_cost"`
	WastedMonthly      float64 `json:"wasted_monthly"`
	SavingsPercent     float64 `json:"savings_percent"`
}

// SummaryCostEstimate holds cluster-wide aggregate cost impact.
type SummaryCostEstimate struct {
	TotalWastedMonthly  float64 `json:"total_wasted_monthly"`
	TotalCurrentMonthly float64 `json:"total_current_monthly"`
	SavingsPercent      float64 `json:"savings_percent"`
	Rates               Rates   `json:"rates"`
}

// EstimateWorkload computes the cost impact for a single workload based on
// the difference between requested and observed (P95) resource usage.
func EstimateWorkload(requestedCPU, p95CPU, requestedMemGi, p95MemGi float64, rates Rates) WorkloadCostEstimate {
	currentCPUCost := requestedCPU * rates.CPUPerCoreHour * hoursPerMonth
	currentMemCost := requestedMemGi * rates.MemoryPerGiBHour * hoursPerMonth
	current := currentCPUCost + currentMemCost

	optimalCPUCost := p95CPU * rates.CPUPerCoreHour * hoursPerMonth
	optimalMemCost := p95MemGi * rates.MemoryPerGiBHour * hoursPerMonth
	optimal := optimalCPUCost + optimalMemCost

	wasted := current - optimal
	if wasted < 0 {
		wasted = 0 // under-provisioned workloads don't have savings
	}

	var savingsPct float64
	if current > 0 {
		savingsPct = wasted / current * 100
	}

	return WorkloadCostEstimate{
		CurrentMonthlyCost: roundCents(current),
		OptimalMonthlyCost: roundCents(optimal),
		WastedMonthly:      roundCents(wasted),
		SavingsPercent:     math.Round(savingsPct*10) / 10,
	}
}

// EstimateSummary computes the aggregate cost impact across all workloads.
func EstimateSummary(wastedCPU, wastedMemGi, totalRequestedCPU, totalRequestedMemGi float64, rates Rates) SummaryCostEstimate {
	wastedCPUCost := wastedCPU * rates.CPUPerCoreHour * hoursPerMonth
	wastedMemCost := wastedMemGi * rates.MemoryPerGiBHour * hoursPerMonth
	totalWasted := wastedCPUCost + wastedMemCost
	if totalWasted < 0 {
		totalWasted = 0
	}

	currentCPUCost := totalRequestedCPU * rates.CPUPerCoreHour * hoursPerMonth
	currentMemCost := totalRequestedMemGi * rates.MemoryPerGiBHour * hoursPerMonth
	totalCurrent := currentCPUCost + currentMemCost

	var savingsPct float64
	if totalCurrent > 0 {
		savingsPct = totalWasted / totalCurrent * 100
	}

	return SummaryCostEstimate{
		TotalWastedMonthly:  roundCents(totalWasted),
		TotalCurrentMonthly: roundCents(totalCurrent),
		SavingsPercent:      math.Round(savingsPct*10) / 10,
		Rates:               rates,
	}
}

// roundCents rounds to the nearest cent.
func roundCents(v float64) float64 {
	return math.Round(v*100) / 100
}
