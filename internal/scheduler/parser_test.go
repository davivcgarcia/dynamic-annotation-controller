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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulerv1 "github.com/davivcgarcia/dynamic-annotation-controller/api/v1"
)

func TestNewScheduleParser(t *testing.T) {
	parser := NewScheduleParser()
	if parser == nil {
		t.Fatal("NewScheduleParser() returned nil")
	}
}

func TestValidateSchedule(t *testing.T) {
	parser := NewScheduleParser()
	now := time.Now()
	futureTime := now.Add(time.Hour)
	pastTime := now.Add(-time.Hour)

	tests := []struct {
		name        string
		schedule    schedulerv1.ScheduleConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid datetime schedule with start and end",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: futureTime},
				EndTime:   &metav1.Time{Time: futureTime.Add(time.Hour)},
			},
			expectError: false,
		},
		{
			name: "valid datetime schedule with only start",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: futureTime},
			},
			expectError: false,
		},
		{
			name: "valid cron schedule",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 9 * * 1-5",
				Action:         "apply",
			},
			expectError: false,
		},
		{
			name: "invalid schedule type",
			schedule: schedulerv1.ScheduleConfig{
				Type: "invalid",
			},
			expectError: true,
			errorMsg:    "invalid schedule type",
		},
		{
			name: "datetime schedule missing start time",
			schedule: schedulerv1.ScheduleConfig{
				Type: "datetime",
			},
			expectError: true,
			errorMsg:    "startTime is required",
		},
		{
			name: "datetime schedule with past start time",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime},
			},
			expectError: false, // ValidateSchedule allows past times, ValidateScheduleForCreation doesn't
		},
		{
			name: "datetime schedule with end before start",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: futureTime},
				EndTime:   &metav1.Time{Time: futureTime.Add(-time.Minute)},
			},
			expectError: true,
			errorMsg:    "endTime",
		},
		{
			name: "datetime schedule with cron fields",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "datetime",
				StartTime:      &metav1.Time{Time: futureTime},
				CronExpression: "0 9 * * *",
			},
			expectError: true,
			errorMsg:    "cronExpression should not be set",
		},
		{
			name: "cron schedule missing expression",
			schedule: schedulerv1.ScheduleConfig{
				Type:   "cron",
				Action: "apply",
			},
			expectError: true,
			errorMsg:    "cronExpression is required",
		},
		{
			name: "cron schedule missing action",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 9 * * *",
			},
			expectError: true,
			errorMsg:    "action is required",
		},
		{
			name: "cron schedule invalid action",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 9 * * *",
				Action:         "invalid",
			},
			expectError: true,
			errorMsg:    "action must be",
		},
		{
			name: "cron schedule invalid expression",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "invalid cron",
				Action:         "apply",
			},
			expectError: true,
			errorMsg:    "invalid cron expression",
		},
		{
			name: "cron schedule with datetime fields",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 9 * * *",
				Action:         "apply",
				StartTime:      &metav1.Time{Time: futureTime},
			},
			expectError: true,
			errorMsg:    "startTime should not be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parser.ValidateSchedule(tt.schedule)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !containsString(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain '%s', got: %s", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCalculateNextExecution(t *testing.T) {
	parser := NewScheduleParser()
	now := time.Now()
	futureTime := now.Add(time.Hour)
	pastTime := now.Add(-time.Hour)

	tests := []struct {
		name          string
		schedule      schedulerv1.ScheduleConfig
		lastExecution *time.Time
		expectTime    bool
		expectError   bool
	}{
		{
			name: "datetime schedule - first execution",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: futureTime},
			},
			lastExecution: nil,
			expectTime:    true,
		},
		{
			name: "datetime schedule - after start, before end",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime},
				EndTime:   &metav1.Time{Time: futureTime},
			},
			lastExecution: &pastTime,
			expectTime:    true,
		},
		{
			name: "datetime schedule - completed",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime},
			},
			lastExecution: &pastTime,
			expectTime:    false,
		},
		{
			name: "cron schedule - first execution",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 9 * * *",
				Action:         "apply",
			},
			lastExecution: nil,
			expectTime:    true,
		},
		{
			name: "cron schedule - recurring",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 9 * * *",
				Action:         "apply",
			},
			lastExecution: &pastTime,
			expectTime:    true,
		},
		{
			name: "invalid schedule",
			schedule: schedulerv1.ScheduleConfig{
				Type: "invalid",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextTime, err := parser.CalculateNextExecution(tt.schedule, tt.lastExecution)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.expectTime {
				if nextTime == nil {
					t.Errorf("expected next execution time but got nil")
				}
			} else {
				if nextTime != nil {
					t.Errorf("expected no next execution time but got: %v", *nextTime)
				}
			}
		})
	}
}

func TestGetSchedulePhase(t *testing.T) {
	parser := NewScheduleParser()
	now := time.Now()
	futureTime := now.Add(time.Hour)
	pastTime := now.Add(-time.Hour)

	tests := []struct {
		name          string
		schedule      schedulerv1.ScheduleConfig
		lastExecution *time.Time
		expectedPhase string
		expectError   bool
	}{
		{
			name: "datetime schedule - pending",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: futureTime},
			},
			expectedPhase: "pending",
		},
		{
			name: "datetime schedule - active with end time",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime},
				EndTime:   &metav1.Time{Time: futureTime},
			},
			expectedPhase: "active",
		},
		{
			name: "datetime schedule - completed with end time",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime.Add(-time.Hour)},
				EndTime:   &metav1.Time{Time: pastTime},
			},
			expectedPhase: "completed",
		},
		{
			name: "datetime schedule - completed without end time",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime},
			},
			expectedPhase: "completed",
		},
		{
			name: "cron schedule - always active",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 9 * * *",
				Action:         "apply",
			},
			expectedPhase: "active",
		},
		{
			name: "invalid schedule",
			schedule: schedulerv1.ScheduleConfig{
				Type: "invalid",
			},
			expectedPhase: "failed",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phase, err := parser.GetSchedulePhase(tt.schedule, tt.lastExecution)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}

			if phase != tt.expectedPhase {
				t.Errorf("expected phase %s, got %s", tt.expectedPhase, phase)
			}
		})
	}
}

