/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resource

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/time/rate"
)

// OperationType represents different types of operations that can be rate limited
type OperationType string

const (
	OperationTypeApply  OperationType = "apply"
	OperationTypeRemove OperationType = "remove"
	OperationTypeQuery  OperationType = "query"
)

// RateLimiterConfig holds configuration for rate limiting
type RateLimiterConfig struct {
	// Global rate limits (operations per second)
	GlobalApplyRate  rate.Limit
	GlobalRemoveRate rate.Limit
	GlobalQueryRate  rate.Limit

	// Burst limits
	GlobalApplyBurst  int
	GlobalRemoveBurst int
	GlobalQueryBurst  int

	// Per-namespace rate limits
	PerNamespaceApplyRate  rate.Limit
	PerNamespaceRemoveRate rate.Limit
	PerNamespaceQueryRate  rate.Limit

	// Per-namespace burst limits
	PerNamespaceApplyBurst  int
	PerNamespaceRemoveBurst int
	PerNamespaceQueryBurst  int
}

// DefaultRateLimiterConfig returns a default rate limiter configuration
func DefaultRateLimiterConfig() *RateLimiterConfig {
	return &RateLimiterConfig{
		// Global limits - conservative defaults
		GlobalApplyRate:  rate.Limit(10), // 10 apply operations per second
		GlobalRemoveRate: rate.Limit(10), // 10 remove operations per second
		GlobalQueryRate:  rate.Limit(50), // 50 query operations per second

		GlobalApplyBurst:  20,
		GlobalRemoveBurst: 20,
		GlobalQueryBurst:  100,

		// Per-namespace limits - more restrictive
		PerNamespaceApplyRate:  rate.Limit(5),  // 5 apply operations per second per namespace
		PerNamespaceRemoveRate: rate.Limit(5),  // 5 remove operations per second per namespace
		PerNamespaceQueryRate:  rate.Limit(20), // 20 query operations per second per namespace

		PerNamespaceApplyBurst:  10,
		PerNamespaceRemoveBurst: 10,
		PerNamespaceQueryBurst:  40,
	}
}

// AnnotationRateLimiter provides rate limiting for annotation operations
type AnnotationRateLimiter struct {
	config *RateLimiterConfig
	log    logr.Logger

	// Global rate limiters
	globalApplyLimiter  *rate.Limiter
	globalRemoveLimiter *rate.Limiter
	globalQueryLimiter  *rate.Limiter

	// Per-namespace rate limiters
	namespaceLimiters map[string]*namespaceLimiters
	mu                sync.RWMutex

	// Statistics
	totalRequests     int64
	throttledRequests int64
	statsMu           sync.RWMutex
}

// namespaceLimiters holds rate limiters for a specific namespace
type namespaceLimiters struct {
	applyLimiter  *rate.Limiter
	removeLimiter *rate.Limiter
	queryLimiter  *rate.Limiter
	lastUsed      time.Time
}

// NewAnnotationRateLimiter creates a new rate limiter for annotation operations
func NewAnnotationRateLimiter(config *RateLimiterConfig, log logr.Logger) *AnnotationRateLimiter {
	if config == nil {
		config = DefaultRateLimiterConfig()
	}

	return &AnnotationRateLimiter{
		config: config,
		log:    log,

		globalApplyLimiter:  rate.NewLimiter(config.GlobalApplyRate, config.GlobalApplyBurst),
		globalRemoveLimiter: rate.NewLimiter(config.GlobalRemoveRate, config.GlobalRemoveBurst),
		globalQueryLimiter:  rate.NewLimiter(config.GlobalQueryRate, config.GlobalQueryBurst),

		namespaceLimiters: make(map[string]*namespaceLimiters),
	}
}

// Wait blocks until the operation is allowed by the rate limiter
func (arl *AnnotationRateLimiter) Wait(ctx context.Context, operation OperationType, namespace string) error {
	arl.statsMu.Lock()
	arl.totalRequests++
	arl.statsMu.Unlock()

	// Check global rate limit first
	if err := arl.waitGlobal(ctx, operation); err != nil {
		arl.statsMu.Lock()
		arl.throttledRequests++
		arl.statsMu.Unlock()
		return err
	}

	// Check per-namespace rate limit
	if namespace != "" {
		if err := arl.waitNamespace(ctx, operation, namespace); err != nil {
			arl.statsMu.Lock()
			arl.throttledRequests++
			arl.statsMu.Unlock()
			return err
		}
	}

	return nil
}

// Allow checks if the operation is allowed without blocking
func (arl *AnnotationRateLimiter) Allow(operation OperationType, namespace string) bool {
	arl.statsMu.Lock()
	arl.totalRequests++
	arl.statsMu.Unlock()

	// Check global rate limit first
	if !arl.allowGlobal(operation) {
		arl.statsMu.Lock()
		arl.throttledRequests++
		arl.statsMu.Unlock()
		return false
	}

	// Check per-namespace rate limit
	if namespace != "" {
		if !arl.allowNamespace(operation, namespace) {
			arl.statsMu.Lock()
			arl.throttledRequests++
			arl.statsMu.Unlock()
			return false
		}
	}

	return true
}

// waitGlobal waits for global rate limit approval
func (arl *AnnotationRateLimiter) waitGlobal(ctx context.Context, operation OperationType) error {
	var limiter *rate.Limiter

	switch operation {
	case OperationTypeApply:
		limiter = arl.globalApplyLimiter
	case OperationTypeRemove:
		limiter = arl.globalRemoveLimiter
	case OperationTypeQuery:
		limiter = arl.globalQueryLimiter
	default:
		return fmt.Errorf("unknown operation type: %s", operation)
	}

	if err := limiter.Wait(ctx); err != nil {
		arl.log.V(1).Info("Global rate limit wait failed", "operation", operation, "error", err)
		return fmt.Errorf("global rate limit exceeded for %s operation: %w", operation, err)
	}

	return nil
}

