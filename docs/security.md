# Security Configuration and Controls

The Dynamic Annotation Controller includes comprehensive security controls to ensure safe annotation management in Kubernetes clusters.

## Overview

The security system consists of:
- **Annotation Validation**: Prevents modification of system annotations and enforces naming conventions
- **RBAC Configuration**: Role-based access control with multiple permission levels
- **Audit Logging**: Comprehensive logging of all annotation operations
- **Configurable Policies**: Flexible security policies via ConfigMaps

## Annotation Security

### Validation Rules

The controller validates all annotations before applying them:

1. **System Annotation Protection**: Prevents modification of critical Kubernetes system annotations
2. **Prefix Restrictions**: Configurable allowed/denied annotation prefixes
3. **Format Validation**: Ensures annotations follow DNS naming conventions
4. **Content Security**: Prevents injection of potentially dangerous content
5. **Length Limits**: Configurable maximum lengths for keys and values

### Default Security Policies

By default, the following prefixes are **denied**:
- `kubernetes.io/`
- `k8s.io/`
- `kubectl.kubernetes.io/`
- `node.kubernetes.io/`
- `volume.kubernetes.io/`
- `service.kubernetes.io/`
- `policy/`
- `security.kubernetes.io/`
- `admission.kubernetes.io/`
- `authorization.kubernetes.io/`
- `authentication.kubernetes.io/`
- `rbac.authorization.kubernetes.io/`

The following prefixes are **allowed** by default:
- `app.kubernetes.io/`
- `scheduler.k8s.io/`
- `example.com/`

### Required Prefix

All managed annotations are prefixed with `scheduler.k8s.io/managed-` to:
- Clearly identify controller-managed annotations
- Prevent conflicts with user-managed annotations
- Enable safe cleanup during rule deletion

## RBAC Configuration

### Controller Service Account

The main controller service account has permissions to:
- Read/write AnnotationScheduleRule resources
- Read/write annotations on Pods, Deployments, StatefulSets, DaemonSets
- Create audit events
- Read security configuration ConfigMaps
- Manage leader election leases

### Additional Roles

Three additional roles are provided for different access levels:

#### Security Admin (`annotation-scheduler-security-admin`)
- Full access to AnnotationScheduleRule resources
- Manage security configuration ConfigMaps
- Access audit events

#### Operator (`annotation-scheduler-operator`)
- Standard access to AnnotationScheduleRule resources
- Read-only access to security configuration
- Access audit events for monitoring

#### Viewer (`annotation-scheduler-viewer`)
- Read-only access to AnnotationScheduleRule resources
- Read-only access to security configuration
- Read-only access to audit events

### Role Binding Configuration

Role bindings are provided as templates in `config/rbac/security_role_binding.yaml`. 
Uncomment and modify the subjects section to assign roles to specific users or service accounts:

```yaml
subjects:
- kind: User
  name: security-admin@company.com
  apiGroup: rbac.authorization.k8s.io
- kind: ServiceAccount
  name: annotation-scheduler-admin
  namespace: annotation-scheduler-system
```

## Security Configuration

### ConfigMap Structure

Security policies are configured via two ConfigMaps:

#### `annotation-scheduler-security-config`
```yaml
data:
  allowed-prefixes: |
    app.kubernetes.io/
    scheduler.k8s.io/
    example.com/
    mycompany.com/
  
  denied-prefixes: |
    kubernetes.io/
    k8s.io/
    # ... (see full list above)
  
  required-prefix: "scheduler.k8s.io/managed-"
  max-key-length: "253"
  max-value-length: "65536"
  audit-enabled: "true"
  audit-level: "info"
  create-security-events: "true"
```

#### `annotation-scheduler-audit-config`
```yaml
data:
  log-format: "json"
  log-level: "info"
  event-retention-hours: "24"
  log-rule-creation: "true"
  log-rule-deletion: "true"
  log-annotation-apply: "true"
  log-annotation-remove: "true"
  log-validation-failures: "true"
  log-security-violations: "true"
  include-user-info: "true"
  include-resource-uid: "true"
  include-timestamp: "true"
```

### Updating Security Configuration

