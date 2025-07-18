# Project Structure

## Standard Kubernetes Controller Layout

```
dynamic-annotation-controller/
├── cmd/
│   └── main.go                 # Application entry point
├── pkg/
│   ├── controller/             # Controller logic
│   │   └── annotation_controller.go
│   ├── apis/                   # Custom Resource Definitions
│   │   └── v1/
│   │       ├── types.go
│   │       └── zz_generated.deepcopy.go
│   └── utils/                  # Utility functions
├── config/
│   ├── crd/                    # Custom Resource Definitions
│   ├── rbac/                   # Role-based access control
│   ├── manager/                # Controller manager config
│   └── samples/                # Example configurations
├── deploy/                     # Deployment manifests
│   ├── deployment.yaml
│   ├── service.yaml
│   └── rbac.yaml
├── hack/                       # Build and development scripts
├── test/
│   ├── unit/                   # Unit tests
│   └── integration/            # Integration tests
├── docs/                       # Documentation
├── Dockerfile
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

## Naming Conventions
- **Files**: snake_case for Go files, kebab-case for YAML
- **Packages**: lowercase, single word when possible
- **Types**: PascalCase for exported types
- **Functions**: camelCase for private, PascalCase for exported
- **Constants**: UPPER_SNAKE_CASE

## Code Organization
- Keep controller logic in `pkg/controller/`
- Place API types in `pkg/apis/`
- Store utilities in `pkg/utils/`
- Configuration files in `config/` directory
- Deployment manifests in `deploy/` directory

## File Patterns
- `*_controller.go` for controller implementations
- `*_types.go` for API type definitions
- `*_test.go` for test files
- `zz_generated.*` for generated code (do not edit manually)