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

package templating

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestTemplateEngine_ProcessTemplate(t *testing.T) {
	engine := NewTemplateEngine()

	// Set up test context
	testTime := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid-123",
			Labels: map[string]string{
				"app":     "web",
				"version": "v1.0",
			},
			Annotations: map[string]string{
				"existing": "value",
			},
		},
	}

	context := TemplateContext{
		Variables: map[string]string{
			"env":     "production",
			"cluster": "us-west-2",
		},
		Resource:      pod,
		RuleName:      "test-rule",
		RuleNamespace: "scheduler",
		RuleID:        "scheduler/test-rule",
		ExecutionTime: testTime,
		Action:        "apply",
	}

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "simple variable substitution with ${} format",
			template: "Environment: ${env}",
			expected: "Environment: production",
		},
		{
			name:     "simple variable substitution with {{}} format",
			template: "Cluster: {{cluster}}",
			expected: "Cluster: us-west-2",
		},
		{
			name:     "multiple variables",
			template: "Deployed to ${env} in {{cluster}}",
			expected: "Deployed to production in us-west-2",
		},
		{
			name:     "system variables",
			template: "Rule: ${rule.name}, Action: {{action}}",
			expected: "Rule: test-rule, Action: apply",
		},
		{
			name:     "resource variables",
			template: "Pod: ${resource.name} in {{resource.namespace}}",
			expected: "Pod: test-pod in default",
		},
		{
			name:     "resource labels",
			template: "App: ${resource.labels.app}, Version: {{resource.labels.version}}",
			expected: "App: web, Version: v1.0",
		},
		{
			name:     "timestamp variables",
			template: "Executed at ${timestamp} on {{date}}",
			expected: "Executed at 2025-01-15T10:30:00Z on 2025-01-15",
		},
		{
			name:     "mixed formats",
			template: "${env}-{{cluster}}-${resource.name}",
			expected: "production-us-west-2-test-pod",
		},
		{
			name:     "no variables",
			template: "static text",
			expected: "static text",
		},
		{
			name:     "empty template",
			template: "",
			expected: "",
		},
		{
			name:     "undefined variable (should remain unchanged)",
			template: "Value: ${undefined}",
			expected: "Value: ${undefined}",
		},
		{
			name:     "complex template",
			template: "Scheduled ${action} for ${resource.name} (${resource.labels.app}:${resource.labels.version}) in ${env} at ${date}",
			expected: "Scheduled apply for test-pod (web:v1.0) in production at 2025-01-15",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.ProcessTemplate(tt.template, context)
			if err != nil {
				t.Errorf("ProcessTemplate() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("ProcessTemplate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTemplateEngine_ValidateTemplate(t *testing.T) {
	engine := NewTemplateEngine()

	tests := []struct {
		name        string
		template    string
		expectError bool
	}{
		{
			name:        "valid template with ${} format",
			template:    "Value: ${variable}",
			expectError: false,
		},
		{
			name:        "valid template with {{}} format",
			template:    "Value: {{variable}}",
			expectError: false,
		},
		{
			name:        "valid template with mixed formats",
			template:    "${var1} and {{var2}}",
			expectError: false,
		},
		{
			name:        "valid template with dots in variable name",
			template:    "${resource.name}",
			expectError: false,
		},
		{
			name:        "valid template with underscores and hyphens",
			template:    "${my_var} and {{my-var}}",
			expectError: false,
		},
		{
			name:        "empty template",
			template:    "",
			expectError: false,
		},
		{
			name:        "static text",
			template:    "no variables here",
			expectError: false,
		},
		{
			name:        "unmatched opening brace ${}",
			template:    "Value: ${variable",
			expectError: true,
		},
		{
			name:        "unmatched opening brace {{}}",
			template:    "Value: {{variable",
			expectError: true,
		},
		{
			name:        "empty variable name ${}",
			template:    "Value: ${}",
			expectError: true,
		},
		{
			name:        "empty variable name {{}}",
			template:    "Value: {{}}",
			expectError: true,
		},
		{
			name:        "whitespace only variable name",
			template:    "Value: ${ }",
			expectError: true,
		},
		{
			name:        "invalid characters in variable name",
			template:    "Value: ${var@name}",
			expectError: true,
		},
		{
			name:        "invalid characters with spaces",
			template:    "Value: ${var name}",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.ValidateTemplate(tt.template)
			if tt.expectError && err == nil {
				t.Errorf("ValidateTemplate() expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateTemplate() unexpected error = %v", err)
			}
		})
	}
}

func TestTemplateEngine_GetTemplateVariables(t *testing.T) {
	engine := NewTemplateEngine()

	tests := []struct {
		name     string
		template string
		expected []string
	}{
		{
			name:     "single variable ${} format",
			template: "Value: ${variable}",
			expected: []string{"variable"},
		},
		{
			name:     "single variable {{}} format",
			template: "Value: {{variable}}",
			expected: []string{"variable"},
		},
		{
			name:     "multiple variables",
			template: "${var1} and {{var2}} and ${var3}",
			expected: []string{"var1", "var2", "var3"},
		},
		{
			name:     "duplicate variables",
			template: "${var1} and ${var1} and {{var1}}",
			expected: []string{"var1"},
		},
		{
			name:     "variables with dots and underscores",
			template: "${resource.name} and {{my_var}}",
			expected: []string{"resource.name", "my_var"},
		},
		{
			name:     "no variables",
			template: "static text",
			expected: []string{},
		},
		{
			name:     "empty template",
			template: "",
			expected: []string{},
		},
		{
			name:     "variables with whitespace",
			template: "${ var1 } and {{ var2 }}",
			expected: []string{"var1", "var2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.GetTemplateVariables(tt.template)

			if len(result) != len(tt.expected) {
				t.Errorf("GetTemplateVariables() returned %d variables, expected %d", len(result), len(tt.expected))
				return
			}

			// Convert to map for easier comparison
			resultMap := make(map[string]bool)
			for _, v := range result {
				resultMap[v] = true
			}

			for _, expected := range tt.expected {
				if !resultMap[expected] {
					t.Errorf("GetTemplateVariables() missing expected variable: %s", expected)
				}
			}
		})
	}
}

func TestTemplateEngine_IsTemplate(t *testing.T) {
	engine := NewTemplateEngine()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "contains ${} variable",
			input:    "Value: ${variable}",
			expected: true,
		},
		{
			name:     "contains {{}} variable",
			input:    "Value: {{variable}}",
			expected: true,
		},
		{
			name:     "contains both formats",
			input:    "${var1} and {{var2}}",
			expected: true,
		},
		{
			name:     "static text",
			input:    "no variables here",
			expected: false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "contains braces but not variables",
			input:    "some {text} with $braces",
			expected: false,
		},
		{
			name:     "malformed variable (missing closing brace)",
			input:    "Value: ${variable",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.IsTemplate(tt.input)
			if result != tt.expected {
				t.Errorf("IsTemplate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTemplateEngine_BuiltinVariables(t *testing.T) {
	engine := NewTemplateEngine()

	// Set some built-in variables
	engine.SetBuiltinVariable("cluster", "test-cluster")
	engine.SetBuiltinVariable("region", "us-east-1")

	context := TemplateContext{
		Variables: map[string]string{
			"env": "test",
		},
		RuleName:      "test-rule",
		RuleNamespace: "default",
		ExecutionTime: time.Now(),
		Action:        "apply",
	}

	tests := []struct {
		name     string
		template string
		contains string
	}{
		{
			name:     "builtin variable",
			template: "Cluster: ${cluster}",
			contains: "test-cluster",
		},
		{
			name:     "user variable overrides builtin",
			template: "Region: ${region}, Env: ${env}",
			contains: "us-east-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.ProcessTemplate(tt.template, context)
			if err != nil {
				t.Errorf("ProcessTemplate() error = %v", err)
				return
			}
			if !containsString(result, tt.contains) {
				t.Errorf("ProcessTemplate() = %v, should contain %v", result, tt.contains)
			}
		})
	}

	// Test GetBuiltinVariables
	builtins := engine.GetBuiltinVariables()
	if builtins["cluster"] != "test-cluster" {
		t.Errorf("GetBuiltinVariables() cluster = %v, want test-cluster", builtins["cluster"])
	}
	if builtins["region"] != "us-east-1" {
		t.Errorf("GetBuiltinVariables() region = %v, want us-east-1", builtins["region"])
	}
}

func TestTemplateEngine_ResourceContext(t *testing.T) {
	engine := NewTemplateEngine()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "production",
			UID:       "uid-123",
			Labels: map[string]string{
				"app":     "web-server",
				"version": "v2.1",
			},
			Annotations: map[string]string{
				"deployment.id": "deploy-456",
			},
		},
	}

	context := TemplateContext{
		Resource:      pod,
		RuleName:      "test-rule",
		RuleNamespace: "scheduler",
		ExecutionTime: time.Now(),
		Action:        "apply",
	}

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "resource name",
			template: "${resource.name}",
			expected: "test-pod",
		},
		{
			name:     "resource namespace",
			template: "${resource.namespace}",
			expected: "production",
		},
		{
			name:     "resource uid",
			template: "${resource.uid}",
			expected: "uid-123",
		},
		{
			name:     "resource label",
			template: "${resource.labels.app}",
			expected: "web-server",
		},
		{
			name:     "resource annotation",
			template: "${resource.annotations.deployment.id}",
			expected: "deploy-456",
		},
		{
			name:     "complex resource template",
			template: "${resource.labels.app}-${resource.labels.version}-${resource.name}",
			expected: "web-server-v2.1-test-pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.ProcessTemplate(tt.template, context)
			if err != nil {
				t.Errorf("ProcessTemplate() error = %v", err)
				return
			}
			if result != tt.expected {
				t.Errorf("ProcessTemplate() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				containsSubstring(s, substr))))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
