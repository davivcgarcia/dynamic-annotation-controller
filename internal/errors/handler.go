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
	"math"
	"net"
	"strings"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ErrorCategory represents different types of errors for retry strategies
type ErrorCategory int

const (
	// ErrorCategoryTransient represents temporary errors that should be retried
	ErrorCategoryTransient ErrorCategory = iota
	// ErrorCategoryPermanent represents permanent errors that should not be retried
	ErrorCategoryPermanent
	// ErrorCategoryResourceNotFound represents missing resource errors
	ErrorCategoryResourceNotFound
	// ErrorCategoryValidation represents validation errors
	ErrorCategoryValidation
	// ErrorCategoryNetwork represents network-related errors
	ErrorCategoryNetwork
	// ErrorCategoryRateLimit represents rate limiting errors
	ErrorCategoryRateLimit
	// ErrorCategoryConflict represents resource conflict errors
	ErrorCategoryConflict
)

// RetryConfig defines retry behavior for different error categories
type RetryConfig struct {
	MaxRetries      int
	BaseDelay       time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	Jitter          bool
	RetryableErrors []ErrorCategory
}

// DefaultRetryConfig returns a sensible default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:    5,
		BaseDelay:     1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
		RetryableErrors: []ErrorCategory{
			ErrorCategoryTransient,
			ErrorCategoryNetwork,
			ErrorCategoryRateLimit,
			ErrorCategoryConflict,
		},
	}
}

// ErrorHandler provides error categorization and retry logic
type ErrorHandler struct {
	config *RetryConfig
	logger logr.Logger
}

// NewErrorHandler creates a new error handler with the given configuration
func NewErrorHandler(config *RetryConfig, logger logr.Logger) *ErrorHandler {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &ErrorHandler{
		config: config,
		logger: logger,
	}
}

// CategorizeError determines the category of an error for retry decisions
func (eh *ErrorHandler) CategorizeError(err error) ErrorCategory {
	if err == nil {
		return ErrorCategoryPermanent // Should not happen, but safe default
	}

	// Check for Kubernetes API errors first
	if apierrors.IsNotFound(err) {
		return ErrorCategoryResourceNotFound
	}

	if apierrors.IsConflict(err) {
		return ErrorCategoryConflict
	}

	if apierrors.IsServerTimeout(err) || apierrors.IsTimeout(err) {
		return ErrorCategoryTransient
	}

	if apierrors.IsServiceUnavailable(err) || apierrors.IsInternalError(err) {
		return ErrorCategoryTransient
	}

	if apierrors.IsTooManyRequests(err) {
		return ErrorCategoryRateLimit
	}

	if apierrors.IsBadRequest(err) || apierrors.IsInvalid(err) {
		return ErrorCategoryValidation
	}

	if apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		return ErrorCategoryPermanent
	}

	// Check for network errors
	if eh.isNetworkError(err) {
		return ErrorCategoryNetwork
	}

	// Check for validation-related errors in error message
	errorMsg := strings.ToLower(err.Error())
	if strings.Contains(errorMsg, "validation") ||
		strings.Contains(errorMsg, "invalid") ||
		strings.Contains(errorMsg, "malformed") {
		return ErrorCategoryValidation
	}

	// Check for transient errors in error message
	if strings.Contains(errorMsg, "connection refused") ||
		strings.Contains(errorMsg, "connection reset") ||
		strings.Contains(errorMsg, "timeout") ||
		strings.Contains(errorMsg, "temporary") {
		return ErrorCategoryTransient
	}

	// Default to permanent for unknown errors
	return ErrorCategoryPermanent
}

// isNetworkError checks if an error is network-related
func (eh *ErrorHandler) isNetworkError(err error) bool {
	if netErr, ok := err.(net.Error); ok {
		return netErr.Timeout() || netErr.Temporary()
	}

	// Check for common network error patterns
	errorMsg := strings.ToLower(err.Error())
	networkPatterns := []string{
		"connection refused",
		"connection reset",
		"network is unreachable",
		"no route to host",
		"dns",
		"dial tcp",
		"i/o timeout",
	}

	for _, pattern := range networkPatterns {
		if strings.Contains(errorMsg, pattern) {
			return true
		}
	}

	return false
}

// ShouldRetry determines if an error should be retried based on its category
func (eh *ErrorHandler) ShouldRetry(err error, attempt int) bool {
	if attempt >= eh.config.MaxRetries {
		return false
	}

	category := eh.CategorizeError(err)

	for _, retryableCategory := range eh.config.RetryableErrors {
		if category == retryableCategory {
			return true
		}
	}

	return false
}

// CalculateDelay calculates the delay before the next retry attempt
func (eh *ErrorHandler) CalculateDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return eh.config.BaseDelay
	}

	// Calculate exponential backoff
	delay := float64(eh.config.BaseDelay) * math.Pow(eh.config.BackoffFactor, float64(attempt-1))

	// Apply maximum delay limit
	if delay > float64(eh.config.MaxDelay) {
		delay = float64(eh.config.MaxDelay)
	}

	duration := time.Duration(delay)

	// Add jitter if enabled
	if eh.config.Jitter {
		jitterFactor := float64(2*time.Now().UnixNano()%2 - 1)
		jitter := time.Duration(float64(duration) * 0.1 * jitterFactor)
		duration += jitter
		if duration < 0 {
			duration = eh.config.BaseDelay
		}
	}

	return duration
}

