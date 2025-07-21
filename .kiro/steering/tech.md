# Technology Stack

## Core Technologies
- **Language**: Go
- **Framework**: Kubebuilder (controller-runtime based)
- **Kubernetes API**: client-go library
- **Configuration**: YAML manifests and CRDs

## Build System
- **Build Tool**: Go modules (`go mod`)
- **Container**: Docker/Podman for containerization
- **Deployment**: Kubernetes manifests (kustomize recommended)

## Common Commands

### Development
```bash
# Initialize Kubebuilder project
kubebuilder init --domain [your-domain] --repo github.com/[org]/dynamic-annotation-controller

# Create API and controller
kubebuilder create api --group config --version v1 --kind AnnotationRule

# Build the controller
make build

# Run tests
make test

# Run with coverage
go test -coverprofile=coverage.out ./...

# Run locally (against configured cluster)
make run
```

### Container Operations
```bash
# Build and push container image
make docker-build IMG=docker.io/davivcgarcia/dynamic-annotation-controller:latest
make docker-push IMG=docker.io/davivcgarcia/dynamic-annotation-controller:latest

# Build multi-arch images
make docker-buildx IMG=docker.io/davivcgarcia/dynamic-annotation-controller:latest
```

### Kubernetes Deployment
```bash
# Install CRDs
make install

# Deploy controller to cluster
make deploy IMG=docker.io/davivcgarcia/dynamic-annotation-controller:latest

# Undeploy controller
make undeploy

# View logs
kubectl logs -f deployment/dynamic-annotation-controller-controller-manager -n dynamic-annotation-controller-system
```

## Dependencies
- `sigs.k8s.io/controller-runtime`
- `k8s.io/client-go`
- `k8s.io/apimachinery`
- Testing: `github.com/onsi/ginkgo` and `github.com/onsi/gomega`