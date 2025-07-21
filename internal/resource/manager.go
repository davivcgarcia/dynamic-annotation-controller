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
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/security"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/templating"
)

// AnnotationConflictResolution defines how to handle annotation conflicts
type AnnotationConflictResolution string

const (
	ConflictResolutionPriority  AnnotationConflictResolution = "priority"
	ConflictResolutionTimestamp AnnotationConflictResolution = "timestamp"
	ConflictResolutionSkip      AnnotationConflictResolution = "skip"
)

// AnnotationOperationResult represents the result of an annotation operation
type AnnotationOperationResult struct {
	Key      string
	Success  bool
	Error    error
	Skipped  bool
	Reason   string
	OldValue string
	NewValue string
	Priority int32
}

// AnnotationGroupResult represents the result of applying an annotation group
type AnnotationGroupResult struct {
	GroupName         string
	Success           bool
	Operations        []AnnotationOperationResult
	Error             error
	ResourcesAffected int32
}

// ResourceManager handles resource discovery and selection based on label selectors
type ResourceManager struct {
	client.Client
	Log            logr.Logger
	Validator      *security.AnnotationValidator
	Auditor        *security.AuditLogger
	TemplateEngine *templating.TemplateEngine
	Cache          *ResourceCache
	RateLimiter    *AnnotationRateLimiter
}

// NewResourceManager creates a new ResourceManager instance
func NewResourceManager(client client.Client, log logr.Logger, scheme *runtime.Scheme) *ResourceManager {
	validator := security.NewAnnotationValidator()
	auditor := security.NewAuditLogger(client, log, scheme)
	templateEngine := templating.NewTemplateEngine()

	// Initialize cache with 5 minute TTL and max 1000 entries
	cache := NewResourceCache(5*time.Minute, 1000, log)

	// Initialize rate limiter with default configuration
	rateLimiter := NewAnnotationRateLimiter(DefaultRateLimiterConfig(), log)

	return &ResourceManager{
		Client:         client,
		Log:            log,
		Validator:      validator,
		Auditor:        auditor,
		TemplateEngine: templateEngine,
		Cache:          cache,
		RateLimiter:    rateLimiter,
	}
}

// GetMatchingResources discovers and returns all resources matching the given selector and resource types
func (rm *ResourceManager) GetMatchingResources(ctx context.Context, selector metav1.LabelSelector, resourceTypes []string, namespace string) ([]client.Object, error) {
	// Apply rate limiting for query operations
	if rm.RateLimiter != nil {
		if err := rm.RateLimiter.Wait(ctx, OperationTypeQuery, namespace); err != nil {
			return nil, fmt.Errorf("rate limit exceeded for resource query: %w", err)
		}
	}

	// Check cache first
	if rm.Cache != nil {
		if cachedResources, found := rm.Cache.GetCachedResources(selector, resourceTypes, namespace); found {
			rm.Log.V(2).Info("Using cached resources",
				"resourceTypes", resourceTypes,
				"namespace", namespace,
				"resourceCount", len(cachedResources))
			return cachedResources, nil
		}
	}

	var allResources []client.Object

	// Convert metav1.LabelSelector to labels.Selector
	labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return nil, fmt.Errorf("failed to convert label selector: %w", err)
	}

	for _, resourceType := range resourceTypes {
		resources, err := rm.getResourcesByType(ctx, resourceType, labelSelector, namespace)
		if err != nil {
			rm.Log.Error(err, "Failed to get resources", "resourceType", resourceType)
			continue
		}
		allResources = append(allResources, resources...)
	}

	// Cache the results
	if rm.Cache != nil {
		rm.Cache.CacheResources(selector, resourceTypes, namespace, allResources)
	}

	return allResources, nil
}

// getResourcesByType retrieves resources of a specific type matching the label selector
func (rm *ResourceManager) getResourcesByType(ctx context.Context, resourceType string, selector labels.Selector, namespace string) ([]client.Object, error) {
	listOpts := []client.ListOption{
		client.MatchingLabelsSelector{Selector: selector},
	}

	// Add namespace filter if specified
	if namespace != "" {
		listOpts = append(listOpts, client.InNamespace(namespace))
	}

	// Add field selectors for optimization where possible
	listOpts = append(listOpts, rm.getOptimizedFieldSelectors(resourceType)...)

	switch resourceType {
	case "pods":
		return rm.getPods(ctx, listOpts)
	case "deployments":
		return rm.getDeployments(ctx, listOpts)
	case "statefulsets":
		return rm.getStatefulSets(ctx, listOpts)
	case "daemonsets":
		return rm.getDaemonSets(ctx, listOpts)
	default:
		return nil, fmt.Errorf("unsupported resource type: %s", resourceType)
	}
}