1. Edit the ConfigMaps in your cluster:
   ```bash
   kubectl edit configmap annotation-scheduler-security-config -n system
   kubectl edit configmap annotation-scheduler-audit-config -n system
   ```

2. The controller will automatically reload the configuration (may take up to 1 minute)

3. Verify the new configuration is loaded by checking the controller logs

## Audit Logging

### Audit Events

The controller logs the following events:
- **Rule Creation**: When a new AnnotationScheduleRule is created
- **Rule Deletion**: When an AnnotationScheduleRule is deleted
- **Annotation Apply**: When annotations are applied to resources
- **Annotation Remove**: When annotations are removed from resources
- **Validation Failures**: When annotation validation fails
- **Security Violations**: When security policies are violated

### Log Format

Audit logs are structured JSON with the following fields:
```json
{
  "timestamp": "2025-01-18T10:30:00Z",
  "action": "apply",
  "ruleId": "default/my-rule",
  "ruleName": "my-rule",
  "ruleNamespace": "default",
  "resource": {
    "kind": "Pod",
    "name": "my-pod",
    "namespace": "default",
    "uid": "12345678-1234-1234-1234-123456789012"
  },
  "annotations": {
    "scheduler.k8s.io/managed-status": "active"
  },
  "success": true,
  "user": "system:serviceaccount:system:controller-manager"
}
```

### Kubernetes Events

Security violations also create Kubernetes Events for visibility:
```bash
kubectl get events --field-selector reason=SecurityViolation
```

### Accessing Audit Logs

Audit logs are written to the controller's standard output and can be accessed via:

1. **kubectl logs**:
   ```bash
   kubectl logs -f deployment/dynamic-annotation-controller-controller-manager -n system
   ```

2. **Log aggregation systems** (if configured):
   - Fluentd/Fluent Bit
   - Logstash
   - Promtail/Loki

3. **Kubernetes Events**:
   ```bash
   kubectl get events --field-selector reason=SecurityViolation -A
   ```

## Security Best Practices

### 1. Principle of Least Privilege
- Use the appropriate RBAC role for each user/service account
- Regularly review and audit role assignments
- Remove unused role bindings

### 2. Configuration Management
- Store security configurations in version control
- Use GitOps for configuration updates
- Regularly review and update security policies

### 3. Monitoring and Alerting
- Monitor audit logs for security violations
- Set up alerts for failed validation attempts
- Track annotation modification patterns

### 4. Regular Security Reviews
- Periodically review allowed/denied prefixes
- Audit annotation usage across the cluster
- Review and rotate service account credentials

### 5. Network Security
- Use network policies to restrict controller access
- Enable TLS for all communications
- Regularly update container images

## Troubleshooting

### Common Security Issues

#### Validation Failures
```
Error: security validation failed: annotation key uses forbidden system prefix 'kubernetes.io/': kubernetes.io/test
```
**Solution**: Use an allowed prefix or update the security configuration

#### RBAC Errors
```
Error: failed to create event: events is forbidden: User "system:serviceaccount:system:controller-manager" cannot create resource "events"
```
**Solution**: Ensure the controller service account has the correct RBAC permissions

#### Configuration Loading Errors
```
Error: failed to load security configuration: configmap "annotation-scheduler-security-config" not found
```
**Solution**: Deploy the security configuration ConfigMaps

### Debugging Security Issues

1. **Check controller logs**:
   ```bash
   kubectl logs -f deployment/dynamic-annotation-controller-controller-manager -n system | grep -i security
   ```

2. **Verify RBAC permissions**:
   ```bash
   kubectl auth can-i create events --as=system:serviceaccount:system:controller-manager
   ```

3. **Check security configuration**:
   ```bash
   kubectl get configmap annotation-scheduler-security-config -n system -o yaml
   ```

4. **Review audit events**:
   ```bash
   kubectl get events --field-selector reason=SecurityViolation -A
   ```

## Security Compliance

The security controls help meet various compliance requirements:

- **SOC 2**: Audit logging and access controls
- **PCI DSS**: Data protection and access monitoring
- **GDPR**: Data processing logging and access controls
- **HIPAA**: Audit trails and access restrictions

For specific compliance requirements, consult with your security team to ensure proper configuration.