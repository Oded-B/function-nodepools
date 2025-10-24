# E2E Tests for Function Nodepools

This directory contains end-to-end tests for the function-nodepools Crossplane function using the Kubernetes e2e framework.

## Prerequisites

Before running the e2e tests, ensure you have the following installed:

1. **Go 1.24+** - Required for building and running the tests
2. **Kind** - For creating local Kubernetes clusters
3. **Helm** - For installing Crossplane
4. **kubectl** - For interacting with the Kubernetes cluster

### Installation

```bash
# Install Kind
go install sigs.k8s.io/kind@latest

# Install Helm
curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash

# Install kubectl (if not already installed)
# Follow instructions at: https://kubernetes.io/docs/tasks/tools/install-kubectl/
```

## Test Structure

The e2e tests are organized as follows:

```
e2e/
├── e2e_test.go          # Main test file with test logic
├── testdata/            # YAML files used by tests
│   ├── function-nodepools.yaml
│   ├── xrd.yaml
│   ├── composition.yaml
│   ├── claim-production.yaml
│   ├── claim-default.yaml
│   ├── virginia.instancetypeofferings.ec2.ec2offering.crossplane.io.yaml
│   ├── mexico.instancetypeofferings.ec2.ec2offering.crossplane.io.yaml
│   ├── us-east-1-spot-data.spotadvisordata.yaml
│   ├── us-west-2-spot-data.spotadvisordata.yaml
│   ├── ec2.ec2offering.crossplane.io_instancetypeofferings.yaml
│   └── ec2.ec2offering.crossplane.io_spotadvisordata.yaml
└── README.md            # This file
```

## Test Flow

The e2e tests perform the following steps in order:

1. **Create Kind Cluster** - Sets up a local Kubernetes cluster
2. **Install Crossplane** - Installs Crossplane using Helm
3. **Install CRDs** - Installs required Custom Resource Definitions
4. **Install Function and XRD** - Installs the function and Composite Resource Definition
5. **Install Composition** - Installs the Crossplane composition
6. **Install Dependent Objects** - Installs InstanceTypeOffering and SpotAdvisorData objects
7. **Install XR Claims** - Installs the Composite Resource claims
8. **Test Created Objects** - Verifies the content and state of created objects

## Running the Tests

### Prerequisites Check

Before running the tests, ensure all prerequisites are installed:

```bash
# Check Go version
go version

# Check Kind installation
kind version

# Check Helm installation
helm version

# Check kubectl installation
kubectl version --client
```

### Running the Tests

From the project root directory:

```bash
# Run all e2e tests
go test ./e2e -v

# Run specific test
go test ./e2e -v -run TestCrossplaneInstallation

# Run tests with timeout
go test ./e2e -v -timeout 30m
```

### Test Options

The tests support various options:

- `-v` - Verbose output
- `-timeout` - Set test timeout (default is 10 minutes)
- `-run` - Run specific test functions
- `-count` - Run tests multiple times

## Test Data

The `testdata/` directory contains all the YAML files required for the tests:

- **function-nodepools.yaml** - Crossplane function definition
- **xrd.yaml** - Composite Resource Definition
- **composition.yaml** - Crossplane composition
- **claim-*.yaml** - Composite Resource claims
- **InstanceTypeOffering objects** - Sample data for testing
- **SpotAdvisorData objects** - Sample spot pricing data
- **CRD files** - Custom Resource Definitions

## Troubleshooting

### Common Issues

1. **Kind cluster creation fails**
   - Ensure Docker is running
   - Check available disk space
   - Verify Kind installation

2. **Crossplane installation fails**
   - Check Helm repository access
   - Verify network connectivity
   - Check cluster resources

3. **YAML application fails**
   - Verify YAML syntax
   - Check resource dependencies
   - Ensure CRDs are installed first

4. **Tests timeout**
   - Increase timeout with `-timeout` flag
   - Check cluster resource availability
   - Verify all dependencies are installed

### Debug Mode

For debugging, you can run tests with additional logging:

```bash
# Run with debug output
go test ./e2e -v -args -test.v

# Keep cluster after test failure
# Modify the test to comment out cluster destruction
```

### Manual Verification

After test completion, you can manually verify the installation:

```bash
# Check Crossplane installation
kubectl get pods -n crossplane-system

# Check function installation
kubectl get function

# Check XRD installation
kubectl get xrd

# Check composition installation
kubectl get composition

# Check claims
kubectl get xr
```

## Cleanup

The tests automatically clean up the Kind cluster after completion. If tests fail, you may need to manually clean up:

```bash
# List Kind clusters
kind get clusters

# Delete specific cluster
kind delete cluster --name <cluster-name>

# Delete all clusters
kind delete clusters --all
```

## Contributing

When adding new tests or modifying existing ones:

1. Follow the existing test structure
2. Add appropriate error handling
3. Include cleanup in teardown steps
4. Update this README if needed
5. Test your changes thoroughly

## References

- [Kubernetes E2E Framework](https://github.com/kubernetes-sigs/e2e-framework)
- [Crossplane Documentation](https://docs.crossplane.io/)
- [Kind Documentation](https://kind.sigs.k8s.io/)
- [Helm Documentation](https://helm.sh/docs/)
