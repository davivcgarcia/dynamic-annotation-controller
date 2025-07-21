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
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CacheEntry represents a cached resource entry
type CacheEntry struct {
	Resources   []client.Object
	LastUpdated time.Time
	Selector    string
	Namespace   string
}

// ResourceCache provides caching for resource queries to reduce API server load
type ResourceCache struct {
	cache      map[string]*CacheEntry
	mu         sync.RWMutex
	ttl        time.Duration
	maxEntries int
	log        logr.Logger

	// Statistics
	hits      int64
	misses    int64
	evictions int64
}

// NewResourceCache creates a new resource cache
func NewResourceCache(ttl time.Duration, maxEntries int, log logr.Logger) *ResourceCache {
	return &ResourceCache{
		cache:      make(map[string]*CacheEntry),
		ttl:        ttl,
		maxEntries: maxEntries,
		log:        log,
	}
}

// GetCachedResources retrieves resources from cache if available and not expired
func (rc *ResourceCache) GetCachedResources(selector metav1.LabelSelector, resourceTypes []string, namespace string) ([]client.Object, bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	key := rc.generateCacheKey(selector, resourceTypes, namespace)
	entry, exists := rc.cache[key]

	if !exists {
		rc.misses++
		return nil, false
	}

	// Check if entry is expired
	if time.Since(entry.LastUpdated) > rc.ttl {
		rc.misses++
		return nil, false
	}

	rc.hits++
	rc.log.V(2).Info("Cache hit", "key", key, "resourceCount", len(entry.Resources))

	// Return a copy to prevent modification
	resources := make([]client.Object, len(entry.Resources))
	for i, resource := range entry.Resources {
		resources[i] = resource.DeepCopyObject().(client.Object)
	}

	return resources, true
}

// CacheResources stores resources in the cache
func (rc *ResourceCache) CacheResources(selector metav1.LabelSelector, resourceTypes []string, namespace string, resources []client.Object) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	key := rc.generateCacheKey(selector, resourceTypes, namespace)

	// Check if we need to evict entries
	if len(rc.cache) >= rc.maxEntries {
		rc.evictOldestEntry()
	}

	// Store a deep copy to prevent external modifications
	cachedResources := make([]client.Object, len(resources))
	for i, resource := range resources {
		cachedResources[i] = resource.DeepCopyObject().(client.Object)
	}

	rc.cache[key] = &CacheEntry{
		Resources:   cachedResources,
		LastUpdated: time.Now(),
		Selector:    selector.String(),
		Namespace:   namespace,
	}

	rc.log.V(2).Info("Cached resources", "key", key, "resourceCount", len(resources))
}

// InvalidateCache removes entries from cache based on various criteria
func (rc *ResourceCache) InvalidateCache(namespace string, resourceType string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	keysToDelete := make([]string, 0)

	for key, entry := range rc.cache {
		// Invalidate if namespace matches (empty namespace means all namespaces)
		if namespace != "" && entry.Namespace != namespace {
			continue
		}

		// Invalidate if resource type is mentioned in the key
		if resourceType != "" && !rc.keyContainsResourceType(key, resourceType) {
			continue
		}

		keysToDelete = append(keysToDelete, key)
	}

	for _, key := range keysToDelete {
		delete(rc.cache, key)
		rc.log.V(2).Info("Invalidated cache entry", "key", key)
	}
}

// InvalidateExpiredEntries removes expired entries from cache
func (rc *ResourceCache) InvalidateExpiredEntries() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	now := time.Now()
	keysToDelete := make([]string, 0)

	for key, entry := range rc.cache {
		if now.Sub(entry.LastUpdated) > rc.ttl {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		delete(rc.cache, key)
		rc.evictions++
		rc.log.V(2).Info("Evicted expired cache entry", "key", key)
	}
}

// GetCacheStats returns cache statistics
func (rc *ResourceCache) GetCacheStats() (hits, misses, evictions int64, size int) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	return rc.hits, rc.misses, rc.evictions, len(rc.cache)
}

// ClearCache removes all entries from cache
func (rc *ResourceCache) ClearCache() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	rc.cache = make(map[string]*CacheEntry)
	rc.log.Info("Cache cleared")
}

// generateCacheKey creates a unique key for cache entries
func (rc *ResourceCache) generateCacheKey(selector metav1.LabelSelector, resourceTypes []string, namespace string) string {
	selectorStr := ""
	if len(selector.MatchLabels) > 0 || len(selector.MatchExpressions) > 0 {
		if labelSelector, err := metav1.LabelSelectorAsSelector(&selector); err == nil {
			selectorStr = labelSelector.String()
		}
	}

	return fmt.Sprintf("%s:%s:%v", namespace, selectorStr, resourceTypes)
}

// evictOldestEntry removes the oldest entry from cache
func (rc *ResourceCache) evictOldestEntry() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range rc.cache {
		if oldestKey == "" || entry.LastUpdated.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.LastUpdated
		}
	}

	if oldestKey != "" {
		delete(rc.cache, oldestKey)
		rc.evictions++
		rc.log.V(2).Info("Evicted oldest cache entry", "key", oldestKey)
	}
}

// keyContainsResourceType checks if a cache key contains a specific resource type
func (rc *ResourceCache) keyContainsResourceType(key, resourceType string) bool {
	// This is a simple implementation - in practice you might want more sophisticated matching
	return strings.Contains(key, resourceType) // Check if the key contains the resource type
}

// StartCleanupRoutine starts a background routine to clean up expired entries
func (rc *ResourceCache) StartCleanupRoutine(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	rc.log.Info("Started cache cleanup routine", "interval", interval)

	for {
		select {
		case <-ctx.Done():
			rc.log.Info("Cache cleanup routine stopped")
			return
		case <-ticker.C:
			rc.InvalidateExpiredEntries()
		}
	}
}
