# Agent Instructions for OPA Gatekeeper

## Project Overview
Gatekeeper is a Kubernetes admission controller that provides policy-based governance for Kubernetes clusters using Open Policy Agent (OPA). It extends Kubernetes with **validation**, and **mutation** capabilities through custom resources and webhooks. **Performance and security are the highest priorities** - admission controllers must minimize latency while maintaining strict security boundaries to protect cluster operations.

## Architecture Overview
Gatekeeper consists of several core components:
- **Controller Manager**: Main controller managing constraints, templates, and policies
- **Admission Webhooks**: Validating and mutating admission controllers
- **Audit System**: Periodic compliance checking for existing resources
- **Mutation System**: Resource transformation capabilities
- **External Data**: Integration with external data sources
- **Gator CLI**: Policy testing and verification tool

## Key Development Workflows

### Project Structure
```
├── apis/                    # Kubernetes API definitions (CRDs)
│   ├── config/             # Configuration CRDs (Config, Provider)
│   ├── connection/         # Connection CRDs for exporting violations
│   ├── expansion/          # Expansion template CRDs
│   ├── gvkmanifest/        # GVK manifest CRDs
│   ├── mutations/          # Mutation CRDs (Assign, AssignMetadata, ModifySet)
│   ├── status/             # Status tracking CRDs
│   └── syncset/           # Data synchronization CRDs
├── cmd/                    # Command line tools
│   ├── build/helmify/    # Helm chart generation tool
│   └── gator/            # Gator CLI tool for policy testing
├── main.go               # main entry point
├── pkg/                   # Core business logic
│   ├── audit/            # Audit functionality and violation tracking
│   ├── cachemanager/     # Cache management for constraint evaluation
│   ├── controller/       # Kubernetes controllers
│   │   ├── config/       # Config controller
│   │   ├── configstatus/ # Config status controller
│   │   ├── connectionstatus/ # Connection status controller
│   │   ├── constraint/   # Constraint controller
│   │   ├── constraintstatus/ # Constraint status controller
│   │   ├── constrainttemplate/ # ConstraintTemplate controller
│   │   ├── constrainttemplatestatus/ # ConstraintTemplate status controller
│   │   ├── expansion/    # Expansion controller
│   │   ├── expansionstatus/ # Expansion status controller
│   │   ├── export/       # Export controller
│   │   ├── externaldata/ # External data controller
│   │   ├── mutators/     # Mutators controller
│   │   ├── mutatorstatus/ # Mutator status controller
│   │   ├── sync/         # Sync controller
│   │   └── syncset/      # Syncset controller
│   ├── drivers/          # Policy engine drivers (CEL)
│   ├── expansion/        # Template expansion engine
│   ├── export/           # Violation export functionality
│   ├── externaldata/     # External data provider integration
│   ├── gator/           # CLI implementation and testing utilities
│   ├── instrumentation/ # Metrics and observability
│   ├── logging/         # Structured logging utilities
│   ├── metrics/         # Prometheus metrics
│   ├── mutation/        # Mutation engine and mutators
│   ├── operations/      # Administrative operations
│   ├── readiness/       # Health and readiness checks
│   ├── syncutil/        # Data synchronization utilities
│   ├── target/          # Target resource management
│   ├── upgrade/         # Version upgrade logic
│   ├── util/           # Shared utilities
│   ├── version/        # Version information
│   ├── watch/          # Resource watching utilities
│   ├── webhook/        # Admission webhook handlers
│   │   ├── admission/  # Main admission logic
│   │   └── mutation/   # Mutation webhook logic
│   └── wildcard/       # Wildcard matching utilities
├── charts/               # Helm charts for deployment
├── config/              # Kubernetes manifests and configuration
│   ├── certmanager/     # Certificate manager configuration
│   ├── default/         # Default deployment configuration
│   ├── manager/         # Manager deployment configuration
│   └── webhook/         # Webhook configuration
├── deploy/              # Deployment configurations and scripts
├── docs/                # Documentation and examples
├── example/             # Example policies and configurations
├── hack/                # Development scripts and utilities
├── test/                # Integration and e2e tests
│   ├── bats/           # BATS test scripts
│   ├── externaldata/   # External data provider tests
│   └── testutils/      # Test utilities and helpers
├── third_party/         # Third-party dependencies
├── vendor/              # Go vendor dependencies
└── website/             # Documentation website source
```