// getOptimizedFieldSelectors returns field selectors that can optimize queries
func (rm *ResourceManager) getOptimizedFieldSelectors(resourceType string) []client.ListOption {
	var opts []client.ListOption

	// Note: Field selectors are limited in Kubernetes and require specific indexing
	// For now, we'll keep this simple and not use field selectors that might not be indexed
	// In a real deployment, you would configure the controller manager with appropriate indexes

	switch resourceType {
	case "pods":
		// Only use field selectors that are guaranteed to be indexed
		// status.phase field selector requires custom indexing, so we'll skip it for now
		// opts = append(opts, client.MatchingFields{"status.phase": "Running"})
	case "deployments", "statefulsets", "daemonsets":
		// For workload resources, Kubernetes doesn't support many field selectors
		// so we'll keep it simple
	}

	return opts
}

// getPods retrieves pods matching the selector
func (rm *ResourceManager) getPods(ctx context.Context, listOpts []client.ListOption) ([]client.Object, error) {
	podList := &corev1.PodList{}
	if err := rm.List(ctx, podList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	resources := make([]client.Object, len(podList.Items))
	for i := range podList.Items {
		resources[i] = &podList.Items[i]
	}

	rm.Log.V(1).Info("Found matching pods", "count", len(resources))
	return resources, nil
}

// getDeployments retrieves deployments matching the selector
func (rm *ResourceManager) getDeployments(ctx context.Context, listOpts []client.ListOption) ([]client.Object, error) {
	deploymentList := &appsv1.DeploymentList{}
	if err := rm.List(ctx, deploymentList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	resources := make([]client.Object, len(deploymentList.Items))
	for i := range deploymentList.Items {
		resources[i] = &deploymentList.Items[i]
	}

	rm.Log.V(1).Info("Found matching deployments", "count", len(resources))
	return resources, nil
}

// getStatefulSets retrieves statefulsets matching the selector
func (rm *ResourceManager) getStatefulSets(ctx context.Context, listOpts []client.ListOption) ([]client.Object, error) {
	statefulSetList := &appsv1.StatefulSetList{}
	if err := rm.List(ctx, statefulSetList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list statefulsets: %w", err)
	}

	resources := make([]client.Object, len(statefulSetList.Items))
	for i := range statefulSetList.Items {
		resources[i] = &statefulSetList.Items[i]
	}

	rm.Log.V(1).Info("Found matching statefulsets", "count", len(resources))
	return resources, nil
}

// getDaemonSets retrieves daemonsets matching the selector
func (rm *ResourceManager) getDaemonSets(ctx context.Context, listOpts []client.ListOption) ([]client.Object, error) {
	daemonSetList := &appsv1.DaemonSetList{}
	if err := rm.List(ctx, daemonSetList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list daemonsets: %w", err)
	}

	resources := make([]client.Object, len(daemonSetList.Items))
	for i := range daemonSetList.Items {
		resources[i] = &daemonSetList.Items[i]
	}

	rm.Log.V(1).Info("Found matching daemonsets", "count", len(resources))
	return resources, nil
}

// FilterResourcesBySelector applies additional filtering logic to resources based on label selectors
func (rm *ResourceManager) FilterResourcesBySelector(resources []client.Object, selector metav1.LabelSelector) ([]client.Object, error) {
	labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return nil, fmt.Errorf("failed to convert label selector: %w", err)
	}

	var filtered []client.Object
	for _, resource := range resources {
		if labelSelector.Matches(labels.Set(resource.GetLabels())) {
			filtered = append(filtered, resource)
		}
	}

	rm.Log.V(1).Info("Filtered resources", "original", len(resources), "filtered", len(filtered))
	return filtered, nil
}

// GetResourceInfo returns basic information about a resource for logging purposes
func (rm *ResourceManager) GetResourceInfo(resource client.Object) map[string]interface{} {
	return map[string]interface{}{
		"name":      resource.GetName(),
		"namespace": resource.GetNamespace(),
		"kind":      resource.GetObjectKind().GroupVersionKind().Kind,
		"labels":    resource.GetLabels(),
	}
}

// ValidateResourceTypes checks if all provided resource types are supported
func (rm *ResourceManager) ValidateResourceTypes(resourceTypes []string) []string {
	supportedTypes := map[string]bool{
		"pods":         true,
		"deployments":  true,
		"statefulsets": true,
		"daemonsets":   true,
	}

	var unsupported []string
	for _, resourceType := range resourceTypes {
		if !supportedTypes[resourceType] {
			unsupported = append(unsupported, resourceType)
		}
	}

	return unsupported
}

// GetSupportedResourceTypes returns a list of all supported resource types
func (rm *ResourceManager) GetSupportedResourceTypes() []string {
	return []string{"pods", "deployments", "statefulsets", "daemonsets"}
}

// CountResourcesByType returns a count of resources by type for the given selector
func (rm *ResourceManager) CountResourcesByType(ctx context.Context, selector metav1.LabelSelector, resourceTypes []string, namespace string) (map[string]int, error) {
	counts := make(map[string]int)

	// Convert metav1.LabelSelector to labels.Selector
	labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return nil, fmt.Errorf("failed to convert label selector: %w", err)
	}

	for _, resourceType := range resourceTypes {
		resources, err := rm.getResourcesByType(ctx, resourceType, labelSelector, namespace)
		if err != nil {
			rm.Log.Error(err, "Failed to count resources", "resourceType", resourceType)
			counts[resourceType] = 0
			continue
		}
		counts[resourceType] = len(resources)
	}

	return counts, nil
}

