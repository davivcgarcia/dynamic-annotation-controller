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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/errors"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/resource"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/scheduler"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/security"
	"github.com/davivcgarcia/dynamic-annotation-controller/internal/templating"
)

const (
	// AnnotationScheduleRuleFinalizer is the finalizer added to AnnotationScheduleRule resources
	AnnotationScheduleRuleFinalizer = "scheduler.k8s.io/annotation-schedule-rule-finalizer"

	// Status phase constants
	PhasePending   = "pending"
	PhaseActive    = "active"
	PhaseCompleted = "completed"
	PhaseFailed    = "failed"

	// Condition types
	ConditionTypeReady     = "Ready"
	ConditionTypeScheduled = "Scheduled"
	ConditionTypeExecuted  = "Executed"
	ConditionTypeValidated = "Validated"
	ConditionTypeError     = "Error"

	// Condition reasons
	ReasonValidationFailed    = "ValidationFailed"
	ReasonSchedulingFailed    = "SchedulingFailed"
	ReasonExecutionFailed     = "ExecutionFailed"
	ReasonExecutionSucceeded  = "ExecutionSucceeded"
	ReasonValidationSucceeded = "ValidationSucceeded"
	ReasonSchedulingSucceeded = "SchedulingSucceeded"
	ReasonRuleReady           = "RuleReady"
	ReasonRuleActive          = "RuleActive"
	ReasonRuleCompleted       = "RuleCompleted"
	ReasonRuleFailed          = "RuleFailed"
	ReasonRulePending         = "RulePending"
)

// ExecutionMetrics tracks execution statistics for monitoring
type ExecutionMetrics struct {
	TotalExecutions      int64
	SuccessfulExecutions int64
	FailedExecutions     int64
	LastExecutionTime    time.Time
	AverageExecutionTime time.Duration
	mu                   sync.RWMutex
}

// RecordExecution records an execution attempt with its result and duration
func (m *ExecutionMetrics) RecordExecution(success bool, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalExecutions++
	m.LastExecutionTime = time.Now()

	if success {
		m.SuccessfulExecutions++
	} else {
		m.FailedExecutions++
	}

	// Calculate rolling average execution time
	if m.AverageExecutionTime == 0 {
		m.AverageExecutionTime = duration
	} else {
		m.AverageExecutionTime = (m.AverageExecutionTime + duration) / 2
	}
}

// GetMetrics returns a copy of the current metrics
func (m *ExecutionMetrics) GetMetrics() (int64, int64, int64, time.Time, time.Duration) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.TotalExecutions, m.SuccessfulExecutions, m.FailedExecutions, m.LastExecutionTime, m.AverageExecutionTime
}

// AnnotationScheduleRuleReconciler reconciles a AnnotationScheduleRule object
type AnnotationScheduleRuleReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	Scheduler            scheduler.TimeScheduler
	ResourceManager      *resource.ResourceManager
	Log                  logr.Logger
	Metrics              *ExecutionMetrics
	ErrorHandler         *errors.ErrorHandler
	RecoveryManager      *errors.StateRecoveryManager
	SecurityValidator    *security.AnnotationValidator
	AuditLogger          *security.AuditLogger
	SecurityConfigLoader *security.SecurityConfigLoader
}

// TaskExecutor implements the scheduler.TaskExecutor interface
type TaskExecutor struct {
	client          client.Client
	resourceManager *resource.ResourceManager
	log             logr.Logger
	reconciler      *AnnotationScheduleRuleReconciler
}

// NewTaskExecutor creates a new TaskExecutor
func NewTaskExecutor(cl client.Client, resourceManager *resource.ResourceManager, log logr.Logger, reconciler *AnnotationScheduleRuleReconciler) *TaskExecutor {
	return &TaskExecutor{
		client:          cl,
		resourceManager: resourceManager,
		log:             log,
		reconciler:      reconciler,
	}
}

// StartPerformanceOptimizations starts background routines for performance optimization
func (r *AnnotationScheduleRuleReconciler) StartPerformanceOptimizations(ctx context.Context) {
	if r.ResourceManager != nil {
		// Start cache cleanup routine
		if r.ResourceManager.Cache != nil {
			go r.ResourceManager.Cache.StartCleanupRoutine(ctx, 10*time.Minute)
		}

		// Start rate limiter cleanup routine
		if r.ResourceManager.RateLimiter != nil {
			go r.ResourceManager.RateLimiter.StartCleanupRoutine(ctx, 15*time.Minute, 1*time.Hour)
		}
	}

	// Start orphaned annotation cleanup
	cleanup := resource.NewOrphanedAnnotationCleanup(r.Client, r.Log)
	go cleanup.StartCleanupRoutine(ctx)

	r.Log.Info("Started performance optimization routines")
}

// SetReconciler sets the reconciler reference (used for circular dependency resolution)
func (te *TaskExecutor) SetReconciler(reconciler *AnnotationScheduleRuleReconciler) {
	te.reconciler = reconciler
}

// ExecuteTask implements the scheduler.TaskExecutor interface
func (te *TaskExecutor) ExecuteTask(ctx context.Context, task scheduler.ScheduledTask) error {
	startTime := time.Now()

	te.log.Info("Executing scheduled task",
		"ruleID", task.RuleID,
		"action", task.Action,
		"targetResources", task.TargetResources,
		"scheduledTime", task.ExecuteAt.Format(time.RFC3339))

	var err error
	var affectedResources int32

	// Execute the task with error handling and retry logic
	if te.reconciler != nil && te.reconciler.ErrorHandler != nil {
		err = te.reconciler.ErrorHandler.RetryWithBackoff(ctx, func() error {
			var execErr error
			switch task.Action {
			case scheduler.ActionApply:
				affectedResources, execErr = te.executeApplyAction(ctx, task)
			case scheduler.ActionRemove:
				affectedResources, execErr = te.executeRemoveAction(ctx, task)
			default:
				execErr = fmt.Errorf("unknown task action: %s", task.Action)
			}
			return execErr
		}, fmt.Sprintf("execute-task-%s-%s", task.RuleID, task.Action))
	} else {
		// Fallback to direct execution if error handler is not available
		switch task.Action {
		case scheduler.ActionApply:
			affectedResources, err = te.executeApplyAction(ctx, task)
		case scheduler.ActionRemove:
			affectedResources, err = te.executeRemoveAction(ctx, task)
		default:
			err = fmt.Errorf("unknown task action: %s", task.Action)
		}
	}

	// Calculate execution duration
	duration := time.Since(startTime)

	// Record metrics
	if te.reconciler != nil && te.reconciler.Metrics != nil {
		te.reconciler.Metrics.RecordExecution(err == nil, duration)
	}

	// Update rule status after execution
	if te.reconciler != nil {
		te.updateRuleStatusAfterExecution(ctx, task, err, affectedResources, startTime)
	}

	if err != nil {
		// Log error details with categorization
		errorSummary := map[string]interface{}{
			"error": err.Error(),
		}
		if te.reconciler != nil && te.reconciler.ErrorHandler != nil {
			errorSummary = te.reconciler.ErrorHandler.GetErrorSummary(err)
		}

		te.log.Error(err, "Task execution failed",
			"ruleID", task.RuleID,
			"action", task.Action,
			"duration", duration,
			"affectedResources", affectedResources,
			"errorSummary", errorSummary)
		return err
	}

	te.log.Info("Task executed successfully",
		"ruleID", task.RuleID,
		"action", task.Action,
		"duration", duration,
		"affectedResources", affectedResources)

	return nil
}

