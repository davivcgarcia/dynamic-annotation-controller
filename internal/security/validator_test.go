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

package security

import (
	"strings"
	"testing"
)

func TestAnnotationValidator_ValidateAnnotationKey(t *testing.T) {
	// Create validator without required prefix for basic tests
	validator := &AnnotationValidator{
		AllowedPrefixes: []string{
			"app.kubernetes.io/",
			"scheduler.k8s.io/",
			"example.com/",
		},
		DeniedPrefixes: []string{
			"kubernetes.io/",
			"k8s.io/",
			"kubectl.kubernetes.io/",
		},
		RequiredPrefix: "", // No required prefix for basic tests
		MaxKeyLength:   253,
		MaxValueLength: 65536,
	}

	tests := []struct {
		name        string
		key         string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid allowed prefix",
			key:         "app.kubernetes.io/name",
			expectError: false,
		},
		{
			name:        "valid scheduler prefix",
			key:         "scheduler.k8s.io/test",
			expectError: false,
		},
		{
			name:        "denied system prefix",
			key:         "kubernetes.io/test",
			expectError: true,
			errorMsg:    "forbidden system prefix",
		},
		{
			name:        "denied k8s prefix",
			key:         "k8s.io/test",
			expectError: true,
			errorMsg:    "forbidden system prefix",
		},
		{
			name:        "empty key",
			key:         "",
			expectError: true,
			errorMsg:    "cannot be empty",
		},
		{
			name:        "key too long",
			key:         strings.Repeat("a", 254),
			expectError: true,
			errorMsg:    "too long",
		},
		{
			name:        "invalid characters",
			key:         "test/INVALID-KEY",
			expectError: true,
			errorMsg:    "allowed prefix",
		},
		{
			name:        "valid example prefix",
			key:         "example.com/test",
			expectError: false,
		},
		{
			name:        "not in allowed list",
			key:         "custom.com/test",
			expectError: true,
			errorMsg:    "allowed prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateAnnotationKey(tt.key)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error message to contain '%s', got: %s", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %s", err.Error())
				}
			}
		})
	}
}

func TestAnnotationValidator_ValidateAnnotationValue(t *testing.T) {
	validator := NewAnnotationValidator()

	tests := []struct {
		name        string
		value       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid simple value",
			value:       "test-value",
			expectError: false,
		},
		{
			name:        "valid json value",
			value:       `{"key": "value"}`,
			expectError: false,
		},
		{
			name:        "value too long",
			value:       strings.Repeat("a", 65537),
			expectError: true,
			errorMsg:    "too long",
		},
		{
			name:        "dangerous script tag",
			value:       "<script>alert('xss')</script>",
			expectError: true,
			errorMsg:    "dangerous pattern",
		},
		{
			name:        "javascript protocol",
			value:       "javascript:alert('xss')",
			expectError: true,
			errorMsg:    "dangerous pattern",
		},
		{
			name:        "control character",
			value:       "test\x00value",
			expectError: true,
			errorMsg:    "control character",
		},
		{
			name:        "valid with newlines",
			value:       "line1\nline2\r\nline3",
			expectError: false,
		},
		{
			name:        "valid with tabs",
			value:       "col1\tcol2\tcol3",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateAnnotationValue(tt.value)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error message to contain '%s', got: %s", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %s", err.Error())
				}
			}
		})
	}
}

func TestAnnotationValidator_ValidateAnnotations(t *testing.T) {
	// Create validator without required prefix for basic tests
	validator := &AnnotationValidator{
		AllowedPrefixes: []string{
			"app.kubernetes.io/",
			"scheduler.k8s.io/",
			"example.com/",
		},
		DeniedPrefixes: []string{
			"kubernetes.io/",
			"k8s.io/",
		},
		RequiredPrefix: "",
		MaxKeyLength:   253,
		MaxValueLength: 65536,
	}

	tests := []struct {
		name        string
		annotations map[string]string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid annotations",
			annotations: map[string]string{
				"app.kubernetes.io/name": "test-app",
				"scheduler.k8s.io/test":  "test-value",
			},
			expectError: false,
		},
		{
			name: "invalid key",
			annotations: map[string]string{
				"kubernetes.io/test": "value",
			},
			expectError: true,
			errorMsg:    "invalid annotation key",
		},
		{
			name: "invalid value",
			annotations: map[string]string{
				"app.kubernetes.io/name": "<script>alert('xss')</script>",
			},
			expectError: true,
			errorMsg:    "invalid annotation value",
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateAnnotations(tt.annotations)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error message to contain '%s', got: %s", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %s", err.Error())
				}
			}
		})
	}
}

