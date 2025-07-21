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
	"fmt"
	"regexp"
	"strings"
)

// AnnotationValidator provides security validation for annotation operations
type AnnotationValidator struct {
	// AllowedPrefixes defines annotation prefixes that are allowed to be modified
	AllowedPrefixes []string
	// DeniedPrefixes defines annotation prefixes that are explicitly denied
	DeniedPrefixes []string
	// RequiredPrefix defines a prefix that all managed annotations must have
	RequiredPrefix string
	// MaxKeyLength defines the maximum allowed length for annotation keys
	MaxKeyLength int
	// MaxValueLength defines the maximum allowed length for annotation values
	MaxValueLength int
}

// NewAnnotationValidator creates a new AnnotationValidator with secure defaults
func NewAnnotationValidator() *AnnotationValidator {
	return &AnnotationValidator{
		AllowedPrefixes: []string{
			"app.kubernetes.io/",
			"scheduler.k8s.io/",
			"example.com/",
		},
		DeniedPrefixes: []string{
			"kubernetes.io/",
			"k8s.io/",
			"kubectl.kubernetes.io/",
			"node.kubernetes.io/",
			"volume.kubernetes.io/",
			"service.kubernetes.io/",
			"endpoints.kubernetes.io/",
			"endpointslice.kubernetes.io/",
			"networking.kubernetes.io/",
			"policy/",
			"security.kubernetes.io/",
			"admission.kubernetes.io/",
			"authorization.kubernetes.io/",
			"authentication.kubernetes.io/",
			"rbac.authorization.kubernetes.io/",
			"apiserver.kubernetes.io/",
			"controller.kubernetes.io/",
			"scheduler.kubernetes.io/",
			"kubelet.kubernetes.io/",
			"kube-proxy.kubernetes.io/",
			"cloud.kubernetes.io/",
			"cluster-autoscaler.kubernetes.io/",
			"node-restriction.kubernetes.io/",
		},
		RequiredPrefix: "scheduler.k8s.io/managed-",
		MaxKeyLength:   253,
		MaxValueLength: 65536,
	}
}