func TestIsTimeToExecute(t *testing.T) {
	parser := NewScheduleParser()
	now := time.Now()
	futureTime := now.Add(time.Hour)
	nearFutureTime := now.Add(30 * time.Second) // Within tolerance
	pastTime := now.Add(-time.Hour)

	tests := []struct {
		name          string
		schedule      schedulerv1.ScheduleConfig
		lastExecution *time.Time
		expectReady   bool
		expectError   bool
	}{
		{
			name: "datetime schedule - ready to execute",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: nearFutureTime},
			},
			expectReady: true,
		},
		{
			name: "datetime schedule - not ready yet",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: futureTime},
			},
			expectReady: false,
		},
		{
			name: "datetime schedule - no more executions",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime},
			},
			lastExecution: &pastTime,
			expectReady:   false,
		},
		{
			name: "invalid schedule",
			schedule: schedulerv1.ScheduleConfig{
				Type: "invalid",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready, err := parser.IsTimeToExecute(tt.schedule, tt.lastExecution)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if ready != tt.expectReady {
				t.Errorf("expected ready=%v, got %v", tt.expectReady, ready)
			}
		})
	}
}

func TestGetExecutionAction(t *testing.T) {
	parser := NewScheduleParser()
	now := time.Now()
	pastTime := now.Add(-time.Hour)

	tests := []struct {
		name           string
		schedule       schedulerv1.ScheduleConfig
		lastExecution  *time.Time
		expectedAction string
		expectError    bool
	}{
		{
			name: "datetime schedule - apply action",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime},
			},
			expectedAction: "apply",
		},
		{
			name: "datetime schedule - remove action",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime.Add(-time.Hour)},
				EndTime:   &metav1.Time{Time: pastTime},
			},
			lastExecution:  func() *time.Time { t := pastTime.Add(-time.Hour); return &t }(),
			expectedAction: "remove",
		},
		{
			name: "cron schedule - apply action",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 9 * * *",
				Action:         "apply",
			},
			expectedAction: "apply",
		},
		{
			name: "cron schedule - remove action",
			schedule: schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: "0 17 * * *",
				Action:         "remove",
			},
			expectedAction: "remove",
		},
		{
			name: "invalid schedule type",
			schedule: schedulerv1.ScheduleConfig{
				Type: "invalid",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := parser.GetExecutionAction(tt.schedule, tt.lastExecution)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if action != tt.expectedAction {
				t.Errorf("expected action %s, got %s", tt.expectedAction, action)
			}
		})
	}
}

func TestValidateScheduleForCreation(t *testing.T) {
	parser := NewScheduleParser()
	now := time.Now()
	futureTime := now.Add(time.Hour)
	pastTime := now.Add(-time.Hour)

	tests := []struct {
		name        string
		schedule    schedulerv1.ScheduleConfig
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid datetime schedule for creation",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: futureTime},
			},
			expectError: false,
		},
		{
			name: "datetime schedule with past start time for creation",
			schedule: schedulerv1.ScheduleConfig{
				Type:      "datetime",
				StartTime: &metav1.Time{Time: pastTime},
			},
			expectError: true,
			errorMsg:    "startTime cannot be in the past",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parser.ValidateScheduleForCreation(tt.schedule)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorMsg != "" && !containsString(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain '%s', got: %s", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestCronExpressionParsing(t *testing.T) {
	parser := NewScheduleParser()

	validExpressions := []string{
		"0 9 * * *",       // Daily at 9 AM
		"0 9 * * 1-5",     // Weekdays at 9 AM
		"*/15 * * * *",    // Every 15 minutes
		"0 0 1 * *",       // First day of month
		"0 0 * * 0",       // Every Sunday
		"30 8-17 * * 1-5", // Every 30 minutes during business hours
	}

	invalidExpressions := []string{
		"invalid",
		"0 25 * * *", // Invalid hour
		"60 * * * *", // Invalid minute
		"* * 32 * *", // Invalid day
		"* * * 13 *", // Invalid month
		"* * * * 8",  // Invalid day of week
	}

	for _, expr := range validExpressions {
		t.Run("valid_"+expr, func(t *testing.T) {
			schedule := schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: expr,
				Action:         "apply",
			}
			err := parser.ValidateSchedule(schedule)
			if err != nil {
				t.Errorf("expected valid cron expression '%s' to pass validation, got error: %v", expr, err)
			}
		})
	}

	for _, expr := range invalidExpressions {
		t.Run("invalid_"+expr, func(t *testing.T) {
			schedule := schedulerv1.ScheduleConfig{
				Type:           "cron",
				CronExpression: expr,
				Action:         "apply",
			}
			err := parser.ValidateSchedule(schedule)
			if err == nil {
				t.Errorf("expected invalid cron expression '%s' to fail validation", expr)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