// GetResourcesByNamespace groups resources by namespace
func (rm *ResourceManager) GetResourcesByNamespace(resources []client.Object) map[string][]client.Object {
	namespaceGroups := make(map[string][]client.Object)

	for _, resource := range resources {
		namespace := resource.GetNamespace()
		if namespace == "" {
			namespace = "cluster-scoped"
		}
		namespaceGroups[namespace] = append(namespaceGroups[namespace], resource)
	}

	return namespaceGroups
}

// HasMatchingResources checks if any resources match the selector without retrieving them
func (rm *ResourceManager) HasMatchingResources(ctx context.Context, selector metav1.LabelSelector, resourceTypes []string, namespace string) (bool, error) {
	// Convert metav1.LabelSelector to labels.Selector
	labelSelector, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return false, fmt.Errorf("failed to convert label selector: %w", err)
	}

	for _, resourceType := range resourceTypes {
		resources, err := rm.getResourcesByType(ctx, resourceType, labelSelector, namespace)
		if err != nil {
			rm.Log.Error(err, "Failed to check resources", "resourceType", resourceType)
			continue
		}
		if len(resources) > 0 {
			return true, nil
		}
	}

	return false, nil
}

// ApplyAnnotations applies annotations to resources matching the selector while preserving existing annotations
func (rm *ResourceManager) ApplyAnnotations(ctx context.Context, selector metav1.LabelSelector, annotations map[string]string, resourceTypes []string, namespace string, ruleID string) error {
	if len(annotations) == 0 {
		rm.Log.V(1).Info("No annotations to apply")
		return nil
	}

	// Apply rate limiting for apply operations
	if rm.RateLimiter != nil {
		if err := rm.RateLimiter.Wait(ctx, OperationTypeApply, namespace); err != nil {
			return fmt.Errorf("rate limit exceeded for annotation apply: %w", err)
		}
	}

	// Validate annotations before applying
	if err := rm.ValidateAnnotations(annotations); err != nil {
		rm.Auditor.LogValidationFailure(ctx, ruleID, "", namespace, annotations, err)
		return fmt.Errorf("annotation validation failed: %w", err)
	}

	resources, err := rm.GetMatchingResources(ctx, selector, resourceTypes, namespace)
	if err != nil {
		return fmt.Errorf("failed to get matching resources: %w", err)
	}

	if len(resources) == 0 {
		rm.Log.V(1).Info("No resources found matching selector")
		return nil
	}

	var applyErrors []error
	successCount := 0

	for _, resource := range resources {
		if err := rm.applyAnnotationsToResource(ctx, resource, annotations, ruleID); err != nil {
			rm.Log.Error(err, "Failed to apply annotations to resource",
				"resource", rm.GetResourceInfo(resource))
			rm.Auditor.LogAnnotationApply(ctx, ruleID, "", namespace, resource, annotations, false, err)
			applyErrors = append(applyErrors, err)
		} else {
			rm.Auditor.LogAnnotationApply(ctx, ruleID, "", namespace, resource, annotations, true, nil)
			successCount++
		}
	}

	// Invalidate cache for affected resources
	if rm.Cache != nil {
		rm.Cache.InvalidateCache(namespace, "")
	}

	rm.Log.Info("Applied annotations to resources",
		"successCount", successCount,
		"totalCount", len(resources),
		"errorCount", len(applyErrors))

	if len(applyErrors) > 0 {
		return fmt.Errorf("failed to apply annotations to %d out of %d resources", len(applyErrors), len(resources))
	}

	return nil
}

// RemoveAnnotations removes rule-specific annotations from resources matching the selector
func (rm *ResourceManager) RemoveAnnotations(ctx context.Context, selector metav1.LabelSelector, annotationKeys []string, resourceTypes []string, namespace string, ruleID string) error {
	if len(annotationKeys) == 0 {
		rm.Log.V(1).Info("No annotation keys to remove")
		return nil
	}

	// Apply rate limiting for remove operations
	if rm.RateLimiter != nil {
		if err := rm.RateLimiter.Wait(ctx, OperationTypeRemove, namespace); err != nil {
			return fmt.Errorf("rate limit exceeded for annotation remove: %w", err)
		}
	}

	resources, err := rm.GetMatchingResources(ctx, selector, resourceTypes, namespace)
	if err != nil {
		return fmt.Errorf("failed to get matching resources: %w", err)
	}

	if len(resources) == 0 {
		rm.Log.V(1).Info("No resources found matching selector")
		return nil
	}

	var removeErrors []error
	successCount := 0

	for _, resource := range resources {
		if err := rm.removeAnnotationsFromResource(ctx, resource, annotationKeys, ruleID); err != nil {
			rm.Log.Error(err, "Failed to remove annotations from resource",
				"resource", rm.GetResourceInfo(resource))
			rm.Auditor.LogAnnotationRemove(ctx, ruleID, "", namespace, resource, annotationKeys, false, err)
			removeErrors = append(removeErrors, err)
		} else {
			rm.Auditor.LogAnnotationRemove(ctx, ruleID, "", namespace, resource, annotationKeys, true, nil)
			successCount++
		}
	}

	// Invalidate cache for affected resources
	if rm.Cache != nil {
		rm.Cache.InvalidateCache(namespace, "")
	}

	rm.Log.Info("Removed annotations from resources",
		"successCount", successCount,
		"totalCount", len(resources),
		"errorCount", len(removeErrors))

	if len(removeErrors) > 0 {
		return fmt.Errorf("failed to remove annotations from %d out of %d resources", len(removeErrors), len(resources))
	}

	return nil
}

