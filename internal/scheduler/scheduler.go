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

package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

// ScheduleAction represents the type of action to perform
type ScheduleAction string

const (
	ActionApply  ScheduleAction = "apply"
	ActionRemove ScheduleAction = "remove"
)

// ScheduledTask represents a task to be executed at a specific time
type ScheduledTask struct {
	RuleID             string
	RuleName           string
	RuleNamespace      string
	Action             ScheduleAction
	ExecuteAt          time.Time
	Annotations        map[string]string             // Legacy field for backward compatibility
	AnnotationGroups   []schedulerv1.AnnotationGroup // New field for multi-annotation support
	Selector           metav1.LabelSelector
	TargetResources    []string
	TemplateVariables  map[string]string
	ConflictResolution string
}

// TaskExecutor defines the interface for executing annotation operations
type TaskExecutor interface {
	ExecuteTask(ctx context.Context, task ScheduledTask) error
}

// TimeScheduler defines the interface for managing scheduled annotation operations
type TimeScheduler interface {
	// ScheduleRule adds or updates a rule in the scheduler
	ScheduleRule(rule *schedulerv1.AnnotationScheduleRule) error

	// UnscheduleRule removes a rule from the scheduler
	UnscheduleRule(ruleID string) error

	// Start begins the scheduler's operation
	Start(ctx context.Context) error

	// Stop gracefully shuts down the scheduler
	Stop() error

	// GetNextExecutionTime returns the next execution time for a rule
	GetNextExecutionTime(ruleID string) (*time.Time, error)

	// GetScheduledTaskCount returns the number of scheduled tasks
	GetScheduledTaskCount() int
}

// timeScheduler implements the TimeScheduler interface
type timeScheduler struct {
	parser   *ScheduleParser
	executor TaskExecutor
	logger   logr.Logger

	// Task management
	tasks     map[string]*ScheduledTask // ruleID -> task
	taskQueue chan *ScheduledTask

	// Synchronization
	mu      sync.RWMutex
	stopCh  chan struct{}
	doneCh  chan struct{}
	started bool

	// Configuration
	tickInterval    time.Duration
	maxConcurrency  int
	workerSemaphore chan struct{}
}

// NewTimeScheduler creates a new time-based scheduler
func NewTimeScheduler(executor TaskExecutor, logger logr.Logger) TimeScheduler {
	return &timeScheduler{
		parser:          NewScheduleParser(),
		executor:        executor,
		logger:          logger,
		tasks:           make(map[string]*ScheduledTask),
		taskQueue:       make(chan *ScheduledTask, 1000), // Buffer for 1000 tasks
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
		tickInterval:    30 * time.Second, // Check every 30 seconds
		maxConcurrency:  10,               // Max 10 concurrent executions
		workerSemaphore: make(chan struct{}, 10),
	}
}

// ScheduleRule adds or updates a rule in the scheduler
func (s *timeScheduler) ScheduleRule(rule *schedulerv1.AnnotationScheduleRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ruleID := s.getRuleID(rule)

	// Validate the schedule
	if err := s.parser.ValidateSchedule(rule.Spec.Schedule); err != nil {
		return fmt.Errorf("invalid schedule for rule %s: %w", ruleID, err)
	}

	// Calculate next execution time
	var lastExecution *time.Time
	if rule.Status.LastExecutionTime != nil {
		lastExecution = &rule.Status.LastExecutionTime.Time
	}

	nextExecution, err := s.parser.CalculateNextExecution(rule.Spec.Schedule, lastExecution)
	if err != nil {
		return fmt.Errorf("failed to calculate next execution for rule %s: %w", ruleID, err)
	}

	// If no next execution, remove the rule
	if nextExecution == nil {
		delete(s.tasks, ruleID)
		s.logger.Info("Rule completed, removed from scheduler", "ruleID", ruleID)
		return nil
	}

	// Determine the action to take
	action, err := s.parser.GetExecutionAction(rule.Spec.Schedule, lastExecution)
	if err != nil {
		return fmt.Errorf("failed to determine action for rule %s: %w", ruleID, err)
	}

	// Create or update the scheduled task
	task := &ScheduledTask{
		RuleID:             ruleID,
		RuleName:           rule.Name,
		RuleNamespace:      rule.Namespace,
		Action:             ScheduleAction(action),
		ExecuteAt:          *nextExecution,
		Annotations:        rule.Spec.Annotations,      // Legacy support
		AnnotationGroups:   rule.Spec.AnnotationGroups, // New multi-annotation support
		Selector:           rule.Spec.Selector,
		TargetResources:    rule.Spec.TargetResources,
		TemplateVariables:  rule.Spec.TemplateVariables,
		ConflictResolution: rule.Spec.ConflictResolution,
	}

	s.tasks[ruleID] = task
	s.logger.Info("Rule scheduled",
		"ruleID", ruleID,
		"action", action,
		"executeAt", nextExecution.Format(time.RFC3339))

	return nil
}

