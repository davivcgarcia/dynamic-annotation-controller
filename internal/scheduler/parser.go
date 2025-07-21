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
	"fmt"
	"time"

	"github.com/robfig/cron/v3"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

// ScheduleParser handles parsing and validation of schedule configurations
type ScheduleParser struct {
	cronParser cron.Parser
}

// NewScheduleParser creates a new schedule parser with standard cron options
func NewScheduleParser() *ScheduleParser {
	return &ScheduleParser{
		cronParser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// ValidateSchedule validates a schedule configuration and returns any validation errors
func (p *ScheduleParser) ValidateSchedule(schedule schedulerv1.ScheduleConfig) error {
	switch schedule.Type {
	case "datetime":
		return p.validateDateTimeSchedule(schedule)
	case "cron":
		return p.validateCronSchedule(schedule)
	default:
		return fmt.Errorf("invalid schedule type: %s, must be 'datetime' or 'cron'", schedule.Type)
	}
}

// ValidateScheduleForCreation validates a schedule configuration for new rule creation
func (p *ScheduleParser) ValidateScheduleForCreation(schedule schedulerv1.ScheduleConfig) error {
	switch schedule.Type {
	case "datetime":
		return p.validateDateTimeScheduleForCreation(schedule)
	case "cron":
		return p.validateCronSchedule(schedule)
	default:
		return fmt.Errorf("invalid schedule type: %s, must be 'datetime' or 'cron'", schedule.Type)
	}
}

// validateDateTimeSchedule validates datetime-based schedule configuration (allows past times for existing schedules)
func (p *ScheduleParser) validateDateTimeSchedule(schedule schedulerv1.ScheduleConfig) error {
	if schedule.StartTime == nil {
		return fmt.Errorf("startTime is required for datetime schedule type")
	}

	startTime := schedule.StartTime.Time

	// If endTime is specified, validate it's after startTime
	if schedule.EndTime != nil {
		endTime := schedule.EndTime.Time
		if !endTime.After(startTime) {
			return fmt.Errorf("endTime (%v) must be after startTime (%v)", endTime, startTime)
		}
	}

	// Validate that cron-specific fields are not set
	if schedule.CronExpression != "" {
		return fmt.Errorf("cronExpression should not be set for datetime schedule type")
	}
	if schedule.Action != "" {
		return fmt.Errorf("action should not be set for datetime schedule type")
	}

	return nil
}

// validateDateTimeScheduleForCreation validates datetime-based schedule configuration for new rules
func (p *ScheduleParser) validateDateTimeScheduleForCreation(schedule schedulerv1.ScheduleConfig) error {
	if err := p.validateDateTimeSchedule(schedule); err != nil {
		return err
	}

	now := time.Now()
	startTime := schedule.StartTime.Time

	// Validate that start time is not in the past (with 1 minute tolerance) for new rules
	if startTime.Before(now.Add(-time.Minute)) {
		return fmt.Errorf("startTime cannot be in the past: %v", startTime)
	}

	return nil
}

// validateCronSchedule validates cron-based schedule configuration
func (p *ScheduleParser) validateCronSchedule(schedule schedulerv1.ScheduleConfig) error {
	if schedule.CronExpression == "" {
		return fmt.Errorf("cronExpression is required for cron schedule type")
	}

	if schedule.Action == "" {
		return fmt.Errorf("action is required for cron schedule type")
	}

	if schedule.Action != "apply" && schedule.Action != "remove" {
		return fmt.Errorf("action must be 'apply' or 'remove', got: %s", schedule.Action)
	}

	// Validate cron expression syntax
	_, err := p.cronParser.Parse(schedule.CronExpression)
	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %w", schedule.CronExpression, err)
	}

	// Validate that datetime-specific fields are not set
	if schedule.StartTime != nil {
		return fmt.Errorf("startTime should not be set for cron schedule type")
	}
	if schedule.EndTime != nil {
		return fmt.Errorf("endTime should not be set for cron schedule type")
	}

	return nil
}

// CalculateNextExecution calculates the next execution time for a given schedule
func (p *ScheduleParser) CalculateNextExecution(schedule schedulerv1.ScheduleConfig, lastExecution *time.Time) (*time.Time, error) {
	if err := p.ValidateSchedule(schedule); err != nil {
		return nil, fmt.Errorf("invalid schedule: %w", err)
	}

	switch schedule.Type {
	case "datetime":
		return p.calculateDateTimeNextExecution(schedule, lastExecution)
	case "cron":
		return p.calculateCronNextExecution(schedule, lastExecution)
	default:
		return nil, fmt.Errorf("unsupported schedule type: %s", schedule.Type)
	}
}

// calculateDateTimeNextExecution calculates next execution for datetime schedules
func (p *ScheduleParser) calculateDateTimeNextExecution(schedule schedulerv1.ScheduleConfig, lastExecution *time.Time) (*time.Time, error) {
	now := time.Now()
	startTime := schedule.StartTime.Time

	// If we haven't executed the start action yet and start time is in the future
	if lastExecution == nil && startTime.After(now) {
		return &startTime, nil
	}

	// If we have an end time and haven't executed the end action yet
	if schedule.EndTime != nil {
		endTime := schedule.EndTime.Time

		// If we've executed the start but not the end, and end time is in the future
		if lastExecution != nil && lastExecution.Equal(startTime) && endTime.After(now) {
			return &endTime, nil
		}
	}

	// No more executions needed
	return nil, nil
}

// calculateCronNextExecution calculates next execution for cron schedules
func (p *ScheduleParser) calculateCronNextExecution(schedule schedulerv1.ScheduleConfig, lastExecution *time.Time) (*time.Time, error) {
	cronSchedule, err := p.cronParser.Parse(schedule.CronExpression)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cron expression: %w", err)
	}

	var baseTime time.Time
	if lastExecution != nil {
		baseTime = *lastExecution
	} else {
		baseTime = time.Now()
	}

	nextTime := cronSchedule.Next(baseTime)
	return &nextTime, nil
}