// ValidateAnnotationKey validates an annotation key according to security policies
func (av *AnnotationValidator) ValidateAnnotationKey(key string) error {
	if key == "" {
		return fmt.Errorf("annotation key cannot be empty")
	}

	// Check key length
	if len(key) > av.MaxKeyLength {
		return fmt.Errorf("annotation key too long (max %d characters): %s", av.MaxKeyLength, key)
	}

	// Check for denied prefixes first (highest priority)
	for _, deniedPrefix := range av.DeniedPrefixes {
		if strings.HasPrefix(key, deniedPrefix) {
			return fmt.Errorf("annotation key uses forbidden system prefix '%s': %s", deniedPrefix, key)
		}
	}

	// If required prefix is set, enforce it
	if av.RequiredPrefix != "" && !strings.HasPrefix(key, av.RequiredPrefix) {
		return fmt.Errorf("annotation key must start with required prefix '%s': %s", av.RequiredPrefix, key)
	}

	// Check if key matches allowed prefixes (if any are defined)
	if len(av.AllowedPrefixes) > 0 {
		allowed := false
		for _, allowedPrefix := range av.AllowedPrefixes {
			if strings.HasPrefix(key, allowedPrefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("annotation key does not match any allowed prefix: %s", key)
		}
	}

	// Validate key format (DNS subdomain format)
	if err := av.validateDNSSubdomain(key); err != nil {
		return fmt.Errorf("annotation key format invalid: %w", err)
	}

	return nil
}

// ValidateAnnotationValue validates an annotation value according to security policies
func (av *AnnotationValidator) ValidateAnnotationValue(value string) error {
	// Check value length
	if len(value) > av.MaxValueLength {
		return fmt.Errorf("annotation value too long (max %d characters)", av.MaxValueLength)
	}

	// Check for potentially dangerous content
	if err := av.validateValueContent(value); err != nil {
		return fmt.Errorf("annotation value contains forbidden content: %w", err)
	}

	return nil
}

// ValidateAnnotations validates a map of annotations
func (av *AnnotationValidator) ValidateAnnotations(annotations map[string]string) error {
	for key, value := range annotations {
		if err := av.ValidateAnnotationKey(key); err != nil {
			return fmt.Errorf("invalid annotation key '%s': %w", key, err)
		}
		if err := av.ValidateAnnotationValue(value); err != nil {
			return fmt.Errorf("invalid annotation value for key '%s': %w", key, err)
		}
	}
	return nil
}

// validateDNSSubdomain validates that a key follows DNS subdomain naming rules
func (av *AnnotationValidator) validateDNSSubdomain(key string) error {
	// Split key into prefix and name parts
	parts := strings.SplitN(key, "/", 2)

	var prefix, name string
	if len(parts) == 2 {
		prefix = parts[0]
		name = parts[1]
	} else {
		name = parts[0]
	}

	// Validate prefix if present
	if prefix != "" {
		if err := av.validateDNSName(prefix); err != nil {
			return fmt.Errorf("invalid prefix '%s': %w", prefix, err)
		}
	}

	// Validate name part
	if err := av.validateDNSLabel(name); err != nil {
		return fmt.Errorf("invalid name '%s': %w", name, err)
	}

	return nil
}

// validateDNSName validates a DNS name (for prefixes)
func (av *AnnotationValidator) validateDNSName(name string) error {
	if len(name) == 0 || len(name) > 253 {
		return fmt.Errorf("DNS name must be 1-253 characters")
	}

	// DNS name regex: lowercase letters, numbers, dots, and hyphens
	dnsNameRegex := regexp.MustCompile(`^[a-z0-9]([a-z0-9\-\.]*[a-z0-9])?$`)
	if !dnsNameRegex.MatchString(name) {
		return fmt.Errorf("DNS name contains invalid characters")
	}

	// Check for consecutive dots
	if strings.Contains(name, "..") {
		return fmt.Errorf("DNS name cannot contain consecutive dots")
	}

	return nil
}

// validateDNSLabel validates a DNS label (for name parts)
func (av *AnnotationValidator) validateDNSLabel(label string) error {
	if len(label) == 0 || len(label) > 63 {
		return fmt.Errorf("DNS label must be 1-63 characters")
	}

	// DNS label regex: lowercase letters, numbers, and hyphens
	dnsLabelRegex := regexp.MustCompile(`^[a-z0-9]([a-z0-9\-]*[a-z0-9])?$`)
	if !dnsLabelRegex.MatchString(label) {
		return fmt.Errorf("DNS label contains invalid characters")
	}

	return nil
}

// validateValueContent checks for potentially dangerous content in annotation values
func (av *AnnotationValidator) validateValueContent(value string) error {
	// Check for script injection patterns
	dangerousPatterns := []string{
		"<script",
		"javascript:",
		"data:text/html",
		"vbscript:",
		"onload=",
		"onerror=",
		"onclick=",
	}

	lowerValue := strings.ToLower(value)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lowerValue, pattern) {
			return fmt.Errorf("value contains potentially dangerous pattern: %s", pattern)
		}
	}

	// Check for control characters (except common whitespace)
	for _, char := range value {
		if char < 32 && char != 9 && char != 10 && char != 13 {
			return fmt.Errorf("value contains control character (code %d)", int(char))
		}
	}

	return nil
}

// IsSystemAnnotation checks if an annotation key is a system annotation
func (av *AnnotationValidator) IsSystemAnnotation(key string) bool {
	for _, deniedPrefix := range av.DeniedPrefixes {
		if strings.HasPrefix(key, deniedPrefix) {
			return true
		}
	}
	return false
}

// GetManagedAnnotationKey converts a user-provided key to a managed key with required prefix
func (av *AnnotationValidator) GetManagedAnnotationKey(userKey string) string {
	if av.RequiredPrefix == "" {
		return userKey
	}

	// If key already has the required prefix, return as-is
	if strings.HasPrefix(userKey, av.RequiredPrefix) {
		return userKey
	}

	// Add the required prefix
	return av.RequiredPrefix + userKey
}

// StripManagedPrefix removes the managed prefix from an annotation key
func (av *AnnotationValidator) StripManagedPrefix(managedKey string) string {
	if av.RequiredPrefix == "" {
		return managedKey
	}

	if strings.HasPrefix(managedKey, av.RequiredPrefix) {
		return strings.TrimPrefix(managedKey, av.RequiredPrefix)
	}

	return managedKey
}