// UnscheduleRule removes a rule from the scheduler
func (s *timeScheduler) UnscheduleRule(ruleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.tasks, ruleID)
	s.logger.Info("Rule unscheduled", "ruleID", ruleID)
	return nil
}

// Start begins the scheduler's operation
func (s *timeScheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return fmt.Errorf("scheduler is already started")
	}

	s.started = true
	s.logger.Info("Starting time scheduler")

	// Start the main scheduler loop
	go s.schedulerLoop(ctx)

	// Start worker goroutines for task execution
	for i := 0; i < s.maxConcurrency; i++ {
		go s.taskWorker(ctx, i)
	}

	return nil
}

// Stop gracefully shuts down the scheduler
func (s *timeScheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	s.logger.Info("Stopping time scheduler")
	close(s.stopCh)

	// Wait for scheduler to stop
	<-s.doneCh

	s.started = false
	s.logger.Info("Time scheduler stopped")
	return nil
}

// GetNextExecutionTime returns the next execution time for a rule
func (s *timeScheduler) GetNextExecutionTime(ruleID string) (*time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[ruleID]
	if !exists {
		return nil, fmt.Errorf("rule %s not found in scheduler", ruleID)
	}

	return &task.ExecuteAt, nil
}

// GetScheduledTaskCount returns the number of scheduled tasks
func (s *timeScheduler) GetScheduledTaskCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.tasks)
}

// schedulerLoop is the main loop that checks for tasks to execute
func (s *timeScheduler) schedulerLoop(ctx context.Context) {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()

	s.logger.Info("Scheduler loop started", "tickInterval", s.tickInterval)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Scheduler loop stopping due to context cancellation")
			return
		case <-s.stopCh:
			s.logger.Info("Scheduler loop stopping due to stop signal")
			return
		case <-ticker.C:
			s.checkAndQueueTasks()
		}
	}
}

// checkAndQueueTasks checks for tasks that are ready to execute and queues them
func (s *timeScheduler) checkAndQueueTasks() {
	s.mu.RLock()
	now := time.Now()
	tasksToExecute := make([]*ScheduledTask, 0)

	for _, task := range s.tasks {
		// Check if task is ready to execute (with 1 minute tolerance)
		if task.ExecuteAt.Before(now.Add(time.Minute)) {
			tasksToExecute = append(tasksToExecute, task)
		}
	}
	s.mu.RUnlock()

	// Queue tasks for execution
	for _, task := range tasksToExecute {
		select {
		case s.taskQueue <- task:
			s.logger.Info("Task queued for execution",
				"ruleID", task.RuleID,
				"action", task.Action,
				"scheduledTime", task.ExecuteAt.Format(time.RFC3339))
		default:
			s.logger.Error(nil, "Task queue is full, dropping task",
				"ruleID", task.RuleID,
				"action", task.Action)
		}
	}
}

// taskWorker processes tasks from the queue
func (s *timeScheduler) taskWorker(ctx context.Context, workerID int) {
	s.logger.Info("Task worker started", "workerID", workerID)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("Task worker stopping due to context cancellation", "workerID", workerID)
			return
		case <-s.stopCh:
			s.logger.Info("Task worker stopping due to stop signal", "workerID", workerID)
			return
		case task := <-s.taskQueue:
			s.executeTaskWithSemaphore(ctx, task, workerID)
		}
	}
}

// executeTaskWithSemaphore executes a task with concurrency control
func (s *timeScheduler) executeTaskWithSemaphore(ctx context.Context, task *ScheduledTask, workerID int) {
	// Acquire semaphore
	select {
	case s.workerSemaphore <- struct{}{}:
		defer func() { <-s.workerSemaphore }()
	case <-ctx.Done():
		return
	case <-s.stopCh:
		return
	}

	s.logger.Info("Executing task",
		"workerID", workerID,
		"ruleID", task.RuleID,
		"action", task.Action,
		"scheduledTime", task.ExecuteAt.Format(time.RFC3339))

	// Execute the task
	if err := s.executor.ExecuteTask(ctx, *task); err != nil {
		s.logger.Error(err, "Failed to execute task",
			"workerID", workerID,
			"ruleID", task.RuleID,
			"action", task.Action)
	} else {
		s.logger.Info("Task executed successfully",
			"workerID", workerID,
			"ruleID", task.RuleID,
			"action", task.Action)
	}

	// Remove the executed task from the scheduler
	// Note: The controller will reschedule the rule if needed
	s.mu.Lock()
	delete(s.tasks, task.RuleID)
	s.mu.Unlock()
}

// getRuleID generates a unique identifier for a rule
func (s *timeScheduler) getRuleID(rule *schedulerv1.AnnotationScheduleRule) string {
	return types.NamespacedName{
		Namespace: rule.Namespace,
		Name:      rule.Name,
	}.String()
}
