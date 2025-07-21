# Changelog

All notable changes to the Dynamic Annotation Controller project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial implementation of Dynamic Annotation Controller
- Support for datetime-based annotation schedules
- Support for cron-based annotation schedules  
- AnnotationScheduleRule Custom Resource Definition
- Controller manager with leader election
- Comprehensive test suite including unit and e2e tests
- Prometheus metrics integration
- Template support for dynamic annotation values
- Annotation groups for atomic operations
- Conflict resolution strategies
- Security configuration and validation
- RBAC permissions and Pod Security Standards compliance
- Comprehensive documentation and examples

### Features

#### Core Functionality
- **Schedule Types**: DateTime and cron-based scheduling
- **Resource Targeting**: Support for pods, deployments, statefulsets, and daemonsets
- **Label Selectors**: Flexible resource selection using Kubernetes label selectors
- **Template Variables**: Dynamic annotation values with Go template support
- **Annotation Groups**: Atomic operations for complex annotation management
- **Conflict Resolution**: Configurable strategies (priority, timestamp, skip)

#### Operational Features
- **Status Tracking**: Comprehensive status reporting with execution history
- **Metrics**: Prometheus metrics for monitoring and observability
- **Health Checks**: Liveness and readiness probes
- **Leader Election**: High availability support
- **Security**: Pod Security Standards compliance and annotation validation
- **Audit Logging**: Event logging for all annotation operations

#### Developer Experience
- **Comprehensive Examples**: Real-world use cases and configuration samples
- **Testing**: Unit tests, integration tests, and e2e test suite
- **Documentation**: Detailed API reference and deployment guides
- **CI/CD**: Automated testing and build pipelines

### Technical Details

#### API Version
- `scheduler.sandbox.davcgar.aws/v1`

#### Supported Kubernetes Versions
- Kubernetes v1.20+
- Go v1.24+

#### Container Images
- Base image: `gcr.io/distroless/static:nonroot`
- Multi-architecture support (amd64, arm64)

#### Dependencies
- controller-runtime v0.19.3
- client-go v0.33.0
- Kubebuilder v4.3.1

### Configuration

#### Default Resource Limits
```yaml
resources:
  limits:
    cpu: 1000m
    memory: 256Mi
  requests:
    cpu: 100m
    memory: 128Mi
```

#### Security Configuration
- Non-root container execution
- Read-only root filesystem
- Dropped capabilities
- Pod Security Standards: Restricted

### Examples Added

#### DateTime Schedule Examples
- Basic maintenance window
- Deployment freeze period
- Time-based resource tagging

#### Cron Schedule Examples  
- Daily backup tagging
- Weekly compliance checks
- Business hours scaling annotations

#### Advanced Examples
- Complex maintenance windows with annotation groups
- Conditional resource tagging
- Template-based dynamic values

### Testing

#### Test Coverage
- Unit tests for core controller logic
- Integration tests for API validation
- End-to-end tests with real Kubernetes clusters
- Performance and load testing

#### Test Environments
- Local development with Kind
- CI/CD with GitHub Actions
- Docker-based and local binary test modes

### Documentation

#### Added Documentation
- Comprehensive README with examples
- API reference documentation
- Deployment and configuration guides
- Troubleshooting guides
- Contributing guidelines
- Security considerations

#### Sample Configurations
- Basic datetime and cron examples
- Advanced annotation group examples
- Real-world use case demonstrations
- Template usage examples

### Known Limitations

- Template variables are evaluated at execution time only
- Maximum of 100 annotation operations per rule
- Cron expressions limited to standard 5-field format
- Resource watching limited to specified namespaces

### Migration Notes

This is the initial release, so no migration is required.

### Security Notes

- All annotation keys are validated to prevent system annotation modification
- Controller runs with minimal required permissions
- Audit logging enabled for all annotation operations
- Security configuration via ConfigMaps

---

## Release Process

### Versioning Strategy
- Major version: Breaking API changes
- Minor version: New features, backward compatible
- Patch version: Bug fixes, security updates

### Release Checklist
- [ ] Update CHANGELOG.md
- [ ] Update version in Makefile
- [ ] Run full test suite
- [ ] Update documentation
- [ ] Create release notes
- [ ] Tag release
- [ ] Build and push container images
- [ ] Update Helm charts (if applicable)

### Support Policy
- Latest major version: Full support
- Previous major version: Security updates only
- Kubernetes version support: Latest 3 minor versions