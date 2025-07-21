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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

var _ = Describe("AnnotationScheduleRule Integration Tests", func() {
	var (
		testNamespace  string
		testPod        *corev1.Pod
		testDeployment *appsv1.Deployment
	)

	BeforeEach(func() {
		// Create a unique namespace for each test
		testNamespace = fmt.Sprintf("test-ns-%d", time.Now().UnixNano())

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Create test resources
		testPod, testDeployment = createTestResources(testNamespace)
	})

	AfterEach(func() {
		// Clean up namespace and all resources
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	Context("Datetime-based Annotation Scheduling", func() {
		It("should apply annotations at start time", func() {
			By("Creating a datetime rule with immediate start time")
			startTime := time.Now().Add(2 * time.Second)

			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-datetime-apply",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:      "datetime",
						StartTime: &metav1.Time{Time: startTime},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "datetime-apply",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/applied": "true",
						"test.scheduler.io/time":    startTime.Format(time.RFC3339),
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Verifying rule status becomes active")
			Eventually(func() string {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return ""
				}
				return updatedRule.Status.Phase
			}, timeout, interval).Should(Equal("active"))

			By("Waiting for start time and verifying annotations are applied")
			time.Sleep(time.Until(startTime) + time.Second)

			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testPod.Name,
					Namespace: testPod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/applied", "true"))

			By("Verifying rule status is updated with execution information")
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
					updatedRule.Status.AffectedResources > 0
			}, timeout, interval).Should(BeTrue())
		})

		It("should remove annotations at end time", func() {
			By("Creating a datetime rule with start and end times")
			startTime := time.Now().Add(1 * time.Second)
			endTime := startTime.Add(3 * time.Second)

			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-datetime-remove",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:      "datetime",
						StartTime: &metav1.Time{Time: startTime},
						EndTime:   &metav1.Time{Time: endTime},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "datetime-remove",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/temporary": "true",
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Waiting for start time and verifying annotations are applied")
			time.Sleep(time.Until(startTime) + time.Second)

			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testPod.Name,
					Namespace: testPod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/temporary", "true"))

			By("Waiting for end time and verifying annotations are removed")
			time.Sleep(time.Until(endTime) + time.Second)

			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testPod.Name,
					Namespace: testPod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).ShouldNot(HaveKey("test.scheduler.io/temporary"))

			By("Verifying rule status shows completion")
			Eventually(func() string {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return ""
				}
				return updatedRule.Status.Phase
			}, timeout, interval).Should(Equal("completed"))
		})
	})

	Context("Cron-based Annotation Scheduling", func() {
		It("should apply annotations based on cron schedule", func() {
			By("Creating a cron rule that runs every minute")
			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron-apply",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:           "cron",
						CronExpression: "* * * * *", // Every minute
						Action:         "apply",
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "cron-apply",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/cron-applied": "true",
						"test.scheduler.io/last-run":     time.Now().Format(time.RFC3339),
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Verifying rule status becomes active")
			Eventually(func() string {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return ""
				}
				return updatedRule.Status.Phase
			}, timeout, interval).Should(Equal("active"))

			By("Waiting for cron execution and verifying annotations are applied")
			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testPod.Name,
					Namespace: testPod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, time.Minute*2, interval).Should(HaveKeyWithValue("test.scheduler.io/cron-applied", "true"))

			By("Verifying rule execution count increases")
			Eventually(func() int64 {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return 0
				}
				return updatedRule.Status.ExecutionCount
			}, time.Minute*2, interval).Should(BeNumerically(">", 0))
		})

		It("should remove annotations based on cron schedule", func() {
			By("Pre-applying annotations to test removal")
			testPod.Annotations["test.scheduler.io/to-remove"] = "true"
			Expect(k8sClient.Update(ctx, testPod)).To(Succeed())

			By("Creating a cron rule that removes annotations")
			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron-remove",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:           "cron",
						CronExpression: "* * * * *", // Every minute
						Action:         "remove",
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "cron-remove",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/to-remove": "true",
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Waiting for cron execution and verifying annotations are removed")
			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testPod.Name,
					Namespace: testPod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, time.Minute*2, interval).ShouldNot(HaveKey("test.scheduler.io/to-remove"))
		})
	})

	Context("Rule Deletion and Cleanup", func() {
		It("should clean up applied annotations when rule is deleted", func() {
			By("Creating a rule and applying annotations")
			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cleanup",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:      "datetime",
						StartTime: &metav1.Time{Time: time.Now().Add(1 * time.Second)},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "cleanup",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/cleanup-test": "true",
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Waiting for annotations to be applied")
			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testPod.Name,
					Namespace: testPod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/cleanup-test", "true"))

			By("Deleting the rule")
			Expect(k8sClient.Delete(ctx, rule)).To(Succeed())

			By("Verifying annotations are cleaned up")
			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testPod.Name,
					Namespace: testPod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).ShouldNot(HaveKey("test.scheduler.io/cleanup-test"))

			By("Verifying rule is completely removed")
			Eventually(func() bool {
				var deletedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &deletedRule)
				return err != nil
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Multiple Resource Types", func() {
		It("should apply annotations to multiple resource types", func() {
			By("Creating a rule targeting multiple resource types")
			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-multi-resource",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:      "datetime",
						StartTime: &metav1.Time{Time: time.Now().Add(1 * time.Second)},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "multi-resource",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/multi": "true",
					},
					TargetResources: []string{"pods", "deployments"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Waiting for annotations to be applied to pod")
			Eventually(func() map[string]string {
				var updatedPod corev1.Pod
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testPod.Name,
					Namespace: testPod.Namespace,
				}, &updatedPod)
				if err != nil {
					return nil
				}
				return updatedPod.Annotations
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/multi", "true"))

			By("Waiting for annotations to be applied to deployment")
			Eventually(func() map[string]string {
				var updatedDeployment appsv1.Deployment
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      testDeployment.Name,
					Namespace: testDeployment.Namespace,
				}, &updatedDeployment)
				if err != nil {
					return nil
				}
				return updatedDeployment.Annotations
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/multi", "true"))

			By("Verifying rule status shows correct affected resource count")
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
			}, timeout, interval).Should(Equal(int32(2))) // Pod + Deployment
		})
	})

	Context("Error Handling and Recovery", func() {
		It("should handle invalid schedule configurations", func() {
			By("Creating a rule with invalid cron expression")
			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-invalid-cron",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:           "cron",
						CronExpression: "invalid-cron",
						Action:         "apply",
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "invalid",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/invalid": "true",
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Verifying rule status shows validation failure")
			Eventually(func() string {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return ""
				}
				return updatedRule.Status.Phase
			}, timeout, interval).Should(Equal("failed"))

			By("Verifying error condition is set")
			Eventually(func() bool {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return false
				}

				for _, condition := range updatedRule.Status.Conditions {
					if condition.Type == "Validated" && condition.Status == metav1.ConditionFalse {
						return true
					}
				}
				return false
			}, timeout, interval).Should(BeTrue())
		})

		It("should handle missing target resources gracefully", func() {
			By("Creating a rule targeting non-existent resources")
			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-no-targets",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:      "datetime",
						StartTime: &metav1.Time{Time: time.Now().Add(1 * time.Second)},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "non-existent",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/no-targets": "true",
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Verifying rule becomes active despite no matching resources")
			Eventually(func() string {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return ""
				}
				return updatedRule.Status.Phase
			}, timeout, interval).Should(Equal("active"))

			By("Verifying execution completes with zero affected resources")
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
	})
})

// createTestResources creates test pods and deployments for integration tests
func createTestResources(namespace string) (*corev1.Pod, *appsv1.Deployment) {
	// Create test pod for datetime-apply tests
	testPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-datetime-apply",
			Namespace: namespace,
			Labels: map[string]string{
				"test": "datetime-apply",
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
	}
	Expect(k8sClient.Create(ctx, testPod)).To(Succeed())

	// Create additional test pods for other scenarios
	scenarios := []string{"datetime-remove", "cron-apply", "cron-remove", "cleanup", "multi-resource"}
	for _, scenario := range scenarios {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-pod-%s", scenario),
				Namespace: namespace,
				Labels: map[string]string{
					"test": scenario,
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
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
	}

	// Create test deployment for multi-resource tests
	testDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment-multi-resource",
			Namespace: namespace,
			Labels: map[string]string{
				"test": "multi-resource",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-deployment",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-deployment",
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
	Expect(k8sClient.Create(ctx, testDeployment)).To(Succeed())

	return testPod, testDeployment
}
