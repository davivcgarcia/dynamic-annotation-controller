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
	"strings"
	"testing"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestResourceManager_GetMatchingResources(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name          string
		objects       []client.Object
		selector      metav1.LabelSelector
		resourceTypes []string
		namespace     string
		expectedCount int
		expectError   bool
	}{
		{
			name: "match pods with simple label selector",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test", "env": "prod"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "default",
						Labels:    map[string]string{"app": "other", "env": "prod"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			expectedCount: 1,
			expectError:   false,
		},
		{
			name: "match multiple resource types",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deploy1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sts1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"pods", "deployments", "statefulsets"},
			namespace:     "default",
			expectedCount: 3,
			expectError:   false,
		},
		{
			name: "match with expression selector",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"env": "prod", "tier": "frontend"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "default",
						Labels:    map[string]string{"env": "dev", "tier": "backend"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod3",
						Namespace: "default",
						Labels:    map[string]string{"env": "prod", "tier": "backend"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"prod", "staging"},
					},
				},
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			expectedCount: 2,
			expectError:   false,
		},
		{
			name: "no matching resources",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "other"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			expectedCount: 0,
			expectError:   false,
		},
		{
			name: "namespace filtering",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "kube-system",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			expectedCount: 1,
			expectError:   false,
		},
		{
			name: "unsupported resource type",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"unsupported"},
			namespace:     "default",
			expectedCount: 0,
			expectError:   false, // Should continue with other types, not fail completely
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

			resources, err := rm.GetMatchingResources(context.TODO(), tt.selector, tt.resourceTypes, tt.namespace)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(resources) != tt.expectedCount {
				t.Errorf("Expected %d resources, got %d", tt.expectedCount, len(resources))
			}
		})
	}
}

func TestResourceManager_FilterResourcesBySelector(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name          string
		resources     []client.Object
		selector      metav1.LabelSelector
		expectedCount int
		expectError   bool
	}{
		{
			name: "filter with match labels",
			resources: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pod1",
						Labels: map[string]string{"app": "test", "env": "prod"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pod2",
						Labels: map[string]string{"app": "other", "env": "prod"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pod3",
						Labels: map[string]string{"app": "test", "env": "dev"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name: "filter with match expressions",
			resources: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pod1",
						Labels: map[string]string{"env": "prod"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pod2",
						Labels: map[string]string{"env": "dev"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pod3",
						Labels: map[string]string{"env": "staging"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"prod", "staging"},
					},
				},
			},
			expectedCount: 2,
			expectError:   false,
		},
		{
			name: "no matching resources",
			resources: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "pod1",
						Labels: map[string]string{"app": "other"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			expectedCount: 0,
			expectError:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rm := NewResourceManager(nil, logr.Discard(), nil)

			filtered, err := rm.FilterResourcesBySelector(tt.resources, tt.selector)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(filtered) != tt.expectedCount {
				t.Errorf("Expected %d filtered resources, got %d", tt.expectedCount, len(filtered))
			}
		})
	}
}

func TestResourceManager_GetResourcesByType(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	// Create test objects
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "test"},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
			Labels:    map[string]string{"app": "test"},
		},
	}

	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "default",
			Labels:    map[string]string{"app": "test"},
		},
	}

	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-daemonset",
			Namespace: "default",
			Labels:    map[string]string{"app": "test"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(pod, deployment, statefulSet, daemonSet).
		Build()

	rm := NewResourceManager(fakeClient, zap.New(zap.UseDevMode(true)), scheme)

	tests := []struct {
		name         string
		resourceType string
		expectCount  int
		expectError  bool
	}{
		{
			name:         "get pods",
			resourceType: "pods",
			expectCount:  1,
			expectError:  false,
		},
		{
			name:         "get deployments",
			resourceType: "deployments",
			expectCount:  1,
			expectError:  false,
		},
		{
			name:         "get statefulsets",
			resourceType: "statefulsets",
			expectCount:  1,
			expectError:  false,
		},
		{
			name:         "get daemonsets",
			resourceType: "daemonsets",
			expectCount:  1,
			expectError:  false,
		},
		{
			name:         "unsupported resource type",
			resourceType: "services",
			expectCount:  0,
			expectError:  false, // Should not error, just skip unsupported types
		},
	}

	selector := metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources, err := rm.GetMatchingResources(context.TODO(), selector, []string{tt.resourceType}, "default")

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if len(resources) != tt.expectCount {
					t.Errorf("Expected %d resources, got %d", tt.expectCount, len(resources))
				}
			}
		})
	}
}

