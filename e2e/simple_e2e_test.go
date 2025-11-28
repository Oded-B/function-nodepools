package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
	"sigs.k8s.io/e2e-framework/pkg/features"
	"sigs.k8s.io/e2e-framework/support/kind"
	"sigs.k8s.io/e2e-framework/third_party/helm"

	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
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
		// Install Karpenter
		installKarpenter,
		// Create namespace for our resources
		envfuncs.CreateNamespace(namespace),
		// Apply all YAML manifests from testdata directory
		applyManifestsFromDir,
	)

	// Teardown steps
	testenv.Finish(
		envfuncs.DeleteNamespace(namespace), // TODO Do we need to delete the namespace if we destroy the cluster?
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
		helm.WithArgs("--create-namespace", "--wait", "--timeout", "10m"),
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

// installKarpenter installs Karpenter using Helm via e2e framework
func installKarpenter(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	// Create Helm manager using the e2e framework
	manager := helm.New(cfg.KubeconfigFile())

	// Get cluster name from kubeconfig context
	config, err := clientcmd.LoadFromFile(cfg.KubeconfigFile())
	if err != nil {
		return ctx, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	clusterName := config.CurrentContext
	if clusterName == "" {
		// Fallback: try to get cluster name from context
		if len(config.Contexts) > 0 {
			for name := range config.Contexts {
				clusterName = name
				break
			}
		}
		if clusterName == "" {
			clusterName = "kind-cluster" // Default fallback
		}
	}

	// Install Karpenter from OCI registry
	// Equivalent to: helm upgrade --install karpenter oci://public.ecr.aws/karpenter/karpenter
	// We only need Karpenter CRDs for our tests, not the controller itself, so we set replicas=0
	// Karpenter requires settings.clusterName to be set
	err = manager.RunInstall(
		helm.WithName("karpenter"),
		helm.WithNamespace("karpenter"),
		helm.WithReleaseName("oci://public.ecr.aws/karpenter/karpenter"),
		helm.WithArgs("--create-namespace", "--wait", "--timeout", "10m",
			"--set", fmt.Sprintf("settings.clusterName=%s", clusterName),
			"--set", "replicas=0"),
	)
	if err != nil {
		return ctx, fmt.Errorf("failed to install karpenter: %w", err)
	}

	// Note: We don't wait for deployment readiness since replicas=0 (controller is disabled)
	// We only need the CRDs to be installed for our NodePool tests

	return ctx, nil
}

// applyManifestsFromDir applies all YAML manifests from the testdata directory
func applyManifestsFromDir(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	// manifestDir := "./e2e/testdata"
	manifestDir := "/Users/oded.benozer/github/Oded-B/function-nodepools/e2e/testdata"

	// List all YAML files in the directory before applying
	fmt.Printf("📁 Scanning directory: %s\n", manifestDir)
	files, err := filepath.Glob(filepath.Join(manifestDir, "*.yaml"))
	if err != nil {
		return ctx, fmt.Errorf("failed to list YAML files in directory: %w", err)
	}

	if len(files) == 0 {
		return ctx, fmt.Errorf("no YAML files found in %s", manifestDir)
	}

	fmt.Printf("📄 Found %d YAML file(s) to apply:\n", len(files))
	for i, file := range files {
		fmt.Printf("  %d. %s\n", i+1, filepath.Base(file))
	}

	// Use the decoder package to apply manifests from directory
	client := cfg.Client()
	resources := client.Resources()
	fmt.Printf("🚀 Applying manifests from %s...\n", manifestDir)
	err = decoder.ApplyWithManifestDir(ctx, resources, manifestDir, "*", nil)
	if err != nil {
		return ctx, fmt.Errorf("failed to apply manifests from directory: %w", err)
	}
	fmt.Println("✓ Successfully applied all manifests from ./e2e/testdata directory")
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

// createDynamicClient creates a dynamic Kubernetes client for accessing CRDs
func createDynamicClient(kubeconfig string) (dynamic.Interface, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return dynamic.NewForConfig(config)
}

// waitForXNodePoolReady waits for an XNodePool claim to be ready
// Note: XNodePool is cluster-scoped, so we don't use namespace
func waitForXNodePoolReady(dynamicClient dynamic.Interface, name string) error {
	xrGVR := schema.GroupVersionResource{
		Group:    "cx.crossplane.io",
		Version:  "v1alpha1",
		Resource: "xnodepools",
	}

	return wait.PollUntilContextTimeout(context.Background(), 2*time.Second, 5*time.Minute, true, func(ctx context.Context) (bool, error) {
		obj, err := dynamicClient.Resource(xrGVR).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		// Check status conditions
		status, found, err := unstructured.NestedMap(obj.Object, "status")
		if !found || err != nil {
			return false, nil
		}

		conditions, found, err := unstructured.NestedSlice(status, "conditions")
		if !found || err != nil {
			return false, nil
		}

		// Check if there's a Ready condition that is True
		for _, cond := range conditions {
			condMap, ok := cond.(map[string]interface{})
			if !ok {
				continue
			}
			condType, _ := condMap["type"].(string)
			condStatus, _ := condMap["status"].(string)
			if condType == "Ready" && condStatus == "True" {
				return true, nil
			}
		}

		return false, nil
	})
}

// getNodePool retrieves a Karpenter NodePool by name
func getNodePool(dynamicClient dynamic.Interface, name string) (*karpenterv1.NodePool, error) {
	nodePoolGVR := schema.GroupVersionResource{
		Group:    "karpenter.sh",
		Version:  "v1",
		Resource: "nodepools",
	}

	obj, err := dynamicClient.Resource(nodePoolGVR).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Convert unstructured to NodePool using JSON marshaling/unmarshaling
	jsonBytes, err := json.Marshal(obj.Object)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal unstructured to JSON: %w", err)
	}

	nodePool := &karpenterv1.NodePool{}
	if err := json.Unmarshal(jsonBytes, nodePool); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON to NodePool: %w", err)
	}

	return nodePool, nil
}

// verifyNodePoolSpec verifies that a NodePool has the expected spec
func verifyNodePoolSpec(t *testing.T, nodePool *karpenterv1.NodePool, expectedCPU, expectedMemory string, expectedInstanceCategories []string) {
	// Verify limits
	if cpuLimit, ok := nodePool.Spec.Limits[corev1.ResourceCPU]; ok {
		expectedCPUQty := k8sresource.MustParse(expectedCPU)
		if cpuLimit.Cmp(expectedCPUQty) != 0 {
			t.Errorf("Expected CPU limit %s, got %s", expectedCPU, cpuLimit.String())
		}
	} else {
		t.Error("CPU limit not found in NodePool spec")
	}

	if memoryLimit, ok := nodePool.Spec.Limits[corev1.ResourceMemory]; ok {
		expectedMemoryQty := k8sresource.MustParse(expectedMemory)
		if memoryLimit.Cmp(expectedMemoryQty) != 0 {
			t.Errorf("Expected Memory limit %s, got %s", expectedMemory, memoryLimit.String())
		}
	} else {
		t.Error("Memory limit not found in NodePool spec")
	}

	// Verify disruption policy
	if nodePool.Spec.Disruption.ConsolidationPolicy != karpenterv1.ConsolidationPolicyWhenEmptyOrUnderutilized {
		t.Errorf("Expected consolidation policy %s, got %s",
			karpenterv1.ConsolidationPolicyWhenEmptyOrUnderutilized,
			nodePool.Spec.Disruption.ConsolidationPolicy)
	}

	// Verify NodeClassRef
	if nodePool.Spec.Template.Spec.NodeClassRef == nil {
		t.Error("NodeClassRef is nil")
	} else {
		if nodePool.Spec.Template.Spec.NodeClassRef.Group != "karpenter.sh" {
			t.Errorf("Expected NodeClassRef.Group karpenter.sh, got %s", nodePool.Spec.Template.Spec.NodeClassRef.Group)
		}
		if nodePool.Spec.Template.Spec.NodeClassRef.Kind != "EC2NodeClass" {
			t.Errorf("Expected NodeClassRef.Kind EC2NodeClass, got %s", nodePool.Spec.Template.Spec.NodeClassRef.Kind)
		}
		if nodePool.Spec.Template.Spec.NodeClassRef.Name != "default2" {
			t.Errorf("Expected NodeClassRef.Name default2, got %s", nodePool.Spec.Template.Spec.NodeClassRef.Name)
		}
	}

	// Verify requirements
	if len(nodePool.Spec.Template.Spec.Requirements) == 0 {
		t.Error("No requirements found in NodePool spec")
	} else {
		foundInstanceCategoryReq := false
		for _, req := range nodePool.Spec.Template.Spec.Requirements {
			if req.Key == "karpenter.k8s.aws/instance-category" && req.Operator == corev1.NodeSelectorOpIn {
				foundInstanceCategoryReq = true
				// Check if values match expected
				if len(req.Values) != len(expectedInstanceCategories) {
					t.Errorf("Expected %d instance categories, got %d", len(expectedInstanceCategories), len(req.Values))
				} else {
					valuesMap := make(map[string]bool)
					for _, v := range req.Values {
						valuesMap[v] = true
					}
					for _, expected := range expectedInstanceCategories {
						if !valuesMap[expected] {
							t.Errorf("Expected instance category %s not found in values", expected)
						}
					}
				}
				break
			}
		}
		if !foundInstanceCategoryReq {
			t.Error("Instance category requirement not found in NodePool spec")
		}
	}
}

// TestDevelopmentNodePoolCreation tests that a development NodePool is created correctly
func TestDevelopmentNodePoolCreation(t *testing.T) {
	feature := features.New("Development NodePool Creation Test").
		Assess("Development NodePool is created with correct spec", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			dynamicClient, err := createDynamicClient(cfg.KubeconfigFile())
			if err != nil {
				t.Fatalf("Failed to create dynamic client: %v", err)
			}

			// Wait for the XNodePool claim to be ready
			// Note: XNodePool is cluster-scoped
			claimName := "nodepool-default"
			t.Logf("Waiting for XNodePool claim %s to be ready", claimName)
			if err := waitForXNodePoolReady(dynamicClient, claimName); err != nil {
				t.Fatalf("XNodePool claim %s not ready: %v", claimName, err)
			}
			t.Logf("XNodePool claim %s is ready", claimName)

			// Wait a bit for the NodePool to be created
			time.Sleep(2 * time.Second)

			// Get the NodePool
			nodePoolName := claimName
			t.Logf("Retrieving NodePool %s", nodePoolName)
			nodePool, err := getNodePool(dynamicClient, nodePoolName)
			if err != nil {
				t.Fatalf("Failed to get NodePool %s: %v", nodePoolName, err)
			}

			t.Logf("NodePool %s retrieved successfully", nodePoolName)

			// Verify NodePool spec
			// Development should have: CPU 1000m, Memory 1000Mi, instance categories ["m"]
			// (mx-central-1 doesn't have c8g.16xlarge, so only "m" category)
			verifyNodePoolSpec(t, nodePool, "1000m", "1000Mi", []string{"m"})

			t.Log("Development NodePool spec verified successfully")
			return ctx
		}).Feature()

	testenv.Test(t, feature)
}

