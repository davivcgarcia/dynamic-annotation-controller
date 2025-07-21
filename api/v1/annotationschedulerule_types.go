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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ScheduleConfig defines the scheduling configuration for annotation operations
type ScheduleConfig struct {
	// Type specifies the schedule type: "datetime" or "cron"
	// +kubebuilder:validation:Enum=datetime;cron
	// +kubebuilder:validation:Required
	Type string `json:"type"`

	// StartTime specifies when to apply annotations (for datetime type)
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// EndTime specifies when to remove annotations (for datetime type)
	// +optional
	EndTime *metav1.Time `json:"endTime,omitempty"`

	// CronExpression specifies the cron schedule (for cron type)
	// Supports standard cron format: minute hour day month weekday$`
	// +optional
	CronExpression string `json:"cronExpression,omitempty"`

	// Action specifies the action for cron schedules: "apply" or "remove"
	// +kubebuilder:validation:Enum=apply;remove
	// +optional
	Action string `json:"action,omitempty"`
}

// AnnotationOperation defines a single annotation operation with templating support
type AnnotationOperation struct {
	// Key is the annotation key to apply or remove
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`

	// Value is the annotation value (supports templating)
	// +optional
	Value string `json:"value,omitempty"`

	// Template indicates if the value should be processed for variable substitution
	// +optional
	Template bool `json:"template,omitempty"`

	// Action specifies whether to apply or remove this annotation
	// +kubebuilder:validation:Enum=apply;remove
	// +optional
	Action string `json:"action,omitempty"`

	// Priority defines the precedence order for conflicting annotations (higher wins)
	// +optional
	Priority int32 `json:"priority,omitempty"`
}

// AnnotationGroup defines a group of annotation operations that should be applied atomically
type AnnotationGroup struct {
	// Name is a descriptive name for this annotation group
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Operations defines the list of annotation operations in this group
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Operations []AnnotationOperation `json:"operations"`

	// Atomic indicates if all operations in this group should succeed or fail together
	// +optional
	Atomic bool `json:"atomic,omitempty"`
}

// AnnotationScheduleRuleSpec defines the desired state of AnnotationScheduleRule
type AnnotationScheduleRuleSpec struct {
	// Schedule defines when and how annotations should be applied or removed
	// +kubebuilder:validation:Required
	Schedule ScheduleConfig `json:"schedule"`

	// Selector specifies which resources to target using label selectors
	// +kubebuilder:validation:Required
	Selector metav1.LabelSelector `json:"selector"`

	// Annotations specifies the key-value pairs to apply or remove (legacy field)
	// +optional
	// +kubebuilder:validation:MinProperties=1
	Annotations map[string]string `json:"annotations,omitempty"`

	// AnnotationGroups defines groups of annotation operations with advanced features
	// +optional
	AnnotationGroups []AnnotationGroup `json:"annotationGroups,omitempty"`

	// TargetResources specifies which resource types to target
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Enum=pods;deployments;statefulsets;daemonsets
	TargetResources []string `json:"targetResources"`

	// TemplateVariables defines variables available for template substitution
	// +optional
	TemplateVariables map[string]string `json:"templateVariables,omitempty"`

	// ConflictResolution defines how to handle annotation conflicts between rules
	// +kubebuilder:validation:Enum=priority;timestamp;skip
	// +optional
	ConflictResolution string `json:"conflictResolution,omitempty"`
}

// AnnotationScheduleRuleStatus defines the observed state of AnnotationScheduleRule
type AnnotationScheduleRuleStatus struct {
	// LastExecutionTime records when the rule was last executed
	// +optional
	LastExecutionTime *metav1.Time `json:"lastExecutionTime,omitempty"`

	// NextExecutionTime records when the rule will be executed next
	// +optional
	NextExecutionTime *metav1.Time `json:"nextExecutionTime,omitempty"`

	// Phase represents the current phase of the rule
	// +kubebuilder:validation:Enum=pending;active;completed;failed
	// +optional
	Phase string `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the rule's state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// AffectedResources tracks the number of resources affected by this rule
	// +optional
	AffectedResources int32 `json:"affectedResources,omitempty"`

	// ExecutionCount tracks the total number of executions
	// +optional
	ExecutionCount int64 `json:"executionCount,omitempty"`

	// SuccessfulExecutions tracks the number of successful executions
	// +optional
	SuccessfulExecutions int64 `json:"successfulExecutions,omitempty"`

	// FailedExecutions tracks the number of failed executions
	// +optional
	FailedExecutions int64 `json:"failedExecutions,omitempty"`

	// LastError contains the last error message if execution failed
	// +optional
	LastError string `json:"lastError,omitempty"`

	// LastErrorTime records when the last error occurred
	// +optional
	LastErrorTime *metav1.Time `json:"lastErrorTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=asr
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Last Execution",type="date",JSONPath=".status.lastExecutionTime"
// +kubebuilder:printcolumn:name="Next Execution",type="date",JSONPath=".status.nextExecutionTime"
// +kubebuilder:printcolumn:name="Affected Resources",type="integer",JSONPath=".status.affectedResources"
// +kubebuilder:printcolumn:name="Success Rate",type="string",JSONPath=".status.successfulExecutions"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AnnotationScheduleRule is the Schema for the annotationschedulerules API
type AnnotationScheduleRule struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of AnnotationScheduleRule
	// +required
	Spec AnnotationScheduleRuleSpec `json:"spec"`

	// status defines the observed state of AnnotationScheduleRule
	// +optional
	Status AnnotationScheduleRuleStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// AnnotationScheduleRuleList contains a list of AnnotationScheduleRule
type AnnotationScheduleRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AnnotationScheduleRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AnnotationScheduleRule{}, &AnnotationScheduleRuleList{})
}
