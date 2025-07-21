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
	"testing"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/templating"
)

func TestResourceManager_ApplyAnnotationGroups(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name                string
		objects             []client.Object
		selector            metav1.LabelSelector
		groups              []schedulerv1.AnnotationGroup
		resourceTypes       []string
		namespace           string
		ruleID              string
		templateContext     templating.TemplateContext
		conflictResolution  AnnotationConflictResolution
		expectedSuccess     bool
		expectedAffected    int32
		expectedAnnotations map[string]string
		description         string
	}{
		{
			name: "apply single annotation group with multiple operations",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "deployment-info",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:    "scheduler.k8s.io/managed-deployment.version",
							Value:  "v1.0",
							Action: "apply",
						},
						{
							Key:    "scheduler.k8s.io/managed-deployment.environment",
							Value:  "production",
							Action: "apply",
						},
					},
					Atomic: true,
				},
			},
			resourceTypes:      []string{"pods"},
			namespace:          "default",
			ruleID:             "test-rule",
			templateContext:    templating.TemplateContext{},
			conflictResolution: ConflictResolutionPriority,
			expectedSuccess:    true,
			expectedAffected:   1,
			expectedAnnotations: map[string]string{
				"scheduler.k8s.io/managed-deployment.version":     "v1.0",
				"scheduler.k8s.io/managed-deployment.environment": "production",
			},
			description: "Should apply multiple annotations atomically",
		},
		{
			name: "apply annotation group with templating",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "web-pod",
						Namespace: "production",
						Labels:    map[string]string{"app": "web"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "web"},
			},
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "templated-info",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:      "scheduler.k8s.io/managed-deployment.target",
							Value:    "${env}-${resource.name}",
							Template: true,
							Action:   "apply",
						},
						{
							Key:      "scheduler.k8s.io/managed-deployment.timestamp",
							Value:    "${timestamp}",
							Template: true,
							Action:   "apply",
						},
					},
				},
			},
			resourceTypes: []string{"pods"},
			namespace:     "production",
			ruleID:        "template-rule",
			templateContext: templating.TemplateContext{
				Variables: map[string]string{
					"env": "prod",
				},
				ExecutionTime: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
			},
			conflictResolution: ConflictResolutionPriority,
			expectedSuccess:    true,
			expectedAffected:   1,
			expectedAnnotations: map[string]string{
				"scheduler.k8s.io/managed-deployment.target":    "prod-web-pod",
				"scheduler.k8s.io/managed-deployment.timestamp": "2025-01-15T10:30:00Z",
			},
			description: "Should process templates in annotation values",
		},
		{
			name: "apply annotation group with priority handling",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "priority-pod",
						Namespace: "default",
						Labels:    map[string]string{"app": "priority-test"},
						Annotations: map[string]string{
							"existing.annotation": "old-value",
						},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "priority-test"},
			},
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "priority-group",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:      "existing.annotation",
							Value:    "high-priority-value",
							Action:   "apply",
							Priority: 100,
						},
						{
							Key:      "new.annotation",
							Value:    "new-value",
							Action:   "apply",
							Priority: 50,
						},
					},
				},
			},
			resourceTypes:      []string{"pods"},
			namespace:          "default",
			ruleID:             "priority-rule",
			templateContext:    templating.TemplateContext{},
			conflictResolution: ConflictResolutionPriority,
			expectedSuccess:    true,
			expectedAffected:   1,
			expectedAnnotations: map[string]string{
				"existing.annotation": "high-priority-value",
				"new.annotation":      "new-value",
			},
			description: "Should handle annotation priorities correctly",
		},
		{
			name: "apply multiple annotation groups",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "multi-group-pod",
						Namespace: "default",
						Labels:    map[string]string{"app": "multi"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "multi"},
			},
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "group-1",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:    "group1.annotation",
							Value:  "value1",
							Action: "apply",
						},
					},
				},
				{
					Name: "group-2",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:    "group2.annotation",
							Value:  "value2",
							Action: "apply",
						},
					},
				},
			},
			resourceTypes:      []string{"pods"},
			namespace:          "default",
			ruleID:             "multi-group-rule",
			templateContext:    templating.TemplateContext{},
			conflictResolution: ConflictResolutionPriority,
			expectedSuccess:    true,
			expectedAffected:   2, // Each group affects the resource
			expectedAnnotations: map[string]string{
				"group1.annotation": "value1",
				"group2.annotation": "value2",
			},
			description: "Should apply multiple annotation groups",
		},
		{
			name: "atomic group failure handling",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "atomic-pod",
						Namespace: "default",
						Labels:    map[string]string{"app": "atomic"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "atomic"},
			},
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "atomic-group",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:    "valid.annotation",
							Value:  "valid-value",
							Action: "apply",
						},
						{
							Key:    "", // Invalid key
							Value:  "invalid",
							Action: "apply",
						},
					},
					Atomic: true,
				},
			},
			resourceTypes:       []string{"pods"},
			namespace:           "default",
			ruleID:              "atomic-rule",
			templateContext:     templating.TemplateContext{},
			conflictResolution:  ConflictResolutionPriority,
			expectedSuccess:     false,
			expectedAffected:    0,
			expectedAnnotations: map[string]string{},
			description:         "Should fail atomic group when any operation fails",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

			results, err := rm.ApplyAnnotationGroups(
				context.TODO(),
				tt.selector,
				tt.groups,
				tt.resourceTypes,
				tt.namespace,
				tt.ruleID,
				tt.templateContext,
				tt.conflictResolution,
			)

			if tt.expectedSuccess {
				if err != nil {
					t.Errorf("Expected success but got error: %v", err)
				}
			} else {
				if err == nil && len(results) > 0 {
					// Check if any group failed
					anyFailed := false
					for _, result := range results {
						if !result.Success {
							anyFailed = true
							break
						}
					}
					if !anyFailed {
						t.Errorf("Expected failure but all groups succeeded")
					}
				}
			}

			// Check affected resources count
			var totalAffected int32
			for _, result := range results {
				totalAffected += result.ResourcesAffected
			}

			if totalAffected != tt.expectedAffected {
				t.Errorf("Expected %d affected resources, got %d", tt.expectedAffected, totalAffected)
			}

			// Verify annotations were applied correctly (if expected to succeed)
			if tt.expectedSuccess && len(tt.expectedAnnotations) > 0 {
				// Get the updated resource
				updatedPod := &corev1.Pod{}
				err := fakeClient.Get(context.TODO(), client.ObjectKey{
					Name:      tt.objects[0].GetName(),
					Namespace: tt.objects[0].GetNamespace(),
				}, updatedPod)

				if err != nil {
					t.Errorf("Failed to get updated resource: %v", err)
				} else {
					annotations := updatedPod.GetAnnotations()
					for expectedKey, expectedValue := range tt.expectedAnnotations {
						if actualValue, exists := annotations[expectedKey]; !exists {
							t.Errorf("Expected annotation %s not found", expectedKey)
						} else if actualValue != expectedValue {
							t.Errorf("Expected annotation %s=%s, got %s", expectedKey, expectedValue, actualValue)
						}
					}
				}
			}
		})
	}
}

