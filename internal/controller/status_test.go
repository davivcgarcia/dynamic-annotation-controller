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

package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/scheduler"
)

var _ = Describe("Status Management", func() {
	var (
		ctx        context.Context
		reconciler *AnnotationScheduleRuleReconciler
		rule       *schedulerv1.AnnotationScheduleRule
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create a test rule
		rule = &schedulerv1.AnnotationScheduleRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rule",
				Namespace: "default",
			},
			Spec: schedulerv1.AnnotationScheduleRuleSpec{
				Schedule: schedulerv1.ScheduleConfig{
					Type:      "datetime",
					StartTime: &metav1.Time{Time: time.Now().Add(time.Hour)},
					EndTime:   &metav1.Time{Time: time.Now().Add(2 * time.Hour)},
				},
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				Annotations: map[string]string{
					"scheduler.k8s.io/managed-test": "test-value",
				},
				TargetResources: []string{"pods"},
			},
		}

		// Create the rule in the cluster
		Expect(k8sClient.Create(ctx, rule)).To(Succeed())

		// Initialize reconciler with test client
		reconciler = &AnnotationScheduleRuleReconciler{
			Client:  k8sClient,
			Scheme:  k8sClient.Scheme(),
			Log:     logf.Log.WithName("test-reconciler"),
			Metrics: &ExecutionMetrics{},
		}
	})

	AfterEach(func() {
		// Clean up the rule
		Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
	})

	Describe("Phase Management", func() {
		It("should update rule phase correctly", func() {
			// Test phase transition from pending to active
			reconciler.updateRuleStatus(ctx, rule, PhaseActive, "Rule is now active")

			Expect(rule.Status.Phase).To(Equal(PhaseActive))

			// Check that condition was set
			found := false
			for _, condition := range rule.Status.Conditions {
				if condition.Type == ConditionTypeReady && condition.Status == metav1.ConditionTrue {
					found = true
					Expect(condition.Reason).To(Equal(ReasonRuleActive))
					Expect(condition.Message).To(Equal("Rule is now active"))
					break
				}
			}
			Expect(found).To(BeTrue(), "Ready condition should be set")
		})

		It("should handle phase transition to failed", func() {
			reconciler.updateRuleStatus(ctx, rule, PhaseFailed, "Validation failed")

			Expect(rule.Status.Phase).To(Equal(PhaseFailed))

			// Check that ready condition was set to false
			found := false
			for _, condition := range rule.Status.Conditions {
				if condition.Type == ConditionTypeReady && condition.Status == metav1.ConditionFalse {
					found = true
					Expect(condition.Reason).To(Equal(ReasonRuleFailed))
					break
				}
			}
			Expect(found).To(BeTrue(), "Ready condition should be false for failed phase")
		})

		It("should handle phase transition to completed", func() {
			reconciler.updateRuleStatus(ctx, rule, PhaseCompleted, "Rule execution completed")

			Expect(rule.Status.Phase).To(Equal(PhaseCompleted))

			// Check that ready condition was set to true with completed reason
			found := false
			for _, condition := range rule.Status.Conditions {
				if condition.Type == ConditionTypeReady && condition.Status == metav1.ConditionTrue {
					found = true
					Expect(condition.Reason).To(Equal(ReasonRuleCompleted))
					break
				}
			}
			Expect(found).To(BeTrue(), "Ready condition should be true for completed phase")
		})
	})

	Describe("Condition Management", func() {
		It("should set validation conditions correctly", func() {
			// Test successful validation
			reconciler.setCondition(rule, ConditionTypeValidated, metav1.ConditionTrue, ReasonValidationSucceeded, "Validation passed")

			found := false
			for _, condition := range rule.Status.Conditions {
				if condition.Type == ConditionTypeValidated {
					found = true
					Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					Expect(condition.Reason).To(Equal(ReasonValidationSucceeded))
					Expect(condition.Message).To(Equal("Validation passed"))
					break
				}
			}
			Expect(found).To(BeTrue(), "Validation condition should be set")
		})

		It("should set error conditions correctly", func() {
			errorMsg := "Test error occurred"
			reconciler.setCondition(rule, ConditionTypeError, metav1.ConditionTrue, ReasonExecutionFailed, errorMsg)

			found := false
			for _, condition := range rule.Status.Conditions {
				if condition.Type == ConditionTypeError {
					found = true
					Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					Expect(condition.Reason).To(Equal(ReasonExecutionFailed))
					Expect(condition.Message).To(Equal(errorMsg))
					break
				}
			}
			Expect(found).To(BeTrue(), "Error condition should be set")
		})

		It("should update existing conditions", func() {
			// Set initial condition
			reconciler.setCondition(rule, ConditionTypeScheduled, metav1.ConditionFalse, ReasonSchedulingFailed, "Initial failure")

			// Update the same condition
			reconciler.setCondition(rule, ConditionTypeScheduled, metav1.ConditionTrue, ReasonSchedulingSucceeded, "Now succeeded")

			// Should have only one condition of this type
			count := 0
			for _, condition := range rule.Status.Conditions {
				if condition.Type == ConditionTypeScheduled {
					count++
					Expect(condition.Status).To(Equal(metav1.ConditionTrue))
					Expect(condition.Reason).To(Equal(ReasonSchedulingSucceeded))
					Expect(condition.Message).To(Equal("Now succeeded"))
				}
			}
			Expect(count).To(Equal(1), "Should have exactly one condition of each type")
		})
	})

	Describe("Execution Metrics", func() {
		It("should record successful execution", func() {
			metrics := &ExecutionMetrics{}

			// Record a successful execution
			metrics.RecordExecution(true, 100*time.Millisecond)

			total, successful, failed, lastExecution, avgDuration := metrics.GetMetrics()
			Expect(total).To(Equal(int64(1)))
			Expect(successful).To(Equal(int64(1)))
			Expect(failed).To(Equal(int64(0)))
			Expect(lastExecution).To(BeTemporally("~", time.Now(), time.Second))
			Expect(avgDuration).To(Equal(100 * time.Millisecond))
		})

		It("should record failed execution", func() {
			metrics := &ExecutionMetrics{}

			// Record a failed execution
			metrics.RecordExecution(false, 50*time.Millisecond)

			total, successful, failed, lastExecution, avgDuration := metrics.GetMetrics()
			Expect(total).To(Equal(int64(1)))
			Expect(successful).To(Equal(int64(0)))
			Expect(failed).To(Equal(int64(1)))
			Expect(lastExecution).To(BeTemporally("~", time.Now(), time.Second))
			Expect(avgDuration).To(Equal(50 * time.Millisecond))
		})

		It("should calculate average execution time correctly", func() {
			metrics := &ExecutionMetrics{}

			// Record multiple executions
			metrics.RecordExecution(true, 100*time.Millisecond)
			metrics.RecordExecution(true, 200*time.Millisecond)

			total, successful, failed, _, avgDuration := metrics.GetMetrics()
			Expect(total).To(Equal(int64(2)))
			Expect(successful).To(Equal(int64(2)))
			Expect(failed).To(Equal(int64(0)))
			Expect(avgDuration).To(Equal(150 * time.Millisecond))
		})
	})

	Describe("Error Handling", func() {
		It("should create detailed error messages", func() {
			testErr := errors.New("test error")

			detailedMsg := reconciler.getDetailedErrorMessage(testErr, "Validation", rule)

			Expect(detailedMsg).To(ContainSubstring("Validation failed"))
			Expect(detailedMsg).To(ContainSubstring("test-rule"))
			Expect(detailedMsg).To(ContainSubstring("default"))
			Expect(detailedMsg).To(ContainSubstring("test error"))
			Expect(detailedMsg).To(ContainSubstring("pods"))
		})

		It("should include cron details in error messages", func() {
			rule.Spec.Schedule = schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 0 * * *",
				Action:         "apply",
			}

			testErr := errors.New("cron error")
			detailedMsg := reconciler.getDetailedErrorMessage(testErr, "Scheduling", rule)

			Expect(detailedMsg).To(ContainSubstring("cron: 0 0 * * *"))
			Expect(detailedMsg).To(ContainSubstring("action: apply"))
		})

		It("should include datetime details in error messages", func() {
			startTime := time.Now().Add(time.Hour)
			endTime := time.Now().Add(2 * time.Hour)

			rule.Spec.Schedule = schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: startTime},
				EndTime:   &metav1.Time{Time: endTime},
			}

			testErr := errors.New("datetime error")
			detailedMsg := reconciler.getDetailedErrorMessage(testErr, "Execution", rule)

			Expect(detailedMsg).To(ContainSubstring("start:"))
			Expect(detailedMsg).To(ContainSubstring("end:"))
		})
	})

	Describe("Status Logging", func() {
		It("should log rule status with all relevant information", func() {
			// Set up rule status
			rule.Status.Phase = PhaseActive
			rule.Status.AffectedResources = 5
			rule.Status.LastExecutionTime = &metav1.Time{Time: time.Now().Add(-time.Hour)}
			rule.Status.NextExecutionTime = &metav1.Time{Time: time.Now().Add(time.Hour)}

			// Add some conditions
			reconciler.setCondition(rule, ConditionTypeReady, metav1.ConditionTrue, ReasonRuleActive, "Rule is active")
			reconciler.setCondition(rule, ConditionTypeValidated, metav1.ConditionTrue, ReasonValidationSucceeded, "Validation passed")

			// This should not panic and should log appropriate information
			Expect(func() { reconciler.logRuleStatus(rule) }).NotTo(Panic())
		})
	})
})

