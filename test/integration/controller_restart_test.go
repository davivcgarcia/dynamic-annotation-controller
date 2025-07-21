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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

var _ = Describe("Controller Restart and State Recovery", func() {
	var (
		testNamespace string
		testPod       *corev1.Pod
		restartMgr    manager.Manager
	)

	BeforeEach(func() {
		// Create a unique namespace for each test
		testNamespace = fmt.Sprintf("restart-test-%d", time.Now().UnixNano())

		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Create test pod
		testPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-restart",
				Namespace: testNamespace,
				Labels: map[string]string{
					"test": "restart-recovery",
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
	})

	AfterEach(func() {
		// Stop restart manager if it was created
		if restartMgr != nil {
			cancel() // This will stop the manager
		}

		// Clean up namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})

	Context("State Recovery After Restart", func() {
		It("should recover active rules after controller restart", func() {
			By("Creating a rule with future execution time")
			futureTime := time.Now().Add(10 * time.Second)

			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-restart-recovery",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:      "datetime",
						StartTime: &metav1.Time{Time: futureTime},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "restart-recovery",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/restart-test": "true",
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Verifying rule becomes active")
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

			By("Simulating controller restart by creating new manager")
			// Cancel current context to stop existing manager
			cancel()

			// Create new context and manager
			var newCancel context.CancelFunc
			ctx, newCancel = context.WithCancel(context.TODO())
			defer newCancel()

			var err error
			restartMgr, err = ctrl.NewManager(cfg, ctrl.Options{
				Scheme: scheme.Scheme,
				// Disable metrics server for tests
				Metrics: server.Options{
					BindAddress: "0",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Setup controller again
			setupControllerForRestart(restartMgr)

			// Start the new manager
			go func() {
				defer GinkgoRecover()
				err = restartMgr.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			// Wait for manager to be ready
			Eventually(func() bool {
				return restartMgr.GetCache().WaitForCacheSync(ctx)
			}, timeout, interval).Should(BeTrue())

			By("Verifying rule is still scheduled after restart")
			Eventually(func() string {
				var recoveredRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &recoveredRule)
				if err != nil {
					return ""
				}
				return recoveredRule.Status.Phase
			}, timeout, interval).Should(Equal("active"))

			By("Waiting for execution time and verifying annotations are still applied")
			time.Sleep(time.Until(futureTime) + 2*time.Second)

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
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/restart-test", "true"))
		})

		It("should handle rules that should have executed during downtime", func() {
			By("Creating a rule with past execution time")
			pastTime := time.Now().Add(-5 * time.Second)

			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-past-execution",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:      "datetime",
						StartTime: &metav1.Time{Time: pastTime},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "restart-recovery",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/past-execution": "true",
					},
					TargetResources: []string{"pods"},
				},
			}

			// Create rule directly in etcd (simulating it was created before restart)
			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Simulating controller restart")
			cancel()

			var newCancel context.CancelFunc
			ctx, newCancel = context.WithCancel(context.TODO())
			defer newCancel()

			var err error
			restartMgr, err = ctrl.NewManager(cfg, ctrl.Options{
				Scheme: scheme.Scheme,
				// Disable metrics server for tests
				Metrics: server.Options{
					BindAddress: "0",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			setupControllerForRestart(restartMgr)

			go func() {
				defer GinkgoRecover()
				err = restartMgr.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return restartMgr.GetCache().WaitForCacheSync(ctx)
			}, timeout, interval).Should(BeTrue())

			By("Verifying past-due rule is executed immediately after restart")
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
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/past-execution", "true"))

			By("Verifying rule status is updated")
			Eventually(func() bool {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return false
				}
				return updatedRule.Status.LastExecutionTime != nil
			}, timeout, interval).Should(BeTrue())
		})

		It("should recover cron-based rules correctly", func() {
			By("Creating a cron rule")
			rule := &schedulerv1.AnnotationScheduleRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cron-restart",
					Namespace: testNamespace,
				},
				Spec: schedulerv1.AnnotationScheduleRuleSpec{
					Schedule: schedulerv1.ScheduleConfig{
						Type:           "cron",
						CronExpression: "*/2 * * * *", // Every 2 minutes
						Action:         "apply",
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "restart-recovery",
						},
					},
					Annotations: map[string]string{
						"test.scheduler.io/cron-restart": "true",
					},
					TargetResources: []string{"pods"},
				},
			}

			Expect(k8sClient.Create(ctx, rule)).To(Succeed())

			By("Waiting for initial execution")
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
			}, time.Minute*3, interval).Should(HaveKeyWithValue("test.scheduler.io/cron-restart", "true"))

			By("Recording initial execution count")
			var initialExecutionCount int64
			Eventually(func() int64 {
				var updatedRule schedulerv1.AnnotationScheduleRule
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      rule.Name,
					Namespace: rule.Namespace,
				}, &updatedRule)
				if err != nil {
					return 0
				}
				initialExecutionCount = updatedRule.Status.ExecutionCount
				return initialExecutionCount
			}, timeout, interval).Should(BeNumerically(">", 0))

			By("Simulating controller restart")
			cancel()

			var newCancel context.CancelFunc
			ctx, newCancel = context.WithCancel(context.TODO())
			defer newCancel()

			var err error
			restartMgr, err = ctrl.NewManager(cfg, ctrl.Options{
				Scheme: scheme.Scheme,
				// Disable metrics server for tests
				Metrics: server.Options{
					BindAddress: "0",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			setupControllerForRestart(restartMgr)

			go func() {
				defer GinkgoRecover()
				err = restartMgr.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return restartMgr.GetCache().WaitForCacheSync(ctx)
			}, timeout, interval).Should(BeTrue())

			By("Verifying cron rule continues executing after restart")
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
			}, time.Minute*3, interval).Should(BeNumerically(">", initialExecutionCount))
		})
	})

	Context("Graceful Shutdown and Recovery", func() {
		It("should handle graceful shutdown without losing scheduled tasks", func() {
			By("Creating multiple rules with different schedules")
			rules := []*schedulerv1.AnnotationScheduleRule{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shutdown-1",
						Namespace: testNamespace,
					},
					Spec: schedulerv1.AnnotationScheduleRuleSpec{
						Schedule: schedulerv1.ScheduleConfig{
							Type:      "datetime",
							StartTime: &metav1.Time{Time: time.Now().Add(15 * time.Second)},
						},
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "restart-recovery",
							},
						},
						Annotations: map[string]string{
							"test.scheduler.io/shutdown-1": "true",
						},
						TargetResources: []string{"pods"},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-shutdown-2",
						Namespace: testNamespace,
					},
					Spec: schedulerv1.AnnotationScheduleRuleSpec{
						Schedule: schedulerv1.ScheduleConfig{
							Type:           "cron",
							CronExpression: "*/3 * * * *", // Every 3 minutes
							Action:         "apply",
						},
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"test": "restart-recovery",
							},
						},
						Annotations: map[string]string{
							"test.scheduler.io/shutdown-2": "true",
						},
						TargetResources: []string{"pods"},
					},
				},
			}

			for _, rule := range rules {
				Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			}

			By("Verifying all rules become active")
			for _, rule := range rules {
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
			}

			By("Performing graceful shutdown and restart")
			cancel() // Graceful shutdown

			// Wait a moment for graceful shutdown
			time.Sleep(2 * time.Second)

			var newCancel context.CancelFunc
			ctx, newCancel = context.WithCancel(context.TODO())
			defer newCancel()

			var err error
			restartMgr, err = ctrl.NewManager(cfg, ctrl.Options{
				Scheme: scheme.Scheme,
				// Disable metrics server for tests
				Metrics: server.Options{
					BindAddress: "0",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			setupControllerForRestart(restartMgr)

			go func() {
				defer GinkgoRecover()
				err = restartMgr.Start(ctx)
				Expect(err).NotTo(HaveOccurred())
			}()

			Eventually(func() bool {
				return restartMgr.GetCache().WaitForCacheSync(ctx)
			}, timeout, interval).Should(BeTrue())

			By("Verifying all rules are recovered and remain active")
			for _, rule := range rules {
				Eventually(func() string {
					var recoveredRule schedulerv1.AnnotationScheduleRule
					err := k8sClient.Get(ctx, types.NamespacedName{
						Name:      rule.Name,
						Namespace: rule.Namespace,
					}, &recoveredRule)
					if err != nil {
						return ""
					}
					return recoveredRule.Status.Phase
				}, timeout, interval).Should(Equal("active"))
			}

			By("Verifying scheduled executions still occur after recovery")
			// Wait for the datetime rule to execute
			time.Sleep(20 * time.Second)

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
			}, timeout, interval).Should(HaveKeyWithValue("test.scheduler.io/shutdown-1", "true"))
		})
	})
})

// setupControllerForRestart sets up the controller for restart tests
func setupControllerForRestart(mgr manager.Manager) {
	setupControllerWithManager(mgr)
}