// GetSchedulePhase determines the current phase of a schedule based on current time and execution history
func (p *ScheduleParser) GetSchedulePhase(schedule schedulerv1.ScheduleConfig, lastExecution *time.Time) (string, error) {
	if err := p.ValidateSchedule(schedule); err != nil {
		return "failed", err
	}

	now := time.Now()

	switch schedule.Type {
	case "datetime":
		return p.getDateTimePhase(schedule, lastExecution, now), nil
	case "cron":
		return p.getCronPhase(schedule, lastExecution, now), nil
	default:
		return "failed", fmt.Errorf("unsupported schedule type: %s", schedule.Type)
	}
}

// getDateTimePhase determines phase for datetime schedules
func (p *ScheduleParser) getDateTimePhase(schedule schedulerv1.ScheduleConfig, lastExecution *time.Time, now time.Time) string {
	startTime := schedule.StartTime.Time

	// If start time is in the future
	if startTime.After(now) {
		return "pending"
	}

	// If we have an end time
	if schedule.EndTime != nil {
		endTime := schedule.EndTime.Time

		// If end time has passed
		if endTime.Before(now) {
			return "completed"
		}

		// If we're between start and end time
		if startTime.Before(now) && endTime.After(now) {
			return "active"
		}
	} else {
		// No end time, so if start time has passed, we're completed
		if startTime.Before(now) {
			return "completed"
		}
	}

	return "active"
}

// getCronPhase determines phase for cron schedules
func (p *ScheduleParser) getCronPhase(schedule schedulerv1.ScheduleConfig, lastExecution *time.Time, now time.Time) string {
	// Cron schedules are always active (recurring)
	return "active"
}

// IsTimeToExecute checks if it's time to execute a schedule
func (p *ScheduleParser) IsTimeToExecute(schedule schedulerv1.ScheduleConfig, lastExecution *time.Time) (bool, error) {
	nextExecution, err := p.CalculateNextExecution(schedule, lastExecution)
	if err != nil {
		return false, err
	}

	if nextExecution == nil {
		return false, nil
	}

	now := time.Now()
	// Allow 1 minute tolerance for execution timing
	return nextExecution.Before(now.Add(time.Minute)), nil
}

// GetExecutionAction determines what action should be taken for the current execution
func (p *ScheduleParser) GetExecutionAction(schedule schedulerv1.ScheduleConfig, lastExecution *time.Time) (string, error) {
	switch schedule.Type {
	case "datetime":
		return p.getDateTimeAction(schedule, lastExecution)
	case "cron":
		return schedule.Action, nil
	default:
		return "", fmt.Errorf("unsupported schedule type: %s", schedule.Type)
	}
}

// getDateTimeAction determines the action for datetime schedules
func (p *ScheduleParser) getDateTimeAction(schedule schedulerv1.ScheduleConfig, lastExecution *time.Time) (string, error) {
	startTime := schedule.StartTime.Time

	// If we haven't executed yet, the next action is apply (regardless of timing)
	if lastExecution == nil {
		return "apply", nil
	}

	// If we have an end time and haven't executed the end action yet
	if schedule.EndTime != nil {
		// If we've executed the start but not the end
		if lastExecution.Equal(startTime) {
			return "remove", nil
		}
	}

	return "", fmt.Errorf("no action needed at this time")
}