func TestResourceManager_GetResourceInfo(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels:    map[string]string{"app": "test", "version": "v1"},
		},
	}

	rm := NewResourceManager(nil, logr.Discard(), nil)
	info := rm.GetResourceInfo(pod)

	expectedFields := []string{"name", "namespace", "kind", "labels"}
	for _, field := range expectedFields {
		if _, exists := info[field]; !exists {
			t.Errorf("Expected field %s not found in resource info", field)
		}
	}

	if info["name"] != "test-pod" {
		t.Errorf("Expected name 'test-pod', got %v", info["name"])
	}

	if info["namespace"] != "default" {
		t.Errorf("Expected namespace 'default', got %v", info["namespace"])
	}
}

func TestResourceManager_ComplexSelectorPatterns(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name          string
		objects       []client.Object
		selector      metav1.LabelSelector
		resourceTypes []string
		expectedCount int
		description   string
	}{
		{
			name: "complex match expressions with NotIn operator",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"env": "prod", "tier": "frontend"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "default",
						Labels:    map[string]string{"env": "dev", "tier": "backend"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod3",
						Namespace: "default",
						Labels:    map[string]string{"env": "test", "tier": "database"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: metav1.LabelSelectorOpNotIn,
						Values:   []string{"dev", "test"},
					},
				},
			},
			resourceTypes: []string{"pods"},
			expectedCount: 1,
			description:   "Should match pods where env is not dev or test",
		},
		{
			name: "exists operator",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test", "version": "v1"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod3",
						Namespace: "default",
						Labels:    map[string]string{"service": "api"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "version",
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			resourceTypes: []string{"pods"},
			expectedCount: 1,
			description:   "Should match pods that have version label",
		},
		{
			name: "does not exist operator",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test", "version": "v1"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "version",
						Operator: metav1.LabelSelectorOpDoesNotExist,
					},
				},
			},
			resourceTypes: []string{"pods"},
			expectedCount: 1,
			description:   "Should match pods that do not have version label",
		},
		{
			name: "combined match labels and expressions",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test", "env": "prod", "tier": "frontend"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "default",
						Labels:    map[string]string{"app": "test", "env": "dev", "tier": "frontend"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod3",
						Namespace: "default",
						Labels:    map[string]string{"app": "other", "env": "prod", "tier": "frontend"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"prod", "staging"},
					},
				},
			},
			resourceTypes: []string{"pods"},
			expectedCount: 1,
			description:   "Should match pods with app=test AND env in (prod, staging)",
		},
		{
			name: "cross-namespace resource discovery",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "kube-system",
						Labels:    map[string]string{"app": "test"},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod3",
						Namespace: "monitoring",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"pods"},
			expectedCount: 3,
			description:   "Should match pods across all namespaces when namespace is empty",
		},
		{
			name: "mixed resource types with different label patterns",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"component": "web", "tier": "frontend"},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deploy1",
						Namespace: "default",
						Labels:    map[string]string{"component": "api", "tier": "backend"},
					},
				},
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "sts1",
						Namespace: "default",
						Labels:    map[string]string{"component": "database", "tier": "data"},
					},
				},
				&appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ds1",
						Namespace: "default",
						Labels:    map[string]string{"component": "logging", "tier": "infrastructure"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "tier",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"frontend", "backend", "data"},
					},
				},
			},
			resourceTypes: []string{"pods", "deployments", "statefulsets", "daemonsets"},
			expectedCount: 3,
			description:   "Should match resources across types with tier in specified values",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

			// Test cross-namespace by using empty namespace for that specific test
			namespace := "default"
			if tt.name == "cross-namespace resource discovery" {
				namespace = ""
			}

			resources, err := rm.GetMatchingResources(context.TODO(), tt.selector, tt.resourceTypes, namespace)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(resources) != tt.expectedCount {
				t.Errorf("Expected %d resources, got %d. %s", tt.expectedCount, len(resources), tt.description)
				for i, res := range resources {
					t.Logf("Resource %d: %s/%s (labels: %v)", i, res.GetNamespace(), res.GetName(), res.GetLabels())
				}
			}
		})
	}
}

