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

package integration

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

// TestResourceBuilder helps create test resources for integration tests
type TestResourceBuilder struct {
	client    client.Client
	namespace string
}

// NewTestResourceBuilder creates a new test resource builder
func NewTestResourceBuilder(client client.Client, namespace string) *TestResourceBuilder {
	return &TestResourceBuilder{
		client:    client,
		namespace: namespace,
	}
}

// CreatePod creates a test pod with the given name and labels
func (b *TestResourceBuilder) CreatePod(name string, labels map[string]string) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "nginx:latest",
				},
			},
		},
	}

	Expect(b.client.Create(context.TODO(), pod)).To(Succeed())
	return pod
}

// CreateDeployment creates a test deployment with the given name and labels
func (b *TestResourceBuilder) CreateDeployment(name string, labels map[string]string) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}

	Expect(b.client.Create(context.TODO(), deployment)).To(Succeed())
	return deployment
}

// CreateStatefulSet creates a test statefulset with the given name and labels
func (b *TestResourceBuilder) CreateStatefulSet(name string, labels map[string]string) *appsv1.StatefulSet {
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.namespace,
			Labels:    labels,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			},
			ServiceName: name,
		},
	}

	Expect(b.client.Create(context.TODO(), statefulSet)).To(Succeed())
	return statefulSet
}

// CreateDaemonSet creates a test daemonset with the given name and labels
func (b *TestResourceBuilder) CreateDaemonSet(name string, labels map[string]string) *appsv1.DaemonSet {
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}

	Expect(b.client.Create(context.TODO(), daemonSet)).To(Succeed())
	return daemonSet
}

// RuleBuilder helps create test AnnotationScheduleRules
type RuleBuilder struct {
	client    client.Client
	namespace string
}

// NewRuleBuilder creates a new rule builder
func NewRuleBuilder(client client.Client, namespace string) *RuleBuilder {
	return &RuleBuilder{
		client:    client,
		namespace: namespace,
	}
}

// CreateDateTimeRule creates a datetime-based rule
func (b *RuleBuilder) CreateDateTimeRule(name string, startTime, endTime *time.Time, selector map[string]string, annotations map[string]string, targetResources []string) *schedulerv1.AnnotationScheduleRule {
	rule := &schedulerv1.AnnotationScheduleRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.namespace,
		},
		Spec: schedulerv1.AnnotationScheduleRuleSpec{
			Schedule: schedulerv1.ScheduleConfig{
				Type: "datetime",
			},
			Selector: metav1.LabelSelector{
				MatchLabels: selector,
			},
			Annotations:     annotations,
			TargetResources: targetResources,
		},
	}

	if startTime != nil {
		rule.Spec.Schedule.StartTime = &metav1.Time{Time: *startTime}
	}
	if endTime != nil {
		rule.Spec.Schedule.EndTime = &metav1.Time{Time: *endTime}
	}

	Expect(b.client.Create(context.TODO(), rule)).To(Succeed())
	return rule
}

// CreateCronRule creates a cron-based rule
func (b *RuleBuilder) CreateCronRule(name, cronExpression, action string, selector map[string]string, annotations map[string]string, targetResources []string) *schedulerv1.AnnotationScheduleRule {
	rule := &schedulerv1.AnnotationScheduleRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: b.namespace,
		},
		Spec: schedulerv1.AnnotationScheduleRuleSpec{
			Schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: cronExpression,
				Action:         action,
			},
			Selector: metav1.LabelSelector{
				MatchLabels: selector,
			},
			Annotations:     annotations,
			TargetResources: targetResources,
		},
	}

	Expect(b.client.Create(context.TODO(), rule)).To(Succeed())
	return rule
}

// TestAssertions provides common assertion helpers for integration tests
type TestAssertions struct {
	client client.Client
}

// NewTestAssertions creates a new test assertions helper
func NewTestAssertions(client client.Client) *TestAssertions {
	return &TestAssertions{client: client}
}

// ExpectRulePhase asserts that a rule has the expected phase
func (a *TestAssertions) ExpectRulePhase(ruleName, namespace, expectedPhase string, timeout, interval time.Duration) {
	Eventually(func() string {
		var rule schedulerv1.AnnotationScheduleRule
		err := a.client.Get(context.TODO(), types.NamespacedName{
			Name:      ruleName,
			Namespace: namespace,
		}, &rule)
		if err != nil {
			return ""
		}
		return rule.Status.Phase
	}, timeout, interval).Should(Equal(expectedPhase))
}

