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
	"fmt"
	"regexp"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TemplateEngine handles variable substitution in annotation values
type TemplateEngine struct {
	// Built-in variables that are always available
	builtinVars map[string]string
}

// TemplateContext provides context for template processing
type TemplateContext struct {
	// Rule-specific variables
	Variables map[string]string

	// Resource context
	Resource client.Object

	// Rule metadata
	RuleName      string
	RuleNamespace string
	RuleID        string

	// Execution context
	ExecutionTime time.Time
	Action        string
}

// NewTemplateEngine creates a new template engine
func NewTemplateEngine() *TemplateEngine {
	return &TemplateEngine{
		builtinVars: make(map[string]string),
	}
}

// ProcessTemplate processes a template string with variable substitution
func (te *TemplateEngine) ProcessTemplate(template string, context TemplateContext) (string, error) {
	if template == "" {
		return "", nil
	}

	// Build the complete variable map
	variables := te.buildVariableMap(context)

	// Find all template variables in the format ${variable} or {{variable}}
	result := template

	// Process ${variable} format
	dollarRegex := regexp.MustCompile(`\$\{([^}]+)\}`)
	result = dollarRegex.ReplaceAllStringFunc(result, func(match string) string {
		varName := dollarRegex.FindStringSubmatch(match)[1]
		if value, exists := variables[varName]; exists {
			return value
		}
		// Return original if variable not found
		return match
	})

	// Process {{variable}} format
	braceRegex := regexp.MustCompile(`\{\{([^}]+)\}\}`)
	result = braceRegex.ReplaceAllStringFunc(result, func(match string) string {
		varName := strings.TrimSpace(braceRegex.FindStringSubmatch(match)[1])
		if value, exists := variables[varName]; exists {
			return value
		}
		// Return original if variable not found
		return match
	})

	return result, nil
}

// ValidateTemplate validates that a template string is well-formed
func (te *TemplateEngine) ValidateTemplate(template string) error {
	if template == "" {
		return nil
	}

	// Check for balanced braces
	dollarRegex := regexp.MustCompile(`\$\{[^}]*\}`)
	braceRegex := regexp.MustCompile(`\{\{[^}]*\}\}`)

	// Find unmatched opening braces
	unmatched := regexp.MustCompile(`\$\{[^}]*$|\{\{[^}]*$`)
	if unmatched.MatchString(template) {
		return fmt.Errorf("template has unmatched opening braces")
	}

	// Check for empty variable names
	emptyDollar := regexp.MustCompile(`\$\{\s*\}`)
	emptyBrace := regexp.MustCompile(`\{\{\s*\}\}`)

	if emptyDollar.MatchString(template) || emptyBrace.MatchString(template) {
		return fmt.Errorf("template contains empty variable names")
	}

	// Extract all variable names and validate them
	allMatches := append(dollarRegex.FindAllString(template, -1), braceRegex.FindAllString(template, -1)...)
	for _, match := range allMatches {
		varName := te.extractVariableName(match)
		if err := te.validateVariableName(varName); err != nil {
			return fmt.Errorf("invalid variable name '%s': %w", varName, err)
		}
	}

	return nil
}

// GetTemplateVariables extracts all variable names from a template
func (te *TemplateEngine) GetTemplateVariables(template string) []string {
	var variables []string
	seen := make(map[string]bool)

	// Find ${variable} format
	dollarRegex := regexp.MustCompile(`\$\{([^}]+)\}`)
	matches := dollarRegex.FindAllStringSubmatch(template, -1)
	for _, match := range matches {
		varName := strings.TrimSpace(match[1])
		if !seen[varName] {
			variables = append(variables, varName)
			seen[varName] = true
		}
	}

	// Find {{variable}} format
	braceRegex := regexp.MustCompile(`\{\{([^}]+)\}\}`)
	matches = braceRegex.FindAllStringSubmatch(template, -1)
	for _, match := range matches {
		varName := strings.TrimSpace(match[1])
		if !seen[varName] {
			variables = append(variables, varName)
			seen[varName] = true
		}
	}

	return variables
}

// buildVariableMap creates a complete variable map from context
func (te *TemplateEngine) buildVariableMap(context TemplateContext) map[string]string {
	variables := make(map[string]string)

	// Add built-in variables
	for k, v := range te.builtinVars {
		variables[k] = v
	}

	// Add system variables
	variables["timestamp"] = context.ExecutionTime.Format(time.RFC3339)
	variables["date"] = context.ExecutionTime.Format("2006-01-02")
	variables["time"] = context.ExecutionTime.Format("15:04:05")
	variables["unix"] = fmt.Sprintf("%d", context.ExecutionTime.Unix())
	variables["rule.name"] = context.RuleName
	variables["rule.namespace"] = context.RuleNamespace
	variables["rule.id"] = context.RuleID
	variables["action"] = context.Action

	// Add resource variables if resource is provided
	if context.Resource != nil {
		variables["resource.name"] = context.Resource.GetName()
		variables["resource.namespace"] = context.Resource.GetNamespace()
		variables["resource.uid"] = string(context.Resource.GetUID())

		// Add resource labels as variables
		for k, v := range context.Resource.GetLabels() {
			variables[fmt.Sprintf("resource.labels.%s", k)] = v
		}

		// Add resource annotations as variables (with prefix to avoid conflicts)
		for k, v := range context.Resource.GetAnnotations() {
			variables[fmt.Sprintf("resource.annotations.%s", k)] = v
		}
	}

	// Add user-defined variables (these can override system variables)
	for k, v := range context.Variables {
		variables[k] = v
	}

	return variables
}

// extractVariableName extracts the variable name from a template match
func (te *TemplateEngine) extractVariableName(match string) string {
	if strings.HasPrefix(match, "${") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}"))
	}
	if strings.HasPrefix(match, "{{") {
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
	}
	return match
}

// validateVariableName validates that a variable name is valid
func (te *TemplateEngine) validateVariableName(name string) error {
	if name == "" {
		return fmt.Errorf("variable name cannot be empty")
	}

	// Variable names should contain only alphanumeric characters, dots, underscores, and hyphens
	validName := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	if !validName.MatchString(name) {
		return fmt.Errorf("variable name contains invalid characters")
	}

	return nil
}

// SetBuiltinVariable sets a built-in variable that's always available
func (te *TemplateEngine) SetBuiltinVariable(name, value string) {
	te.builtinVars[name] = value
}

// GetBuiltinVariables returns a copy of all built-in variables
func (te *TemplateEngine) GetBuiltinVariables() map[string]string {
	result := make(map[string]string)
	for k, v := range te.builtinVars {
		result[k] = v
	}
	return result
}

// IsTemplate checks if a string contains template variables
func (te *TemplateEngine) IsTemplate(s string) bool {
	dollarRegex := regexp.MustCompile(`\$\{[^}]+\}`)
	braceRegex := regexp.MustCompile(`\{\{[^}]+\}\}`)

	return dollarRegex.MatchString(s) || braceRegex.MatchString(s)
}
