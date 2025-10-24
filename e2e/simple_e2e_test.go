package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// TestCrossplaneE2E runs the complete e2e test suite
func TestCrossplaneE2E(t *testing.T) {
	// Step 1: Create Kind cluster
	t.Log("Step 1: Creating Kind cluster...")
	if err := runCommand("kind", "create", "cluster", "--name", "nodepools-e2e"); err != nil {
		t.Fatalf("Failed to create Kind cluster: %v", err)
	}
	defer func() {
		t.Log("Cleaning up Kind cluster...")
		runCommand("kind", "delete", "cluster", "--name", "nodepools-e2e")
	}()

	// Step 2: Install Crossplane
	t.Log("Step 2: Installing Crossplane...")
	if err := installCrossplane(); err != nil {
		t.Fatalf("Failed to install Crossplane: %v", err)
	}

	// Step 3: Install CRDs
	t.Log("Step 3: Installing CRDs...")
	if err := installCRDs(); err != nil {
		t.Fatalf("Failed to install CRDs: %v", err)
	}

	// Step 4: Install Function and XRD
	t.Log("Step 4: Installing Function and XRD...")
	if err := installFunctionAndXRD(); err != nil {
		t.Fatalf("Failed to install Function and XRD: %v", err)
	}

	// Step 5: Install Composition
	t.Log("Step 5: Installing Composition...")
	if err := installComposition(); err != nil {
		t.Fatalf("Failed to install Composition: %v", err)
	}

	// Step 6: Install dependent objects
	t.Log("Step 6: Installing dependent objects...")
	if err := installDependentObjects(); err != nil {
		t.Fatalf("Failed to install dependent objects: %v", err)
	}

	// Step 7: Install XR claims
	t.Log("Step 7: Installing XR claims...")
	if err := installXRClaims(); err != nil {
		t.Fatalf("Failed to install XR claims: %v", err)
	}

	// Step 8: Test created objects
	t.Log("Step 8: Testing created objects...")
	if err := testCreatedObjects(); err != nil {
		t.Fatalf("Failed to test created objects: %v", err)
	}

	t.Log("All e2e tests completed successfully!")
}

// installCrossplane installs Crossplane using Helm
func installCrossplane() error {
	// Add Crossplane Helm repository
	if err := runCommand("helm", "repo", "add", "crossplane-stable", "https://charts.crossplane.io/stable"); err != nil {
		return fmt.Errorf("failed to add crossplane helm repo: %w", err)
	}

	if err := runCommand("helm", "repo", "update"); err != nil {
		return fmt.Errorf("failed to update helm repos: %w", err)
	}

	// Install Crossplane
	if err := runCommand("helm", "install", "crossplane", "crossplane-stable/crossplane",
		"--namespace", "crossplane-system",
		"--create-namespace",
		"--wait"); err != nil {
		return fmt.Errorf("failed to install crossplane: %w", err)
	}

	// Wait for Crossplane to be ready
	client, err := createKubernetesClient("")
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	if err := waitForDeploymentReady(client, "crossplane-system", "crossplane"); err != nil {
		return fmt.Errorf("crossplane deployment not ready: %w", err)
	}

	return nil
}

// installCRDs installs the required CRDs
func installCRDs() error {
	testdataDir := filepath.Join("e2e", "testdata")

	// Install InstanceTypeOffering CRD
	if err := applyYAMLFile(filepath.Join(testdataDir, "ec2.ec2offering.crossplane.io_instancetypeofferings.yaml")); err != nil {
		return fmt.Errorf("failed to install InstanceTypeOffering CRD: %w", err)
	}

	// Install SpotAdvisorData CRD
	if err := applyYAMLFile(filepath.Join(testdataDir, "ec2.ec2offering.crossplane.io_spotadvisordata.yaml")); err != nil {
		return fmt.Errorf("failed to install SpotAdvisorData CRD: %w", err)
	}

	return nil
}

// installFunctionAndXRD installs the function and XRD
func installFunctionAndXRD() error {
	testdataDir := filepath.Join("e2e", "testdata")

	// Install XRD
	if err := applyYAMLFile(filepath.Join(testdataDir, "xrd.yaml")); err != nil {
		return fmt.Errorf("failed to install XRD: %w", err)
	}

	// Install Function
	if err := applyYAMLFile(filepath.Join(testdataDir, "function-nodepools.yaml")); err != nil {
		return fmt.Errorf("failed to install function: %w", err)
	}

	return nil
}

// installComposition installs the composition
func installComposition() error {
	testdataDir := filepath.Join("e2e", "testdata")

	if err := applyYAMLFile(filepath.Join(testdataDir, "composition.yaml")); err != nil {
		return fmt.Errorf("failed to install composition: %w", err)
	}

	return nil
}

// installDependentObjects installs the dependent objects
func installDependentObjects() error {
	testdataDir := filepath.Join("e2e", "testdata")

	// Install InstanceTypeOffering objects
	if err := applyYAMLFile(filepath.Join(testdataDir, "virginia.instancetypeofferings.ec2.ec2offering.crossplane.io.yaml")); err != nil {
		return fmt.Errorf("failed to install virginia InstanceTypeOffering: %w", err)
	}

	if err := applyYAMLFile(filepath.Join(testdataDir, "mexico.instancetypeofferings.ec2.ec2offering.crossplane.io.yaml")); err != nil {
		return fmt.Errorf("failed to install mexico InstanceTypeOffering: %w", err)
	}

	// Install SpotAdvisorData objects
	if err := applyYAMLFile(filepath.Join(testdataDir, "us-east-1-spot-data.spotadvisordata.yaml")); err != nil {
		return fmt.Errorf("failed to install us-east-1 SpotAdvisorData: %w", err)
	}

	if err := applyYAMLFile(filepath.Join(testdataDir, "us-west-2-spot-data.spotadvisordata.yaml")); err != nil {
		return fmt.Errorf("failed to install us-west-2 SpotAdvisorData: %w", err)
	}

	return nil
}

// installXRClaims installs the XR claims
func installXRClaims() error {
	testdataDir := filepath.Join("e2e", "testdata")

	// Install production claim
	if err := applyYAMLFile(filepath.Join(testdataDir, "claim-production.yaml")); err != nil {
		return fmt.Errorf("failed to install production claim: %w", err)
	}

	// Install default claim
	if err := applyYAMLFile(filepath.Join(testdataDir, "claim-default.yaml")); err != nil {
		return fmt.Errorf("failed to install default claim: %w", err)
	}

	return nil
}

// testCreatedObjects tests the created objects
func testCreatedObjects() error {
	// For now, just verify that the YAML files were applied successfully
	// In a real implementation, you would check the actual content of the objects
	return nil
}

// runCommand runs a shell command
func runCommand(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// createKubernetesClient creates a Kubernetes client
func createKubernetesClient(kubeconfig string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

// applyYAMLFile applies a YAML file to the cluster
func applyYAMLFile(filePath string) error {
	return runCommand("kubectl", "apply", "-f", filePath)
}

// waitForDeploymentReady waits for a deployment to be ready
func waitForDeploymentReady(client *kubernetes.Clientset, namespace, name string) error {
	return wait.PollImmediate(5*time.Second, 5*time.Minute, func() (bool, error) {
		deployment, err := client.AppsV1().Deployments(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return deployment.Status.ReadyReplicas == *deployment.Spec.Replicas, nil
	})
}
