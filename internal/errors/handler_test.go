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
	"net"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func init() {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))
}

func TestErrorHandler_CategorizeError(t *testing.T) {
	logger := logf.Log.WithName("test")
	handler := NewErrorHandler(DefaultRetryConfig(), logger)

	tests := []struct {
		name     string
		err      error
		expected ErrorCategory
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: ErrorCategoryPermanent,
		},
		{
			name:     "not found error",
			err:      apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test"),
			expected: ErrorCategoryResourceNotFound,
		},
		{
			name:     "conflict error",
			err:      apierrors.NewConflict(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test", fmt.Errorf("conflict")),
			expected: ErrorCategoryConflict,
		},
		{
			name:     "timeout error",
			err:      apierrors.NewTimeoutError("timeout", 30),
			expected: ErrorCategoryTransient,
		},
		{
			name:     "server timeout error",
			err:      apierrors.NewServerTimeout(schema.GroupResource{Group: "apps", Resource: "deployments"}, "list", 30),
			expected: ErrorCategoryTransient,
		},
		{
			name:     "service unavailable error",
			err:      apierrors.NewServiceUnavailable("service unavailable"),
			expected: ErrorCategoryTransient,
		},
		{
			name:     "internal error",
			err:      apierrors.NewInternalError(fmt.Errorf("internal error")),
			expected: ErrorCategoryTransient,
		},
		{
			name:     "too many requests error",
			err:      apierrors.NewTooManyRequests("too many requests", 30),
			expected: ErrorCategoryRateLimit,
		},
		{
			name:     "bad request error",
			err:      apierrors.NewBadRequest("bad request"),
			expected: ErrorCategoryValidation,
		},
		{
			name:     "forbidden error",
			err:      apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test", fmt.Errorf("forbidden")),
			expected: ErrorCategoryPermanent,
		},
		{
			name:     "unauthorized error",
			err:      apierrors.NewUnauthorized("unauthorized"),
			expected: ErrorCategoryPermanent,
		},
		{
			name:     "network timeout error",
			err:      &net.OpError{Op: "dial", Err: &timeoutError{}},
			expected: ErrorCategoryNetwork,
		},
		{
			name:     "connection refused error",
			err:      fmt.Errorf("connection refused"),
			expected: ErrorCategoryNetwork,
		},
		{
			name:     "validation error in message",
			err:      fmt.Errorf("validation failed for field"),
			expected: ErrorCategoryValidation,
		},
		{
			name:     "unknown error",
			err:      fmt.Errorf("unknown error"),
			expected: ErrorCategoryPermanent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category := handler.CategorizeError(tt.err)
			if category != tt.expected {
				t.Errorf("CategorizeError() = %v, want %v", category, tt.expected)
			}
		})
	}
}

func TestErrorHandler_ShouldRetry(t *testing.T) {
	config := &RetryConfig{
		MaxRetries: 3,
		RetryableErrors: []ErrorCategory{
			ErrorCategoryTransient,
			ErrorCategoryNetwork,
			ErrorCategoryConflict,
		},
	}
	logger := logf.Log.WithName("test")
	handler := NewErrorHandler(config, logger)

	tests := []struct {
		name     string
		err      error
		attempt  int
		expected bool
	}{
		{
			name:     "transient error, first attempt",
			err:      apierrors.NewServiceUnavailable("service unavailable"),
			attempt:  0,
			expected: true,
		},
		{
			name:     "transient error, max attempts reached",
			err:      apierrors.NewServiceUnavailable("service unavailable"),
			attempt:  3,
			expected: false,
		},
		{
			name:     "permanent error",
			err:      apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test", fmt.Errorf("forbidden")),
			attempt:  0,
			expected: false,
		},
		{
			name:     "network error",
			err:      &net.OpError{Op: "dial", Err: &timeoutError{}},
			attempt:  1,
			expected: true,
		},
		{
			name:     "validation error",
			err:      apierrors.NewBadRequest("bad request"),
			attempt:  0,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldRetry := handler.ShouldRetry(tt.err, tt.attempt)
			if shouldRetry != tt.expected {
				t.Errorf("ShouldRetry() = %v, want %v", shouldRetry, tt.expected)
			}
		})
	}
}

