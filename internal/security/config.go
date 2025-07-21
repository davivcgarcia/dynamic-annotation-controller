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
	"context"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SecurityConfig holds the security configuration loaded from ConfigMaps
type SecurityConfig struct {
	AllowedPrefixes      []string
	DeniedPrefixes       []string
	RequiredPrefix       string
	MaxKeyLength         int
	MaxValueLength       int
	AuditEnabled         bool
	AuditLevel           string
	CreateSecurityEvents bool
}

// SecurityConfigLoader loads security configuration from Kubernetes ConfigMaps
type SecurityConfigLoader struct {
	client.Client
	Namespace string
}

// NewSecurityConfigLoader creates a new SecurityConfigLoader
func NewSecurityConfigLoader(k8sClient client.Client, namespace string) *SecurityConfigLoader {
	return &SecurityConfigLoader{
		Client:    k8sClient,
		Namespace: namespace,
	}
}

// LoadSecurityConfig loads security configuration from ConfigMaps
func (scl *SecurityConfigLoader) LoadSecurityConfig(ctx context.Context) (*SecurityConfig, error) {
	// Load main security configuration
	securityConfigMap := &corev1.ConfigMap{}
	err := scl.Get(ctx, types.NamespacedName{
		Name:      "annotation-scheduler-security-config",
		Namespace: scl.Namespace,
	}, securityConfigMap)
	if err != nil {
		return nil, fmt.Errorf("failed to load security configuration: %w", err)
	}

	// Load audit configuration
	auditConfigMap := &corev1.ConfigMap{}
	err = scl.Get(ctx, types.NamespacedName{
		Name:      "annotation-scheduler-audit-config",
		Namespace: scl.Namespace,
	}, auditConfigMap)
	if err != nil {
		return nil, fmt.Errorf("failed to load audit configuration: %w", err)
	}

	config := &SecurityConfig{}

	// Parse allowed prefixes
	if allowedPrefixesStr, exists := securityConfigMap.Data["allowed-prefixes"]; exists {
		config.AllowedPrefixes = parseStringList(allowedPrefixesStr)
	}

	// Parse denied prefixes
	if deniedPrefixesStr, exists := securityConfigMap.Data["denied-prefixes"]; exists {
		config.DeniedPrefixes = parseStringList(deniedPrefixesStr)
	}

	// Parse required prefix
	if requiredPrefix, exists := securityConfigMap.Data["required-prefix"]; exists {
		config.RequiredPrefix = strings.TrimSpace(requiredPrefix)
	}

	// Parse max key length
	config.MaxKeyLength = 253 // default
	if maxKeyLengthStr, exists := securityConfigMap.Data["max-key-length"]; exists {
		if maxKeyLength, err := strconv.Atoi(strings.TrimSpace(maxKeyLengthStr)); err == nil {
			config.MaxKeyLength = maxKeyLength
		}
	}

	// Parse max value length
	config.MaxValueLength = 65536 // default
	if maxValueLengthStr, exists := securityConfigMap.Data["max-value-length"]; exists {
		if maxValueLength, err := strconv.Atoi(strings.TrimSpace(maxValueLengthStr)); err == nil {
			config.MaxValueLength = maxValueLength
		}
	}

	// Parse audit settings
	config.AuditEnabled = parseBool(securityConfigMap.Data["audit-enabled"], true)
	config.AuditLevel = parseString(securityConfigMap.Data["audit-level"], "info")
	config.CreateSecurityEvents = parseBool(securityConfigMap.Data["create-security-events"], true)

	return config, nil
}

// CreateValidatorFromConfig creates an AnnotationValidator from the loaded configuration
func (scl *SecurityConfigLoader) CreateValidatorFromConfig(ctx context.Context) (*AnnotationValidator, error) {
	config, err := scl.LoadSecurityConfig(ctx)
	if err != nil {
		return nil, err
	}

	return &AnnotationValidator{
		AllowedPrefixes: config.AllowedPrefixes,
		DeniedPrefixes:  config.DeniedPrefixes,
		RequiredPrefix:  config.RequiredPrefix,
		MaxKeyLength:    config.MaxKeyLength,
		MaxValueLength:  config.MaxValueLength,
	}, nil
}

// WatchConfigChanges sets up a watch for configuration changes
func (scl *SecurityConfigLoader) WatchConfigChanges(ctx context.Context, callback func(*SecurityConfig)) error {
	// This would typically use a controller-runtime watch
	// For now, we'll implement a simple polling mechanism
	// In production, you'd want to use a proper watch mechanism

	// TODO: Implement proper ConfigMap watching using controller-runtime
	// This would involve setting up a controller that watches the ConfigMaps
	// and calls the callback when changes are detected

	return nil
}

// parseStringList parses a multi-line string into a slice of strings
func parseStringList(input string) []string {
	if input == "" {
		return nil
	}

	lines := strings.Split(input, "\n")
	var result []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			result = append(result, line)
		}
	}

	return result
}

// parseBool parses a string to bool with a default value
func parseBool(input string, defaultValue bool) bool {
	if input == "" {
		return defaultValue
	}

	input = strings.ToLower(strings.TrimSpace(input))
	switch input {
	case "true", "yes", "1", "on", "enabled":
		return true
	case "false", "no", "0", "off", "disabled":
		return false
	default:
		return defaultValue
	}
}

// parseString parses a string with a default value
func parseString(input string, defaultValue string) string {
	if input == "" {
		return defaultValue
	}
	return strings.TrimSpace(input)
}

// ValidateConfiguration validates the loaded security configuration
func (config *SecurityConfig) ValidateConfiguration() error {
	// Check for conflicting prefixes
	for _, allowedPrefix := range config.AllowedPrefixes {
		for _, deniedPrefix := range config.DeniedPrefixes {
			if strings.HasPrefix(allowedPrefix, deniedPrefix) || strings.HasPrefix(deniedPrefix, allowedPrefix) {
				return fmt.Errorf("conflicting prefix configuration: allowed '%s' conflicts with denied '%s'", allowedPrefix, deniedPrefix)
			}
		}
	}

	// Validate required prefix against denied prefixes
	if config.RequiredPrefix != "" {
		for _, deniedPrefix := range config.DeniedPrefixes {
			if strings.HasPrefix(config.RequiredPrefix, deniedPrefix) {
				return fmt.Errorf("required prefix '%s' conflicts with denied prefix '%s'", config.RequiredPrefix, deniedPrefix)
			}
		}
	}

	// Validate length limits
	if config.MaxKeyLength <= 0 || config.MaxKeyLength > 253 {
		return fmt.Errorf("invalid max key length: %d (must be 1-253)", config.MaxKeyLength)
	}

	if config.MaxValueLength <= 0 || config.MaxValueLength > 1048576 { // 1MB limit
		return fmt.Errorf("invalid max value length: %d (must be 1-1048576)", config.MaxValueLength)
	}

	// Validate audit level
	validAuditLevels := []string{"debug", "info", "warn", "error"}
	validLevel := false
	for _, level := range validAuditLevels {
		if config.AuditLevel == level {
			validLevel = true
			break
		}
	}
	if !validLevel {
		return fmt.Errorf("invalid audit level '%s' (must be one of: %s)", config.AuditLevel, strings.Join(validAuditLevels, ", "))
	}

	return nil
}
