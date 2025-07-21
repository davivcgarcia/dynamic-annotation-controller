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
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

func init() {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))
}

// BenchmarkResourceCachePerformance tests cache performance with different cache sizes and TTLs
func BenchmarkResourceCachePerformance(b *testing.B) {
	log := logf.Log.WithName("benchmark")

	testCases := []struct {
		name       string
		ttl        time.Duration
		maxEntries int
		resources  int
	}{
		{"SmallCache_ShortTTL", 1 * time.Minute, 100, 50},
		{"MediumCache_MediumTTL", 5 * time.Minute, 500, 250},
		{"LargeCache_LongTTL", 15 * time.Minute, 1000, 500},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			cache := NewResourceCache(tc.ttl, tc.maxEntries, log)

			// Create test resources
			resources := createTestPods(tc.resources, "test-namespace")
			selector := metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			}
			resourceTypes := []string{"pods"}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					// Try to get from cache
					if _, found := cache.GetCachedResources(selector, resourceTypes, "test-namespace"); !found {
						// Cache miss - store resources
						cache.CacheResources(selector, resourceTypes, "test-namespace", resources)
					}
				}
			})
		})
	}
}

// BenchmarkRateLimiterPerformance tests rate limiter performance under high load
func BenchmarkRateLimiterPerformance(b *testing.B) {
	log := logf.Log.WithName("benchmark")

	testCases := []struct {
		name        string
		config      *RateLimiterConfig
		concurrency int
	}{
		{
			name: "LowLimits_LowConcurrency",
			config: &RateLimiterConfig{
				GlobalApplyRate:  10,
				GlobalApplyBurst: 20,
			},
			concurrency: 5,
		},
		{
			name: "MediumLimits_MediumConcurrency",
			config: &RateLimiterConfig{
				GlobalApplyRate:  50,
				GlobalApplyBurst: 100,
			},
			concurrency: 20,
		},
		{
			name: "HighLimits_HighConcurrency",
			config: &RateLimiterConfig{
				GlobalApplyRate:  200,
				GlobalApplyBurst: 400,
			},
			concurrency: 50,
		},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			rateLimiter := NewAnnotationRateLimiter(tc.config, log)
			ctx := context.Background()

			b.ResetTimer()
			b.SetParallelism(tc.concurrency)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if err := rateLimiter.Wait(ctx, OperationTypeApply, "test-namespace"); err != nil {
						b.Errorf("Rate limiter wait failed: %v", err)
					}
				}
			})
		})
	}
}

// BenchmarkResourceManagerScaling tests ResourceManager performance with increasing resource counts
func BenchmarkResourceManagerScaling(b *testing.B) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = schedulerv1.AddToScheme(scheme)

	log := logf.Log.WithName("benchmark")

	resourceCounts := []int{100, 500, 1000, 5000}

	for _, count := range resourceCounts {
		b.Run(fmt.Sprintf("Resources_%d", count), func(b *testing.B) {
			// Create fake client with test resources
			resources := createTestResourcesForClient(count)
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(resources...).
				Build()

			rm := NewResourceManager(fakeClient, log, scheme)

			selector := metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			}
			resourceTypes := []string{"pods", "deployments"}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := rm.GetMatchingResources(context.Background(), selector, resourceTypes, "")
				if err != nil {
					b.Errorf("GetMatchingResources failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkConcurrentAnnotationOperations tests concurrent annotation apply/remove operations
func BenchmarkConcurrentAnnotationOperations(b *testing.B) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = schedulerv1.AddToScheme(scheme)

	log := logf.Log.WithName("benchmark")

	// Create fake client with test resources
	resources := createTestResourcesForClient(1000)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(resources...).
		Build()

	rm := NewResourceManager(fakeClient, log, scheme)

	selector := metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "test"},
	}
	resourceTypes := []string{"pods"}
	annotations := map[string]string{
		"test.annotation/key1": "value1",
		"test.annotation/key2": "value2",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ruleID := fmt.Sprintf("test-rule-%d", time.Now().UnixNano())
		for pb.Next() {
			// Apply annotations
			err := rm.ApplyAnnotations(context.Background(), selector, annotations, resourceTypes, "test-namespace", ruleID)
			if err != nil {
				b.Errorf("ApplyAnnotations failed: %v", err)
			}

			// Remove annotations
			keys := make([]string, 0, len(annotations))
			for key := range annotations {
				keys = append(keys, key)
			}
			err = rm.RemoveAnnotations(context.Background(), selector, keys, resourceTypes, "test-namespace", ruleID)
			if err != nil {
				b.Errorf("RemoveAnnotations failed: %v", err)
			}
		}
	})
}