// executeApplyAction executes annotation application and returns the number of affected resources
func (te *TaskExecutor) executeApplyAction(ctx context.Context, task scheduler.ScheduledTask) (int32, error) {
	// Prepare template context
	templateContext := te.buildTemplateContext(task)

	// Determine conflict resolution strategy
	conflictResolution := resource.ConflictResolutionPriority // default
	if task.ConflictResolution != "" {
		conflictResolution = resource.AnnotationConflictResolution(task.ConflictResolution)
	}

	var totalAffected int32
	var err error

	// Handle new annotation groups if present
	if len(task.AnnotationGroups) > 0 {
		var results []resource.AnnotationGroupResult

		if te.reconciler != nil && te.reconciler.ErrorHandler != nil {
			err = te.reconciler.ErrorHandler.RetryWithBackoff(ctx, func() error {
				var applyErr error
				results, applyErr = te.resourceManager.ApplyAnnotationGroups(
					ctx,
					task.Selector,
					task.AnnotationGroups,
					task.TargetResources,
					task.RuleNamespace,
					task.RuleID,
					templateContext,
					conflictResolution,
				)
				return applyErr
			}, fmt.Sprintf("apply-annotation-groups-%s", task.RuleID))
		} else {
			results, err = te.resourceManager.ApplyAnnotationGroups(
				ctx,
				task.Selector,
				task.AnnotationGroups,
				task.TargetResources,
				task.RuleNamespace,
				task.RuleID,
				templateContext,
				conflictResolution,
			)
		}

		// Calculate total affected resources and log results
		for _, result := range results {
			totalAffected += result.ResourcesAffected

			te.log.Info("Applied annotation group",
				"ruleID", task.RuleID,
				"groupName", result.GroupName,
				"success", result.Success,
				"resourcesAffected", result.ResourcesAffected,
				"operationsCount", len(result.Operations))

			if !result.Success && err == nil {
				err = result.Error
			}
		}

		return totalAffected, err
	}

	// Fallback to legacy annotation handling for backward compatibility
	if len(task.Annotations) > 0 {
		// Get matching resources to count them
		var resources []client.Object

		if te.reconciler != nil && te.reconciler.ErrorHandler != nil {
			err = te.reconciler.ErrorHandler.RetryWithBackoff(ctx, func() error {
				var getErr error
				resources, getErr = te.resourceManager.GetMatchingResources(
					ctx,
					task.Selector,
					task.TargetResources,
					task.RuleNamespace,
				)
				return getErr
			}, fmt.Sprintf("get-matching-resources-%s", task.RuleID))
		} else {
			resources, err = te.resourceManager.GetMatchingResources(
				ctx,
				task.Selector,
				task.TargetResources,
				task.RuleNamespace,
			)
		}

		if err != nil {
			return 0, fmt.Errorf("failed to get matching resources: %w", err)
		}

		if len(resources) == 0 {
			te.log.Info("No resources found matching selector for apply action",
				"ruleID", task.RuleID,
				"selector", task.Selector)
			return 0, nil
		}

		// Apply legacy annotations
		if te.reconciler != nil && te.reconciler.ErrorHandler != nil {
			err = te.reconciler.ErrorHandler.RetryWithBackoff(ctx, func() error {
				return te.resourceManager.ApplyAnnotations(
					ctx,
					task.Selector,
					task.Annotations,
					task.TargetResources,
					task.RuleNamespace,
					task.RuleID,
				)
			}, fmt.Sprintf("apply-annotations-%s", task.RuleID))
		} else {
			err = te.resourceManager.ApplyAnnotations(
				ctx,
				task.Selector,
				task.Annotations,
				task.TargetResources,
				task.RuleNamespace,
				task.RuleID,
			)
		}

		// Audit log the annotation application for each affected resource
		if te.reconciler != nil && te.reconciler.AuditLogger != nil {
			for _, res := range resources {
				te.reconciler.AuditLogger.LogAnnotationApply(
					ctx,
					task.RuleID,
					task.RuleName,
					task.RuleNamespace,
					res,
					task.Annotations,
					err == nil,
					err,
				)
			}
		}

		return int32(len(resources)), err
	}

	te.log.Info("No annotations or annotation groups to apply", "ruleID", task.RuleID)
	return 0, nil
}

