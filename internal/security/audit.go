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
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AuditLogger provides audit logging capabilities for annotation operations
type AuditLogger struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// NewAuditLogger creates a new AuditLogger instance
func NewAuditLogger(client client.Client, log logr.Logger, scheme *runtime.Scheme) *AuditLogger {
	return &AuditLogger{
		Client: client,
		Log:    log.WithName("audit"),
		Scheme: scheme,
	}
}

// AuditEvent represents an audit event for annotation operations
type AuditEvent struct {
	Timestamp     time.Time         `json:"timestamp"`
	Action        string            `json:"action"` // "apply", "remove", "validate"
	RuleID        string            `json:"ruleId"`
	RuleName      string            `json:"ruleName"`
	RuleNamespace string            `json:"ruleNamespace"`
	Resource      ResourceInfo      `json:"resource"`
	Annotations   map[string]string `json:"annotations"`
	Success       bool              `json:"success"`
	Error         string            `json:"error,omitempty"`
	User          string            `json:"user,omitempty"`
}

// ResourceInfo contains information about the target resource
type ResourceInfo struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
}

// LogAnnotationApply logs an annotation application event
func (al *AuditLogger) LogAnnotationApply(ctx context.Context, ruleID, ruleName, ruleNamespace string, resource client.Object, annotations map[string]string, success bool, err error) {
	event := AuditEvent{
		Timestamp:     time.Now(),
		Action:        "apply",
		RuleID:        ruleID,
		RuleName:      ruleName,
		RuleNamespace: ruleNamespace,
		Resource:      al.getResourceInfo(resource),
		Annotations:   annotations,
		Success:       success,
	}

	if err != nil {
		event.Error = err.Error()
	}

	al.logEvent(ctx, event)
}

// LogAnnotationRemove logs an annotation removal event
func (al *AuditLogger) LogAnnotationRemove(ctx context.Context, ruleID, ruleName, ruleNamespace string, resource client.Object, annotationKeys []string, success bool, err error) {
	// Convert keys to map for consistent logging format
	annotations := make(map[string]string)
	for _, key := range annotationKeys {
		annotations[key] = "<removed>"
	}

	event := AuditEvent{
		Timestamp:     time.Now(),
		Action:        "remove",
		RuleID:        ruleID,
		RuleName:      ruleName,
		RuleNamespace: ruleNamespace,
		Resource:      al.getResourceInfo(resource),
		Annotations:   annotations,
		Success:       success,
	}

	if err != nil {
		event.Error = err.Error()
	}

	al.logEvent(ctx, event)
}

// LogValidationFailure logs an annotation validation failure
func (al *AuditLogger) LogValidationFailure(ctx context.Context, ruleID, ruleName, ruleNamespace string, annotations map[string]string, err error) {
	event := AuditEvent{
		Timestamp:     time.Now(),
		Action:        "validate",
		RuleID:        ruleID,
		RuleName:      ruleName,
		RuleNamespace: ruleNamespace,
		Annotations:   annotations,
		Success:       false,
		Error:         err.Error(),
	}

	al.logEvent(ctx, event)
}

// LogSecurityViolation logs a security policy violation
func (al *AuditLogger) LogSecurityViolation(ctx context.Context, ruleID, ruleName, ruleNamespace string, resource client.Object, violation string, annotations map[string]string) {
	event := AuditEvent{
		Timestamp:     time.Now(),
		Action:        "security_violation",
		RuleID:        ruleID,
		RuleName:      ruleName,
		RuleNamespace: ruleNamespace,
		Resource:      al.getResourceInfo(resource),
		Annotations:   annotations,
		Success:       false,
		Error:         violation,
	}

	al.logEvent(ctx, event)
	al.createSecurityEvent(ctx, event)
}

