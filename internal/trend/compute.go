package trend

// WorkloadTrend shows how a workload's skew changed over the history window.
type WorkloadTrend struct {
	Namespace  string  `json:"namespace"`
	Workload   string  `json:"workload"`
	CurrentCPU float64 `json:"current_skew_cpu"`
	CurrentMem float64 `json:"current_skew_memory"`
	DeltaCPU   float64 `json:"delta_skew_cpu"`    // current - oldest
	DeltaMem   float64 `json:"delta_skew_memory"` // current - oldest
	Direction  string  `json:"direction"`         // improving, worsening, stable
	Snapshots  int     `json:"snapshots"`         // how many data points
}

// Summary is the computed trend across all workloads.
type Summary struct {
	Days       int             `json:"days"`
	Snapshots  int             `json:"snapshots"`
	Workloads  []WorkloadTrend `json:"workloads"`
	WasteDelta WasteDelta      `json:"waste_delta"`
}

// WasteDelta captures change in total waste over the trend window.
type WasteDelta struct {
	OldestCPU  float64 `json:"oldest_cpu_cores"`
	CurrentCPU float64 `json:"current_cpu_cores"`
	DeltaCPU   float64 `json:"delta_cpu_cores"`
	OldestMem  float64 `json:"oldest_memory_gi"`
	CurrentMem float64 `json:"current_memory_gi"`
	DeltaMem   float64 `json:"delta_memory_gi"`
}

// ComputeTrend calculates per-workload trends from historical snapshots.
func ComputeTrend(history []Snapshot) *Summary {
	if len(history) == 0 {
		return &Summary{}
	}

	oldest := history[0]
	latest := history[len(history)-1]

	// Build lookup for oldest snapshot
	oldSkew := make(map[string]WorkloadSnapshot)
	for _, w := range oldest.Workloads {
		oldSkew[w.Namespace+"/"+w.Workload] = w
	}

	var trends []WorkloadTrend
	for _, w := range latest.Workloads {
		key := w.Namespace + "/" + w.Workload
		t := WorkloadTrend{
			Namespace:  w.Namespace,
			Workload:   w.Workload,
			CurrentCPU: w.SkewCPU,
			CurrentMem: w.SkewMem,
			Snapshots:  countWorkloadAppearances(history, key),
		}

		if old, ok := oldSkew[key]; ok {
			t.DeltaCPU = w.SkewCPU - old.SkewCPU
			t.DeltaMem = w.SkewMem - old.SkewMem
		}

		t.Direction = classifyDirection(t.DeltaCPU, t.DeltaMem)
		trends = append(trends, t)
	}

	return &Summary{
		Snapshots: len(history),
		Workloads: trends,
		WasteDelta: WasteDelta{
			OldestCPU:  oldest.TotalWaste.CPU,
			CurrentCPU: latest.TotalWaste.CPU,
			DeltaCPU:   latest.TotalWaste.CPU - oldest.TotalWaste.CPU,
			OldestMem:  oldest.TotalWaste.MemGi,
			CurrentMem: latest.TotalWaste.MemGi,
			DeltaMem:   latest.TotalWaste.MemGi - oldest.TotalWaste.MemGi,
		},
	}
}

func countWorkloadAppearances(history []Snapshot, key string) int {
	count := 0
	for _, snap := range history {
		for _, w := range snap.Workloads {
			if w.Namespace+"/"+w.Workload == key {
				count++
				break
			}
		}
	}
	return count
}

// classifyDirection determines trend direction from combined CPU+memory delta.
// Uses 0.05 (5%) as threshold for stability.
func classifyDirection(deltaCPU, deltaMem float64) string {
	combined := (deltaCPU + deltaMem) / 2
	switch {
	case combined < -0.05:
		return "improving"
	case combined > 0.05:
		return "worsening"
	default:
		return "stable"
	}
}