// executeRemoveAction executes annotation removal and returns the number of affected resources
func (te *TaskExecutor) executeRemoveAction(ctx context.Context, task scheduler.ScheduledTask) (int32, error) {
	var totalAffected int32
	var err error

	// Handle new annotation groups if present
	if len(task.AnnotationGroups) > 0 {
		var results []resource.AnnotationGroupResult

		if te.reconciler != nil && te.reconciler.ErrorHandler != nil {
			err = te.reconciler.ErrorHandler.RetryWithBackoff(ctx, func() error {
				var removeErr error
				results, removeErr = te.resourceManager.RemoveAnnotationGroups(
					ctx,
					task.Selector,
					task.AnnotationGroups,
					task.TargetResources,
					task.RuleNamespace,
					task.RuleID,
				)
				return removeErr
			}, fmt.Sprintf("remove-annotation-groups-%s", task.RuleID))
		} else {
			results, err = te.resourceManager.RemoveAnnotationGroups(
				ctx,
				task.Selector,
				task.AnnotationGroups,
				task.TargetResources,
				task.RuleNamespace,
				task.RuleID,
			)
		}

		// Calculate total affected resources and log results
		for _, result := range results {
			totalAffected += result.ResourcesAffected

			te.log.Info("Removed annotation group",
				"ruleID", task.RuleID,
				"groupName", result.GroupName,
				"success", result.Success,
				"resourcesAffected", result.ResourcesAffected)

			if !result.Success && err == nil {
				err = result.Error
			}
		}

		return totalAffected, err
	}

	// Fallback to legacy annotation handling for backward compatibility
	if len(task.Annotations) > 0 {
		// Get matching resources to count them
		var resources []client.Object

		if te.reconciler != nil && te.reconciler.ErrorHandler != nil {
			err = te.reconciler.ErrorHandler.RetryWithBackoff(ctx, func() error {
				var getErr error
				resources, getErr = te.resourceManager.GetMatchingResources(
					ctx,
					task.Selector,
					task.TargetResources,
					task.RuleNamespace,
				)
				return getErr
			}, fmt.Sprintf("get-matching-resources-remove-%s", task.RuleID))
		} else {
			resources, err = te.resourceManager.GetMatchingResources(
				ctx,
				task.Selector,
				task.TargetResources,
				task.RuleNamespace,
			)
		}

		if err != nil {
			return 0, fmt.Errorf("failed to get matching resources: %w", err)
		}

		if len(resources) == 0 {
			te.log.Info("No resources found matching selector for remove action",
				"ruleID", task.RuleID,
				"selector", task.Selector)
			return 0, nil
		}

		// Extract annotation keys from the annotations map
		keys := make([]string, 0, len(task.Annotations))
		for key := range task.Annotations {
			keys = append(keys, key)
		}

		// Remove annotations with error handling
		if te.reconciler != nil && te.reconciler.ErrorHandler != nil {
			err = te.reconciler.ErrorHandler.RetryWithBackoff(ctx, func() error {
				return te.resourceManager.RemoveAnnotations(
					ctx,
					task.Selector,
					keys,
					task.TargetResources,
					task.RuleNamespace,
					task.RuleID,
				)
			}, fmt.Sprintf("remove-annotations-%s", task.RuleID))
		} else {
			err = te.resourceManager.RemoveAnnotations(
				ctx,
				task.Selector,
				keys,
				task.TargetResources,
				task.RuleNamespace,
				task.RuleID,
			)
		}

		// Audit log the annotation removal for each affected resource
		if te.reconciler != nil && te.reconciler.AuditLogger != nil {
			for _, res := range resources {
				te.reconciler.AuditLogger.LogAnnotationRemove(
					ctx,
					task.RuleID,
					task.RuleName,
					task.RuleNamespace,
					res,
					keys,
					err == nil,
					err,
				)
			}
		}

		return int32(len(resources)), err
	}

	te.log.Info("No annotations or annotation groups to remove", "ruleID", task.RuleID)
	return 0, nil
}

// updateRuleStatusAfterExecution updates the rule status after task execution
func (te *TaskExecutor) updateRuleStatusAfterExecution(ctx context.Context, task scheduler.ScheduledTask, execErr error, affectedResources int32, executionTime time.Time) {
	// Parse rule ID to get namespace and name
	ruleNamespace := task.RuleNamespace
	ruleName := task.RuleName

	if ruleNamespace == "" || ruleName == "" {
		// Try to parse from RuleID if not available (format: namespace/name)
		parts := strings.Split(task.RuleID, "/")
		if len(parts) == 2 {
			ruleNamespace = parts[0]
			ruleName = parts[1]
		} else {
			te.log.Error(nil, "Failed to parse rule ID for status update", "ruleID", task.RuleID)
			return
		}
	}

	// Update rule status with error handling and retry logic
	updateOperation := func() error {
		// Fetch the current rule
		rule := &schedulerv1.AnnotationScheduleRule{}
		namespacedName := types.NamespacedName{
			Namespace: ruleNamespace,
			Name:      ruleName,
		}

		if err := te.client.Get(ctx, namespacedName, rule); err != nil {
			return fmt.Errorf("failed to fetch rule for status update: %w", err)
		}

		// Update execution status
		rule.Status.LastExecutionTime = &metav1.Time{Time: executionTime}
		rule.Status.AffectedResources = affectedResources
		rule.Status.ExecutionCount++

		// Set execution condition based on result
		if execErr != nil {
			rule.Status.FailedExecutions++
			rule.Status.LastError = execErr.Error()
			rule.Status.LastErrorTime = &metav1.Time{Time: executionTime}

			te.reconciler.setCondition(rule, ConditionTypeExecuted, metav1.ConditionFalse, ReasonExecutionFailed,
				fmt.Sprintf("Task execution failed: %v", execErr))
			te.reconciler.setCondition(rule, ConditionTypeError, metav1.ConditionTrue, ReasonExecutionFailed,
				fmt.Sprintf("Execution error for action %s: %v", task.Action, execErr))
		} else {
			rule.Status.SuccessfulExecutions++
			// Clear last error on successful execution
			rule.Status.LastError = ""
			rule.Status.LastErrorTime = nil

			te.reconciler.setCondition(rule, ConditionTypeExecuted, metav1.ConditionTrue, ReasonExecutionSucceeded,
				fmt.Sprintf("Task executed successfully, affected %d resources", affectedResources))
			// Clear error condition if execution succeeded
			te.reconciler.setCondition(rule, ConditionTypeError, metav1.ConditionFalse, ReasonExecutionSucceeded,
				"No execution errors")
		}

		// Update the status
		return te.client.Status().Update(ctx, rule)
	}

	// Execute status update with retry logic
	if te.reconciler != nil && te.reconciler.ErrorHandler != nil {
		if err := te.reconciler.ErrorHandler.RetryWithBackoff(ctx, updateOperation,
			fmt.Sprintf("update-rule-status-%s", task.RuleID)); err != nil {
			te.log.Error(err, "Failed to update rule status after execution with retries", "ruleID", task.RuleID)
		} else {
			te.log.V(1).Info("Updated rule status after execution",
				"ruleID", task.RuleID,
				"success", execErr == nil,
				"affectedResources", affectedResources)
		}
	} else {
		// Fallback to direct execution
		if err := updateOperation(); err != nil {
			te.log.Error(err, "Failed to update rule status after execution", "ruleID", task.RuleID)
		} else {
			te.log.V(1).Info("Updated rule status after execution",
				"ruleID", task.RuleID,
				"success", execErr == nil,
				"affectedResources", affectedResources)
		}
	}
}

