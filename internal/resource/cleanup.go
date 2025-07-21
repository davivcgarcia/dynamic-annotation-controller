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
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

// OrphanedAnnotationCleanup handles cleanup of annotations left by deleted rules
type OrphanedAnnotationCleanup struct {
	client.Client
	log logr.Logger

	// Configuration
	cleanupInterval time.Duration
	maxAge          time.Duration
	batchSize       int
}

// NewOrphanedAnnotationCleanup creates a new cleanup manager
func NewOrphanedAnnotationCleanup(client client.Client, log logr.Logger) *OrphanedAnnotationCleanup {
	return &OrphanedAnnotationCleanup{
		Client:          client,
		log:             log,
		cleanupInterval: 30 * time.Minute, // Run cleanup every 30 minutes
		maxAge:          24 * time.Hour,   // Clean up annotations older than 24 hours
		batchSize:       100,              // Process 100 resources at a time
	}
}

// StartCleanupRoutine starts the background cleanup routine
func (oac *OrphanedAnnotationCleanup) StartCleanupRoutine(ctx context.Context) {
	ticker := time.NewTicker(oac.cleanupInterval)
	defer ticker.Stop()

	oac.log.Info("Started orphaned annotation cleanup routine",
		"interval", oac.cleanupInterval,
		"maxAge", oac.maxAge)

	for {
		select {
		case <-ctx.Done():
			oac.log.Info("Orphaned annotation cleanup routine stopped")
			return
		case <-ticker.C:
			if err := oac.CleanupOrphanedAnnotations(ctx); err != nil {
				oac.log.Error(err, "Failed to cleanup orphaned annotations")
			}
		}
	}
}

// CleanupOrphanedAnnotations removes annotations from resources where the corresponding rule no longer exists
func (oac *OrphanedAnnotationCleanup) CleanupOrphanedAnnotations(ctx context.Context) error {
	oac.log.Info("Starting orphaned annotation cleanup")

	// Get all existing rules to build a set of valid rule IDs
	validRuleIDs, err := oac.getValidRuleIDs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get valid rule IDs: %w", err)
	}

	oac.log.V(1).Info("Found valid rules", "count", len(validRuleIDs))

	// Clean up orphaned annotations from different resource types
	resourceTypes := []string{"pods", "deployments", "statefulsets", "daemonsets"}
	totalCleaned := 0

	for _, resourceType := range resourceTypes {
		cleaned, err := oac.cleanupResourceType(ctx, resourceType, validRuleIDs)
		if err != nil {
			oac.log.Error(err, "Failed to cleanup resource type", "resourceType", resourceType)
			continue
		}
		totalCleaned += cleaned
	}

	oac.log.Info("Completed orphaned annotation cleanup", "totalCleaned", totalCleaned)
	return nil
}

// getValidRuleIDs returns a set of all valid rule IDs currently in the cluster
func (oac *OrphanedAnnotationCleanup) getValidRuleIDs(ctx context.Context) (map[string]bool, error) {
	ruleList := &schedulerv1.AnnotationScheduleRuleList{}
	if err := oac.List(ctx, ruleList); err != nil {
		return nil, fmt.Errorf("failed to list annotation schedule rules: %w", err)
	}

	validRuleIDs := make(map[string]bool)
	for _, rule := range ruleList.Items {
		ruleID := fmt.Sprintf("%s/%s", rule.Namespace, rule.Name)
		validRuleIDs[ruleID] = true
	}

	return validRuleIDs, nil
}

