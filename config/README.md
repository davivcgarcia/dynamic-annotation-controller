# Dynamic Annotation Controller - Deployment Configuration

This directory contains Kubernetes manifests and Kustomize configurations for deploying the Dynamic Annotation Controller.

## Quick Start

### Prerequisites

- Kubernetes cluster (v1.20+)
- kubectl configured to access your cluster
- kustomize (v3.8+) or kubectl with built-in kustomize support

### Basic Deployment

1. **Install CRDs and Controller:**
   ```bash
   # Deploy the controller with default configuration
   kubectl apply -k config/default
   ```

2. **Verify Installation:**
   ```bash
   # Check controller deployment
   kubectl get deployment -n dynamic-annotation-controller-system
   
   # Check CRD installation
   kubectl get crd annotationschedulerules.scheduler.sandbox.davcgar.aws
   ```

3. **Create Sample Rules:**
   ```bash
   # Apply sample annotation schedule rules
   kubectl apply -k config/samples
   ```

## Configuration Structure

```
config/
├── crd/                    # Custom Resource Definitions
├── default/                # Default deployment configuration
├── manager/                # Controller manager deployment
├── namespace/              # Namespace configuration
├── rbac/                   # Role-based access control
├── samples/                # Example AnnotationScheduleRule manifests
├── security/               # Security configuration
├── prometheus/             # Monitoring configuration (optional)
└── network-policy/         # Network policies (optional)
```

## Customization

### Resource Limits

Edit `config/manager/manager.yaml` to adjust resource limits:

```yaml
resources:
  limits:
    cpu: 1000m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

### Security Configuration

The controller includes security features configured via ConfigMaps:

```bash
# View security configuration
kubectl get configmap annotation-scheduler-security-config -n dynamic-annotation-controller-system
```

### RBAC Permissions

The controller requires the following permissions:
- **Pods, Deployments, StatefulSets, DaemonSets**: get, list, watch, patch, update
- **AnnotationScheduleRules**: full CRUD operations
- **Events**: create, patch (for audit logging)
- **ConfigMaps**: get, list, watch (for security configuration)
- **Leases**: full access (for leader election)

## Sample Configurations

### Basic DateTime Rule

```yaml
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: maintenance-window
spec:
  schedule:
    type: datetime
    startTime: "2025-01-20T09:00:00Z"
    endTime: "2025-01-20T17:00:00Z"
  selector:
    matchLabels:
      app: web
  annotations:
    maintenance.example.com/window: "active"
  targetResources:
    - pods
    - deployments
```

### Basic Cron Rule

```yaml
apiVersion: scheduler.sandbox.davcgar.aws/v1
kind: AnnotationScheduleRule
metadata:
  name: weekly-backup
spec:
  schedule:
    type: cron
    cronExpression: "0 2 * * 1"  # Every Monday at 2 AM
    action: apply
  selector:
    matchLabels:
      environment: production
  annotations:
    backup.example.com/enabled: "true"
  targetResources:
    - statefulsets
```

## Monitoring

### Health Checks

The controller exposes health endpoints:
- **Liveness**: `http://localhost:8081/healthz`
- **Readiness**: `http://localhost:8081/readyz`

### Metrics

Metrics are available at `http://localhost:8080/metrics` (when enabled).

To enable Prometheus monitoring:
```bash
kubectl apply -k config/prometheus
```

## Troubleshooting

### Check Controller Logs

```bash
kubectl logs -f deployment/dynamic-annotation-controller-controller-manager \
  -n dynamic-annotation-controller-system
```

### Check Rule Status

```bash
kubectl get annotationschedulerules -A
kubectl describe annotationschedulerule <rule-name>
```

### Common Issues

1. **RBAC Permissions**: Ensure the controller has proper permissions
2. **CRD Installation**: Verify CRDs are installed before creating rules
3. **Resource Selection**: Check that label selectors match target resources
4. **Schedule Format**: Validate datetime and cron expression formats

## Security Considerations

- The controller runs with restricted Pod Security Standards
- Annotation keys are validated to prevent system annotation modification
- All annotation operations are logged for audit purposes
- ConfigMaps control security policies and restrictions

## Uninstallation

```bash
# Remove sample rules
kubectl delete -k config/samples

# Remove controller and CRDs
kubectl delete -k config/default
```

**Note**: Removing the controller will not automatically remove annotations that were applied by rules. Clean up annotations manually if needed.