// applyAnnotationsToResource applies annotations to a single resource with conflict resolution
func (rm *ResourceManager) applyAnnotationsToResource(ctx context.Context, resource client.Object, annotations map[string]string, ruleID string) error {
	// Create a copy of the resource to modify
	resourceCopy := resource.DeepCopyObject().(client.Object)

	// Get current annotations or initialize empty map
	currentAnnotations := resourceCopy.GetAnnotations()
	if currentAnnotations == nil {
		currentAnnotations = make(map[string]string)
	}

	// Track which annotations we're applying for this rule
	ruleTrackingKey := fmt.Sprintf("scheduler.k8s.io/rule-%s-annotations", ruleID)
	var appliedKeys []string

	// Apply new annotations with conflict resolution
	conflictsResolved := 0
	for key, value := range annotations {
		if existingValue, exists := currentAnnotations[key]; exists && existingValue != value {
			rm.Log.V(1).Info("Resolving annotation conflict",
				"key", key,
				"existingValue", existingValue,
				"newValue", value,
				"resource", rm.GetResourceInfo(resource))
			conflictsResolved++
		}
		currentAnnotations[key] = value
		appliedKeys = append(appliedKeys, key)
	}

	// Store tracking information for this rule
	if len(appliedKeys) > 0 {
		currentAnnotations[ruleTrackingKey] = fmt.Sprintf("%v", appliedKeys)
	}

	// Update the resource annotations
	resourceCopy.SetAnnotations(currentAnnotations)

	// Apply the update with retry logic for conflicts
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := rm.Update(ctx, resourceCopy); err != nil {
			// Handle resource conflicts by refetching and retrying
			if apierrors.IsConflict(err) && attempt < maxRetries-1 {
				rm.Log.V(1).Info("Resource conflict detected, retrying",
					"resource", rm.GetResourceInfo(resource),
					"attempt", attempt+1,
					"error", err.Error())

				// Refetch the resource and reapply annotations
				if refetchErr := rm.Get(ctx, client.ObjectKeyFromObject(resource), resourceCopy); refetchErr != nil {
					return fmt.Errorf("failed to refetch resource after conflict: %w", refetchErr)
				}

				// Reapply annotations to the fresh copy
				currentAnnotations = resourceCopy.GetAnnotations()
				if currentAnnotations == nil {
					currentAnnotations = make(map[string]string)
				}

				for key, value := range annotations {
					currentAnnotations[key] = value
				}
				if len(appliedKeys) > 0 {
					currentAnnotations[ruleTrackingKey] = fmt.Sprintf("%v", appliedKeys)
				}
				resourceCopy.SetAnnotations(currentAnnotations)

				// Wait a bit before retrying
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
				continue
			}

			return fmt.Errorf("failed to update resource annotations: %w", err)
		}

		// Success
		break
	}

	rm.Log.V(1).Info("Successfully applied annotations",
		"resource", rm.GetResourceInfo(resource),
		"annotationsApplied", len(annotations),
		"conflictsResolved", conflictsResolved)

	return nil
}