// TestCacheEffectiveness tests cache hit rates under different scenarios
func TestCacheEffectiveness(t *testing.T) {
	log := logf.Log.WithName("test")
	cache := NewResourceCache(5*time.Minute, 1000, log)

	resources := createTestPods(100, "test-namespace")
	selector := metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "test"},
	}
	resourceTypes := []string{"pods"}

	// First access - should be a miss
	if _, found := cache.GetCachedResources(selector, resourceTypes, "test-namespace"); found {
		t.Error("Expected cache miss on first access")
	}

	// Cache the resources
	cache.CacheResources(selector, resourceTypes, "test-namespace", resources)

	// Second access - should be a hit
	if _, found := cache.GetCachedResources(selector, resourceTypes, "test-namespace"); !found {
		t.Error("Expected cache hit on second access")
	}

	// Check cache stats
	hits, misses, _, _ := cache.GetCacheStats()
	if hits != 1 {
		t.Errorf("Expected 1 hit, got %d", hits)
	}
	if misses != 1 {
		t.Errorf("Expected 1 miss, got %d", misses)
	}
}

// TestRateLimiterUnderLoad tests rate limiter behavior under high concurrent load
func TestRateLimiterUnderLoad(t *testing.T) {
	log := logf.Log.WithName("test")
	config := &RateLimiterConfig{
		GlobalApplyRate:  10, // 10 ops/sec
		GlobalApplyBurst: 20, // burst of 20
	}

	rateLimiter := NewAnnotationRateLimiter(config, log)

	// Launch multiple goroutines to test concurrent access
	concurrency := 50
	operations := 100

	var wg sync.WaitGroup
	var successCount int64
	var mu sync.Mutex

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				if rateLimiter.Allow(OperationTypeApply, "test-namespace") {
					mu.Lock()
					successCount++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	total, throttled, _ := rateLimiter.GetStats()
	expectedTotal := int64(concurrency * operations)

	if total != expectedTotal {
		t.Errorf("Expected %d total requests, got %d", expectedTotal, total)
	}

	if throttled == 0 {
		t.Error("Expected some requests to be throttled")
	}

	t.Logf("Rate limiter test completed in %v", duration)
	t.Logf("Total requests: %d, Throttled: %d, Success rate: %.2f%%",
		total, throttled, float64(successCount)/float64(total)*100)
}

// TestCleanupPerformance tests cleanup performance with large numbers of resources
func TestCleanupPerformance(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = schedulerv1.AddToScheme(scheme)

	log := logf.Log.WithName("test")

	// Create resources with orphaned annotations
	resources := createResourcesWithOrphanedAnnotations(1000)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(resources...).
		Build()

	cleanup := NewOrphanedAnnotationCleanup(fakeClient, log)

	start := time.Now()
	err := cleanup.CleanupOrphanedAnnotations(context.Background())
	duration := time.Since(start)

	if err != nil {
		t.Errorf("Cleanup failed: %v", err)
	}

	t.Logf("Cleanup of 1000 resources completed in %v", duration)

	// Verify cleanup was effective
	stats, err := cleanup.GetCleanupStats(context.Background())
	if err != nil {
		t.Errorf("Failed to get cleanup stats: %v", err)
	}

	t.Logf("Cleanup stats: %+v", stats)
}

// Helper functions

func createTestPods(count int, namespace string) []client.Object {
	resources := make([]client.Object, count)
	for i := 0; i < count; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-pod-%d", i),
				Namespace: namespace,
				Labels: map[string]string{
					"app": "test",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "test-image",
					},
				},
			},
		}
		resources[i] = pod
	}
	return resources
}

func createTestResourcesForClient(count int) []client.Object {
	resources := make([]client.Object, 0, count*2) // pods + deployments

	// Create pods
	for i := 0; i < count; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-pod-%d", i),
				Namespace: "test-namespace",
				Labels: map[string]string{
					"app": "test",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "test-image",
					},
				},
			},
		}
		resources = append(resources, pod)
	}

	// Create deployments
	for i := 0; i < count/10; i++ { // Fewer deployments than pods
		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-deployment-%d", i),
				Namespace: "test-namespace",
				Labels: map[string]string{
					"app": "test",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: int32Ptr(1),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "test",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "test",
						},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  "test-container",
								Image: "test-image",
							},
						},
					},
				},
			},
		}
		resources = append(resources, deployment)
	}

	return resources
}

func createResourcesWithOrphanedAnnotations(count int) []client.Object {
	resources := make([]client.Object, count)

	for i := 0; i < count; i++ {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-pod-%d", i),
				Namespace: "test-namespace",
				Labels: map[string]string{
					"app": "test",
				},
				Annotations: map[string]string{
					// Orphaned annotation from non-existent rule
					fmt.Sprintf("scheduler.k8s.io/rule-nonexistent-rule-%d-annotations", i): "[test.annotation/key1 test.annotation/key2]",
					"other.annotation/key": "value",
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "test-image",
					},
				},
			},
		}
		resources[i] = pod
	}

	return resources
}

func int32Ptr(i int32) *int32 {
	return &i
}
