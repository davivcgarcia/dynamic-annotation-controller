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
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

var _ = Describe("Edge Cases and Error Scenarios", func() {
	var (
		testNamespace string
		builder       *TestResourceBuilder
		ruleBuilder   *RuleBuilder
		assertions    *TestAssertions
	)

	BeforeEach(func() {
		// Create a unique namespace for each test
		testNamespace = fmt.Sprintf("edge-test-%d", time.Now().UnixNano())

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Initialize test helpers
		builder = NewTestResourceBuilder(k8sClient, testNamespace)
		ruleBuilder = NewRuleBuilder(k8sClient, testNamespace)
		assertions = NewTestAssertions(k8sClient)
	})

	AfterEach(func() {
		// Clean up namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	Context("Validation Edge Cases", func() {
		It("should reject rules with invalid resource types", func() {
			By("Creating a rule with unsupported resource type")
			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-invalid-resource",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:      "datetime",
						StartTime: &metav1.Time{Time: time.Now().Add(1 * time.Second)},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "invalid-resource",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/invalid": "true",
					},
					TargetResources: []string{"services", "configmaps"}, // Unsupported types
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Verifying rule validation fails")
			assertions.ExpectRulePhase(rule.Name, rule.Namespace, "failed", timeout, interval)
			assertions.ExpectRuleCondition(rule.Name, rule.Namespace, "Validated", metav1.ConditionFalse, timeout, interval)
		})

		It("should reject datetime rules with end time before start time", func() {
			By("Creating a rule with invalid time range")
			startTime := time.Now().Add(10 * time.Second)
			endTime := startTime.Add(-5 * time.Second) // End before start

			rule := ruleBuilder.CreateDateTimeRule(
				"test-invalid-time-range",
				&startTime,
				&endTime,
				map[string]string{"test": "invalid-time"},
				map[string]string{"test.scheduler.io/invalid": "true"},
				[]string{"pods"},
			)

			By("Verifying rule validation fails")
			assertions.ExpectRulePhase(rule.Name, rule.Namespace, "failed", timeout, interval)
		})

		It("should reject cron rules without required fields", func() {
			By("Creating a cron rule without action")
			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron-no-action",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:           "cron",
						CronExpression: "0 * * * *",
						// Missing Action field
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "cron-no-action",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/cron": "true",
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Verifying rule validation fails")
			assertions.ExpectRulePhase(rule.Name, rule.Namespace, "failed", timeout, interval)
		})
	})

	Context("Resource Lifecycle Edge Cases", func() {
		It("should handle resource deletion during annotation application", func() {
			By("Creating a test pod")
			pod := builder.CreatePod("test-pod-deletion", map[string]string{
				"test": "resource-deletion",
			})

			By("Creating a rule with future execution")
			futureTime := time.Now().Add(3 * time.Second)
			rule := ruleBuilder.CreateDateTimeRule(
				"test-resource-deletion",
				&futureTime,
				nil,
				map[string]string{"test": "resource-deletion"},
				map[string]string{"test.scheduler.io/deleted": "true"},
				[]string{"pods"},
			)

			By("Deleting the pod before rule execution")
			time.Sleep(1 * time.Second)
			Expect(k8sClient.Delete(ctx, pod)).To(Succeed())

			By("Waiting for rule execution time")
			time.Sleep(time.Until(futureTime) + 2*time.Second)

			By("Verifying rule execution completes gracefully with zero affected resources")
			Eventually(func() bool {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return false
				}
				return updatedRule.Status.LastExecutionTime != nil &&
					updatedRule.Status.AffectedResources == 0
			}, timeout, interval).Should(BeTrue())
		})

		It("should handle resource creation after rule creation", func() {
			By("Creating a rule before target resources exist")
			rule := ruleBuilder.CreateDateTimeRule(
				"test-late-resource",
				&time.Time{}, // Immediate execution
				nil,
				map[string]string{"test": "late-resource"},
				map[string]string{"test.scheduler.io/late": "true"},
				[]string{"pods"},
			)

			By("Waiting for initial execution with no resources")
			time.Sleep(2 * time.Second)

			By("Creating target resource after rule execution")
			pod := builder.CreatePod("test-pod-late", map[string]string{
				"test": "late-resource",
			})

			By("Updating rule to trigger re-execution")
			var updatedRule schedulerv1.AnnotationScheduleRule
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      rule.Name,
				Namespace: rule.Namespace,
			}, &updatedRule)).To(Succeed())

			// Update the rule to trigger reconciliation
			updatedRule.Spec.Annotations["test.scheduler.io/updated"] = "true"
			Expect(k8sClient.Update(ctx, &updatedRule)).To(Succeed())

			By("Verifying annotations are not applied to late-created resource")
			// Since this is a datetime rule that already executed, it shouldn't apply to new resources
			Consistently(func() map[string]string {
				var currentPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &currentPod)
				if err != nil {
					return nil
				}
				return currentPod.Annotations
			}, 5*time.Second, interval).ShouldNot(HaveKey("test.scheduler.io/late"))
		})
	})

	Context("Concurrent Operations", func() {
		It("should handle multiple rules targeting the same resources", func() {
			By("Creating a test pod")
			pod := builder.CreatePod("test-pod-concurrent", map[string]string{
				"test": "concurrent",
			})

			By("Creating multiple rules with overlapping schedules")
			startTime := time.Now().Add(2 * time.Second)

			rule1 := ruleBuilder.CreateDateTimeRule(
				"test-concurrent-1",
				&startTime,
				nil,
				map[string]string{"test": "concurrent"},
				map[string]string{"test.scheduler.io/rule1": "true"},
				[]string{"pods"},
			)

			rule2 := ruleBuilder.CreateDateTimeRule(
				"test-concurrent-2",
				&startTime,
				nil,
				map[string]string{"test": "concurrent"},
				map[string]string{"test.scheduler.io/rule2": "true"},
				[]string{"pods"},
			)

			By("Waiting for both rules to execute")
			time.Sleep(time.Until(startTime) + 3*time.Second)

			By("Verifying both annotations are applied")
			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).Should(And(
				HaveKeyWithValue("test.scheduler.io/rule1", "true"),
				HaveKeyWithValue("test.scheduler.io/rule2", "true"),
			))

			By("Verifying both rules show successful execution")
			assertions.ExpectRuleExecutionCount(rule1.Name, rule1.Namespace, 1, timeout, interval)
			assertions.ExpectRuleExecutionCount(rule2.Name, rule2.Namespace, 1, timeout, interval)
		})

		It("should handle annotation conflicts gracefully", func() {
			By("Creating a test pod with existing annotations")
			pod := builder.CreatePod("test-pod-conflict", map[string]string{
				"test": "conflict",
			})

			// Add existing annotation
			pod.Annotations = map[string]string{
				"test.scheduler.io/conflict": "original",
			}
			Expect(k8sClient.Update(ctx, pod)).To(Succeed())

			By("Creating a rule that conflicts with existing annotation")
			rule := ruleBuilder.CreateDateTimeRule(
				"test-conflict",
				&time.Time{}, // Immediate execution
				nil,
				map[string]string{"test": "conflict"},
				map[string]string{"test.scheduler.io/conflict": "overwritten"},
				[]string{"pods"},
			)

			By("Verifying rule overwrites the existing annotation")
			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/conflict", "overwritten"))

			By("Verifying rule execution is successful")
			assertions.ExpectRuleExecutionCount(rule.Name, rule.Namespace, 1, timeout, interval)
		})
	})

	Context("Large Scale Operations", func() {
		It("should handle rules with many target resources", func() {
			By("Creating multiple test pods")
			podCount := 10
			pods := make([]*corev1.Pod, podCount)

			for i := 0; i < podCount; i++ {
				pods[i] = builder.CreatePod(
					fmt.Sprintf("test-pod-scale-%d", i),
					map[string]string{"test": "scale"},
				)
			}

			By("Creating a rule targeting all pods")
			rule := ruleBuilder.CreateDateTimeRule(
				"test-scale",
				&time.Time{}, // Immediate execution
				nil,
				map[string]string{"test": "scale"},
				map[string]string{"test.scheduler.io/scale": "true"},
				[]string{"pods"},
			)

			By("Verifying all pods receive annotations")
			for i, pod := range pods {
				Eventually(func() map[string]string {
					var updatedPod corev1.Pod
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      pod.Name,
						Namespace: pod.Namespace,
					}, &updatedPod)
					if err != nil {
						return nil
					}
					return updatedPod.Annotations
				}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/scale", "true"),
					fmt.Sprintf("Pod %d should have annotation", i))
			}

			By("Verifying rule shows correct affected resource count")
			Eventually(func() int32 {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return 0
				}
				return updatedRule.Status.AffectedResources
			}, timeout, interval).Should(Equal(int32(podCount)))
		})
	})

	Context("Timing Edge Cases", func() {
		It("should handle rules with very short execution windows", func() {
			By("Creating a rule with 1-second execution window")
			startTime := time.Now().Add(1 * time.Second)
			endTime := startTime.Add(1 * time.Second)

			pod := builder.CreatePod("test-pod-short-window", map[string]string{
				"test": "short-window",
			})

			rule := ruleBuilder.CreateDateTimeRule(
				"test-short-window",
				&startTime,
				&endTime,
				map[string]string{"test": "short-window"},
				map[string]string{"test.scheduler.io/short": "true"},
				[]string{"pods"},
			)

			By("Verifying annotation is applied during the window")
			time.Sleep(time.Until(startTime) + 500*time.Millisecond)

			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/short", "true"))

			By("Verifying annotation is removed after the window")
			time.Sleep(time.Until(endTime) + 2*time.Second)

			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).ShouldNot(HaveKey("test.scheduler.io/short"))

			By("Verifying rule is marked as completed")
			assertions.ExpectRulePhase(rule.Name, rule.Namespace, "completed", timeout, interval)
		})

		It("should handle cron rules with high frequency", func() {
			By("Creating a cron rule that runs every minute")
			pod := builder.CreatePod("test-pod-high-freq", map[string]string{
				"test": "high-frequency",
			})

			rule := ruleBuilder.CreateCronRule(
				"test-high-frequency",
				"* * * * *", // Every minute
				"apply",
				map[string]string{"test": "high-frequency"},
				map[string]string{"test.scheduler.io/high-freq": time.Now().Format(time.RFC3339)},
				[]string{"pods"},
			)

			By("Verifying rule executes multiple times")
			// Wait for at least 2 executions
			time.Sleep(2*time.Minute + 30*time.Second)

			assertions.ExpectRuleExecutionCount(rule.Name, rule.Namespace, 2, timeout, interval)

			By("Verifying annotations are updated with each execution")
			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).Should(HaveKey("test.scheduler.io/high-freq"))
		})
	})
})