// RetryWithBackoff executes a function with exponential backoff retry logic
func (eh *ErrorHandler) RetryWithBackoff(ctx context.Context, operation func() error, operationName string) error {
	var lastErr error

	for attempt := 0; attempt <= eh.config.MaxRetries; attempt++ {
		// Execute the operation
		err := operation()
		if err == nil {
			if attempt > 0 {
				eh.logger.Info("Operation succeeded after retry",
					"operation", operationName,
					"attempt", attempt+1,
					"totalAttempts", attempt+1)
			}
			return nil
		}

		lastErr = err
		category := eh.CategorizeError(err)

		eh.logger.V(1).Info("Operation failed",
			"operation", operationName,
			"attempt", attempt+1,
			"error", err.Error(),
			"category", eh.categoryToString(category))

		// Check if we should retry
		if !eh.ShouldRetry(err, attempt) {
			eh.logger.Info("Operation failed, not retrying",
				"operation", operationName,
				"attempt", attempt+1,
				"error", err.Error(),
				"category", eh.categoryToString(category),
				"reason", eh.getNoRetryReason(err, attempt))
			break
		}

		// Don't wait after the last attempt
		if attempt == eh.config.MaxRetries {
			break
		}

		// Calculate delay and wait
		delay := eh.CalculateDelay(attempt + 1)
		eh.logger.V(1).Info("Retrying operation after delay",
			"operation", operationName,
			"attempt", attempt+1,
			"nextAttempt", attempt+2,
			"delay", delay,
			"category", eh.categoryToString(category))

		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled during retry: %w", ctx.Err())
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("operation %s failed after %d attempts: %w", operationName, eh.config.MaxRetries+1, lastErr)
}

// RetryWithBackoffAndCondition executes a function with retry logic and a custom retry condition
func (eh *ErrorHandler) RetryWithBackoffAndCondition(ctx context.Context, operation func() error, shouldRetry func(error, int) bool, operationName string) error {
	var lastErr error

	for attempt := 0; attempt <= eh.config.MaxRetries; attempt++ {
		// Execute the operation
		err := operation()
		if err == nil {
			if attempt > 0 {
				eh.logger.Info("Operation succeeded after retry",
					"operation", operationName,
					"attempt", attempt+1)
			}
			return nil
		}

		lastErr = err
		category := eh.CategorizeError(err)

		eh.logger.V(1).Info("Operation failed",
			"operation", operationName,
			"attempt", attempt+1,
			"error", err.Error(),
			"category", eh.categoryToString(category))

		// Check custom retry condition
		if !shouldRetry(err, attempt) {
			eh.logger.Info("Operation failed, custom condition says not to retry",
				"operation", operationName,
				"attempt", attempt+1,
				"error", err.Error())
			break
		}

		// Don't wait after the last attempt
		if attempt == eh.config.MaxRetries {
			break
		}

		// Calculate delay and wait
		delay := eh.CalculateDelay(attempt + 1)
		eh.logger.V(1).Info("Retrying operation after delay",
			"operation", operationName,
			"attempt", attempt+1,
			"delay", delay)

		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled during retry: %w", ctx.Err())
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("operation %s failed after %d attempts: %w", operationName, eh.config.MaxRetries+1, lastErr)
}

// WaitForCondition waits for a condition to be met with timeout and retry logic
func (eh *ErrorHandler) WaitForCondition(ctx context.Context, condition func() (bool, error), timeout time.Duration, operationName string) error {
	return wait.PollImmediate(eh.config.BaseDelay, timeout, func() (bool, error) {
		done, err := condition()
		if err != nil {
			category := eh.CategorizeError(err)
			eh.logger.V(1).Info("Condition check failed",
				"operation", operationName,
				"error", err.Error(),
				"category", eh.categoryToString(category))

			// For transient errors, continue polling
			if category == ErrorCategoryTransient || category == ErrorCategoryNetwork {
				return false, nil
			}
			// For permanent errors, stop polling
			return false, err
		}
		return done, nil
	})
}

// getNoRetryReason returns a human-readable reason why an error is not being retried
func (eh *ErrorHandler) getNoRetryReason(err error, attempt int) string {
	if attempt >= eh.config.MaxRetries {
		return "maximum retry attempts reached"
	}

	category := eh.CategorizeError(err)
	switch category {
	case ErrorCategoryPermanent:
		return "permanent error"
	case ErrorCategoryValidation:
		return "validation error"
	case ErrorCategoryResourceNotFound:
		return "resource not found"
	default:
		return "error category not configured for retry"
	}
}

// categoryToString converts an error category to a string for logging
func (eh *ErrorHandler) categoryToString(category ErrorCategory) string {
	switch category {
	case ErrorCategoryTransient:
		return "transient"
	case ErrorCategoryPermanent:
		return "permanent"
	case ErrorCategoryResourceNotFound:
		return "resource-not-found"
	case ErrorCategoryValidation:
		return "validation"
	case ErrorCategoryNetwork:
		return "network"
	case ErrorCategoryRateLimit:
		return "rate-limit"
	case ErrorCategoryConflict:
		return "conflict"
	default:
		return "unknown"
	}
}

// GetErrorSummary returns a summary of error information for logging/monitoring
func (eh *ErrorHandler) GetErrorSummary(err error) map[string]interface{} {
	if err == nil {
		return map[string]interface{}{
			"hasError": false,
		}
	}

	category := eh.CategorizeError(err)

	summary := map[string]interface{}{
		"hasError":  true,
		"error":     err.Error(),
		"category":  eh.categoryToString(category),
		"retryable": eh.ShouldRetry(err, 0),
	}

	// Add Kubernetes API error details if applicable
	if statusErr, ok := err.(apierrors.APIStatus); ok {
		if status := statusErr.Status(); status.Code != 0 {
			summary["apiErrorCode"] = status.Code
			summary["apiErrorReason"] = string(status.Reason)
		}
	}

	return summary
}