func TestAnnotationValidator_IsSystemAnnotation(t *testing.T) {
	validator := NewAnnotationValidator()

	tests := []struct {
		name     string
		key      string
		expected bool
	}{
		{
			name:     "system annotation",
			key:      "kubernetes.io/test",
			expected: true,
		},
		{
			name:     "k8s system annotation",
			key:      "k8s.io/test",
			expected: true,
		},
		{
			name:     "allowed annotation",
			key:      "app.kubernetes.io/name",
			expected: false,
		},
		{
			name:     "custom annotation",
			key:      "custom.com/test",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.IsSystemAnnotation(tt.key)
			if result != tt.expected {
				t.Errorf("expected %v but got %v", tt.expected, result)
			}
		})
	}
}

func TestAnnotationValidator_GetManagedAnnotationKey(t *testing.T) {
	validator := NewAnnotationValidator()

	tests := []struct {
		name     string
		userKey  string
		expected string
	}{
		{
			name:     "add required prefix",
			userKey:  "test-key",
			expected: "scheduler.k8s.io/managed-test-key",
		},
		{
			name:     "already has prefix",
			userKey:  "scheduler.k8s.io/managed-test",
			expected: "scheduler.k8s.io/managed-test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.GetManagedAnnotationKey(tt.userKey)
			if result != tt.expected {
				t.Errorf("expected %s but got %s", tt.expected, result)
			}
		})
	}
}

func TestAnnotationValidator_StripManagedPrefix(t *testing.T) {
	validator := NewAnnotationValidator()

	tests := []struct {
		name       string
		managedKey string
		expected   string
	}{
		{
			name:       "strip managed prefix",
			managedKey: "scheduler.k8s.io/managed-test-key",
			expected:   "test-key",
		},
		{
			name:       "no prefix to strip",
			managedKey: "custom.com/test",
			expected:   "custom.com/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validator.StripManagedPrefix(tt.managedKey)
			if result != tt.expected {
				t.Errorf("expected %s but got %s", tt.expected, result)
			}
		})
	}
}

func TestAnnotationValidator_RequiredPrefix(t *testing.T) {
	// Test with required prefix (default configuration)
	validator := NewAnnotationValidator()

	tests := []struct {
		name        string
		key         string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid with required prefix",
			key:         "scheduler.k8s.io/managed-test",
			expectError: false,
		},
		{
			name:        "missing required prefix",
			key:         "app.kubernetes.io/name",
			expectError: true,
			errorMsg:    "required prefix",
		},
		{
			name:        "denied prefix overrides required",
			key:         "kubernetes.io/test",
			expectError: true,
			errorMsg:    "forbidden system prefix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateAnnotationKey(tt.key)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error message to contain '%s', got: %s", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %s", err.Error())
				}
			}
		})
	}
}

func TestAnnotationValidator_CustomConfiguration(t *testing.T) {
	// Test with custom configuration
	validator := &AnnotationValidator{
		AllowedPrefixes: []string{"custom.com/", "example.org/"},
		DeniedPrefixes:  []string{"forbidden.com/"},
		RequiredPrefix:  "", // No required prefix for this test
		MaxKeyLength:    100,
		MaxValueLength:  1000,
	}

	tests := []struct {
		name        string
		key         string
		expectError bool
	}{
		{
			name:        "allowed custom prefix",
			key:         "custom.com/test",
			expectError: false,
		},
		{
			name:        "denied custom prefix",
			key:         "forbidden.com/test",
			expectError: true,
		},
		{
			name:        "not in allowed list",
			key:         "other.com/test",
			expectError: true,
		},
		{
			name:        "exceeds custom max length",
			key:         strings.Repeat("a", 101),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateAnnotationKey(tt.key)

			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			} else if !tt.expectError && err != nil {
				t.Errorf("expected no error but got: %s", err.Error())
			}
		})
	}
}