### Build Commands
- `make all`: Build, lint, and test everything
- `make manager`: Build the controller manager binary
- `make gator`: Build the gator CLI tool
- `make test`: Run unit tests in containers
- `make native-test`: Run unit tests natively
- `make test-e2e`: Run end-to-end tests
- `make docker-build`: Build Docker images
- `make deploy`: Deploy to Kubernetes cluster

### Testing Strategy
- **Unit Tests**: Go tests with testify/suite for component testing
- **Integration Tests**: Kubernetes controller integration tests using envtest
- **E2E Tests**: Full cluster tests using BATS and Kind
- **Gator Tests**: Policy verification using gator CLI
- **Performance Tests**: Webhook latency and throughput benchmarks

### CRD Development Patterns
When working with Custom Resource Definitions:

1. **API Definitions** (`apis/` directory):
   - Use controller-gen markers for OpenAPI schema generation
   - Follow Kubernetes API conventions for field naming
   - Include comprehensive field validation and documentation

2. **Controller Implementation** (`pkg/controller/` directory):
   - Use controller-runtime framework patterns
   - Implement proper reconciliation loops with exponential backoff
   - Handle finalizers for cleanup logic
   - Use proper indexing for efficient lookups

3. **Webhook Implementation** (`pkg/webhook/` directory):
   - Separate admission logic into validation and mutation
   - Handle webhook failure modes gracefully
   - Implement proper error messages for policy violations
   - Use structured logging for debugging

### Policy Development Guidelines
- **Constraint Templates**: Define reusable policy templates with parameters
- **Constraints**: Instantiate templates with specific configuration
- **Rego Policies**: Write efficient OPA policies with proper error handling
- **Data Sync**: Configure data dependencies for policies requiring external data

### Code Style & Conventions
- Follow standard Go conventions and use gofmt/goimports
- Use structured logging with logr interface
- Implement proper error wrapping and context propagation
- Follow Kubernetes API machinery patterns for controllers
- Use dependency injection for testability

### Security Considerations
**Security is paramount** - every component must be designed with security-first principles:

- **Critical**: Validate and sanitize all user inputs in admission webhooks
- **Mandatory**: Implement strict RBAC with principle of least privilege
- **Essential**: Use secure defaults for all configurations - never trust user input
- **Required**: Audit and log all policy decisions and violations for security monitoring
- **Must**: Ensure webhook certificates are properly managed and rotated
- **Always**: Assume hostile input and implement defense in depth
- **Never**: Expose sensitive data in logs, error messages, or responses

### Performance Guidelines
**Performance is critical** - admission controllers must be lightning fast to avoid blocking cluster operations:

- **Critical**: Minimize webhook latency (target <100ms p99, <50ms p95)
- **Mandatory**: Use efficient CEL over Rego for policy evaluation due to superior performance
- **Essential**: Implement proper caching for frequently accessed data
- **Required**: Monitor memory usage in long-running controllers
- **Must**: Optimize Kubernetes API calls with proper batching
- **Always**: Profile and benchmark code changes for performance impact
- **Never**: Trade performance for convenience - cluster stability depends on speed

### Testing Patterns
- Use table-driven tests for policy evaluation logic
- Mock external dependencies using interfaces
- Test error conditions and edge cases thoroughly
- Use envtest for controller integration testing
- Implement comprehensive e2e scenarios