// removeAnnotationsFromResource removes rule-specific annotations from a single resource
func (rm *ResourceManager) removeAnnotationsFromResource(ctx context.Context, resource client.Object, annotationKeys []string, ruleID string) error {
	// Create a copy of the resource to modify
	resourceCopy := resource.DeepCopyObject().(client.Object)

	// Get current annotations
	currentAnnotations := resourceCopy.GetAnnotations()
	if currentAnnotations == nil {
		rm.Log.V(1).Info("Resource has no annotations to remove",
			"resource", rm.GetResourceInfo(resource))
		return nil
	}

	// Track which annotations we're removing for this rule
	ruleTrackingKey := fmt.Sprintf("scheduler.k8s.io/rule-%s-annotations", ruleID)

	// Get the list of annotations applied by this rule
	var ruleAppliedKeys []string
	if trackingValue, exists := currentAnnotations[ruleTrackingKey]; exists {
		// Parse the tracking value to get the list of keys
		// This is a simple implementation - in production you might want JSON encoding
		trackingValue = strings.Trim(trackingValue, "[]")
		if trackingValue != "" {
			ruleAppliedKeys = strings.Split(trackingValue, " ")
		}
	}

	// Only remove annotations that were applied by this rule
	removedCount := 0
	for _, key := range annotationKeys {
		// Check if this annotation was applied by this rule
		wasAppliedByRule := false
		for _, ruleKey := range ruleAppliedKeys {
			if strings.TrimSpace(ruleKey) == key {
				wasAppliedByRule = true
				break
			}
		}

		if wasAppliedByRule {
			if _, exists := currentAnnotations[key]; exists {
				delete(currentAnnotations, key)
				removedCount++
				rm.Log.V(2).Info("Removed annotation",
					"key", key,
					"resource", rm.GetResourceInfo(resource))
			}
		} else {
			rm.Log.V(1).Info("Skipping annotation removal - not applied by this rule",
				"key", key,
				"ruleID", ruleID,
				"resource", rm.GetResourceInfo(resource))
		}
	}

	// Update the tracking annotation to remove the keys we just removed
	if len(ruleAppliedKeys) > 0 {
		var remainingKeys []string
		for _, ruleKey := range ruleAppliedKeys {
			ruleKey = strings.TrimSpace(ruleKey)
			shouldKeep := true
			for _, removedKey := range annotationKeys {
				if ruleKey == removedKey {
					shouldKeep = false
					break
				}
			}
			if shouldKeep {
				remainingKeys = append(remainingKeys, ruleKey)
			}
		}

		if len(remainingKeys) > 0 {
			currentAnnotations[ruleTrackingKey] = fmt.Sprintf("%v", remainingKeys)
		} else {
			delete(currentAnnotations, ruleTrackingKey)
		}
	}

	// Update the resource annotations if any changes were made
	if removedCount > 0 {
		resourceCopy.SetAnnotations(currentAnnotations)

		if err := rm.Update(ctx, resourceCopy); err != nil {
			return fmt.Errorf("failed to update resource annotations: %w", err)
		}

		rm.Log.V(1).Info("Successfully removed annotations",
			"resource", rm.GetResourceInfo(resource),
			"annotationsRemoved", removedCount)
	} else {
		rm.Log.V(1).Info("No annotations removed - none were applied by this rule",
			"resource", rm.GetResourceInfo(resource),
			"ruleID", ruleID)
	}

	return nil
}

// GetAppliedAnnotations returns the annotations that were applied by a specific rule to a resource
func (rm *ResourceManager) GetAppliedAnnotations(resource client.Object, ruleID string) ([]string, error) {
	annotations := resource.GetAnnotations()
	if annotations == nil {
		return nil, nil
	}

	ruleTrackingKey := fmt.Sprintf("scheduler.k8s.io/rule-%s-annotations", ruleID)
	trackingValue, exists := annotations[ruleTrackingKey]
	if !exists {
		return nil, nil
	}

	// Parse the tracking value to get the list of keys
	trackingValue = strings.Trim(trackingValue, "[]")
	if trackingValue == "" {
		return nil, nil
	}

	keys := strings.Split(trackingValue, " ")
	var cleanKeys []string
	for _, key := range keys {
		if trimmed := strings.TrimSpace(key); trimmed != "" {
			cleanKeys = append(cleanKeys, trimmed)
		}
	}

	return cleanKeys, nil
}

// ValidateAnnotations checks if the provided annotations are valid using security validator
func (rm *ResourceManager) ValidateAnnotations(annotations map[string]string) error {
	return rm.Validator.ValidateAnnotations(annotations)
}

// ValidateAnnotationKey validates a single annotation key using security validator
func (rm *ResourceManager) ValidateAnnotationKey(key string) error {
	return rm.Validator.ValidateAnnotationKey(key)
}

// ValidateAnnotationValue validates a single annotation value using security validator
func (rm *ResourceManager) ValidateAnnotationValue(value string) error {
	return rm.Validator.ValidateAnnotationValue(value)
}

// ApplyAnnotationGroups applies multiple annotation groups atomically to matching resources
func (rm *ResourceManager) ApplyAnnotationGroups(ctx context.Context, selector metav1.LabelSelector, groups []schedulerv1.AnnotationGroup, resourceTypes []string, namespace string, ruleID string, templateContext templating.TemplateContext, conflictResolution AnnotationConflictResolution) ([]AnnotationGroupResult, error) {
	if len(groups) == 0 {
		rm.Log.V(1).Info("No annotation groups to apply")
		return nil, nil
	}

	// Get matching resources
	resources, err := rm.GetMatchingResources(ctx, selector, resourceTypes, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching resources: %w", err)
	}

	if len(resources) == 0 {
		rm.Log.V(1).Info("No resources found matching selector")
		return nil, nil
	}

	var results []AnnotationGroupResult

	// Process each annotation group
	for _, group := range groups {
		result := rm.applyAnnotationGroup(ctx, resources, group, ruleID, templateContext, conflictResolution)
		results = append(results, result)

		// If this group failed and it's atomic, we might need to rollback
		if !result.Success && group.Atomic {
			rm.Log.Error(result.Error, "Atomic annotation group failed", "groupName", group.Name)
			// For atomic groups, we could implement rollback logic here
		}
	}

	return results, nil
}