func TestResourceManager_RemoveAnnotationGroups(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name             string
		objects          []client.Object
		selector         metav1.LabelSelector
		groups           []schedulerv1.AnnotationGroup
		resourceTypes    []string
		namespace        string
		ruleID           string
		expectedSuccess  bool
		expectedAffected int32
		description      string
	}{
		{
			name: "remove annotation group",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-pod",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						Annotations: map[string]string{
							"deployment.version":                          "v1.0",
							"deployment.environment":                      "production",
							"scheduler.k8s.io/rule-test-rule-annotations": "[deployment.version deployment.environment]",
							"other.annotation":                            "keep-this",
						},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "deployment-info",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:    "deployment.version",
							Action: "remove",
						},
						{
							Key:    "deployment.environment",
							Action: "remove",
						},
					},
				},
			},
			resourceTypes:    []string{"pods"},
			namespace:        "default",
			ruleID:           "test-rule",
			expectedSuccess:  true,
			expectedAffected: 1,
			description:      "Should remove annotations that were applied by the rule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

			results, err := rm.RemoveAnnotationGroups(
				context.TODO(),
				tt.selector,
				tt.groups,
				tt.resourceTypes,
				tt.namespace,
				tt.ruleID,
			)

			if tt.expectedSuccess {
				if err != nil {
					t.Errorf("Expected success but got error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Expected error but got success")
				}
			}

			// Check affected resources count
			var totalAffected int32
			for _, result := range results {
				totalAffected += result.ResourcesAffected
			}

			if totalAffected != tt.expectedAffected {
				t.Errorf("Expected %d affected resources, got %d", tt.expectedAffected, totalAffected)
			}

			// Verify annotations were removed correctly (if expected to succeed)
			if tt.expectedSuccess {
				// Get the updated resource
				updatedPod := &corev1.Pod{}
				err := fakeClient.Get(context.TODO(), client.ObjectKey{
					Name:      tt.objects[0].GetName(),
					Namespace: tt.objects[0].GetNamespace(),
				}, updatedPod)

				if err != nil {
					t.Errorf("Failed to get updated resource: %v", err)
				} else {
					annotations := updatedPod.GetAnnotations()

					// Check that rule-applied annotations were removed
					if _, exists := annotations["deployment.version"]; exists {
						t.Errorf("Expected annotation deployment.version to be removed")
					}
					if _, exists := annotations["deployment.environment"]; exists {
						t.Errorf("Expected annotation deployment.environment to be removed")
					}

					// Check that other annotations were preserved
					if _, exists := annotations["other.annotation"]; !exists {
						t.Errorf("Expected annotation other.annotation to be preserved")
					}
				}
			}
		})
	}
}