// TestProductionNodePoolCreation tests that a production NodePool is created correctly
func TestProductionNodePoolCreation(t *testing.T) {
	feature := features.New("Production NodePool Creation Test").
		Assess("Production NodePool is created with correct spec", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			dynamicClient, err := createDynamicClient(cfg.KubeconfigFile())
			if err != nil {
				t.Fatalf("Failed to create dynamic client: %v", err)
			}

			// Wait for the XNodePool claim to be ready
			// Note: XNodePool is cluster-scoped
			claimName := "nodepool-production"
			t.Logf("Waiting for XNodePool claim %s to be ready", claimName)
			if err := waitForXNodePoolReady(dynamicClient, claimName); err != nil {
				t.Fatalf("XNodePool claim %s not ready: %v", claimName, err)
			}
			t.Logf("XNodePool claim %s is ready", claimName)

			// Wait a bit for the NodePool to be created
			time.Sleep(2 * time.Second)

			// Get the NodePool
			nodePoolName := claimName
			t.Logf("Retrieving NodePool %s", nodePoolName)
			nodePool, err := getNodePool(dynamicClient, nodePoolName)
			if err != nil {
				t.Fatalf("Failed to get NodePool %s: %v", nodePoolName, err)
			}

			t.Logf("NodePool %s retrieved successfully", nodePoolName)

			// Verify NodePool spec
			// Production should have: CPU 2000m, Memory 2000Mi
			// Instance categories depend on region - if c8g.16xlarge is available, should be ["m", "c"]
			// Otherwise just ["m"]

			// Verify CPU and Memory limits
			if cpuLimit, ok := nodePool.Spec.Limits[corev1.ResourceCPU]; ok {
				expectedCPUQty := k8sresource.MustParse("2000m")
				if cpuLimit.Cmp(expectedCPUQty) != 0 {
					t.Errorf("Expected CPU limit 2000m, got %s", cpuLimit.String())
				}
			} else {
				t.Error("CPU limit not found in NodePool spec")
			}

			if memoryLimit, ok := nodePool.Spec.Limits[corev1.ResourceMemory]; ok {
				expectedMemoryQty := k8sresource.MustParse("2000Mi")
				if memoryLimit.Cmp(expectedMemoryQty) != 0 {
					t.Errorf("Expected Memory limit 2000Mi, got %s", memoryLimit.String())
				}
			} else {
				t.Error("Memory limit not found in NodePool spec")
			}

			// Verify disruption policy
			if nodePool.Spec.Disruption.ConsolidationPolicy != karpenterv1.ConsolidationPolicyWhenEmptyOrUnderutilized {
				t.Errorf("Expected consolidation policy %s, got %s",
					karpenterv1.ConsolidationPolicyWhenEmptyOrUnderutilized,
					nodePool.Spec.Disruption.ConsolidationPolicy)
			}

			// Verify NodeClassRef
			if nodePool.Spec.Template.Spec.NodeClassRef == nil {
				t.Error("NodeClassRef is nil")
			} else {
				if nodePool.Spec.Template.Spec.NodeClassRef.Group != "karpenter.sh" {
					t.Errorf("Expected NodeClassRef.Group karpenter.sh, got %s", nodePool.Spec.Template.Spec.NodeClassRef.Group)
				}
				if nodePool.Spec.Template.Spec.NodeClassRef.Kind != "EC2NodeClass" {
					t.Errorf("Expected NodeClassRef.Kind EC2NodeClass, got %s", nodePool.Spec.Template.Spec.NodeClassRef.Kind)
				}
				if nodePool.Spec.Template.Spec.NodeClassRef.Name != "default2" {
					t.Errorf("Expected NodeClassRef.Name default2, got %s", nodePool.Spec.Template.Spec.NodeClassRef.Name)
				}
			}

			// Verify instance categories - must include "m", may also include "c" depending on region
			if len(nodePool.Spec.Template.Spec.Requirements) == 0 {
				t.Error("No requirements found in NodePool spec")
			} else {
				foundInstanceCategoryReq := false
				for _, req := range nodePool.Spec.Template.Spec.Requirements {
					if req.Key == "karpenter.k8s.aws/instance-category" && req.Operator == corev1.NodeSelectorOpIn {
						foundInstanceCategoryReq = true
						hasM := false
						hasC := false
						for _, v := range req.Values {
							if v == "m" {
								hasM = true
							}
							if v == "c" {
								hasC = true
							}
						}
						if !hasM {
							t.Error("Instance category 'm' not found in production NodePool")
						}
						if len(req.Values) < 1 || len(req.Values) > 2 {
							t.Errorf("Expected 1-2 instance categories, got %d: %v", len(req.Values), req.Values)
						}
						if len(req.Values) == 2 && !hasC {
							t.Error("Expected instance category 'c' when 2 categories are present")
						}
						t.Logf("Production NodePool instance categories: %v", req.Values)
						break
					}
				}
				if !foundInstanceCategoryReq {
					t.Error("Instance category requirement not found in NodePool spec")
				}
			}

			t.Log("Production NodePool spec verified successfully")
			return ctx
		}).Feature()

	testenv.Test(t, feature)
}
