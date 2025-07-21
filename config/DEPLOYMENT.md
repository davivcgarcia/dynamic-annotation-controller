# Dynamic Annotation Controller - Deployment Guide

This guide provides comprehensive instructions for deploying the Dynamic Annotation Controller in various environments.

## Quick Start

### Prerequisites
- Kubernetes cluster v1.20+
- kubectl configured with cluster access
- kustomize v3.8+ (or kubectl with built-in kustomize)

### Basic Installation

```bash
# 1. Install CRDs and controller
kubectl apply -k config/default

# 2. Verify installation
kubectl get deployment -n dynamic-annotation-controller-system
kubectl get crd annotationschedulerules.scheduler.sandbox.davcgar.aws

# 3. Deploy sample rules (optional)
kubectl apply -k config/samples
```

## Environment-Specific Deployments

### Development Environment

```bash
# Deploy with development settings (reduced resources, debug logging)
make deploy-dev

# Or using kustomize directly
kubectl apply -k config/development
```

**Development Features:**
- Reduced resource requirements (50m CPU, 64Mi memory)
- Debug logging enabled
- Single replica
- Separate namespace: `dynamic-annotation-controller-dev`

### Production Environment

```bash
# Deploy with production settings (HA, increased resources)
make deploy-prod

# Or using kustomize directly
kubectl apply -k config/production
```

**Production Features:**
- High availability (2 replicas)
- Increased resources (200m-2000m CPU, 256Mi-512Mi memory)
- Enhanced leader election settings
- Production labels and annotations

## Configuration Options

### Resource Limits

| Environment | CPU Request | CPU Limit | Memory Request | Memory Limit |
|-------------|-------------|-----------|----------------|--------------|
| Development | 50m         | 500m      | 64Mi           | 128Mi        |
| Default     | 100m        | 1000m     | 128Mi          | 256Mi        |
| Production  | 200m        | 2000m     | 256Mi          | 512Mi        |

### Security Configuration

The controller includes several security features:

1. **Pod Security Standards**: Restricted profile enforced
2. **RBAC**: Minimal required permissions
3. **Security Context**: Non-root, read-only filesystem
4. **Annotation Validation**: Prevents system annotation modification

### RBAC Roles

| Role | Purpose | Permissions |
|------|---------|-------------|
| `manager-role` | Controller operations | Core resource access for annotation management |
| `security-admin` | Security configuration | Full access to security configs and audit logs |
| `operator` | Standard operations | Standard rule management and monitoring |
| `viewer` | Read-only access | View rules and status information |

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

## Makefile Targets

| Target | Description |
|--------|-------------|
| `make deploy` | Deploy to default environment |
| `make deploy-dev` | Deploy to development environment |
| `make deploy-prod` | Deploy to production environment |
| `make deploy-samples` | Deploy sample rules |
| `make undeploy` | Remove controller from cluster |
| `make undeploy-samples` | Remove sample rules |

## Monitoring and Health Checks

### Health Endpoints
- **Liveness**: `http://localhost:8081/healthz`
- **Readiness**: `http://localhost:8081/readyz`

### Metrics
- **Endpoint**: `http://localhost:8080/metrics`
- **Enable Prometheus**: `kubectl apply -k config/prometheus`

### Logging
```bash
# View controller logs
kubectl logs -f deployment/dynamic-annotation-controller-controller-manager \
  -n dynamic-annotation-controller-system

# View rule status
kubectl get annotationschedulerules -A
kubectl describe annotationschedulerule <rule-name>
```

## Troubleshooting

### Common Issues

1. **CRD Not Found**
   ```bash
   # Ensure CRDs are installed
   kubectl get crd | grep annotationschedulerule
   ```

2. **RBAC Permissions**
   ```bash
   # Check service account permissions
   kubectl auth can-i get pods --as=system:serviceaccount:dynamic-annotation-controller-system:controller-manager
   ```

3. **Resource Selection**
   ```bash
   # Test label selectors
   kubectl get pods -l app=web
   ```

4. **Schedule Format**
   - Datetime: ISO 8601 format (`2025-01-20T09:00:00Z`)
   - Cron: Standard cron format (`0 2 * * 1`)

### Debug Commands

```bash
# Check controller status
kubectl get deployment -n dynamic-annotation-controller-system

# View events
kubectl get events -n dynamic-annotation-controller-system

# Check rule status
kubectl get annotationschedulerules -o wide

# Describe specific rule
kubectl describe annotationschedulerule <rule-name>
```

## Uninstallation

```bash
# Remove sample rules
kubectl delete -k config/samples

# Remove controller and CRDs
kubectl delete -k config/default
```

**Note**: Removing the controller does not automatically clean up annotations that were applied by rules. Manual cleanup may be required.

## Security Considerations

- Controller runs with minimal required permissions
- Pod Security Standards enforced (restricted profile)
- Annotation key validation prevents system annotation modification
- All operations are logged for audit purposes
- ConfigMaps control security policies and restrictions