func TestResourceManager_ValidateAnnotationGroups(t *testing.T) {
	rm := NewResourceManager(nil, logr.Discard(), nil)

	tests := []struct {
		name        string
		groups      []schedulerv1.AnnotationGroup
		expectError bool
		description string
	}{
		{
			name: "valid annotation group",
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "valid-group",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:    "scheduler.k8s.io/managed-valid.key",
							Value:  "valid-value",
							Action: "apply",
						},
					},
				},
			},
			expectError: false,
			description: "Should validate correct annotation group",
		},
		{
			name: "empty group name",
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:    "valid.key",
							Value:  "valid-value",
							Action: "apply",
						},
					},
				},
			},
			expectError: true,
			description: "Should reject empty group name",
		},
		{
			name: "no operations",
			groups: []schedulerv1.AnnotationGroup{
				{
					Name:       "empty-group",
					Operations: []schedulerv1.AnnotationOperation{},
				},
			},
			expectError: true,
			description: "Should reject group with no operations",
		},
		{
			name: "invalid annotation key",
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "invalid-key-group",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:    "",
							Value:  "valid-value",
							Action: "apply",
						},
					},
				},
			},
			expectError: true,
			description: "Should reject empty annotation key",
		},
		{
			name: "invalid action",
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "invalid-action-group",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:    "valid.key",
							Value:  "valid-value",
							Action: "invalid-action",
						},
					},
				},
			},
			expectError: true,
			description: "Should reject invalid action",
		},
		{
			name: "invalid template",
			groups: []schedulerv1.AnnotationGroup{
				{
					Name: "invalid-template-group",
					Operations: []schedulerv1.AnnotationOperation{
						{
							Key:      "valid.key",
							Value:    "${invalid template",
							Template: true,
							Action:   "apply",
						},
					},
				},
			},
			expectError: true,
			description: "Should reject invalid template syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rm.ValidateAnnotationGroups(tt.groups)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none: %s", tt.description)
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v (%s)", err, tt.description)
			}
		})
	}
}

func TestAnnotationConflictResolution(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name               string
		existingAnnotation string
		newAnnotation      string
		resolution         AnnotationConflictResolution
		expectedValue      string
		shouldOverride     bool
		description        string
	}{
		{
			name:               "priority resolution - should override",
			existingAnnotation: "old-value",
			newAnnotation:      "new-value",
			resolution:         ConflictResolutionPriority,
			expectedValue:      "new-value",
			shouldOverride:     true,
			description:        "Priority resolution should allow override",
		},
		{
			name:               "skip resolution - should not override",
			existingAnnotation: "old-value",
			newAnnotation:      "new-value",
			resolution:         ConflictResolutionSkip,
			expectedValue:      "old-value",
			shouldOverride:     false,
			description:        "Skip resolution should preserve existing value",
		},
		{
			name:               "timestamp resolution - should override",
			existingAnnotation: "old-value",
			newAnnotation:      "new-value",
			resolution:         ConflictResolutionTimestamp,
			expectedValue:      "new-value",
			shouldOverride:     true,
			description:        "Timestamp resolution should allow override with newer timestamp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rm := NewResourceManager(nil, logr.Discard(), scheme)

			shouldOverride := rm.shouldOverrideAnnotation(
				"test.annotation",
				tt.existingAnnotation,
				tt.newAnnotation,
				100, // priority
				tt.resolution,
				"test-rule",
			)

			if shouldOverride != tt.shouldOverride {
				t.Errorf("Expected shouldOverride=%v, got %v (%s)",
					tt.shouldOverride, shouldOverride, tt.description)
			}
		})
	}
}
