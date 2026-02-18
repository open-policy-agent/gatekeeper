# Agent Instructions for OPA Gatekeeper

## Project Overview
Gatekeeper is a Kubernetes admission controller that provides policy-based governance for Kubernetes clusters using Open Policy Agent (OPA). It extends Kubernetes with **validation** and **mutation** capabilities through custom resources and webhooks. **Performance and security are the highest priorities** - admission controllers must minimize latency while maintaining strict security boundaries to protect cluster operations.

## Architecture & Repository Overview

**Core Components:**
- **Controller Manager**: Main controller managing constraints, templates, and policies
- **Admission Webhooks**: Validating and mutating admission controllers
- **Audit System**: Periodic compliance checking for existing resources
- **Mutation System**: Resource transformation capabilities
- **External Data**: Integration with external data sources
- **Gator CLI**: Policy testing and verification tool

**Repository Details:**
- **Size & Type:** Large-scale Go project (~165k lines) focused on Kubernetes admission control and policy governance with extensive test coverage
- **Primary Language:** Go 1.24+ with vendored dependencies
- **Key Frameworks:** controller-runtime, OPA/Rego, Common Expression Language (CEL)
- **Container Technology:** Docker with multi-stage builds using buildx
- **Testing Stack:** Go test, BATS (Bash Automated Testing System), envtest for Kubernetes integration
- **CI/CD:** GitHub Actions with comprehensive matrix testing across Kubernetes versions

**Core Dependencies:**
- **Kubernetes API server**: Tested against latest three versions
- **controller-runtime**: Kubernetes controller framework
- **OPA Frameworks/Constraint**: Policy evaluation engine and constraint framework
- **cert-controller**: Automatic TLS certificate management and rotation

## Key Development Workflows

### Project Structure & Architecture

**Core Directory Layout:**
```
├── apis/                              # Kubernetes API definitions (CRDs)
│   ├── config/                        # Configuration CRDs (Config, Provider)
│   ├── connection/                    # Connection CRDs for exporting violations
│   ├── expansion/                     # Expansion template CRDs
│   ├── gvkmanifest/                   # GVK manifest CRDs
│   ├── mutations/                     # Mutation CRDs (Assign, AssignMetadata, ModifySet)
│   ├── status/                        # Status tracking CRDs
│   └── syncset/                       # Data synchronization CRDs
├── cmd/                               # Command line tools
│   ├── build/helmify/                 # Helm chart generation tool
│   └── gator/                         # Gator CLI tool for policy testing
├── main.go                            # main entry point
├── pkg/                               # Core business logic
│   ├── audit/                         # Audit functionality and violation tracking
│   ├── cachemanager/                  # Cache management for constraint evaluation
│   ├── controller/                    # Kubernetes controllers
│   │   ├── config/                    # Config controller
│   │   ├── configstatus/              # Config status controller
│   │   ├── connectionstatus/          # Connection status controller
│   │   ├── constraint/                # Constraint controller
│   │   ├── constraintstatus/          # Constraint status controller
│   │   ├── constrainttemplate/        # ConstraintTemplate controller
│   │   ├── constrainttemplatestatus/  # ConstraintTemplate status controller
│   │   ├── expansion/                 # Expansion controller
│   │   ├── expansionstatus/           # Expansion status controller
│   │   ├── export/                    # Export controller
│   │   ├── externaldata/              # External data controller
│   │   ├── mutators/                  # Mutators controller
│   │   ├── mutatorstatus/             # Mutator status controller
│   │   ├── sync/                      # Sync controller
│   │   └── syncset/                   # Syncset controller
│   ├── drivers/                       # Policy engine drivers (CEL)
│   ├── expansion/                     # Template expansion engine
│   ├── export/                        # Violation export functionality
│   ├── externaldata/                  # External data provider integration
│   ├── gator/                         # CLI implementation and testing utilities
│   ├── instrumentation/               # Metrics and observability
│   ├── logging/                       # Structured logging utilities
│   ├── metrics/                       # Prometheus metrics
│   ├── mutation/                      # Mutation engine and mutators
│   ├── operations/                    # Administrative operations
│   ├── readiness/                     # Health and readiness checks
│   ├── syncutil/                      # Data synchronization utilities
│   ├── target/                        # Target resource management
│   ├── upgrade/                       # Version upgrade logic
│   ├── util/                          # Shared utilities
│   ├── version/                       # Version information
│   ├── watch/                         # Resource watching utilities
│   ├── webhook/                       # Admission webhook handlers
│   │   ├── admission/                 # Main admission logic
│   │   └── mutation/                  # Mutation webhook logic
│   └── wildcard/                      # Wildcard matching utilities
├── charts/                            # Helm charts for deployment
├── config/                            # Kubernetes manifests and configuration
│   ├── certmanager/                   # Certificate manager configuration
│   ├── default/                       # Default deployment configuration
│   ├── manager/                       # Manager deployment configuration
│   └── webhook/                       # Webhook configuration
├── deploy/                            # Deployment configurations and scripts
├── docs/                              # Documentation and examples
├── example/                           # Example policies and configurations
├── hack/                              # Development scripts and utilities
├── test/                              # Integration and e2e tests
│   ├── bats/                          # BATS test scripts
│   ├── externaldata/                  # External data provider tests
│   └── testutils/                     # Test utilities and helpers
├── third_party/                       # Third-party dependencies
├── vendor/                            # Go vendor dependencies
└── website/                           # Documentation website source
```

**Critical Configuration Files:**
- **`.golangci.yaml`** - Linting configuration, specifies exact rules and versions
- **`Makefile`** - Primary build system, all development commands defined here
- **`go.mod`** - Go modules with specific version pinning for stability
- **`main.go`** - Application entry point with webhook and controller setup