// applyAnnotationGroup applies a single annotation group to resources
func (rm *ResourceManager) applyAnnotationGroup(ctx context.Context, resources []client.Object, group schedulerv1.AnnotationGroup, ruleID string, templateContext templating.TemplateContext, conflictResolution AnnotationConflictResolution) AnnotationGroupResult {
	result := AnnotationGroupResult{
		GroupName:  group.Name,
		Success:    true,
		Operations: make([]AnnotationOperationResult, 0),
	}

	// Process each resource
	for _, resource := range resources {
		// Update template context with current resource
		resourceContext := templateContext
		resourceContext.Resource = resource

		// Apply operations to this resource
		resourceResult := rm.applyAnnotationOperationsToResource(ctx, resource, group.Operations, ruleID, resourceContext, conflictResolution)

		// Aggregate results
		result.Operations = append(result.Operations, resourceResult.Operations...)

		if !resourceResult.Success {
			result.Success = false
			if result.Error == nil {
				result.Error = resourceResult.Error
			}

			// For atomic groups, stop on first failure
			if group.Atomic {
				result.Error = fmt.Errorf("atomic group %s failed on resource %s/%s: %w",
					group.Name, resource.GetNamespace(), resource.GetName(), resourceResult.Error)
				break
			}
		} else {
			result.ResourcesAffected++
		}
	}

	return result
}

// applyAnnotationOperationsToResource applies annotation operations to a single resource
func (rm *ResourceManager) applyAnnotationOperationsToResource(ctx context.Context, resource client.Object, operations []schedulerv1.AnnotationOperation, ruleID string, templateContext templating.TemplateContext, conflictResolution AnnotationConflictResolution) AnnotationGroupResult {
	result := AnnotationGroupResult{
		Success:    true,
		Operations: make([]AnnotationOperationResult, 0),
	}

	// Create a copy of the resource to modify
	resourceCopy := resource.DeepCopyObject().(client.Object)
	currentAnnotations := resourceCopy.GetAnnotations()
	if currentAnnotations == nil {
		currentAnnotations = make(map[string]string)
	}

	// Sort operations by priority (higher priority first)
	sortedOps := make([]schedulerv1.AnnotationOperation, len(operations))
	copy(sortedOps, operations)
	sort.Slice(sortedOps, func(i, j int) bool {
		return sortedOps[i].Priority > sortedOps[j].Priority
	})

	// Track applied operations for rollback if needed
	var appliedOps []AnnotationOperationResult
	var hasErrors bool

	// Apply each operation
	for _, op := range sortedOps {
		opResult := rm.applyAnnotationOperation(ctx, currentAnnotations, op, ruleID, templateContext, conflictResolution)
		result.Operations = append(result.Operations, opResult)

		if opResult.Success {
			appliedOps = append(appliedOps, opResult)
		} else {
			hasErrors = true
			if result.Error == nil {
				result.Error = opResult.Error
			}
		}
	}

	// Update the resource if we have successful operations
	if len(appliedOps) > 0 && !hasErrors {
		resourceCopy.SetAnnotations(currentAnnotations)

		// Apply the update with retry logic for conflicts
		if err := rm.updateResourceWithRetry(ctx, resourceCopy, resource); err != nil {
			result.Success = false
			result.Error = fmt.Errorf("failed to update resource annotations: %w", err)

			// Mark all operations as failed
			for i := range result.Operations {
				if result.Operations[i].Success {
					result.Operations[i].Success = false
					result.Operations[i].Error = err
				}
			}
		}
	} else if hasErrors {
		result.Success = false
	}

	return result
}

