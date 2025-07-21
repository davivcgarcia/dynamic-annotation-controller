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

package errors

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

// StateRecoveryManager handles controller restart scenarios and state recovery
type StateRecoveryManager struct {
	client       client.Client
	logger       logr.Logger
	errorHandler *ErrorHandler
}

// NewStateRecoveryManager creates a new state recovery manager
func NewStateRecoveryManager(client client.Client, logger logr.Logger, errorHandler *ErrorHandler) *StateRecoveryManager {
	return &StateRecoveryManager{
		client:       client,
		logger:       logger,
		errorHandler: errorHandler,
	}
}

// RecoveryResult represents the result of a state recovery operation
type RecoveryResult struct {
	RulesProcessed   int
	RulesRecovered   int
	RulesFailed      int
	RecoveryDuration time.Duration
	Errors           []error
}

// RecoverControllerState recovers the controller state after a restart
func (srm *StateRecoveryManager) RecoverControllerState(ctx context.Context) (*RecoveryResult, error) {
	startTime := time.Now()
	result := &RecoveryResult{}

	srm.logger.Info("Starting controller state recovery")

	// Get all AnnotationScheduleRule resources
	rules, err := srm.getAllRules(ctx)
	if err != nil {
		return result, fmt.Errorf("failed to get rules for recovery: %w", err)
	}

	result.RulesProcessed = len(rules)
	srm.logger.Info("Found rules for recovery", "count", len(rules))

	// Process each rule for recovery
	for _, rule := range rules {
		if err := srm.recoverRule(ctx, &rule); err != nil {
			result.RulesFailed++
			result.Errors = append(result.Errors, fmt.Errorf("failed to recover rule %s/%s: %w", rule.Namespace, rule.Name, err))
			srm.logger.Error(err, "Failed to recover rule", "rule", rule.Name, "namespace", rule.Namespace)
		} else {
			result.RulesRecovered++
			srm.logger.V(1).Info("Successfully recovered rule", "rule", rule.Name, "namespace", rule.Namespace)
		}
	}

	result.RecoveryDuration = time.Since(startTime)

	srm.logger.Info("Controller state recovery completed",
		"rulesProcessed", result.RulesProcessed,
		"rulesRecovered", result.RulesRecovered,
		"rulesFailed", result.RulesFailed,
		"duration", result.RecoveryDuration)

	return result, nil
}

// getAllRules retrieves all AnnotationScheduleRule resources from the cluster
func (srm *StateRecoveryManager) getAllRules(ctx context.Context) ([]schedulerv1.AnnotationScheduleRule, error) {
	var rules []schedulerv1.AnnotationScheduleRule

	err := srm.errorHandler.RetryWithBackoff(ctx, func() error {
		ruleList := &schedulerv1.AnnotationScheduleRuleList{}
		if err := srm.client.List(ctx, ruleList); err != nil {
			return fmt.Errorf("failed to list AnnotationScheduleRules: %w", err)
		}
		rules = ruleList.Items
		return nil
	}, "list-annotation-schedule-rules")

	return rules, err
}

// recoverRule recovers the state of a single rule
func (srm *StateRecoveryManager) recoverRule(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule) error {
	// Skip rules that are being deleted
	if rule.DeletionTimestamp != nil {
		srm.logger.V(1).Info("Skipping rule marked for deletion", "rule", rule.Name, "namespace", rule.Namespace)
		return nil
	}

	// Update rule status to indicate recovery is in progress
	if err := srm.updateRuleStatusForRecovery(ctx, rule); err != nil {
		return fmt.Errorf("failed to update rule status for recovery: %w", err)
	}

	// Validate rule configuration
	if err := srm.validateRuleForRecovery(rule); err != nil {
		return fmt.Errorf("rule validation failed during recovery: %w", err)
	}

	// Check if rule needs immediate attention (e.g., missed executions)
	if err := srm.checkForMissedExecutions(ctx, rule); err != nil {
		srm.logger.Error(err, "Failed to check for missed executions", "rule", rule.Name, "namespace", rule.Namespace)
		// Don't fail recovery for this, just log the error
	}

	return nil
}

// updateRuleStatusForRecovery updates the rule status to indicate recovery
func (srm *StateRecoveryManager) updateRuleStatusForRecovery(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule) error {
	return srm.errorHandler.RetryWithBackoffAndCondition(ctx, func() error {
		// Fetch the latest version of the rule
		latestRule := &schedulerv1.AnnotationScheduleRule{}
		if err := srm.client.Get(ctx, client.ObjectKeyFromObject(rule), latestRule); err != nil {
			return err
		}

		// Add recovery condition
		now := metav1.NewTime(time.Now())
		recoveryCondition := metav1.Condition{
			Type:               "Recovered",
			Status:             metav1.ConditionTrue,
			Reason:             "ControllerRestart",
			Message:            "Rule state recovered after controller restart",
			LastTransitionTime: now,
		}

		// Update or add the recovery condition
		conditionUpdated := false
		for i, condition := range latestRule.Status.Conditions {
			if condition.Type == "Recovered" {
				latestRule.Status.Conditions[i] = recoveryCondition
				conditionUpdated = true
				break
			}
		}
		if !conditionUpdated {
			latestRule.Status.Conditions = append(latestRule.Status.Conditions, recoveryCondition)
		}

		// Update the status
		return srm.client.Status().Update(ctx, latestRule)
	}, func(err error, attempt int) bool {
		// Don't retry on not found errors - the rule might have been deleted
		if apierrors.IsNotFound(err) {
			return false
		}
		return srm.errorHandler.ShouldRetry(err, attempt)
	}, fmt.Sprintf("update-rule-status-recovery-%s-%s", rule.Namespace, rule.Name))
}