// buildTemplateContext creates a template context for processing templates
func (te *TaskExecutor) buildTemplateContext(task scheduler.ScheduledTask) templating.TemplateContext {
	return templating.TemplateContext{
		Variables:     task.TemplateVariables,
		RuleName:      task.RuleName,
		RuleNamespace: task.RuleNamespace,
		RuleID:        task.RuleID,
		ExecutionTime: time.Now(),
		Action:        string(task.Action),
	}
}

// +kubebuilder:rbac:groups=scheduler.sandbox.davcgar.aws,resources=annotationschedulerules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=scheduler.sandbox.davcgar.aws,resources=annotationschedulerules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=scheduler.sandbox.davcgar.aws,resources=annotationschedulerules/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch,resourceNames=annotation-scheduler-security-config;annotation-scheduler-audit-config
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create;get;list;patch;update;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *AnnotationScheduleRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Use safe execution with panic recovery
	if r.RecoveryManager != nil {
		return r.safeReconcile(ctx, req, log)
	}

	return r.doReconcile(ctx, req, log)
}

// safeReconcile wraps the reconcile logic with panic recovery
func (r *AnnotationScheduleRuleReconciler) safeReconcile(ctx context.Context, req ctrl.Request, log logr.Logger) (result ctrl.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in reconcile: %v", r)
			log.Error(err, "Panic occurred during reconciliation", "request", req.String())
			result = ctrl.Result{RequeueAfter: time.Minute * 5}
		}
	}()

	return r.doReconcile(ctx, req, log)
}

// doReconcile contains the main reconciliation logic
func (r *AnnotationScheduleRuleReconciler) doReconcile(ctx context.Context, req ctrl.Request, log logr.Logger) (ctrl.Result, error) {
	// Fetch the AnnotationScheduleRule instance with retry logic
	rule, fetchResult, fetchErr := r.fetchRule(ctx, req, log)
	if fetchErr != nil {
		return fetchResult, fetchErr
	}

	// Handle deletion
	if rule.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, rule)
	}

	// Add finalizer if not present with error handling
	finalizerResult, finalizerErr := r.ensureFinalizer(ctx, rule, req, log)
	// nolint:staticcheck // Using Requeue for backward compatibility
	if finalizerErr != nil || finalizerResult.RequeueAfter > 0 || finalizerResult.Requeue {
		return finalizerResult, finalizerErr
	}

	// Validate the rule
	if validationErr := r.validateAndUpdateRuleOnFailure(ctx, rule, log); validationErr != nil {
		return ctrl.Result{}, nil
	}

	// Schedule the rule
	scheduleResult, scheduleErr := r.scheduleRuleWithErrorHandling(ctx, rule, log)
	if scheduleErr != nil {
		return scheduleResult, scheduleErr
	}

	// Process next execution time
	nextExecResult, nextExecErr := r.processNextExecutionTime(ctx, rule, log)
	if nextExecErr != nil || nextExecResult.RequeueAfter > 0 {
		return nextExecResult, nextExecErr
	}

	// Update rule status
	r.updateRuleActiveStatus(ctx, rule)

	// Update status with error handling
	statusResult, statusErr := r.updateRuleStatusWithErrorHandling(ctx, rule, log)
	if statusErr != nil {
		return statusResult, statusErr
	}

	// Requeue to check for status updates periodically
	return ctrl.Result{RequeueAfter: time.Minute * 10}, nil
}

// fetchRule fetches the rule with error handling
func (r *AnnotationScheduleRuleReconciler) fetchRule(ctx context.Context, req ctrl.Request, log logr.Logger) (*schedulerv1.AnnotationScheduleRule, ctrl.Result, error) {
	rule := &schedulerv1.AnnotationScheduleRule{}
	var fetchErr error

	if r.ErrorHandler != nil {
		fetchErr = r.ErrorHandler.RetryWithBackoff(ctx, func() error {
			return r.Get(ctx, req.NamespacedName, rule)
		}, fmt.Sprintf("get-rule-%s", req.String()))
	} else {
		fetchErr = r.Get(ctx, req.NamespacedName, rule)
	}

	if fetchErr != nil {
		if apierrors.IsNotFound(fetchErr) {
			// Rule was deleted, unschedule it
			ruleID := req.String()
			if err := r.Scheduler.UnscheduleRule(ruleID); err != nil {
				log.Error(err, "Failed to unschedule deleted rule", "ruleID", ruleID)
			}
			return nil, ctrl.Result{}, nil
		}

		// Log error details with categorization
		errorSummary := map[string]interface{}{
			"error": fetchErr.Error(),
		}
		if r.ErrorHandler != nil {
			errorSummary = r.ErrorHandler.GetErrorSummary(fetchErr)
		}

		log.Error(fetchErr, "Failed to get AnnotationScheduleRule", "errorSummary", errorSummary)

		// Determine requeue strategy based on error type
		if r.ErrorHandler != nil && r.ErrorHandler.ShouldRetry(fetchErr, 0) {
			delay := r.ErrorHandler.CalculateDelay(1)
			return nil, ctrl.Result{RequeueAfter: delay}, nil
		}

		return nil, ctrl.Result{}, fetchErr
	}

	return rule, ctrl.Result{}, nil
}