**Key Integration Points:**
- **OPA Frameworks:** `pkg/controller/constrainttemplate/` integrates constraint evaluation
- **cert-controller:** `main.go` sets up automatic TLS certificate rotation
- **Webhook Logic:** `pkg/webhook/admission/` handles policy evaluation during admission

**Testing Infrastructure:**
- **Unit Tests:** `*_test.go` files use testify/suite with envtest for Kubernetes APIs
- **E2E Tests:** `test/bats/test.bats` with full cluster scenarios using kind
- **Integration:** `pkg/controller/` tests use controller-runtime's envtest framework

### Build & Development Workflow

**Critical Prerequisites:**
- **Always install Go 1.24.2 or later** - verified compatible version
- **Docker with buildx support** - required for all builds
- **vendor/ directory must be present** - run `go mod vendor` if missing

**Essential Build Commands:**
```bash
# Complete build, lint, and test (primary development target)
make all                    # ~3-5 minutes, includes lint + test + manager

# Individual components
make manager               # Build controller binary (~2 minutes)
make gator                 # Build CLI tool (~1 minute)
make lint                  # Lint code using golangci-lint v2.3.0 (~1 minute)
make native-test          # Run unit tests natively (~4 minutes)
make test                 # Run tests in Docker (~5 minutes)
make test-e2e            # End-to-end tests with BATS (~10-15 minutes)
make docker-build         # Build Docker images
make deploy               # Deploy to Kubernetes cluster
```

**Critical Build Issues & Solutions:**
- **Empty test files cause lint failures** - remove any empty `.go` files in `pkg/` directories
- **Docker buildx required** - standard docker build will fail, must use buildx
- **Vendor dependencies** - run `go mod vendor` before building if vendor/ missing
- **Tool versions matter** - golangci-lint v2.3.0 specifically required

**Development Environment Setup:**
1. Install Go 1.24.2+ and Docker with buildx
2. Clone with full dependency chain: `git clone --recurse-submodules`
3. **Always run `go mod vendor` after fresh clone**
4. **Always run `make lint` before committing** - catches common issues
5. Use `make native-test` for fast feedback during development

### Testing Strategy

**Test Types & Commands:**
- **Unit Tests**: Go tests with testify/suite for component testing
  ```bash
  make native-test           # Fast, comprehensive unit testing (~4 minutes)
  make test                 # Run tests in Docker container (~5 minutes)
  ```
- **Integration Tests**: Kubernetes controller integration tests using envtest
- **E2E Tests**: Full cluster tests using BATS and Kind
  ```bash
  make test-e2e             # Requires kind cluster, tests real scenarios (~10-15 minutes)
  ```
- **Gator Tests**: Policy verification using gator CLI
  ```bash
  make test-gator           # Policy verification and CLI functionality
  ```
- **Performance Tests**: Webhook latency and throughput benchmarks
  ```bash
  make benchmark-test       # Generate performance metrics
  ```

**Testing Best Practices:**
- **Always run `make native-test` before submitting changes** - fast feedback cycle
- **Run `make test-e2e` for webhook/controller changes** - validates integration scenarios
- **Use `make test-gator` for policy/CLI changes** - ensures policy functionality works correctly

### Development Guidelines & Patterns

Make sure that there is a new line at the end of any file you edit. This is a common convention in Go and many other programming languages.

**CRD Development:**
1. **API Definitions** (`apis/` directory): Use controller-gen markers, follow Kubernetes conventions
2. **Controller Implementation** (`pkg/controller/` directory): Use controller-runtime patterns with reconciliation loops
3. **Webhook Implementation** (`pkg/webhook/` directory): Separate validation/mutation logic with structured logging

**Policy Development:**
- **Constraint Templates**: Define reusable policy templates with parameters
- **Constraints**: Instantiate templates with specific configuration
- **Rego/CEL Policies**: Write efficient policies with proper error handling (prefer CEL for performance)

**Code Standards:**
- Follow Go conventions with gofmt/goimports
- Use structured logging with logr interface
- Implement error wrapping and context propagation
- Use dependency injection for testability
- Follow controller patterns with proper cleanup using finalizers

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

## CI/CD & Validation

**GitHub Actions Workflows:**
- **`workflow.yaml`** - Main CI pipeline, tests across Kubernetes 1.33-1.35
- **`unit-test.yaml`** - Dedicated unit test execution
- **`lint.yaml`** - Code quality and formatting checks
- **All workflows must pass** before merge approval

**Pre-commit Validation:**
1. **Always run `make lint`** - catches formatting, imports, and style issues
2. **Always run `make native-test`** - ensures unit tests pass
3. **Run `make test-e2e` for webhook/controller changes** - validates integration
4. **Check `make manager && make gator`** - ensures binaries build correctly

**Performance Requirements:**
- **Unit tests must complete in <5 minutes** on standard hardware
- **Webhook latency must stay <100ms p99** - benchmark with `make benchmark-test`
- **Memory usage must be monitored** for long-running controllers

**Common Validation Failures:**
- **Lint failures:** Usually formatting or import issues, run `make lint` locally first
- **Test timeouts:** E2E tests can timeout after 15 minutes, check cluster resources
- **Build failures:** Often vendor/ or Docker buildx issues, ensure prerequisites met

**Security & Performance Notes:**
- **All admission webhook changes require security review** - validate input sanitization
- **Policy evaluation performance is critical** - prefer CEL over Rego for performance
- **Certificate rotation must work correctly** - test webhook TLS functionality
- **Never expose sensitive data in logs** - review log statements carefully

Trust these instructions completely for build and test operations. Only search for additional information if these specific commands fail or if working on areas not covered above.