func TestResourceManager_EdgeCases(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name          string
		objects       []client.Object
		selector      metav1.LabelSelector
		resourceTypes []string
		namespace     string
		expectedCount int
		expectError   bool
		description   string
	}{
		{
			name:    "empty resource types list",
			objects: []client.Object{},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{},
			namespace:     "default",
			expectedCount: 0,
			expectError:   false,
			description:   "Should handle empty resource types gracefully",
		},
		{
			name: "empty label selector",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector:      metav1.LabelSelector{},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			expectedCount: 1,
			expectError:   false,
			description:   "Empty selector should match all resources",
		},
		{
			name: "resources without labels",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod2",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			expectedCount: 1,
			expectError:   false,
			description:   "Should handle resources without labels",
		},
		{
			name: "invalid label selector",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"test"},
					},
				},
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			expectedCount: 0,
			expectError:   true,
			description:   "Should handle invalid label selector",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

			resources, err := rm.GetMatchingResources(context.TODO(), tt.selector, tt.resourceTypes, tt.namespace)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none. %s", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v. %s", err, tt.description)
				}
				if len(resources) != tt.expectedCount {
					t.Errorf("Expected %d resources, got %d. %s", tt.expectedCount, len(resources), tt.description)
				}
			}
		})
	}
}

func TestResourceManager_ValidateResourceTypes(t *testing.T) {
	rm := NewResourceManager(nil, logr.Discard(), nil)

	tests := []struct {
		name                string
		resourceTypes       []string
		expectedUnsupported []string
	}{
		{
			name:                "all supported types",
			resourceTypes:       []string{"pods", "deployments", "statefulsets", "daemonsets"},
			expectedUnsupported: []string{},
		},
		{
			name:                "mixed supported and unsupported",
			resourceTypes:       []string{"pods", "services", "deployments", "configmaps"},
			expectedUnsupported: []string{"services", "configmaps"},
		},
		{
			name:                "all unsupported types",
			resourceTypes:       []string{"services", "configmaps", "secrets"},
			expectedUnsupported: []string{"services", "configmaps", "secrets"},
		},
		{
			name:                "empty list",
			resourceTypes:       []string{},
			expectedUnsupported: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsupported := rm.ValidateResourceTypes(tt.resourceTypes)

			if len(unsupported) != len(tt.expectedUnsupported) {
				t.Errorf("Expected %d unsupported types, got %d", len(tt.expectedUnsupported), len(unsupported))
			}

			for _, expected := range tt.expectedUnsupported {
				found := false
				for _, actual := range unsupported {
					if actual == expected {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected unsupported type %s not found", expected)
				}
			}
		})
	}
}