var _ = Describe("Task Executor Status Updates", func() {
	var (
		ctx          context.Context
		reconciler   *AnnotationScheduleRuleReconciler
		taskExecutor *TaskExecutor
		rule         *schedulerv1.AnnotationScheduleRule
		task         scheduler.ScheduledTask
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Create a test rule
		rule = &schedulerv1.AnnotationScheduleRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-task-rule",
				Namespace: "default",
			},
			Spec: schedulerv1.AnnotationScheduleRuleSpec{
				Schedule: schedulerv1.ScheduleConfig{
					Type:      "datetime",
					StartTime: &metav1.Time{Time: time.Now().Add(time.Hour)},
				},
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "test"},
				},
				Annotations: map[string]string{
					"scheduler.k8s.io/managed-test": "test-value",
				},
				TargetResources: []string{"pods"},
			},
		}

		// Create the rule in the cluster
		Expect(k8sClient.Create(ctx, rule)).To(Succeed())

		// Initialize reconciler and task executor
		reconciler = &AnnotationScheduleRuleReconciler{
			Client:  k8sClient,
			Scheme:  k8sClient.Scheme(),
			Log:     logf.Log.WithName("test-reconciler"),
			Metrics: &ExecutionMetrics{},
		}

		taskExecutor = &TaskExecutor{
			client:     k8sClient,
			log:        logf.Log.WithName("test-executor"),
			reconciler: reconciler,
		}

		// Create a test task
		task = scheduler.ScheduledTask{
			RuleID:          fmt.Sprintf("%s/%s", rule.Namespace, rule.Name),
			RuleName:        rule.Name,
			RuleNamespace:   rule.Namespace,
			Action:          scheduler.ActionApply,
			ExecuteAt:       time.Now(),
			Annotations:     rule.Spec.Annotations,
			Selector:        rule.Spec.Selector,
			TargetResources: rule.Spec.TargetResources,
		}
	})

	AfterEach(func() {
		// Clean up the rule
		Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
	})

	It("should update rule status after successful execution", func() {
		// Mock successful execution by not having any matching resources
		// This will result in 0 affected resources but no error

		taskExecutor.updateRuleStatusAfterExecution(ctx, task, nil, 0, time.Now())

		// Fetch updated rule
		updatedRule := &schedulerv1.AnnotationScheduleRule{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: rule.Name, Namespace: rule.Namespace}, updatedRule)).To(Succeed())

		// Check that execution was recorded as successful
		Expect(updatedRule.Status.ExecutionCount).To(Equal(int64(1)))
		Expect(updatedRule.Status.SuccessfulExecutions).To(Equal(int64(1)))
		Expect(updatedRule.Status.FailedExecutions).To(Equal(int64(0)))
		Expect(updatedRule.Status.AffectedResources).To(Equal(int32(0)))
		Expect(updatedRule.Status.LastError).To(BeEmpty())
		Expect(updatedRule.Status.LastErrorTime).To(BeNil())

		// Check conditions
		executedCondition := findCondition(updatedRule.Status.Conditions, ConditionTypeExecuted)
		Expect(executedCondition).NotTo(BeNil())
		Expect(executedCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(executedCondition.Reason).To(Equal(ReasonExecutionSucceeded))

		errorCondition := findCondition(updatedRule.Status.Conditions, ConditionTypeError)
		Expect(errorCondition).NotTo(BeNil())
		Expect(errorCondition.Status).To(Equal(metav1.ConditionFalse))
	})

	It("should update rule status after failed execution", func() {
		testError := errors.New("execution failed")

		taskExecutor.updateRuleStatusAfterExecution(ctx, task, testError, 0, time.Now())

		// Fetch updated rule
		updatedRule := &schedulerv1.AnnotationScheduleRule{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: rule.Name, Namespace: rule.Namespace}, updatedRule)).To(Succeed())

		// Check that execution was recorded as failed
		Expect(updatedRule.Status.ExecutionCount).To(Equal(int64(1)))
		Expect(updatedRule.Status.SuccessfulExecutions).To(Equal(int64(0)))
		Expect(updatedRule.Status.FailedExecutions).To(Equal(int64(1)))
		Expect(updatedRule.Status.LastError).To(Equal("execution failed"))
		Expect(updatedRule.Status.LastErrorTime).NotTo(BeNil())

		// Check conditions
		executedCondition := findCondition(updatedRule.Status.Conditions, ConditionTypeExecuted)
		Expect(executedCondition).NotTo(BeNil())
		Expect(executedCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(executedCondition.Reason).To(Equal(ReasonExecutionFailed))

		errorCondition := findCondition(updatedRule.Status.Conditions, ConditionTypeError)
		Expect(errorCondition).NotTo(BeNil())
		Expect(errorCondition.Status).To(Equal(metav1.ConditionTrue))
		Expect(errorCondition.Reason).To(Equal(ReasonExecutionFailed))
	})

	It("should handle multiple executions correctly", func() {
		// First execution - success
		taskExecutor.updateRuleStatusAfterExecution(ctx, task, nil, 2, time.Now())

		// Second execution - failure
		taskExecutor.updateRuleStatusAfterExecution(ctx, task, errors.New("second failure"), 0, time.Now())

		// Third execution - success
		taskExecutor.updateRuleStatusAfterExecution(ctx, task, nil, 3, time.Now())

		// Fetch updated rule
		updatedRule := &schedulerv1.AnnotationScheduleRule{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: rule.Name, Namespace: rule.Namespace}, updatedRule)).To(Succeed())

		// Check cumulative statistics
		Expect(updatedRule.Status.ExecutionCount).To(Equal(int64(3)))
		Expect(updatedRule.Status.SuccessfulExecutions).To(Equal(int64(2)))
		Expect(updatedRule.Status.FailedExecutions).To(Equal(int64(1)))
		Expect(updatedRule.Status.AffectedResources).To(Equal(int32(3))) // Last execution value
		Expect(updatedRule.Status.LastError).To(BeEmpty())               // Cleared by successful execution
		Expect(updatedRule.Status.LastErrorTime).To(BeNil())             // Cleared by successful execution
	})
})