// ExpectResourceAnnotations asserts that a resource has the expected annotations
func (a *TestAssertions) ExpectResourceAnnotations(resourceName, namespace string, resource client.Object, expectedAnnotations map[string]string, timeout, interval time.Duration) {
	Eventually(func() bool {
		err := a.client.Get(context.TODO(), types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}, resource)
		if err != nil {
			return false
		}
		annotations := resource.GetAnnotations()
		if annotations == nil {
			return len(expectedAnnotations) == 0
		}
		for key, expectedValue := range expectedAnnotations {
			if actualValue, exists := annotations[key]; !exists || actualValue != expectedValue {
				return false
			}
		}
		return true
	}, timeout, interval).Should(BeTrue())
}

// ExpectResourceMissingAnnotations asserts that a resource does not have specific annotations
func (a *TestAssertions) ExpectResourceMissingAnnotations(resourceName, namespace string, resource client.Object, missingKeys []string, timeout, interval time.Duration) {
	Eventually(func() bool {
		err := a.client.Get(context.TODO(), types.NamespacedName{
			Name:      resourceName,
			Namespace: namespace,
		}, resource)
		if err != nil {
			return false
		}
		annotations := resource.GetAnnotations()
		if annotations == nil {
			return true
		}
		for _, key := range missingKeys {
			if _, exists := annotations[key]; exists {
				return false
			}
		}
		return true
	}, timeout, interval).Should(BeTrue())
}

// ExpectRuleCondition asserts that a rule has a specific condition
func (a *TestAssertions) ExpectRuleCondition(ruleName, namespace, conditionType string, status metav1.ConditionStatus, timeout, interval time.Duration) {
	Eventually(func() bool {
		var rule schedulerv1.AnnotationScheduleRule
		err := a.client.Get(context.TODO(), types.NamespacedName{
			Name:      ruleName,
			Namespace: namespace,
		}, &rule)
		if err != nil {
			return false
		}

		for _, condition := range rule.Status.Conditions {
			if condition.Type == conditionType && condition.Status == status {
				return true
			}
		}
		return false
	}, timeout, interval).Should(BeTrue())
}

// ExpectRuleExecutionCount asserts that a rule has executed a minimum number of times
func (a *TestAssertions) ExpectRuleExecutionCount(ruleName, namespace string, minCount int64, timeout, interval time.Duration) {
	Eventually(func() int64 {
		var rule schedulerv1.AnnotationScheduleRule
		err := a.client.Get(context.TODO(), types.NamespacedName{
			Name:      ruleName,
			Namespace: namespace,
		}, &rule)
		if err != nil {
			return 0
		}
		return rule.Status.ExecutionCount
	}, timeout, interval).Should(BeNumerically(">=", minCount))
}

// WaitForRuleDeletion waits for a rule to be completely deleted
func (a *TestAssertions) WaitForRuleDeletion(ruleName, namespace string, timeout, interval time.Duration) {
	Eventually(func() bool {
		var rule schedulerv1.AnnotationScheduleRule
		err := a.client.Get(context.TODO(), types.NamespacedName{
			Name:      ruleName,
			Namespace: namespace,
		}, &rule)
		return err != nil
	}, timeout, interval).Should(BeTrue())
}

// TestScenario represents a complete test scenario
type TestScenario struct {
	Name        string
	Description string
	Setup       func() error
	Execute     func() error
	Verify      func() error
	Cleanup     func() error
}

// RunScenario executes a complete test scenario
func RunScenario(scenario TestScenario) {
	By(fmt.Sprintf("Setting up scenario: %s", scenario.Name))
	if scenario.Setup != nil {
		Expect(scenario.Setup()).To(Succeed())
	}

	By(fmt.Sprintf("Executing scenario: %s", scenario.Name))
	if scenario.Execute != nil {
		Expect(scenario.Execute()).To(Succeed())
	}

	By(fmt.Sprintf("Verifying scenario: %s", scenario.Name))
	if scenario.Verify != nil {
		Expect(scenario.Verify()).To(Succeed())
	}

	By(fmt.Sprintf("Cleaning up scenario: %s", scenario.Name))
	if scenario.Cleanup != nil {
		Expect(scenario.Cleanup()).To(Succeed())
	}
}

// int32Ptr returns a pointer to an int32 value
func int32Ptr(i int32) *int32 {
	return &i
}
