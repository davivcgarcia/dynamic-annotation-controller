# End-to-End (E2E) Tests

This directory contains end-to-end tests for the Dynamic Annotation Controller.

## Prerequisites

- [Kind](https://kind.sigs.k8s.io/) installed and available in PATH
- Docker running (for full Docker-based tests)
- kubectl configured

## Running E2E Tests

### Local Binary Mode (Recommended for Development)

This mode builds the controller binary locally and tests CRD functionality without requiring Docker image builds:

```bash
USE_LOCAL_BINARY=true make test-e2e
```

This mode:
- ✅ Builds the manager binary locally
- ✅ Creates a Kind cluster
- ✅ Installs CertManager
- ✅ Installs CRDs
- ✅ Tests AnnotationScheduleRule creation and validation
- ✅ Skips controller deployment (avoiding Docker image requirements)
- ✅ Cleans up all resources

### Docker-based Mode (Full Integration)

This mode builds a Docker image and deploys the full controller:

```bash
make test-e2e
```

This mode requires:
- Working Docker daemon
- Network connectivity for Go module downloads
- More time for Docker image building

### Skip Docker Build (Use Existing Image)

If you have a pre-built image:

```bash
DOCKER_BUILD_SKIP=true make test-e2e
```

### Skip CertManager Installation

If CertManager is already installed in your cluster:

```bash
CERT_MANAGER_INSTALL_SKIP=true make test-e2e
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `USE_LOCAL_BINARY` | Use locally built binary instead of Docker image | `false` |
| `DOCKER_BUILD_SKIP` | Skip Docker image build (assumes image exists) | `false` |
| `CERT_MANAGER_INSTALL_SKIP` | Skip CertManager installation | `false` |
| `KIND_CLUSTER` | Name of the Kind cluster to use | `dynamic-annotation-controller-test-e2e` |

## Cleanup

To clean up the test environment:

```bash
make cleanup-test-e2e
```

This command:
- Deletes the Kind cluster
- Removes all associated resources
- Always succeeds (safe to run multiple times)

## Test Structure

The e2e tests include:

1. **Manager Tests**: Validate controller deployment and pod status
2. **Metrics Tests**: Verify metrics endpoint functionality
3. **CRD Tests**: Test AnnotationScheduleRule creation and validation

## Troubleshooting

### Docker Build Failures

If you encounter network timeouts during Docker builds:
- Use `USE_LOCAL_BINARY=true` mode
- Check your network connectivity
- Consider using a Go proxy

### Kind Cluster Issues

If Kind cluster creation fails:
- Ensure Docker is running
- Check available disk space
- Try cleaning up existing clusters: `kind delete cluster --name dynamic-annotation-controller-test-e2e`

### CertManager Issues

If CertManager installation fails:
- Use `CERT_MANAGER_INSTALL_SKIP=true` if already installed
- Check cluster connectivity
- Verify kubectl context is correct