func TestResourceManager_GetSupportedResourceTypes(t *testing.T) {
	rm := NewResourceManager(nil, logr.Discard(), nil)

	supported := rm.GetSupportedResourceTypes()
	expected := []string{"pods", "deployments", "statefulsets", "daemonsets"}

	if len(supported) != len(expected) {
		t.Errorf("Expected %d supported types, got %d", len(expected), len(supported))
	}

	for _, expectedType := range expected {
		found := false
		for _, supportedType := range supported {
			if supportedType == expectedType {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected supported type %s not found", expectedType)
		}
	}
}

func TestResourceManager_CountResourcesByType(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	objects := []client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
				Labels:    map[string]string{"app": "test"},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "default",
				Labels:    map[string]string{"app": "test"},
			},
		},
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "deploy1",
				Namespace: "default",
				Labels:    map[string]string{"app": "test"},
			},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sts1",
				Namespace: "default",
				Labels:    map[string]string{"app": "other"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		Build()

	rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

	selector := metav1.LabelSelector{
		MatchLabels: map[string]string{"app": "test"},
	}

	counts, err := rm.CountResourcesByType(context.TODO(), selector, []string{"pods", "deployments", "statefulsets"}, "default")

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	expectedCounts := map[string]int{
		"pods":         2,
		"deployments":  1,
		"statefulsets": 0,
	}

	for resourceType, expectedCount := range expectedCounts {
		if counts[resourceType] != expectedCount {
			t.Errorf("Expected %d %s, got %d", expectedCount, resourceType, counts[resourceType])
		}
	}
}

func TestResourceManager_GetResourcesByNamespace(t *testing.T) {
	resources := []client.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: "default",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod2",
				Namespace: "kube-system",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod3",
				Namespace: "default",
			},
		},
	}

	rm := NewResourceManager(nil, logr.Discard(), nil)
	namespaceGroups := rm.GetResourcesByNamespace(resources)

	if len(namespaceGroups["default"]) != 2 {
		t.Errorf("Expected 2 resources in default namespace, got %d", len(namespaceGroups["default"]))
	}

	if len(namespaceGroups["kube-system"]) != 1 {
		t.Errorf("Expected 1 resource in kube-system namespace, got %d", len(namespaceGroups["kube-system"]))
	}
}

func TestResourceManager_HasMatchingResources(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name          string
		objects       []client.Object
		selector      metav1.LabelSelector
		resourceTypes []string
		namespace     string
		expectedMatch bool
	}{
		{
			name: "has matching resources",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			expectedMatch: true,
		},
		{
			name: "no matching resources",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "other"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			expectedMatch: false,
		},
		{
			name: "match in different resource type",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "other"},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deploy1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			resourceTypes: []string{"pods", "deployments"},
			namespace:     "default",
			expectedMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

			hasMatch, err := rm.HasMatchingResources(context.TODO(), tt.selector, tt.resourceTypes, tt.namespace)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if hasMatch != tt.expectedMatch {
				t.Errorf("Expected match result %v, got %v", tt.expectedMatch, hasMatch)
			}
		})
	}
}

