package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/support/kind"
	"sigs.k8s.io/e2e-framework/third_party/helm"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

var testenv env.Environment

func TestMain(m *testing.M) {
	// Create a new environment
	testenv = env.New()

	// Generate unique cluster name
	clusterName := envconf.RandomName("nodepools-e2e", 16)
	namespace := envconf.RandomName("crossplane-test", 16)

	// Ensure namespace name doesn't end with dash
	if namespace[len(namespace)-1] == '-' {
		namespace = namespace[:len(namespace)-1]
	}

	// Setup steps
	testenv.Setup(
		// Create Kind cluster
		envfuncs.CreateCluster(kind.NewProvider(), clusterName),
		// Install Crossplane
		installCrossplane,
		// Create namespace for our resources
		envfuncs.CreateNamespace(namespace),
		// Apply all YAML manifests from testdata directory
		applyManifestsFromDir,
	)

	// Teardown steps
	testenv.Finish(
		envfuncs.DeleteNamespace(namespace),
		envfuncs.DestroyCluster(clusterName),
	)

	// Run tests
	os.Exit(testenv.Run(m))
}

// installCrossplane installs Crossplane using Helm via e2e framework
func installCrossplane(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	// Create Helm manager using the e2e framework
	manager := helm.New(cfg.KubeconfigFile())

	// Add Crossplane Helm repository
	err := manager.RunRepo(helm.WithArgs("add", "crossplane-stable", "https://charts.crossplane.io/stable"))
	if err != nil {
		return ctx, fmt.Errorf("failed to add crossplane helm repo: %w", err)
	}

	// Update Helm repositories
	err = manager.RunRepo(helm.WithArgs("update"))
	if err != nil {
		return ctx, fmt.Errorf("failed to update helm repos: %w", err)
	}

	// Install Crossplane
	err = manager.RunInstall(
		helm.WithName("crossplane"),
		helm.WithNamespace("crossplane-system"),
		helm.WithReleaseName("crossplane-stable/crossplane"),
		helm.WithArgs("--create-namespace", "--wait"),
	)
	if err != nil {
		return ctx, fmt.Errorf("failed to install crossplane: %w", err)
	}

	// Wait for Crossplane to be ready
	client, err := createKubernetesClient(cfg.KubeconfigFile())
	if err != nil {
		return ctx, fmt.Errorf("failed to create client: %w", err)
	}

	if err := waitForDeploymentReady(client, "crossplane-system", "crossplane"); err != nil {
		return ctx, fmt.Errorf("crossplane deployment not ready: %w", err)
	}

	return ctx, nil
}

// applyManifestsFromDir applies all YAML manifests from the testdata directory
func applyManifestsFromDir(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	// Use the decoder package to apply manifests from directory
	client := cfg.Client()
	resources := client.Resources()
	err := decoder.ApplyWithManifestDir(ctx, resources, "./e2e/testdata", "", nil)
	if err != nil {
		return ctx, fmt.Errorf("failed to apply manifests from directory: %w", err)
	}
	return ctx, nil
}

// createKubernetesClient creates a Kubernetes client
func createKubernetesClient(kubeconfig string) (*kubernetes.Clientset, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

// waitForDeploymentReady waits for a deployment to be ready
func waitForDeploymentReady(client *kubernetes.Clientset, namespace, name string) error {
	return wait.PollUntilContextTimeout(context.Background(), 5*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return deployment.Status.ReadyReplicas == *deployment.Spec.Replicas, nil
	})
}

// TestCrossplaneInstallation tests that Crossplane is properly installed
func TestCrossplaneInstallation(t *testing.T) {
	feature := features.New("Crossplane Installation Test").
		Assess("Crossplane is installed and ready", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			client, err := createKubernetesClient(cfg.KubeconfigFile())
			if err != nil {
				t.Fatalf("Failed to create client: %v", err)
			}

			// Check Crossplane deployment
			deployment, err := client.AppsV1().Deployments("crossplane-system").Get(ctx, "crossplane", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to get Crossplane deployment: %v", err)
			}

			if deployment.Status.ReadyReplicas != *deployment.Spec.Replicas {
				t.Fatalf("Crossplane deployment not ready: %d/%d replicas ready", deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
			}

			t.Log("Crossplane installation verified")
			return ctx
		}).Feature()

	testenv.Test(t, feature)
}