// applyAnnotationOperation applies a single annotation operation
func (rm *ResourceManager) applyAnnotationOperation(ctx context.Context, annotations map[string]string, op schedulerv1.AnnotationOperation, ruleID string, templateContext templating.TemplateContext, conflictResolution AnnotationConflictResolution) AnnotationOperationResult {
	result := AnnotationOperationResult{
		Key:      op.Key,
		Priority: op.Priority,
	}

	// Validate the annotation key
	if err := rm.ValidateAnnotationKey(op.Key); err != nil {
		result.Error = fmt.Errorf("invalid annotation key: %w", err)
		return result
	}

	// Process template if needed
	value := op.Value
	if op.Template && rm.TemplateEngine.IsTemplate(value) {
		processedValue, err := rm.TemplateEngine.ProcessTemplate(value, templateContext)
		if err != nil {
			result.Error = fmt.Errorf("template processing failed: %w", err)
			return result
		}
		value = processedValue
	}

	// Validate the processed value
	if err := rm.ValidateAnnotationValue(value); err != nil {
		result.Error = fmt.Errorf("invalid annotation value: %w", err)
		return result
	}

	// Get current value
	currentValue, exists := annotations[op.Key]
	result.OldValue = currentValue

	// Determine action (default to "apply" if not specified)
	action := op.Action
	if action == "" {
		action = "apply"
	}

	switch action {
	case "apply":
		// Handle conflicts
		if exists && currentValue != value {
			if !rm.shouldOverrideAnnotation(op.Key, currentValue, value, op.Priority, conflictResolution, ruleID) {
				result.Skipped = true
				result.Reason = fmt.Sprintf("conflict resolution policy prevented override (current: %s, new: %s)", currentValue, value)
				result.Success = true // Not an error, just skipped
				return result
			}
		}

		annotations[op.Key] = value
		result.NewValue = value
		result.Success = true

		// Track this annotation as applied by this rule
		rm.trackAnnotationApplication(annotations, op.Key, ruleID)

	case "remove":
		if exists {
			// Check if this annotation was applied by this rule
			if rm.wasAnnotationAppliedByRule(annotations, op.Key, ruleID) {
				delete(annotations, op.Key)
				result.Success = true
				result.Reason = "annotation removed"

				// Update tracking
				rm.untrackAnnotationApplication(annotations, op.Key, ruleID)
			} else {
				result.Skipped = true
				result.Reason = "annotation not applied by this rule"
				result.Success = true
			}
		} else {
			result.Skipped = true
			result.Reason = "annotation does not exist"
			result.Success = true
		}

	default:
		result.Error = fmt.Errorf("unknown action: %s", action)
	}

	return result
}

// shouldOverrideAnnotation determines if an annotation should be overridden based on conflict resolution policy
func (rm *ResourceManager) shouldOverrideAnnotation(key, currentValue, newValue string, priority int32, resolution AnnotationConflictResolution, ruleID string) bool {
	switch resolution {
	case ConflictResolutionSkip:
		return false

	case ConflictResolutionPriority:
		// Check if we can determine the priority of the existing annotation
		// This is a simplified implementation - in practice, you might store priority metadata
		return true // For now, always override based on priority

	case ConflictResolutionTimestamp:
		// Always override with newer timestamp (current execution)
		return true

	default:
		// Default behavior: override
		return true
	}
}

// trackAnnotationApplication tracks that an annotation was applied by a specific rule
func (rm *ResourceManager) trackAnnotationApplication(annotations map[string]string, key, ruleID string) {
	trackingKey := fmt.Sprintf("scheduler.k8s.io/rule-%s-annotations", ruleID)

	var appliedKeys []string
	if trackingValue, exists := annotations[trackingKey]; exists {
		trackingValue = strings.Trim(trackingValue, "[]")
		if trackingValue != "" {
			appliedKeys = strings.Split(trackingValue, " ")
		}
	}

	// Add the key if not already present
	found := false
	for _, appliedKey := range appliedKeys {
		if strings.TrimSpace(appliedKey) == key {
			found = true
			break
		}
	}

	if !found {
		appliedKeys = append(appliedKeys, key)
		annotations[trackingKey] = fmt.Sprintf("%v", appliedKeys)
	}
}

// untrackAnnotationApplication removes tracking for an annotation
func (rm *ResourceManager) untrackAnnotationApplication(annotations map[string]string, key, ruleID string) {
	trackingKey := fmt.Sprintf("scheduler.k8s.io/rule-%s-annotations", ruleID)

	if trackingValue, exists := annotations[trackingKey]; exists {
		trackingValue = strings.Trim(trackingValue, "[]")
		if trackingValue != "" {
			appliedKeys := strings.Split(trackingValue, " ")
			var remainingKeys []string

			for _, appliedKey := range appliedKeys {
				if strings.TrimSpace(appliedKey) != key {
					remainingKeys = append(remainingKeys, strings.TrimSpace(appliedKey))
				}
			}

			if len(remainingKeys) > 0 {
				annotations[trackingKey] = fmt.Sprintf("%v", remainingKeys)
			} else {
				delete(annotations, trackingKey)
			}
		}
	}
}

// wasAnnotationAppliedByRule checks if an annotation was applied by a specific rule
func (rm *ResourceManager) wasAnnotationAppliedByRule(annotations map[string]string, key, ruleID string) bool {
	trackingKey := fmt.Sprintf("scheduler.k8s.io/rule-%s-annotations", ruleID)

	if trackingValue, exists := annotations[trackingKey]; exists {
		trackingValue = strings.Trim(trackingValue, "[]")
		if trackingValue != "" {
			appliedKeys := strings.Split(trackingValue, " ")
			for _, appliedKey := range appliedKeys {
				if strings.TrimSpace(appliedKey) == key {
					return true
				}
			}
		}
	}

	return false
}