// validateRuleForRecovery validates a rule configuration during recovery
func (srm *StateRecoveryManager) validateRuleForRecovery(rule *schedulerv1.AnnotationScheduleRule) error {
	// Basic validation checks
	if rule.Spec.Schedule.Type == "" {
		return fmt.Errorf("schedule type is empty")
	}

	if rule.Spec.Schedule.Type != "datetime" && rule.Spec.Schedule.Type != "cron" {
		return fmt.Errorf("invalid schedule type: %s", rule.Spec.Schedule.Type)
	}

	if len(rule.Spec.Annotations) == 0 {
		return fmt.Errorf("no annotations specified")
	}

	if len(rule.Spec.TargetResources) == 0 {
		return fmt.Errorf("no target resources specified")
	}

	// Schedule-specific validation
	switch rule.Spec.Schedule.Type {
	case "datetime":
		if rule.Spec.Schedule.StartTime == nil && rule.Spec.Schedule.EndTime == nil {
			return fmt.Errorf("datetime schedule must specify at least startTime or endTime")
		}
	case "cron":
		if rule.Spec.Schedule.CronExpression == "" {
			return fmt.Errorf("cron schedule must specify cronExpression")
		}
		if rule.Spec.Schedule.Action == "" {
			return fmt.Errorf("cron schedule must specify action")
		}
	}

	return nil
}

// checkForMissedExecutions checks if any executions were missed during controller downtime
func (srm *StateRecoveryManager) checkForMissedExecutions(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule) error {
	// Only check for datetime schedules with specific start times
	if rule.Spec.Schedule.Type != "datetime" || rule.Spec.Schedule.StartTime == nil {
		return nil
	}

	now := time.Now()
	startTime := rule.Spec.Schedule.StartTime.Time

	// Check if start time has passed and we haven't executed yet
	if startTime.Before(now) && rule.Status.LastExecutionTime == nil {
		srm.logger.Info("Detected missed execution during controller downtime",
			"rule", rule.Name,
			"namespace", rule.Namespace,
			"scheduledStartTime", startTime.Format(time.RFC3339),
			"currentTime", now.Format(time.RFC3339))

		// Update rule status to indicate missed execution
		return srm.updateRuleStatusForMissedExecution(ctx, rule, startTime)
	}

	return nil
}

// updateRuleStatusForMissedExecution updates rule status when a missed execution is detected
func (srm *StateRecoveryManager) updateRuleStatusForMissedExecution(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule, missedTime time.Time) error {
	return srm.errorHandler.RetryWithBackoffAndCondition(ctx, func() error {
		// Fetch the latest version of the rule
		latestRule := &schedulerv1.AnnotationScheduleRule{}
		if err := srm.client.Get(ctx, client.ObjectKeyFromObject(rule), latestRule); err != nil {
			return err
		}

		// Add missed execution condition
		now := metav1.NewTime(time.Now())
		missedCondition := metav1.Condition{
			Type:   "MissedExecution",
			Status: metav1.ConditionTrue,
			Reason: "ControllerDowntime",
			Message: fmt.Sprintf("Missed execution at %s due to controller downtime",
				missedTime.Format(time.RFC3339)),
			LastTransitionTime: now,
		}

		// Update or add the missed execution condition
		conditionUpdated := false
		for i, condition := range latestRule.Status.Conditions {
			if condition.Type == "MissedExecution" {
				latestRule.Status.Conditions[i] = missedCondition
				conditionUpdated = true
				break
			}
		}
		if !conditionUpdated {
			latestRule.Status.Conditions = append(latestRule.Status.Conditions, missedCondition)
		}

		// Update the status
		return srm.client.Status().Update(ctx, latestRule)
	}, func(err error, attempt int) bool {
		// Don't retry on not found errors - the rule might have been deleted
		if apierrors.IsNotFound(err) {
			return false
		}
		return srm.errorHandler.ShouldRetry(err, attempt)
	}, fmt.Sprintf("update-rule-status-missed-execution-%s-%s", rule.Namespace, rule.Name))
}

// RecoverFromPanic recovers from panics in critical operations
func (srm *StateRecoveryManager) RecoverFromPanic(operation string) {
	if r := recover(); r != nil {
		srm.logger.Error(fmt.Errorf("panic recovered: %v", r), "Panic occurred during operation", "operation", operation)

		// Log stack trace if available
		if err, ok := r.(error); ok {
			srm.logger.Error(err, "Panic error details", "operation", operation)
		}
	}
}

// SafeExecute executes a function with panic recovery
func (srm *StateRecoveryManager) SafeExecute(operation string, fn func() error) error {
	defer srm.RecoverFromPanic(operation)
	return fn()
}

// SafeExecuteWithContext executes a function with panic recovery and context
func (srm *StateRecoveryManager) SafeExecuteWithContext(ctx context.Context, operation string, fn func(context.Context) error) error {
	defer srm.RecoverFromPanic(operation)
	return fn(ctx)
}

// GetRecoveryMetrics returns metrics about the recovery process
func (srm *StateRecoveryManager) GetRecoveryMetrics(result *RecoveryResult) map[string]interface{} {
	if result == nil {
		return map[string]interface{}{
			"recoveryCompleted": false,
		}
	}

	successRate := float64(0)
	if result.RulesProcessed > 0 {
		successRate = float64(result.RulesRecovered) / float64(result.RulesProcessed) * 100
	}

	return map[string]interface{}{
		"recoveryCompleted": true,
		"rulesProcessed":    result.RulesProcessed,
		"rulesRecovered":    result.RulesRecovered,
		"rulesFailed":       result.RulesFailed,
		"successRate":       fmt.Sprintf("%.2f%%", successRate),
		"recoveryDuration":  result.RecoveryDuration.String(),
		"errorCount":        len(result.Errors),
	}
}
