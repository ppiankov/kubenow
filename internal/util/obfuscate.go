package util

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// Obfuscator provides deterministic obfuscation of sensitive strings
type Obfuscator struct {
	enabled bool
	cache   map[string]string
	mu      sync.RWMutex
}

// NewObfuscator creates a new obfuscator
func NewObfuscator(enabled bool) *Obfuscator {
	return &Obfuscator{
		enabled: enabled,
		cache:   make(map[string]string),
	}
}

// Namespace obfuscates a namespace name
func (o *Obfuscator) Namespace(name string) string {
	if !o.enabled || name == "" {
		return name
	}
	return o.obfuscate("ns", name)
}

// Pod obfuscates a pod name
func (o *Obfuscator) Pod(name string) string {
	if !o.enabled || name == "" {
		return name
	}
	return o.obfuscate("pod", name)
}

// Service obfuscates a service name
func (o *Obfuscator) Service(name string) string {
	if !o.enabled || name == "" {
		return name
	}
	return o.obfuscate("svc", name)
}

// Node obfuscates a node name
func (o *Obfuscator) Node(name string) string {
	if !o.enabled || name == "" {
		return name
	}
	return o.obfuscate("node", name)
}

// Container obfuscates a container name
func (o *Obfuscator) Container(name string) string {
	if !o.enabled || name == "" {
		return name
	}
	return o.obfuscate("ctr", name)
}

// Workload obfuscates a workload name (deployment, statefulset, etc.)
func (o *Obfuscator) Workload(name string) string {
	if !o.enabled || name == "" {
		return name
	}
	return o.obfuscate("wl", name)
}

// Image obfuscates an image name
func (o *Obfuscator) Image(name string) string {
	if !o.enabled || name == "" {
		return name
	}
	return o.obfuscate("img", name)
}

// obfuscate generates a deterministic fake name from a real name
func (o *Obfuscator) obfuscate(prefix, realName string) string {
	o.mu.RLock()
	if cached, exists := o.cache[realName]; exists {
		o.mu.RUnlock()
		return cached
	}
	o.mu.RUnlock()

	// Generate deterministic hash
	hash := sha256.Sum256([]byte(realName))
	hashStr := hex.EncodeToString(hash[:])

	// Create short, readable fake name
	fakeName := fmt.Sprintf("%s-%s", prefix, hashStr[:8])

	// Cache it
	o.mu.Lock()
	o.cache[realName] = fakeName
	o.mu.Unlock()

	return fakeName
}

// IsEnabled returns whether obfuscation is enabled
func (o *Obfuscator) IsEnabled() bool {
	return o.enabled
}
