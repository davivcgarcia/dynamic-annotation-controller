# dynamic-annotation-controller

> This project was developed as a personal experiment using vibe coding on Kiro. It's not intended for production use and is primarily a learning exercise.

A Kubernetes controller that dynamically manages annotations on resources based on configurable rules and schedules. This controller enables automated annotation management for infrastructure automation, policy enforcement, and resource lifecycle management.

## Description

The Dynamic Annotation Controller provides a declarative way to apply and remove annotations on Kubernetes resources based on time-based schedules. It supports both datetime-based schedules (for specific time windows) and cron-based schedules (for recurring operations). This is particularly useful for:

- **Maintenance Windows**: Automatically apply maintenance annotations during scheduled maintenance periods
- **Resource Lifecycle Management**: Tag resources based on their lifecycle stage or operational status  
- **Policy Enforcement**: Apply compliance or governance annotations based on schedules
- **Cost Management**: Tag resources for billing or cost allocation during specific periods
- **Operational Automation**: Coordinate with other tools by applying status annotations at scheduled times

The controller watches for `AnnotationScheduleRule` custom resources and executes the defined annotation operations on matching Kubernetes resources according to the specified schedule.

## Features

- **Multiple Schedule Types**: Support for datetime-based and cron-based schedules
- **Flexible Resource Targeting**: Target pods, deployments, statefulsets, and daemonsets using label selectors
- **Template Support**: Dynamic annotation values using Go templates with custom variables
- **Annotation Groups**: Atomic operations for complex annotation management
- **Conflict Resolution**: Configurable strategies for handling annotation conflicts
- **Status Tracking**: Comprehensive status reporting with execution history and metrics
- **Resource Efficiency**: Optimized controller with minimal resource footprint

## Getting Started

### Prerequisites
- go version v1.24.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=docker.io/davivcgarcia/dynamic-annotation-controller:latest
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=docker.io/davivcgarcia/dynamic-annotation-controller:latest
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

## Examples

### DateTime-Based Schedule Examples

#### Basic Maintenance Window
Apply maintenance annotations during a specific time window:

```yaml
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: maintenance-window
  namespace: production
spec:
  schedule:
    type: datetime
    startTime: "2025-02-15T02:00:00Z"
    endTime: "2025-02-15T06:00:00Z"
  selector:
    matchLabels:
      environment: production
      tier: backend
  annotations:
    maintenance.company.com/window: "active"
    maintenance.company.com/contact: "sre-team@company.com"
    maintenance.company.com/reason: "security-patches"
  targetResources:
    - deployments
    - statefulsets
```

#### Deployment Freeze Period
Mark deployments as frozen during a release freeze:

```yaml
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: deployment-freeze
  namespace: default
spec:
  schedule:
    type: datetime
    startTime: "2025-12-20T00:00:00Z"
    endTime: "2026-01-05T00:00:00Z"
  selector:
    matchLabels:
      app.kubernetes.io/component: web
  annotations:
    deployment.company.com/freeze: "holiday-freeze"
    deployment.company.com/approved-changes: "emergency-only"
    deployment.company.com/freeze-end: "2026-01-05"
  targetResources:
    - deployments
```

### Cron-Based Schedule Examples

#### Daily Backup Tagging
Tag resources for daily backup operations:

```yaml
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: daily-backup-tag
  namespace: data
spec:
  schedule:
    type: cron
    cronExpression: "0 2 * * *"  # Daily at 2 AM
    action: apply
  selector:
    matchLabels:
      backup: "required"
  annotations:
    backup.company.com/scheduled: "true"
    backup.company.com/last-scheduled: "{{ .Timestamp }}"
    backup.company.com/retention: "30d"
  targetResources:
    - pods
    - statefulsets
  templateVariables:
    Timestamp: "{{ now.Format \"2006-01-02T15:04:05Z\" }}"
```

#### Weekly Compliance Check
Apply compliance annotations weekly:

```yaml
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: weekly-compliance-check
  namespace: compliance
spec:
  schedule:
    type: cron
    cronExpression: "0 9 * * 1"  # Every Monday at 9 AM
    action: apply
  selector:
    matchLabels:
      compliance.required: "true"
  annotations:
    compliance.company.com/check-due: "true"
    compliance.company.com/check-date: "{{ .CheckDate }}"
    compliance.company.com/auditor: "compliance-team"
  targetResources:
    - deployments
    - daemonsets
  templateVariables:
    CheckDate: "{{ now.Format \"2006-01-02\" }}"
```

#### Business Hours Tagging
Apply business hours annotations during work days:

```yaml
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: business-hours-start
  namespace: default
spec:
  schedule:
    type: cron
    cronExpression: "0 9 * * 1-5"  # Weekdays at 9 AM
    action: apply
  selector:
    matchLabels:
      scaling: "business-hours"
  annotations:
    scaling.company.com/mode: "business-hours"
    scaling.company.com/min-replicas: "3"
    scaling.company.com/max-replicas: "10"
  targetResources:
    - deployments
---
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: business-hours-end
  namespace: default
spec:
  schedule:
    type: cron
    cronExpression: "0 18 * * 1-5"  # Weekdays at 6 PM
    action: apply
  selector:
    matchLabels:
      scaling: "business-hours"
  annotations:
    scaling.company.com/mode: "off-hours"
    scaling.company.com/min-replicas: "1"
    scaling.company.com/max-replicas: "3"
  targetResources:
    - deployments
```

### Advanced Examples with Annotation Groups

#### Complex Maintenance Window with Multiple Operations
Use annotation groups for atomic operations:

```yaml
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: complex-maintenance
  namespace: production
spec:
  schedule:
    type: datetime
    startTime: "2025-03-01T03:00:00Z"
    endTime: "2025-03-01T05:00:00Z"
  selector:
    matchLabels:
      tier: database
  annotationGroups:
    - name: maintenance-start
      atomic: true
      operations:
        - key: maintenance.company.com/status
          value: "in-progress"
          action: apply
          priority: 100
        - key: maintenance.company.com/start-time
          value: "{{ .StartTime }}"
          template: true
          action: apply
        - key: scaling.company.com/enabled
          action: remove
    - name: backup-preparation
      operations:
        - key: backup.company.com/pre-maintenance
          value: "required"
          action: apply
        - key: backup.company.com/priority
          value: "high"
          action: apply
  targetResources:
    - statefulsets
  templateVariables:
    StartTime: "{{ now.Format \"2006-01-02T15:04:05Z\" }}"
  conflictResolution: priority
```

#### Conditional Resource Tagging
Apply different annotations based on resource characteristics:

```yaml
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: cost-optimization-tagging
  namespace: default
spec:
  schedule:
    type: cron
    cronExpression: "0 0 1 * *"  # First day of every month
    action: apply
  selector:
    matchLabels:
      cost-optimization: "enabled"
  annotationGroups:
    - name: cost-analysis
      operations:
        - key: cost.company.com/analysis-date
          value: "{{ .AnalysisDate }}"
          template: true
          action: apply
        - key: cost.company.com/optimization-candidate
          value: "true"
          action: apply
        - key: cost.company.com/review-required
          value: "monthly"
          action: apply
  targetResources:
    - deployments
    - statefulsets
  templateVariables:
    AnalysisDate: "{{ now.Format \"2006-01-02\" }}"
```

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=docker.io/davivcgarcia/dynamic-annotation-controller:latest
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/dynamic-annotation-controller/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## API Reference

### AnnotationScheduleRule

The `AnnotationScheduleRule` is the primary custom resource for defining annotation schedules.

#### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schedule` | `ScheduleConfig` | Yes | Defines when and how annotations should be applied |
| `selector` | `metav1.LabelSelector` | Yes | Specifies which resources to target using label selectors |
| `annotations` | `map[string]string` | No | Simple key-value pairs to apply (legacy field) |
| `annotationGroups` | `[]AnnotationGroup` | No | Advanced annotation operations with grouping |
| `targetResources` | `[]string` | Yes | Resource types to target (`pods`, `deployments`, `statefulsets`, `daemonsets`) |
| `templateVariables` | `map[string]string` | No | Variables available for template substitution |
| `conflictResolution` | `string` | No | Strategy for handling conflicts (`priority`, `timestamp`, `skip`) |

#### Schedule Types