// logEvent logs the audit event to structured logs
func (al *AuditLogger) logEvent(ctx context.Context, event AuditEvent) {
	logLevel := al.Log.V(0) // Info level for successful operations
	if !event.Success {
		logLevel = al.Log.V(0) // Keep at info level but with error context
	}

	logLevel.Info("Annotation operation audit",
		"timestamp", event.Timestamp.Format(time.RFC3339),
		"action", event.Action,
		"ruleId", event.RuleID,
		"ruleName", event.RuleName,
		"ruleNamespace", event.RuleNamespace,
		"resource.kind", event.Resource.Kind,
		"resource.name", event.Resource.Name,
		"resource.namespace", event.Resource.Namespace,
		"resource.uid", event.Resource.UID,
		"annotations", event.Annotations,
		"success", event.Success,
		"error", event.Error,
	)

	// Also log to the controller's main log for visibility
	if event.Success {
		al.Log.Info("Annotation operation completed",
			"action", event.Action,
			"rule", fmt.Sprintf("%s/%s", event.RuleNamespace, event.RuleName),
			"resource", fmt.Sprintf("%s/%s", event.Resource.Namespace, event.Resource.Name),
			"annotationCount", len(event.Annotations))
	} else {
		al.Log.Error(fmt.Errorf("%s", event.Error), "Annotation operation failed",
			"action", event.Action,
			"rule", fmt.Sprintf("%s/%s", event.RuleNamespace, event.RuleName),
			"resource", fmt.Sprintf("%s/%s", event.Resource.Namespace, event.Resource.Name))
	}
}

// createSecurityEvent creates a Kubernetes Event for security violations
func (al *AuditLogger) createSecurityEvent(ctx context.Context, event AuditEvent) {
	// Create an event in the same namespace as the rule
	eventNamespace := event.RuleNamespace
	if eventNamespace == "" {
		eventNamespace = "default"
	}

	kubernetesEvent := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("security-violation-%d", time.Now().Unix()),
			Namespace: eventNamespace,
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:      event.Resource.Kind,
			Name:      event.Resource.Name,
			Namespace: event.Resource.Namespace,
			UID:       types.UID(event.Resource.UID),
		},
		Reason:  "SecurityViolation",
		Message: fmt.Sprintf("Security policy violation in annotation rule %s: %s", event.RuleName, event.Error),
		Source: corev1.EventSource{
			Component: "dynamic-annotation-controller",
		},
		FirstTimestamp: metav1.NewTime(event.Timestamp),
		LastTimestamp:  metav1.NewTime(event.Timestamp),
		Count:          1,
		Type:           corev1.EventTypeWarning,
	}

	if err := al.Create(ctx, kubernetesEvent); err != nil {
		al.Log.Error(err, "Failed to create security violation event",
			"rule", event.RuleName,
			"violation", event.Error)
	} else {
		al.Log.Info("Created security violation event",
			"rule", event.RuleName,
			"event", kubernetesEvent.Name)
	}
}

// getResourceInfo extracts resource information for audit logging
func (al *AuditLogger) getResourceInfo(resource client.Object) ResourceInfo {
	gvk := resource.GetObjectKind().GroupVersionKind()

	return ResourceInfo{
		Kind:      gvk.Kind,
		Name:      resource.GetName(),
		Namespace: resource.GetNamespace(),
		UID:       string(resource.GetUID()),
	}
}

// LogRuleCreation logs the creation of a new annotation rule
func (al *AuditLogger) LogRuleCreation(ctx context.Context, ruleID, ruleName, ruleNamespace string, annotations map[string]string) {
	event := AuditEvent{
		Timestamp:     time.Now(),
		Action:        "rule_created",
		RuleID:        ruleID,
		RuleName:      ruleName,
		RuleNamespace: ruleNamespace,
		Annotations:   annotations,
		Success:       true,
	}

	al.logEvent(ctx, event)
}

// LogRuleDeletion logs the deletion of an annotation rule
func (al *AuditLogger) LogRuleDeletion(ctx context.Context, ruleID, ruleName, ruleNamespace string, annotations map[string]string) {
	event := AuditEvent{
		Timestamp:     time.Now(),
		Action:        "rule_deleted",
		RuleID:        ruleID,
		RuleName:      ruleName,
		RuleNamespace: ruleNamespace,
		Annotations:   annotations,
		Success:       true,
	}

	al.logEvent(ctx, event)
}

// GetAuditSummary returns a summary of recent audit events (for monitoring/debugging)
func (al *AuditLogger) GetAuditSummary(ctx context.Context, namespace string, since time.Duration) (map[string]int, error) {
	// This is a simplified implementation - in production you might want to store
	// audit events in a database or use a more sophisticated logging system

	summary := map[string]int{
		"total_operations":    0,
		"successful_applies":  0,
		"successful_removes":  0,
		"failed_operations":   0,
		"security_violations": 0,
		"validation_failures": 0,
	}

	// For now, return empty summary as we're using structured logging
	// In a production system, you would query your audit log storage here

	return summary, nil
}