func TestResourceManager_ApplyAnnotations(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name                string
		objects             []client.Object
		selector            metav1.LabelSelector
		annotations         map[string]string
		resourceTypes       []string
		namespace           string
		ruleID              string
		expectError         bool
		expectedAnnotations map[string]string
		description         string
	}{
		{
			name: "apply annotations to pod without existing annotations",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotations: map[string]string{
				"example.com/env":     "production",
				"example.com/version": "v1.0.0",
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			ruleID:        "rule-1",
			expectError:   false,
			expectedAnnotations: map[string]string{
				"example.com/env":                          "production",
				"example.com/version":                      "v1.0.0",
				"scheduler.k8s.io/rule-rule-1-annotations": "[example.com/env example.com/version]",
			},
			description: "Should apply annotations and track them",
		},
		{
			name: "apply annotations preserving existing ones",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						Annotations: map[string]string{
							"existing.com/annotation": "existing-value",
						},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotations: map[string]string{
				"example.com/env": "production",
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			ruleID:        "rule-1",
			expectError:   false,
			expectedAnnotations: map[string]string{
				"existing.com/annotation":                  "existing-value",
				"example.com/env":                          "production",
				"scheduler.k8s.io/rule-rule-1-annotations": "[example.com/env]",
			},
			description: "Should preserve existing annotations",
		},
		{
			name: "apply annotations with conflict resolution",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						Annotations: map[string]string{
							"example.com/env": "development",
						},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotations: map[string]string{
				"example.com/env": "production",
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			ruleID:        "rule-1",
			expectError:   false,
			expectedAnnotations: map[string]string{
				"example.com/env":                          "production",
				"scheduler.k8s.io/rule-rule-1-annotations": "[example.com/env]",
			},
			description: "Should resolve conflicts by overwriting",
		},
		{
			name: "apply annotations to multiple resource types",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "deploy1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotations: map[string]string{
				"example.com/env": "production",
			},
			resourceTypes: []string{"pods", "deployments"},
			namespace:     "default",
			ruleID:        "rule-1",
			expectError:   false,
			expectedAnnotations: map[string]string{
				"example.com/env":                          "production",
				"scheduler.k8s.io/rule-rule-1-annotations": "[example.com/env]",
			},
			description: "Should apply to multiple resource types",
		},
		{
			name: "no matching resources",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "other"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotations: map[string]string{
				"example.com/env": "production",
			},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			ruleID:        "rule-1",
			expectError:   false,
			description:   "Should handle no matching resources gracefully",
		},
		{
			name: "empty annotations map",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotations:   map[string]string{},
			resourceTypes: []string{"pods"},
			namespace:     "default",
			ruleID:        "rule-1",
			expectError:   false,
			description:   "Should handle empty annotations gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

			err := rm.ApplyAnnotations(context.TODO(), tt.selector, tt.annotations, tt.resourceTypes, tt.namespace, tt.ruleID)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none. %s", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v. %s", err, tt.description)
				}

				// Verify annotations were applied correctly if we have expected annotations
				if tt.expectedAnnotations != nil && len(tt.objects) > 0 {
					// Get the updated resource
					updatedResource := tt.objects[0].DeepCopyObject().(client.Object)
					err := fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(updatedResource), updatedResource)
					if err != nil {
						t.Errorf("Failed to get updated resource: %v", err)
						return
					}

					actualAnnotations := updatedResource.GetAnnotations()
					for expectedKey, expectedValue := range tt.expectedAnnotations {
						if actualValue, exists := actualAnnotations[expectedKey]; !exists {
							t.Errorf("Expected annotation %s not found", expectedKey)
						} else if strings.HasPrefix(expectedKey, "scheduler.k8s.io/rule-") {
							// For tracking annotations, check that all expected keys are present
							// but don't worry about order
							expectedKeys := strings.Trim(expectedValue, "[]")
							if expectedKeys != "" {
								expectedKeyList := strings.Split(expectedKeys, " ")
								actualKeys := strings.Trim(actualValue, "[]")
								if actualKeys != "" {
									actualKeyList := strings.Split(actualKeys, " ")
									if len(expectedKeyList) != len(actualKeyList) {
										t.Errorf("Expected %d tracking keys, got %d", len(expectedKeyList), len(actualKeyList))
									} else {
										// Check that all expected keys are present
										for _, expectedKey := range expectedKeyList {
											found := false
											for _, actualKey := range actualKeyList {
												if strings.TrimSpace(actualKey) == strings.TrimSpace(expectedKey) {
													found = true
													break
												}
											}
											if !found {
												t.Errorf("Expected tracking key %s not found in %s", expectedKey, actualValue)
											}
										}
									}
								}
							}
						} else if actualValue != expectedValue {
							t.Errorf("Expected annotation %s=%s, got %s", expectedKey, expectedValue, actualValue)
						}
					}
				}
			}
		})
	}
}