### Key Files to Reference
- `pkg/controller/constrainttemplate/`: Constraint template controller
- `pkg/webhook/admission/`: Admission webhook implementation
- `pkg/audit/manager.go`: Audit system
- `pkg/mutation/`: Mutation system
- `cmd/gator/`: CLI tool implementation
- `Makefile`: Build targets and development commands

### External Dependencies
- **controller-runtime**: Kubernetes controller framework
- **OPA**: Policy evaluation engine
- **OPA Frameworks/Constraint**: Constraint framework for policy templates and evaluation
- **cert-controller**: Automatic TLS certificate management and rotation for webhooks
- **cobra**: CLI framework for gator
- **gomega/ginkgo**: Testing framework
- **envtest**: Kubernetes API server for testing

### OPA Frameworks Integration
Gatekeeper heavily relies on the **OPA Frameworks/Constraint** library (`github.com/open-policy-agent/frameworks/constraint`) for core constraint and policy functionality:

- **Constraint Client**: Provides the core constraint evaluation engine that processes ConstraintTemplates and Constraints
- **Policy Drivers**: Supports both Rego and CEL policy engines through pluggable drivers
- **Template Management**: Handles ConstraintTemplate compilation, validation, and CRD generation
- **Review Processing**: Processes admission review requests against constraint policies
- **Error Handling**: Provides structured error reporting for policy violations and system errors
- **Instrumentation**: Built-in metrics and observability for constraint evaluation performance

**Key Integration Points:**
- `pkg/controller/constrainttemplate/`: Uses frameworks for template validation and CRD management
- `pkg/webhook/admission/`: Leverages constraint client for policy evaluation during admission
- `pkg/audit/`: Uses frameworks for periodic compliance checking of existing resources
- `pkg/drivers/`: Integrates with frameworks' policy engine drivers (Rego/CEL)

### Cert-Controller Integration
Gatekeeper uses **cert-controller** (`github.com/open-policy-agent/cert-controller`) for automatic TLS certificate management:

- **Certificate Rotation**: Automatically generates and rotates TLS certificates for webhook endpoints
- **CA Management**: Creates and maintains Certificate Authority for webhook validation
- **Secret Management**: Manages Kubernetes secrets containing TLS certificates and keys
- **Webhook Configuration**: Automatically updates webhook configurations with current CA bundles
- **Readiness Integration**: Provides readiness checks to ensure certificates are valid before serving

**Key Integration Points:**
- `main.go`: Sets up CertRotator with webhook configuration and certificate settings
- `pkg/webhook/policy.go`: Uses rotator for validating admission webhook TLS
- `pkg/webhook/mutation.go`: Uses rotator for mutating admission webhook TLS

### Common Patterns
- Use `context.Context` for all long-running operations
- Implement graceful shutdown handling
- Use proper Kubernetes owner references for resource relationships
- Follow the controller pattern with reconciliation loops
- Implement proper cleanup using finalizers when needed

### Communication Guidelines
When contributing to Gatekeeper, maintain clear and human-friendly communication:

**Code & Documentation:**
- Write self-documenting code with meaningful variable and function names
- Keep comments concise but informative - explain "why" not just "what"
- Use clear, descriptive commit messages that explain the intent behind changes
- Structure PR descriptions with context, changes made, and testing approach

**Error Messages:**
- Provide actionable error messages that guide users toward solutions
- Include relevant context (resource names, namespaces, constraint violations)
- Use plain language that both developers and operators can understand
- Suggest next steps or point to documentation when appropriate

**Policy Violations:**
- Write violation messages that clearly explain what was violated and why
- Include specific examples of compliant configurations when possible
- Keep messages concise but comprehensive enough for troubleshooting
- Use consistent terminology aligned with Kubernetes and OPA conventions

**Response Style:**
- Be direct and helpful without being overly verbose
- Focus on practical solutions and actionable advice
- Prioritize clarity over technical jargon when communicating with users
- Maintain a collaborative and welcoming tone in all interactions