// allowGlobal checks global rate limit without blocking
func (arl *AnnotationRateLimiter) allowGlobal(operation OperationType) bool {
	var limiter *rate.Limiter

	switch operation {
	case OperationTypeApply:
		limiter = arl.globalApplyLimiter
	case OperationTypeRemove:
		limiter = arl.globalRemoveLimiter
	case OperationTypeQuery:
		limiter = arl.globalQueryLimiter
	default:
		return false
	}

	return limiter.Allow()
}

// waitNamespace waits for namespace-specific rate limit approval
func (arl *AnnotationRateLimiter) waitNamespace(ctx context.Context, operation OperationType, namespace string) error {
	limiters := arl.getOrCreateNamespaceLimiters(namespace)

	var limiter *rate.Limiter
	switch operation {
	case OperationTypeApply:
		limiter = limiters.applyLimiter
	case OperationTypeRemove:
		limiter = limiters.removeLimiter
	case OperationTypeQuery:
		limiter = limiters.queryLimiter
	default:
		return fmt.Errorf("unknown operation type: %s", operation)
	}

	if err := limiter.Wait(ctx); err != nil {
		arl.log.V(1).Info("Namespace rate limit wait failed",
			"operation", operation, "namespace", namespace, "error", err)
		return fmt.Errorf("namespace rate limit exceeded for %s operation in namespace %s: %w",
			operation, namespace, err)
	}

	return nil
}

// allowNamespace checks namespace-specific rate limit without blocking
func (arl *AnnotationRateLimiter) allowNamespace(operation OperationType, namespace string) bool {
	limiters := arl.getOrCreateNamespaceLimiters(namespace)

	var limiter *rate.Limiter
	switch operation {
	case OperationTypeApply:
		limiter = limiters.applyLimiter
	case OperationTypeRemove:
		limiter = limiters.removeLimiter
	case OperationTypeQuery:
		limiter = limiters.queryLimiter
	default:
		return false
	}

	return limiter.Allow()
}

// getOrCreateNamespaceLimiters gets or creates rate limiters for a namespace
func (arl *AnnotationRateLimiter) getOrCreateNamespaceLimiters(namespace string) *namespaceLimiters {
	arl.mu.RLock()
	limiters, exists := arl.namespaceLimiters[namespace]
	arl.mu.RUnlock()

	if exists {
		// Update last used time
		limiters.lastUsed = time.Now()
		return limiters
	}

	// Create new limiters for this namespace
	arl.mu.Lock()
	defer arl.mu.Unlock()

	// Double-check after acquiring write lock
	if limiters, exists := arl.namespaceLimiters[namespace]; exists {
		limiters.lastUsed = time.Now()
		return limiters
	}

	limiters = &namespaceLimiters{
		applyLimiter:  rate.NewLimiter(arl.config.PerNamespaceApplyRate, arl.config.PerNamespaceApplyBurst),
		removeLimiter: rate.NewLimiter(arl.config.PerNamespaceRemoveRate, arl.config.PerNamespaceRemoveBurst),
		queryLimiter:  rate.NewLimiter(arl.config.PerNamespaceQueryRate, arl.config.PerNamespaceQueryBurst),
		lastUsed:      time.Now(),
	}

	arl.namespaceLimiters[namespace] = limiters
	arl.log.V(2).Info("Created rate limiters for namespace", "namespace", namespace)

	return limiters
}

// GetStats returns rate limiter statistics
func (arl *AnnotationRateLimiter) GetStats() (totalRequests, throttledRequests int64, namespaceCount int) {
	arl.statsMu.RLock()
	total := arl.totalRequests
	throttled := arl.throttledRequests
	arl.statsMu.RUnlock()

	arl.mu.RLock()
	nsCount := len(arl.namespaceLimiters)
	arl.mu.RUnlock()

	return total, throttled, nsCount
}

// CleanupUnusedNamespaceLimiters removes rate limiters for namespaces that haven't been used recently
func (arl *AnnotationRateLimiter) CleanupUnusedNamespaceLimiters(maxAge time.Duration) {
	arl.mu.Lock()
	defer arl.mu.Unlock()

	now := time.Now()
	namespacesToDelete := make([]string, 0)

	for namespace, limiters := range arl.namespaceLimiters {
		if now.Sub(limiters.lastUsed) > maxAge {
			namespacesToDelete = append(namespacesToDelete, namespace)
		}
	}

	for _, namespace := range namespacesToDelete {
		delete(arl.namespaceLimiters, namespace)
		arl.log.V(2).Info("Cleaned up unused namespace rate limiters", "namespace", namespace)
	}

	if len(namespacesToDelete) > 0 {
		arl.log.Info("Cleaned up unused namespace rate limiters", "count", len(namespacesToDelete))
	}
}

// StartCleanupRoutine starts a background routine to clean up unused namespace limiters
func (arl *AnnotationRateLimiter) StartCleanupRoutine(ctx context.Context, interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	arl.log.Info("Started rate limiter cleanup routine", "interval", interval, "maxAge", maxAge)

	for {
		select {
		case <-ctx.Done():
			arl.log.Info("Rate limiter cleanup routine stopped")
			return
		case <-ticker.C:
			arl.CleanupUnusedNamespaceLimiters(maxAge)
		}
	}
}