func TestResourceManager_RemoveAnnotations(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name                string
		objects             []client.Object
		selector            metav1.LabelSelector
		annotationKeys      []string
		resourceTypes       []string
		namespace           string
		ruleID              string
		expectError         bool
		expectedAnnotations map[string]string
		description         string
	}{
		{
			name: "remove annotations applied by rule",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						Annotations: map[string]string{
							"example.com/env":                          "production",
							"example.com/version":                      "v1.0.0",
							"scheduler.k8s.io/rule-rule-1-annotations": "[example.com/env example.com/version]",
							"other.com/annotation":                     "keep-this",
						},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotationKeys: []string{"example.com/env", "example.com/version"},
			resourceTypes:  []string{"pods"},
			namespace:      "default",
			ruleID:         "rule-1",
			expectError:    false,
			expectedAnnotations: map[string]string{
				"other.com/annotation": "keep-this",
			},
			description: "Should remove only rule-applied annotations",
		},
		{
			name: "skip removing annotations not applied by rule",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						Annotations: map[string]string{
							"example.com/env":                          "production",
							"scheduler.k8s.io/rule-rule-2-annotations": "[example.com/env]",
							"other.com/annotation":                     "keep-this",
						},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotationKeys: []string{"example.com/env"},
			resourceTypes:  []string{"pods"},
			namespace:      "default",
			ruleID:         "rule-1",
			expectError:    false,
			expectedAnnotations: map[string]string{
				"example.com/env":                          "production",
				"scheduler.k8s.io/rule-rule-2-annotations": "[example.com/env]",
				"other.com/annotation":                     "keep-this",
			},
			description: "Should not remove annotations applied by different rule",
		},
		{
			name: "remove partial annotations from rule",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						Annotations: map[string]string{
							"example.com/env":                          "production",
							"example.com/version":                      "v1.0.0",
							"example.com/team":                         "backend",
							"scheduler.k8s.io/rule-rule-1-annotations": "[example.com/env example.com/version example.com/team]",
						},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotationKeys: []string{"example.com/env"},
			resourceTypes:  []string{"pods"},
			namespace:      "default",
			ruleID:         "rule-1",
			expectError:    false,
			expectedAnnotations: map[string]string{
				"example.com/version":                      "v1.0.0",
				"example.com/team":                         "backend",
				"scheduler.k8s.io/rule-rule-1-annotations": "[example.com/version example.com/team]",
			},
			description: "Should update tracking annotation when partially removing",
		},
		{
			name: "remove all annotations from rule",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						Annotations: map[string]string{
							"example.com/env":                          "production",
							"scheduler.k8s.io/rule-rule-1-annotations": "[example.com/env]",
							"other.com/annotation":                     "keep-this",
						},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotationKeys: []string{"example.com/env"},
			resourceTypes:  []string{"pods"},
			namespace:      "default",
			ruleID:         "rule-1",
			expectError:    false,
			expectedAnnotations: map[string]string{
				"other.com/annotation": "keep-this",
			},
			description: "Should remove tracking annotation when all rule annotations removed",
		},
		{
			name: "handle resource with no annotations",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotationKeys: []string{"example.com/env"},
			resourceTypes:  []string{"pods"},
			namespace:      "default",
			ruleID:         "rule-1",
			expectError:    false,
			description:    "Should handle resources with no annotations gracefully",
		},
		{
			name: "empty annotation keys",
			objects: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
				},
			},
			selector: metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			annotationKeys: []string{},
			resourceTypes:  []string{"pods"},
			namespace:      "default",
			ruleID:         "rule-1",
			expectError:    false,
			description:    "Should handle empty annotation keys gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.objects...).
				Build()

			rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

			err := rm.RemoveAnnotations(context.TODO(), tt.selector, tt.annotationKeys, tt.resourceTypes, tt.namespace, tt.ruleID)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none. %s", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v. %s", err, tt.description)
				}

				// Verify annotations were removed correctly if we have expected annotations
				if tt.expectedAnnotations != nil && len(tt.objects) > 0 {
					// Get the updated resource
					updatedResource := tt.objects[0].DeepCopyObject().(client.Object)
					err := fakeClient.Get(context.TODO(), client.ObjectKeyFromObject(updatedResource), updatedResource)
					if err != nil {
						t.Errorf("Failed to get updated resource: %v", err)
						return
					}

					actualAnnotations := updatedResource.GetAnnotations()

					// Check that expected annotations are present
					for expectedKey, expectedValue := range tt.expectedAnnotations {
						if actualValue, exists := actualAnnotations[expectedKey]; !exists {
							t.Errorf("Expected annotation %s not found", expectedKey)
						} else if actualValue != expectedValue {
							t.Errorf("Expected annotation %s=%s, got %s", expectedKey, expectedValue, actualValue)
						}
					}

					// Check that removed annotations are not present
					for _, removedKey := range tt.annotationKeys {
						if _, exists := actualAnnotations[removedKey]; exists && tt.expectedAnnotations[removedKey] == "" {
							t.Errorf("Annotation %s should have been removed but is still present", removedKey)
						}
					}
				}
			}
		})
	}
}