// ensureFinalizer ensures the finalizer is present on the rule
func (r *AnnotationScheduleRuleReconciler) ensureFinalizer(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule, req ctrl.Request, log logr.Logger) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(rule, AnnotationScheduleRuleFinalizer) {
		controllerutil.AddFinalizer(rule, AnnotationScheduleRuleFinalizer)

		var updateErr error
		if r.ErrorHandler != nil {
			updateErr = r.ErrorHandler.RetryWithBackoff(ctx, func() error {
				return r.Update(ctx, rule)
			}, fmt.Sprintf("add-finalizer-%s", req.String()))
		} else {
			updateErr = r.Update(ctx, rule)
		}

		if updateErr != nil {
			log.Error(updateErr, "Failed to add finalizer")

			// Determine requeue strategy based on error type
			if r.ErrorHandler != nil && r.ErrorHandler.ShouldRetry(updateErr, 0) {
				delay := r.ErrorHandler.CalculateDelay(1)
				return ctrl.Result{RequeueAfter: delay}, nil
			}

			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

// validateAndUpdateRuleOnFailure validates the rule and updates status on failure
func (r *AnnotationScheduleRuleReconciler) validateAndUpdateRuleOnFailure(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule, log logr.Logger) error {
	if err := r.validateRule(rule); err != nil {
		log.Error(err, "Rule validation failed", "rule", rule.Name, "namespace", rule.Namespace)
		r.setCondition(rule, ConditionTypeValidated, metav1.ConditionFalse, ReasonValidationFailed,
			fmt.Sprintf("Validation failed: %v", err))
		r.setCondition(rule, ConditionTypeError, metav1.ConditionTrue, ReasonValidationFailed, err.Error())
		r.updateRuleStatus(ctx, rule, PhaseFailed, "Validation failed: "+err.Error())

		// Update status and return without requeue to avoid validation loop
		if statusErr := r.Status().Update(ctx, rule); statusErr != nil {
			log.Error(statusErr, "Failed to update rule status after validation failure")
		}
		return err
	}

	// Update validation condition
	r.setCondition(rule, ConditionTypeValidated, metav1.ConditionTrue, ReasonValidationSucceeded, "Rule validation passed")
	// Clear any previous validation errors
	r.setCondition(rule, ConditionTypeError, metav1.ConditionFalse, ReasonValidationSucceeded, "No validation errors")

	return nil
}

// scheduleRuleWithErrorHandling schedules the rule with error handling
func (r *AnnotationScheduleRuleReconciler) scheduleRuleWithErrorHandling(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule, log logr.Logger) (ctrl.Result, error) {
	var scheduleErr error
	if r.ErrorHandler != nil {
		scheduleErr = r.ErrorHandler.RetryWithBackoff(ctx, func() error {
			return r.Scheduler.ScheduleRule(rule)
		}, fmt.Sprintf("schedule-rule-%s-%s", rule.Namespace, rule.Name))
	} else {
		scheduleErr = r.Scheduler.ScheduleRule(rule)
	}

	if scheduleErr != nil {
		errorSummary := map[string]interface{}{
			"error": scheduleErr.Error(),
		}
		if r.ErrorHandler != nil {
			errorSummary = r.ErrorHandler.GetErrorSummary(scheduleErr)
		}

		log.Error(scheduleErr, "Failed to schedule rule",
			"rule", rule.Name,
			"namespace", rule.Namespace,
			"errorSummary", errorSummary)

		r.setCondition(rule, ConditionTypeScheduled, metav1.ConditionFalse, ReasonSchedulingFailed, scheduleErr.Error())
		r.setCondition(rule, ConditionTypeError, metav1.ConditionTrue, ReasonSchedulingFailed,
			fmt.Sprintf("Scheduling failed: %v", scheduleErr))
		r.updateRuleStatus(ctx, rule, PhaseFailed, "Scheduling failed: "+scheduleErr.Error())

		// Update status before requeue with error handling
		statusUpdateErr := func() error {
			return r.Status().Update(ctx, rule)
		}

		if r.ErrorHandler != nil {
			if err := r.ErrorHandler.RetryWithBackoff(ctx, statusUpdateErr,
				fmt.Sprintf("update-status-scheduling-failed-%s-%s", rule.Namespace, rule.Name)); err != nil {
				log.Error(err, "Failed to update rule status after scheduling failure with retries")
			}
		} else {
			if err := statusUpdateErr(); err != nil {
				log.Error(err, "Failed to update rule status after scheduling failure")
			}
		}

		// Determine requeue delay based on error type
		requeueDelay := time.Minute * 5
		if r.ErrorHandler != nil {
			if r.ErrorHandler.ShouldRetry(scheduleErr, 0) {
				requeueDelay = r.ErrorHandler.CalculateDelay(1)
				if requeueDelay < time.Minute {
					requeueDelay = time.Minute // Minimum delay for scheduling errors
				}
			}
		}

		return ctrl.Result{RequeueAfter: requeueDelay}, scheduleErr
	}

	// Update scheduling condition
	r.setCondition(rule, ConditionTypeScheduled, metav1.ConditionTrue, ReasonSchedulingSucceeded, "Rule successfully scheduled")
	// Clear any previous scheduling errors
	r.setCondition(rule, ConditionTypeError, metav1.ConditionFalse, ReasonSchedulingSucceeded, "No scheduling errors")

	return ctrl.Result{}, nil
}

// processNextExecutionTime processes the next execution time for the rule
// nolint:unparam // Keeping consistent return type for all reconciler methods
func (r *AnnotationScheduleRuleReconciler) processNextExecutionTime(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule, log logr.Logger) (ctrl.Result, error) {
	ruleID := r.getRuleID(rule)
	nextExecution, err := r.Scheduler.GetNextExecutionTime(ruleID)
	if err != nil {
		log.Error(err, "Failed to get next execution time", "ruleID", ruleID)
		r.setCondition(rule, ConditionTypeError, metav1.ConditionTrue, "NextExecutionError",
			fmt.Sprintf("Failed to get next execution time: %v", err))
		return ctrl.Result{}, err
	}

	if nextExecution != nil {
		rule.Status.NextExecutionTime = &metav1.Time{Time: *nextExecution}
		log.V(1).Info("Next execution time updated",
			"ruleID", ruleID,
			"nextExecution", nextExecution.Format(time.RFC3339))
		return ctrl.Result{}, nil
	}

	// No next execution means rule is completed
	log.Info("Rule has no next execution time, marking as completed", "ruleID", ruleID)
	r.updateRuleStatus(ctx, rule, PhaseCompleted, "Rule schedule completed")
	r.setCondition(rule, ConditionTypeReady, metav1.ConditionTrue, ReasonRuleCompleted, "Rule schedule completed")

	// Update status and return
	if statusErr := r.Status().Update(ctx, rule); statusErr != nil {
		log.Error(statusErr, "Failed to update rule status after completion")
	}
	return ctrl.Result{}, nil
}

// updateRuleActiveStatus updates the rule status to active if not already
func (r *AnnotationScheduleRuleReconciler) updateRuleActiveStatus(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule) {
	if rule.Status.Phase == "" || rule.Status.Phase == PhasePending {
		r.updateRuleStatus(ctx, rule, PhaseActive, "Rule is active and scheduled")

		// Audit log rule creation when it becomes active for the first time
		if r.AuditLogger != nil && rule.Status.Phase == "" {
			r.AuditLogger.LogRuleCreation(ctx, r.getRuleID(rule), rule.Name, rule.Namespace, rule.Spec.Annotations)
		}
	}

	// Set ready condition
	r.setCondition(rule, ConditionTypeReady, metav1.ConditionTrue, ReasonRuleReady, "Rule is ready and scheduled")

	// Log current status for monitoring
	r.logRuleStatus(rule)
}

// updateRuleStatusWithErrorHandling updates the rule status with error handling
func (r *AnnotationScheduleRuleReconciler) updateRuleStatusWithErrorHandling(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule, log logr.Logger) (ctrl.Result, error) {
	statusUpdateErr := func() error {
		return r.Status().Update(ctx, rule)
	}

	if r.ErrorHandler != nil {
		if err := r.ErrorHandler.RetryWithBackoff(ctx, statusUpdateErr,
			fmt.Sprintf("update-status-%s-%s", rule.Namespace, rule.Name)); err != nil {
			log.Error(err, "Failed to update rule status with retries")

			// Determine if we should requeue based on error type
			if r.ErrorHandler.ShouldRetry(err, 0) {
				delay := r.ErrorHandler.CalculateDelay(1)
				return ctrl.Result{RequeueAfter: delay}, nil
			}

			return ctrl.Result{}, err
		}
	} else {
		if err := statusUpdateErr(); err != nil {
			log.Error(err, "Failed to update rule status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// handleDeletion handles the deletion of an AnnotationScheduleRule
func (r *AnnotationScheduleRuleReconciler) handleDeletion(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if controllerutil.ContainsFinalizer(rule, AnnotationScheduleRuleFinalizer) {
		// Audit log rule deletion
		if r.AuditLogger != nil {
			r.AuditLogger.LogRuleDeletion(ctx, r.getRuleID(rule), rule.Name, rule.Namespace, rule.Spec.Annotations)
		}

		// Unschedule the rule
		ruleID := r.getRuleID(rule)
		if err := r.Scheduler.UnscheduleRule(ruleID); err != nil {
			log.Error(err, "Failed to unschedule rule during deletion")
		}

		// Remove annotations applied by this rule
		if err := r.cleanupRuleAnnotations(ctx, rule); err != nil {
			log.Error(err, "Failed to cleanup rule annotations")
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(rule, AnnotationScheduleRuleFinalizer)
		if err := r.Update(ctx, rule); err != nil {
			log.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// cleanupRuleAnnotations removes all annotations applied by this rule
func (r *AnnotationScheduleRuleReconciler) cleanupRuleAnnotations(ctx context.Context, rule *schedulerv1.AnnotationScheduleRule) error {
	log := logf.FromContext(ctx)

	// Get annotation keys to remove
	keys := make([]string, 0, len(rule.Spec.Annotations))
	for key := range rule.Spec.Annotations {
		keys = append(keys, key)
	}

	if len(keys) == 0 {
		return nil
	}

	ruleID := r.getRuleID(rule)

	// Remove annotations from matching resources
	err := r.ResourceManager.RemoveAnnotations(
		ctx,
		rule.Spec.Selector,
		keys,
		rule.Spec.TargetResources,
		rule.Namespace,
		ruleID,
	)

	if err != nil {
		log.Error(err, "Failed to remove annotations during cleanup")
		return err
	}

	log.Info("Successfully cleaned up rule annotations", "ruleID", ruleID)
	return nil
}

// validateRule validates the AnnotationScheduleRule configuration
func (r *AnnotationScheduleRuleReconciler) validateRule(rule *schedulerv1.AnnotationScheduleRule) error {
	// Validate resource types
	if err := r.validateResourceTypes(rule); err != nil {
		return err
	}

	// Validate annotations configuration
	if err := r.validateAnnotationsConfig(rule); err != nil {
		return err
	}

	// Validate conflict resolution strategy
	if err := r.validateConflictResolution(rule); err != nil {
		return err
	}

	// Validate template variables
	if err := r.validateTemplateVariables(rule); err != nil {
		return err
	}

	// Validate schedule configuration
	if err := r.validateScheduleConfig(rule); err != nil {
		return err
	}

	return nil
}

// validateResourceTypes validates the target resource types
func (r *AnnotationScheduleRuleReconciler) validateResourceTypes(rule *schedulerv1.AnnotationScheduleRule) error {
	if unsupported := r.ResourceManager.ValidateResourceTypes(rule.Spec.TargetResources); len(unsupported) > 0 {
		return fmt.Errorf("unsupported resource types: %v", unsupported)
	}
	return nil
}

// validateAnnotationsConfig validates the annotations configuration
func (r *AnnotationScheduleRuleReconciler) validateAnnotationsConfig(rule *schedulerv1.AnnotationScheduleRule) error {
	hasLegacyAnnotations := len(rule.Spec.Annotations) > 0
	hasAnnotationGroups := len(rule.Spec.AnnotationGroups) > 0

	// Check that at least one annotation method is specified
	if !hasLegacyAnnotations && !hasAnnotationGroups {
		return fmt.Errorf("rule must specify either annotations or annotationGroups")
	}

	// Check that only one annotation method is specified
	if hasLegacyAnnotations && hasAnnotationGroups {
		return fmt.Errorf("rule cannot specify both annotations and annotationGroups, use one or the other")
	}

	// Validate legacy annotations if present
	if hasLegacyAnnotations {
		return r.validateLegacyAnnotations(rule)
	}

	// Validate annotation groups if present
	if hasAnnotationGroups {
		return r.validateAnnotationGroups(rule)
	}

	return nil
}

// validateLegacyAnnotations validates the legacy annotations
func (r *AnnotationScheduleRuleReconciler) validateLegacyAnnotations(rule *schedulerv1.AnnotationScheduleRule) error {
	// Security validation for annotations
	if r.SecurityValidator != nil {
		if err := r.SecurityValidator.ValidateAnnotations(rule.Spec.Annotations); err != nil {
			// Log security validation failure for audit
			if r.AuditLogger != nil {
				r.AuditLogger.LogValidationFailure(context.Background(),
					r.getRuleID(rule), rule.Name, rule.Namespace,
					rule.Spec.Annotations, err)
			}
			return fmt.Errorf("security validation failed: %w", err)
		}
	}

	// Basic annotation validation (format, etc.)
	if err := r.ResourceManager.ValidateAnnotations(rule.Spec.Annotations); err != nil {
		return fmt.Errorf("invalid annotations: %w", err)
	}

	return nil
}

// validateAnnotationGroups validates the annotation groups
func (r *AnnotationScheduleRuleReconciler) validateAnnotationGroups(rule *schedulerv1.AnnotationScheduleRule) error {
	if err := r.ResourceManager.ValidateAnnotationGroups(rule.Spec.AnnotationGroups); err != nil {
		return fmt.Errorf("invalid annotation groups: %w", err)
	}

	// Validate template variables if any operations use templates
	for _, group := range rule.Spec.AnnotationGroups {
		for _, op := range group.Operations {
			if op.Template && op.Value != "" {
				if err := r.ResourceManager.TemplateEngine.ValidateTemplate(op.Value); err != nil {
					return fmt.Errorf("invalid template in group %s, operation %s: %w", group.Name, op.Key, err)
				}
			}
		}
	}

	return nil
}

// validateConflictResolution validates the conflict resolution strategy
func (r *AnnotationScheduleRuleReconciler) validateConflictResolution(rule *schedulerv1.AnnotationScheduleRule) error {
	if rule.Spec.ConflictResolution != "" {
		validStrategies := []string{"priority", "timestamp", "skip"}
		valid := false
		for _, strategy := range validStrategies {
			if rule.Spec.ConflictResolution == strategy {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid conflict resolution strategy: %s (must be one of: %v)",
				rule.Spec.ConflictResolution, validStrategies)
		}
	}
	return nil
}

// validateTemplateVariables validates the template variables
func (r *AnnotationScheduleRuleReconciler) validateTemplateVariables(rule *schedulerv1.AnnotationScheduleRule) error {
	for key, value := range rule.Spec.TemplateVariables {
		if key == "" {
			return fmt.Errorf("template variable key cannot be empty")
		}
		// Validate variable name format
		if err := r.ResourceManager.TemplateEngine.ValidateTemplate("${" + key + "}"); err != nil {
			return fmt.Errorf("invalid template variable name %s: %w", key, err)
		}
		// Value can be any string, including empty
		_ = value
	}
	return nil
}

// validateScheduleConfig validates the schedule configuration
func (r *AnnotationScheduleRuleReconciler) validateScheduleConfig(rule *schedulerv1.AnnotationScheduleRule) error {
	schedule := rule.Spec.Schedule

	switch schedule.Type {
	case "datetime":
		return r.validateDatetimeSchedule(schedule)
	case "cron":
		return r.validateCronSchedule(schedule)
	default:
		return fmt.Errorf("schedule type must be 'datetime' or 'cron'")
	}
}

// validateDatetimeSchedule validates a datetime schedule
func (r *AnnotationScheduleRuleReconciler) validateDatetimeSchedule(schedule schedulerv1.ScheduleConfig) error {
	if schedule.StartTime == nil && schedule.EndTime == nil {
		return fmt.Errorf("datetime schedule must specify at least startTime or endTime")
	}

	if schedule.StartTime != nil && schedule.EndTime != nil {
		if schedule.EndTime.Time.Before(schedule.StartTime.Time) {
			return fmt.Errorf("endTime cannot be before startTime")
		}
	}

	return nil
}

// validateCronSchedule validates a cron schedule
func (r *AnnotationScheduleRuleReconciler) validateCronSchedule(schedule schedulerv1.ScheduleConfig) error {
	if schedule.CronExpression == "" {
		return fmt.Errorf("cron schedule must specify cronExpression")
	}

	if schedule.Action == "" {
		return fmt.Errorf("cron schedule must specify action (apply or remove)")
	}

	if schedule.Action != "apply" && schedule.Action != "remove" {
		return fmt.Errorf("cron schedule action must be 'apply' or 'remove'")
	}

	return nil
}

// updateRuleStatus updates the rule's status phase and adds a condition
func (r *AnnotationScheduleRuleReconciler) updateRuleStatus(_ context.Context, rule *schedulerv1.AnnotationScheduleRule, phase, message string) {
	previousPhase := rule.Status.Phase
	rule.Status.Phase = phase

	// Log phase transition
	if previousPhase != phase {
		r.Log.Info("Rule phase transition",
			"rule", rule.Name,
			"namespace", rule.Namespace,
			"previousPhase", previousPhase,
			"newPhase", phase,
			"message", message)
	}

	// Add condition based on phase
	var conditionType, reason string
	var status metav1.ConditionStatus

	switch phase {
	case PhaseFailed:
		conditionType = ConditionTypeReady
		status = metav1.ConditionFalse
		reason = ReasonRuleFailed
	case PhaseActive:
		conditionType = ConditionTypeReady
		status = metav1.ConditionTrue
		reason = ReasonRuleActive
	case PhaseCompleted:
		conditionType = ConditionTypeReady
		status = metav1.ConditionTrue
		reason = ReasonRuleCompleted
	default:
		conditionType = ConditionTypeReady
		status = metav1.ConditionUnknown
		reason = ReasonRulePending
	}

	r.setCondition(rule, conditionType, status, reason, message)
}

// setCondition sets a condition on the rule status
func (r *AnnotationScheduleRuleReconciler) setCondition(rule *schedulerv1.AnnotationScheduleRule, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metav1.NewTime(time.Now()),
	}

	// Find existing condition and update it, or append new one
	for i, existingCondition := range rule.Status.Conditions {
		if existingCondition.Type == conditionType {
			// Only update if status changed
			if existingCondition.Status != status {
				rule.Status.Conditions[i] = condition
			} else {
				// Update message and timestamp even if status is the same
				rule.Status.Conditions[i].Message = message
				rule.Status.Conditions[i].LastTransitionTime = condition.LastTransitionTime
			}
			return
		}
	}

	// Condition not found, append new one
	rule.Status.Conditions = append(rule.Status.Conditions, condition)
}

// getRuleID generates a unique identifier for a rule
func (r *AnnotationScheduleRuleReconciler) getRuleID(rule *schedulerv1.AnnotationScheduleRule) string {
	return types.NamespacedName{
		Namespace: rule.Namespace,
		Name:      rule.Name,
	}.String()
}

// logRuleStatus logs the current status of a rule for monitoring purposes
func (r *AnnotationScheduleRuleReconciler) logRuleStatus(rule *schedulerv1.AnnotationScheduleRule) {
	ruleID := r.getRuleID(rule)

	// Prepare status information
	statusInfo := map[string]interface{}{
		"ruleID":            ruleID,
		"phase":             rule.Status.Phase,
		"affectedResources": rule.Status.AffectedResources,
	}

	if rule.Status.LastExecutionTime != nil {
		statusInfo["lastExecutionTime"] = rule.Status.LastExecutionTime.Format(time.RFC3339)
	}

	if rule.Status.NextExecutionTime != nil {
		statusInfo["nextExecutionTime"] = rule.Status.NextExecutionTime.Format(time.RFC3339)
	}

	// Add condition summary
	conditionSummary := make(map[string]string)
	for _, condition := range rule.Status.Conditions {
		conditionSummary[condition.Type] = string(condition.Status)
	}
	statusInfo["conditions"] = conditionSummary

	// Log with appropriate level based on phase
	switch rule.Status.Phase {
	case PhaseFailed:
		r.Log.Error(nil, "Rule status", "statusInfo", statusInfo)
	case PhaseCompleted:
		r.Log.Info("Rule status", "statusInfo", statusInfo)
	default:
		r.Log.V(1).Info("Rule status", "statusInfo", statusInfo)
	}
}

// logExecutionMetrics logs execution metrics for monitoring
func (r *AnnotationScheduleRuleReconciler) logExecutionMetrics() {
	if r.Metrics == nil {
		return
	}

	total, successful, failed, lastExecution, avgDuration := r.Metrics.GetMetrics()

	r.Log.Info("Execution metrics",
		"totalExecutions", total,
		"successfulExecutions", successful,
		"failedExecutions", failed,
		"successRate", fmt.Sprintf("%.2f%%", float64(successful)/float64(total)*100),
		"lastExecutionTime", lastExecution.Format(time.RFC3339),
		"averageExecutionTime", avgDuration)
}

// getDetailedErrorMessage creates a detailed error message with context
func (r *AnnotationScheduleRuleReconciler) getDetailedErrorMessage(err error, contextMsg string, rule *schedulerv1.AnnotationScheduleRule) string {
	baseMsg := fmt.Sprintf("%s failed for rule %s/%s: %v", contextMsg, rule.Namespace, rule.Name, err)

	// Add additional context based on rule configuration
	switch rule.Spec.Schedule.Type {
	case "cron":
		baseMsg += fmt.Sprintf(" (cron: %s, action: %s)", rule.Spec.Schedule.CronExpression, rule.Spec.Schedule.Action)
	case "datetime":
		if rule.Spec.Schedule.StartTime != nil {
			baseMsg += fmt.Sprintf(" (start: %s)", rule.Spec.Schedule.StartTime.Format(time.RFC3339))
		}
		if rule.Spec.Schedule.EndTime != nil {
			baseMsg += fmt.Sprintf(" (end: %s)", rule.Spec.Schedule.EndTime.Format(time.RFC3339))
		}
	}

	baseMsg += fmt.Sprintf(" (targets: %v)", rule.Spec.TargetResources)

	return baseMsg
}

// SetupWithManager sets up the controller with the Manager.
func (r *AnnotationScheduleRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Initialize components if not already set
	if r.Log.GetSink() == nil {
		r.Log = ctrl.Log.WithName("controllers").WithName("AnnotationScheduleRule")
	}

	// Initialize error handler if not already set
	if r.ErrorHandler == nil {
		r.ErrorHandler = errors.NewErrorHandler(errors.DefaultRetryConfig(), r.Log.WithName("ErrorHandler"))
	}

	// Initialize recovery manager if not already set
	if r.RecoveryManager == nil {
		r.RecoveryManager = errors.NewStateRecoveryManager(r.Client, r.Log.WithName("RecoveryManager"), r.ErrorHandler)
	}

	if r.ResourceManager == nil {
		r.ResourceManager = resource.NewResourceManager(r.Client, r.Log.WithName("ResourceManager"), r.Scheme)
	}

	// Initialize metrics if not already set
	if r.Metrics == nil {
		r.Metrics = &ExecutionMetrics{}
	}

	if r.Scheduler == nil {
		taskExecutor := NewTaskExecutor(r.Client, r.ResourceManager, r.Log.WithName("TaskExecutor"), r)
		r.Scheduler = scheduler.NewTimeScheduler(taskExecutor, r.Log.WithName("TimeScheduler"))

		// Start the scheduler
		go func() {
			if err := r.Scheduler.Start(context.Background()); err != nil {
				r.Log.Error(err, "Failed to start scheduler")
			}
		}()

		// Start metrics logging goroutine
		go r.startMetricsLogging()

		// Perform state recovery after a short delay to allow the manager to start
		go func() {
			time.Sleep(10 * time.Second) // Wait for manager to be ready
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			if result, err := r.RecoveryManager.RecoverControllerState(ctx); err != nil {
				r.Log.Error(err, "Failed to recover controller state")
			} else {
				metrics := r.RecoveryManager.GetRecoveryMetrics(result)
				r.Log.Info("Controller state recovery completed", "metrics", metrics)
			}
		}()
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&schedulerv1.AnnotationScheduleRule{}).
		Named("annotationschedulerule").
		Complete(r)
}

// startMetricsLogging starts a goroutine that periodically logs execution metrics
func (r *AnnotationScheduleRuleReconciler) startMetricsLogging() {
	ticker := time.NewTicker(10 * time.Minute) // Log metrics every 10 minutes
	defer ticker.Stop()

	for range ticker.C {
		r.logExecutionMetrics()
	}
}