**DateTime Schedule:**
```yaml
schedule:
  type: datetime
  startTime: "2025-02-15T02:00:00Z"
  endTime: "2025-02-15T06:00:00Z"
```

**Cron Schedule:**
```yaml
schedule:
  type: cron
  cronExpression: "0 2 * * *"  # Standard cron format
  action: apply  # or "remove"
```

#### Status Fields

The controller maintains comprehensive status information:

| Field | Type | Description |
|-------|------|-------------|
| `phase` | `string` | Current phase (`pending`, `active`, `completed`, `failed`) |
| `lastExecutionTime` | `metav1.Time` | When the rule was last executed |
| `nextExecutionTime` | `metav1.Time` | When the rule will execute next |
| `affectedResources` | `int32` | Number of resources affected by this rule |
| `executionCount` | `int64` | Total number of executions |
| `successfulExecutions` | `int64` | Number of successful executions |
| `failedExecutions` | `int64` | Number of failed executions |
| `conditions` | `[]metav1.Condition` | Detailed condition information |

### Template Variables

The controller supports Go template syntax in annotation values when `template: true` is set:

```yaml
annotations:
  example.com/timestamp: "{{ .Timestamp }}"
  example.com/resource-name: "{{ .ResourceName }}"
  example.com/namespace: "{{ .ResourceNamespace }}"
```

Built-in template functions include:
- `now`: Current timestamp
- `env`: Environment variables
- Standard Go template functions

## Monitoring and Observability

The controller exposes Prometheus metrics for monitoring:

- `annotation_schedule_rule_executions_total`: Total number of rule executions
- `annotation_schedule_rule_execution_duration_seconds`: Execution duration histogram
- `annotation_schedule_rule_affected_resources`: Number of resources affected per rule
- `annotation_schedule_rule_errors_total`: Total number of execution errors

Health checks are available at:
- `/healthz`: Liveness probe
- `/readyz`: Readiness probe

## Troubleshooting

### Common Issues

#### Controller Not Starting
```bash
# Check controller logs
kubectl logs -f deployment/dynamic-annotation-controller-controller-manager \
  -n dynamic-annotation-controller-system

# Check RBAC permissions
kubectl auth can-i get pods --as=system:serviceaccount:dynamic-annotation-controller-system:dynamic-annotation-controller-manager
```

#### Rules Not Executing
```bash
# Check rule status
kubectl get annotationschedulerules -A
kubectl describe annotationschedulerule <rule-name>

# Verify resource selection
kubectl get pods -l <your-label-selector>
```

#### Schedule Format Issues
- **DateTime**: Use RFC3339 format (`2025-01-20T10:00:00Z`)
- **Cron**: Use standard 5-field format (`minute hour day month weekday`)
- **Timezone**: All times are in UTC

#### Permission Errors
The controller needs these permissions:
- Read/write access to target resources (pods, deployments, etc.)
- Full access to AnnotationScheduleRule CRDs
- ConfigMap read access for security configuration
- Event creation for audit logging

### FAQ

**Q: Can I use custom annotation keys?**
A: Yes, but avoid system annotations (kubernetes.io/*, k8s.io/*) as they're restricted for security.

**Q: How do I handle timezone-specific schedules?**
A: Convert your local times to UTC for datetime schedules. For cron schedules, consider the controller's timezone (UTC by default).

**Q: Can I target resources across all namespaces?**
A: Yes, use cluster-wide deployment and appropriate RBAC permissions. The controller can target resources in any namespace it has access to.

**Q: What happens if the controller restarts during execution?**
A: The controller uses leader election and will resume operations. In-progress operations may be retried.

**Q: How do I remove annotations applied by a rule?**
A: For datetime rules, annotations are automatically removed at endTime. For cron rules, create a separate rule with `action: remove`.

**Q: Can I use the same annotation key in multiple rules?**
A: Yes, use the `conflictResolution` field to control behavior when multiple rules target the same annotation.

## Contributing

While I appreciate your interest, I'm not actively seeking external contributions as this is just an experimental project to explore Kubernetes controller patterns and annotation management concepts.

### Code of Conduct

Please be respectful and inclusive in all interactions. We follow the CNCF Code of Conduct.

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

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