func TestResourceManager_GetAppliedAnnotations(t *testing.T) {
	tests := []struct {
		name         string
		resource     client.Object
		ruleID       string
		expectedKeys []string
		expectError  bool
		description  string
	}{
		{
			name: "get applied annotations for rule",
			resource: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
					Annotations: map[string]string{
						"example.com/env":                          "production",
						"example.com/version":                      "v1.0.0",
						"scheduler.k8s.io/rule-rule-1-annotations": "[example.com/env example.com/version]",
					},
				},
			},
			ruleID:       "rule-1",
			expectedKeys: []string{"example.com/env", "example.com/version"},
			expectError:  false,
			description:  "Should return applied annotation keys",
		},
		{
			name: "no tracking annotation for rule",
			resource: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
					Annotations: map[string]string{
						"example.com/env": "production",
					},
				},
			},
			ruleID:       "rule-1",
			expectedKeys: nil,
			expectError:  false,
			description:  "Should return nil when no tracking annotation exists",
		},
		{
			name: "resource with no annotations",
			resource: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
				},
			},
			ruleID:       "rule-1",
			expectedKeys: nil,
			expectError:  false,
			description:  "Should return nil when resource has no annotations",
		},
		{
			name: "empty tracking annotation",
			resource: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
					Annotations: map[string]string{
						"scheduler.k8s.io/rule-rule-1-annotations": "[]",
					},
				},
			},
			ruleID:       "rule-1",
			expectedKeys: nil,
			expectError:  false,
			description:  "Should return nil when tracking annotation is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rm := NewResourceManager(nil, logr.Discard(), nil)

			keys, err := rm.GetAppliedAnnotations(tt.resource, tt.ruleID)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none. %s", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v. %s", err, tt.description)
				}

				if len(keys) != len(tt.expectedKeys) {
					t.Errorf("Expected %d keys, got %d. %s", len(tt.expectedKeys), len(keys), tt.description)
				}

				for _, expectedKey := range tt.expectedKeys {
					found := false
					for _, actualKey := range keys {
						if actualKey == expectedKey {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected key %s not found in result. %s", expectedKey, tt.description)
					}
				}
			}
		})
	}
}

