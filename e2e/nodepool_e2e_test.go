package e2e

import (
	"context"
	"testing"
	"time"

	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

// TestNodePoolCreationAndContent tests that a development NodePool is created correctly
func TestNodePoolCreationAndContent(t *testing.T) {
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