// cleanupResourceType cleans up orphaned annotations from a specific resource type
func (oac *OrphanedAnnotationCleanup) cleanupResourceType(ctx context.Context, resourceType string, validRuleIDs map[string]bool) (int, error) {
	oac.log.V(1).Info("Cleaning up resource type", "resourceType", resourceType)

	// Get all resources of this type that have scheduler annotations
	resources, err := oac.getResourcesWithSchedulerAnnotations(ctx, resourceType)
	if err != nil {
		return 0, fmt.Errorf("failed to get resources with scheduler annotations: %w", err)
	}

	cleanedCount := 0

	// Process resources in batches
	for i := 0; i < len(resources); i += oac.batchSize {
		end := i + oac.batchSize
		if end > len(resources) {
			end = len(resources)
		}

		batch := resources[i:end]
		batchCleaned, err := oac.cleanupResourceBatch(ctx, batch, validRuleIDs)
		if err != nil {
			oac.log.Error(err, "Failed to cleanup resource batch",
				"resourceType", resourceType,
				"batchStart", i,
				"batchSize", len(batch))
			continue
		}

		cleanedCount += batchCleaned
	}

	oac.log.V(1).Info("Completed cleanup for resource type",
		"resourceType", resourceType,
		"cleanedCount", cleanedCount)

	return cleanedCount, nil
}