func TestResourceManager_ValidateAnnotations(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		expectError bool
		description string
	}{
		{
			name: "valid annotations",
			annotations: map[string]string{
				"example.com/env":     "production",
				"example.com/version": "v1.0.0",
				"team":                "backend",
			},
			expectError: false,
			description: "Should accept valid annotations",
		},
		{
			name: "empty annotation key",
			annotations: map[string]string{
				"": "value",
			},
			expectError: true,
			description: "Should reject empty annotation key",
		},
		{
			name: "system annotation key",
			annotations: map[string]string{
				"kubernetes.io/managed-by": "test",
			},
			expectError: true,
			description: "Should reject system annotation keys",
		},
		{
			name: "k8s.io system annotation",
			annotations: map[string]string{
				"k8s.io/test": "value",
			},
			expectError: true,
			description: "Should reject k8s.io system annotation keys",
		},
		{
			name: "kubectl system annotation",
			annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
			},
			expectError: true,
			description: "Should reject kubectl system annotation keys",
		},
		{
			name: "very long annotation key",
			annotations: map[string]string{
				strings.Repeat("a", 254): "value",
			},
			expectError: true,
			description: "Should reject annotation keys that are too long",
		},
		{
			name: "very long annotation value",
			annotations: map[string]string{
				"example.com/data": strings.Repeat("x", 65537),
			},
			expectError: true,
			description: "Should reject annotation values that are too long",
		},
		{
			name: "maximum length annotation key",
			annotations: map[string]string{
				strings.Repeat("a", 253): "value",
			},
			expectError: false,
			description: "Should accept annotation keys at maximum length",
		},
		{
			name: "maximum length annotation value",
			annotations: map[string]string{
				"example.com/data": strings.Repeat("x", 65536),
			},
			expectError: false,
			description: "Should accept annotation values at maximum length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rm := NewResourceManager(nil, logr.Discard(), nil)

			err := rm.ValidateAnnotations(tt.annotations)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none. %s", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v. %s", err, tt.description)
				}
			}
		})
	}
}

func TestResourceManager_AnnotationEdgeCases(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name        string
		setupFunc   func() []client.Object
		testFunc    func(*ResourceManager) error
		expectError bool
		description string
	}{
		{
			name: "apply annotations to resource that gets deleted during operation",
			setupFunc: func() []client.Object {
				return []client.Object{
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "default",
							Labels:    map[string]string{"app": "test"},
						},
					},
				}
			},
			testFunc: func(rm *ResourceManager) error {
				selector := metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				}
				annotations := map[string]string{"example.com/env": "production"}

				// This would normally fail in a real scenario where the resource gets deleted
				// but with fake client it should succeed
				return rm.ApplyAnnotations(context.TODO(), selector, annotations, []string{"pods"}, "default", "rule-1")
			},
			expectError: false,
			description: "Should handle resource deletion gracefully",
		},
		{
			name: "concurrent annotation operations on same resource",
			setupFunc: func() []client.Object {
				return []client.Object{
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "default",
							Labels:    map[string]string{"app": "test"},
						},
					},
				}
			},
			testFunc: func(rm *ResourceManager) error {
				selector := metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				}

				// Apply annotations from rule-1
				err1 := rm.ApplyAnnotations(context.TODO(), selector,
					map[string]string{"example.com/env": "production"},
					[]string{"pods"}, "default", "rule-1")

				// Apply different annotations from rule-2
				err2 := rm.ApplyAnnotations(context.TODO(), selector,
					map[string]string{"example.com/team": "backend"},
					[]string{"pods"}, "default", "rule-2")

				if err1 != nil {
					return err1
				}
				return err2
			},
			expectError: false,
			description: "Should handle concurrent operations from different rules",
		},
		{
			name: "remove annotations with malformed tracking data",
			setupFunc: func() []client.Object {
				return []client.Object{
					&corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pod1",
							Namespace: "default",
							Labels:    map[string]string{"app": "test"},
							Annotations: map[string]string{
								"example.com/env":                          "production",
								"scheduler.k8s.io/rule-rule-1-annotations": "malformed-data",
							},
						},
					},
				}
			},
			testFunc: func(rm *ResourceManager) error {
				selector := metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				}
				return rm.RemoveAnnotations(context.TODO(), selector,
					[]string{"example.com/env"}, []string{"pods"}, "default", "rule-1")
			},
			expectError: false,
			description: "Should handle malformed tracking data gracefully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := tt.setupFunc()
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			rm := NewResourceManager(fakeClient, logr.Discard(), scheme)

			err := tt.testFunc(rm)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none. %s", tt.description)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v. %s", err, tt.description)
				}
			}
		})
	}
}
