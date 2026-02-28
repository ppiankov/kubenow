// Package cost provides resource cost estimation for Kubernetes workloads.
package cost

// Rates holds cost-per-unit-hour pricing for resource estimation.
type Rates struct {
	CPUPerCoreHour   float64 `json:"cpu_per_core_hour"`
	MemoryPerGiBHour float64 `json:"memory_per_gib_hour"`
	Source           string  `json:"source"` // "user", "instance-type", "default"
}

// hoursPerMonth is the standard cloud billing constant (365.25/12 × 24).
const hoursPerMonth = 730.0

// sourceUser identifies rates provided via CLI flags.
const sourceUser = "user"

// sourceInstanceType identifies rates looked up from the pricing table.
const sourceInstanceType = "instance-type"

// sourceDefault identifies fallback rates when no other source is available.
const sourceDefault = "default"

// DefaultRates returns conservative fallback pricing based on AWS m5.xlarge
// on-demand rates (us-east-1). These are intentionally conservative to avoid
// overstating savings.
func DefaultRates() Rates {
	return Rates{
		CPUPerCoreHour:   0.031,
		MemoryPerGiBHour: 0.004,
		Source:           sourceDefault,
	}
}

// CustomRates creates rates from user-provided values.
func CustomRates(cpuRate, memRate float64) Rates {
	return Rates{
		CPUPerCoreHour:   cpuRate,
		MemoryPerGiBHour: memRate,
		Source:           sourceUser,
	}
}

// LookupRates returns pricing for a known instance type.
// Returns false if the instance type is not in the pricing table.
func LookupRates(instanceType string) (Rates, bool) {
	r, ok := pricingTable[instanceType]
	return r, ok
}

// ResolveRates determines the best available pricing using priority:
// 1. User-provided custom rates (if either > 0)
// 2. Instance-type lookup (if known)
// 3. Default fallback rates
func ResolveRates(instanceType string, customCPU, customMem float64) Rates {
	if customCPU > 0 || customMem > 0 {
		rates := DefaultRates()
		rates.Source = sourceUser
		if customCPU > 0 {
			rates.CPUPerCoreHour = customCPU
		}
		if customMem > 0 {
			rates.MemoryPerGiBHour = customMem
		}
		return rates
	}

	if instanceType != "" {
		if r, ok := LookupRates(instanceType); ok {
			return r
		}
	}

	return DefaultRates()
}

// pricingTable maps instance types to derived per-unit-hour rates.
// Rates are derived from public on-demand pricing (us-east-1 / us-central1)
// as of Feb 2026. CPU rate = price / vCPUs / hour. Memory rate is the
// residual after subtracting CPU cost from the total hourly price.
//
// These are estimates for guidance only, not billing-grade data.
var pricingTable = map[string]Rates{
	// AWS EC2 — compute-optimized
	"c5.large":   {CPUPerCoreHour: 0.0425, MemoryPerGiBHour: 0.0, Source: sourceInstanceType},
	"c5.xlarge":  {CPUPerCoreHour: 0.0425, MemoryPerGiBHour: 0.0, Source: sourceInstanceType},
	"c5.2xlarge": {CPUPerCoreHour: 0.0425, MemoryPerGiBHour: 0.0, Source: sourceInstanceType},
	"c5.4xlarge": {CPUPerCoreHour: 0.0425, MemoryPerGiBHour: 0.0, Source: sourceInstanceType},

	// AWS EC2 — general-purpose
	"m5.large":   {CPUPerCoreHour: 0.048, MemoryPerGiBHour: 0.006, Source: sourceInstanceType},
	"m5.xlarge":  {CPUPerCoreHour: 0.048, MemoryPerGiBHour: 0.006, Source: sourceInstanceType},
	"m5.2xlarge": {CPUPerCoreHour: 0.048, MemoryPerGiBHour: 0.006, Source: sourceInstanceType},

	// AWS EC2 — memory-optimized
	"r5.large":   {CPUPerCoreHour: 0.063, MemoryPerGiBHour: 0.008, Source: sourceInstanceType},
	"r5.xlarge":  {CPUPerCoreHour: 0.063, MemoryPerGiBHour: 0.008, Source: sourceInstanceType},
	"r5.2xlarge": {CPUPerCoreHour: 0.063, MemoryPerGiBHour: 0.008, Source: sourceInstanceType},

	// GCP — general-purpose
	"n2-standard-2": {CPUPerCoreHour: 0.035, MemoryPerGiBHour: 0.005, Source: sourceInstanceType},
	"n2-standard-4": {CPUPerCoreHour: 0.035, MemoryPerGiBHour: 0.005, Source: sourceInstanceType},
	"n2-standard-8": {CPUPerCoreHour: 0.035, MemoryPerGiBHour: 0.005, Source: sourceInstanceType},

	// GCP — cost-optimized
	"e2-standard-2": {CPUPerCoreHour: 0.025, MemoryPerGiBHour: 0.003, Source: sourceInstanceType},
	"e2-standard-4": {CPUPerCoreHour: 0.025, MemoryPerGiBHour: 0.003, Source: sourceInstanceType},

	// Azure — general-purpose
	"Standard_D2s_v3": {CPUPerCoreHour: 0.048, MemoryPerGiBHour: 0.006, Source: sourceInstanceType},
	"Standard_D4s_v3": {CPUPerCoreHour: 0.048, MemoryPerGiBHour: 0.006, Source: sourceInstanceType},
	"Standard_D8s_v3": {CPUPerCoreHour: 0.048, MemoryPerGiBHour: 0.006, Source: sourceInstanceType},
}