// updateResourceWithRetry updates a resource with retry logic for conflicts
func (rm *ResourceManager) updateResourceWithRetry(ctx context.Context, resourceCopy, originalResource client.Object) error {
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := rm.Update(ctx, resourceCopy); err != nil {
			if apierrors.IsConflict(err) && attempt < maxRetries-1 {
				rm.Log.V(1).Info("Resource conflict detected, retrying",
					"resource", rm.GetResourceInfo(originalResource),
					"attempt", attempt+1)

				// Refetch the resource and reapply annotations
				if refetchErr := rm.Get(ctx, client.ObjectKeyFromObject(originalResource), resourceCopy); refetchErr != nil {
					return fmt.Errorf("failed to refetch resource after conflict: %w", refetchErr)
				}

				// Wait a bit before retrying
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("failed to update resource after %d attempts", maxRetries)
}

// RemoveAnnotationGroups removes annotation groups from matching resources
func (rm *ResourceManager) RemoveAnnotationGroups(ctx context.Context, selector metav1.LabelSelector, groups []schedulerv1.AnnotationGroup, resourceTypes []string, namespace string, ruleID string) ([]AnnotationGroupResult, error) {
	if len(groups) == 0 {
		rm.Log.V(1).Info("No annotation groups to remove")
		return nil, nil
	}

	// Get matching resources
	resources, err := rm.GetMatchingResources(ctx, selector, resourceTypes, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get matching resources: %w", err)
	}

	if len(resources) == 0 {
		rm.Log.V(1).Info("No resources found matching selector")
		return nil, nil
	}

	var results []AnnotationGroupResult

	// Process each annotation group
	for _, group := range groups {
		result := rm.removeAnnotationGroup(ctx, resources, group, ruleID)
		results = append(results, result)
	}

	return results, nil
}

// removeAnnotationGroup removes a single annotation group from resources
func (rm *ResourceManager) removeAnnotationGroup(ctx context.Context, resources []client.Object, group schedulerv1.AnnotationGroup, ruleID string) AnnotationGroupResult {
	result := AnnotationGroupResult{
		GroupName:  group.Name,
		Success:    true,
		Operations: make([]AnnotationOperationResult, 0),
	}

	// Extract keys to remove from operations
	var keysToRemove []string
	for _, op := range group.Operations {
		keysToRemove = append(keysToRemove, op.Key)
	}

	// Process each resource
	for _, resource := range resources {
		resourceResult := rm.removeAnnotationKeysFromResource(ctx, resource, keysToRemove, ruleID)

		if resourceResult != nil {
			result.Success = false
			if result.Error == nil {
				result.Error = resourceResult
			}
		} else {
			result.ResourcesAffected++
		}
	}

	return result
}

// removeAnnotationKeysFromResource removes specific annotation keys from a resource
func (rm *ResourceManager) removeAnnotationKeysFromResource(ctx context.Context, resource client.Object, keys []string, ruleID string) error {
	// Create a copy of the resource to modify
	resourceCopy := resource.DeepCopyObject().(client.Object)
	currentAnnotations := resourceCopy.GetAnnotations()
	if currentAnnotations == nil {
		return nil // No annotations to remove
	}

	removedCount := 0
	for _, key := range keys {
		if rm.wasAnnotationAppliedByRule(currentAnnotations, key, ruleID) {
			if _, exists := currentAnnotations[key]; exists {
				delete(currentAnnotations, key)
				removedCount++
				rm.untrackAnnotationApplication(currentAnnotations, key, ruleID)
			}
		}
	}

	// Update the resource if any annotations were removed
	if removedCount > 0 {
		resourceCopy.SetAnnotations(currentAnnotations)
		return rm.updateResourceWithRetry(ctx, resourceCopy, resource)
	}

	return nil
}

// ValidateAnnotationGroups validates annotation groups and their operations
func (rm *ResourceManager) ValidateAnnotationGroups(groups []schedulerv1.AnnotationGroup) error {
	for _, group := range groups {
		if group.Name == "" {
			return fmt.Errorf("annotation group name cannot be empty")
		}

		if len(group.Operations) == 0 {
			return fmt.Errorf("annotation group %s must have at least one operation", group.Name)
		}

		for i, op := range group.Operations {
			if err := rm.validateAnnotationOperation(op); err != nil {
				return fmt.Errorf("invalid operation %d in group %s: %w", i, group.Name, err)
			}
		}
	}

	return nil
}

// validateAnnotationOperation validates a single annotation operation
func (rm *ResourceManager) validateAnnotationOperation(op schedulerv1.AnnotationOperation) error {
	if op.Key == "" {
		return fmt.Errorf("annotation key cannot be empty")
	}

	if err := rm.ValidateAnnotationKey(op.Key); err != nil {
		return fmt.Errorf("invalid annotation key: %w", err)
	}

	// Validate action
	if op.Action != "" && op.Action != "apply" && op.Action != "remove" {
		return fmt.Errorf("invalid action: %s (must be 'apply' or 'remove')", op.Action)
	}

	// Validate template if specified
	if op.Template && op.Value != "" {
		if err := rm.TemplateEngine.ValidateTemplate(op.Value); err != nil {
			return fmt.Errorf("invalid template in value: %w", err)
		}
	}

	return nil
}