// getResourcesWithSchedulerAnnotations gets resources that have scheduler-related annotations
func (oac *OrphanedAnnotationCleanup) getResourcesWithSchedulerAnnotations(ctx context.Context, resourceType string) ([]client.Object, error) {
	// We can't use field selectors to filter by annotations, so we need to get all resources
	// and filter them in memory. This is not ideal for large clusters, but necessary.

	listOpts := []client.ListOption{}

	switch resourceType {
	case "pods":
		return oac.getPodsWithSchedulerAnnotations(ctx, listOpts)
	case "deployments":
		return oac.getDeploymentsWithSchedulerAnnotations(ctx, listOpts)
	case "statefulsets":
		return oac.getStatefulSetsWithSchedulerAnnotations(ctx, listOpts)
	case "daemonsets":
		return oac.getDaemonSetsWithSchedulerAnnotations(ctx, listOpts)
	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

// getPodsWithSchedulerAnnotations gets pods that have scheduler annotations
func (oac *OrphanedAnnotationCleanup) getPodsWithSchedulerAnnotations(ctx context.Context, listOpts []client.ListOption) ([]client.Object, error) {
	podList := &corev1.PodList{}
	if err := oac.List(ctx, podList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var resources []client.Object
	for i := range podList.Items {
		pod := &podList.Items[i]
		if oac.hasSchedulerAnnotations(pod) {
			resources = append(resources, pod)
		}
	}

	return resources, nil
}

// getDeploymentsWithSchedulerAnnotations gets deployments that have scheduler annotations
func (oac *OrphanedAnnotationCleanup) getDeploymentsWithSchedulerAnnotations(ctx context.Context, listOpts []client.ListOption) ([]client.Object, error) {
	deploymentList := &appsv1.DeploymentList{}
	if err := oac.List(ctx, deploymentList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	var resources []client.Object
	for i := range deploymentList.Items {
		deployment := &deploymentList.Items[i]
		if oac.hasSchedulerAnnotations(deployment) {
			resources = append(resources, deployment)
		}
	}

	return resources, nil
}

// getStatefulSetsWithSchedulerAnnotations gets statefulsets that have scheduler annotations
func (oac *OrphanedAnnotationCleanup) getStatefulSetsWithSchedulerAnnotations(ctx context.Context, listOpts []client.ListOption) ([]client.Object, error) {
	statefulSetList := &appsv1.StatefulSetList{}
	if err := oac.List(ctx, statefulSetList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list statefulsets: %w", err)
	}

	var resources []client.Object
	for i := range statefulSetList.Items {
		statefulSet := &statefulSetList.Items[i]
		if oac.hasSchedulerAnnotations(statefulSet) {
			resources = append(resources, statefulSet)
		}
	}

	return resources, nil
}

// getDaemonSetsWithSchedulerAnnotations gets daemonsets that have scheduler annotations
func (oac *OrphanedAnnotationCleanup) getDaemonSetsWithSchedulerAnnotations(ctx context.Context, listOpts []client.ListOption) ([]client.Object, error) {
	daemonSetList := &appsv1.DaemonSetList{}
	if err := oac.List(ctx, daemonSetList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list daemonsets: %w", err)
	}

	var resources []client.Object
	for i := range daemonSetList.Items {
		daemonSet := &daemonSetList.Items[i]
		if oac.hasSchedulerAnnotations(daemonSet) {
			resources = append(resources, daemonSet)
		}
	}

	return resources, nil
}

// hasSchedulerAnnotations checks if a resource has any scheduler-related annotations
func (oac *OrphanedAnnotationCleanup) hasSchedulerAnnotations(resource client.Object) bool {
	annotations := resource.GetAnnotations()
	if annotations == nil {
		return false
	}

	for key := range annotations {
		if strings.HasPrefix(key, "scheduler.k8s.io/rule-") {
			return true
		}
	}

	return false
}

// cleanupResourceBatch cleans up orphaned annotations from a batch of resources
func (oac *OrphanedAnnotationCleanup) cleanupResourceBatch(ctx context.Context, resources []client.Object, validRuleIDs map[string]bool) (int, error) {
	cleanedCount := 0

	for _, resource := range resources {
		cleaned, err := oac.cleanupResourceAnnotations(ctx, resource, validRuleIDs)
		if err != nil {
			oac.log.Error(err, "Failed to cleanup resource annotations",
				"resource", fmt.Sprintf("%s/%s", resource.GetNamespace(), resource.GetName()))
			continue
		}

		if cleaned {
			cleanedCount++
		}
	}

	return cleanedCount, nil
}

// cleanupResourceAnnotations removes orphaned annotations from a single resource
func (oac *OrphanedAnnotationCleanup) cleanupResourceAnnotations(ctx context.Context, resource client.Object, validRuleIDs map[string]bool) (bool, error) {
	annotations := resource.GetAnnotations()
	if annotations == nil {
		return false, nil
	}

	// Find orphaned scheduler annotations
	orphanedKeys := make([]string, 0)
	for key := range annotations {
		if strings.HasPrefix(key, "scheduler.k8s.io/rule-") {
			// Extract rule ID from the annotation key
			// Format: scheduler.k8s.io/rule-{ruleID}-annotations
			parts := strings.Split(key, "-")
			if len(parts) >= 3 {
				// Reconstruct rule ID (handle cases where rule ID contains hyphens)
				ruleIDParts := parts[2 : len(parts)-1] // Remove "scheduler.k8s.io/rule" and "annotations"
				ruleID := strings.Join(ruleIDParts, "-")

				if !validRuleIDs[ruleID] {
					orphanedKeys = append(orphanedKeys, key)
					oac.log.V(2).Info("Found orphaned annotation",
						"resource", fmt.Sprintf("%s/%s", resource.GetNamespace(), resource.GetName()),
						"annotation", key,
						"ruleID", ruleID)
				}
			}
		}
	}

	if len(orphanedKeys) == 0 {
		return false, nil
	}

	// Remove orphaned annotations
	resourceCopy := resource.DeepCopyObject().(client.Object)
	currentAnnotations := resourceCopy.GetAnnotations()

	for _, key := range orphanedKeys {
		delete(currentAnnotations, key)
	}

	resourceCopy.SetAnnotations(currentAnnotations)

	// Update the resource
	if err := oac.Update(ctx, resourceCopy); err != nil {
		return false, fmt.Errorf("failed to update resource: %w", err)
	}

	oac.log.Info("Cleaned up orphaned annotations",
		"resource", fmt.Sprintf("%s/%s", resource.GetNamespace(), resource.GetName()),
		"cleanedCount", len(orphanedKeys))

	return true, nil
}

// GetCleanupStats returns statistics about the cleanup process
func (oac *OrphanedAnnotationCleanup) GetCleanupStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Count resources with scheduler annotations by type
	resourceTypes := []string{"pods", "deployments", "statefulsets", "daemonsets"}
	for _, resourceType := range resourceTypes {
		resources, err := oac.getResourcesWithSchedulerAnnotations(ctx, resourceType)
		if err != nil {
			oac.log.Error(err, "Failed to get resources for stats", "resourceType", resourceType)
			continue
		}
		stats[resourceType+"_with_scheduler_annotations"] = len(resources)
	}

	// Count total valid rules
	validRuleIDs, err := oac.getValidRuleIDs(ctx)
	if err != nil {
		oac.log.Error(err, "Failed to get valid rule IDs for stats")
	} else {
		stats["valid_rules"] = len(validRuleIDs)
	}

	return stats, nil
}