func TestErrorHandler_CalculateDelay(t *testing.T) {
	config := &RetryConfig{
		BaseDelay:     1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        false, // Disable jitter for predictable testing
	}
	logger := logf.Log.WithName("test")
	handler := NewErrorHandler(config, logger)

	tests := []struct {
		name     string
		attempt  int
		expected time.Duration
	}{
		{
			name:     "first attempt",
			attempt:  0,
			expected: 1 * time.Second,
		},
		{
			name:     "second attempt",
			attempt:  1,
			expected: 1 * time.Second,
		},
		{
			name:     "third attempt",
			attempt:  2,
			expected: 2 * time.Second,
		},
		{
			name:     "fourth attempt",
			attempt:  3,
			expected: 4 * time.Second,
		},
		{
			name:     "high attempt (should cap at max)",
			attempt:  10,
			expected: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delay := handler.CalculateDelay(tt.attempt)
			if delay != tt.expected {
				t.Errorf("CalculateDelay() = %v, want %v", delay, tt.expected)
			}
		})
	}
}

func TestErrorHandler_RetryWithBackoff(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:    2,
		BaseDelay:     10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
		RetryableErrors: []ErrorCategory{
			ErrorCategoryTransient,
		},
	}
	logger := logf.Log.WithName("test")
	handler := NewErrorHandler(config, logger)

	t.Run("success on first attempt", func(t *testing.T) {
		attempts := 0
		operation := func() error {
			attempts++
			return nil
		}

		err := handler.RetryWithBackoff(context.Background(), operation, "test-operation")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if attempts != 1 {
			t.Errorf("Expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("success after retries", func(t *testing.T) {
		attempts := 0
		operation := func() error {
			attempts++
			if attempts < 3 {
				return apierrors.NewServiceUnavailable("service unavailable")
			}
			return nil
		}

		err := handler.RetryWithBackoff(context.Background(), operation, "test-operation")
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
		if attempts != 3 {
			t.Errorf("Expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("failure after max retries", func(t *testing.T) {
		attempts := 0
		operation := func() error {
			attempts++
			return apierrors.NewServiceUnavailable("service unavailable")
		}

		err := handler.RetryWithBackoff(context.Background(), operation, "test-operation")
		if err == nil {
			t.Error("Expected error, got nil")
		}
		if attempts != 3 { // MaxRetries + 1
			t.Errorf("Expected 3 attempts, got %d", attempts)
		}
	})

	t.Run("permanent error no retry", func(t *testing.T) {
		attempts := 0
		operation := func() error {
			attempts++
			return apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "test", fmt.Errorf("forbidden"))
		}

		err := handler.RetryWithBackoff(context.Background(), operation, "test-operation")
		if err == nil {
			t.Error("Expected error, got nil")
		}
		if attempts != 1 {
			t.Errorf("Expected 1 attempt, got %d", attempts)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		attempts := 0
		operation := func() error {
			attempts++
			if attempts == 2 {
				cancel() // Cancel context on second attempt
			}
			return apierrors.NewServiceUnavailable("service unavailable")
		}

		err := handler.RetryWithBackoff(ctx, operation, "test-operation")
		if err == nil {
			t.Error("Expected error, got nil")
		}
		if !strings.Contains(fmt.Sprintf("%v", err), "cancelled") {
			t.Errorf("Expected cancellation error, got %v", err)
		}
	})
}

func TestErrorHandler_GetErrorSummary(t *testing.T) {
	logger := logf.Log.WithName("test")
	handler := NewErrorHandler(DefaultRetryConfig(), logger)

	t.Run("nil error", func(t *testing.T) {
		summary := handler.GetErrorSummary(nil)
		if summary["hasError"].(bool) {
			t.Error("Expected hasError to be false for nil error")
		}
	})

	t.Run("regular error", func(t *testing.T) {
		err := fmt.Errorf("test error")
		summary := handler.GetErrorSummary(err)

		if !summary["hasError"].(bool) {
			t.Error("Expected hasError to be true")
		}
		if summary["error"].(string) != "test error" {
			t.Errorf("Expected error message 'test error', got %v", summary["error"])
		}
		if summary["category"].(string) != "permanent" {
			t.Errorf("Expected category 'permanent', got %v", summary["category"])
		}
		if summary["retryable"].(bool) {
			t.Error("Expected retryable to be false for permanent error")
		}
	})

	t.Run("transient error", func(t *testing.T) {
		err := apierrors.NewServiceUnavailable("service unavailable")
		summary := handler.GetErrorSummary(err)

		if summary["category"].(string) != "transient" {
			t.Errorf("Expected category 'transient', got %v", summary["category"])
		}
		if !summary["retryable"].(bool) {
			t.Error("Expected retryable to be true for transient error")
		}
	})
}

// timeoutError implements net.Error for testing
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
