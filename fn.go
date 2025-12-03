package main

import (
	"context"
	"encoding/json"

	"github.com/crossplane/function-nodepools/input/v1beta1"
	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/resource/composed"
	"github.com/crossplane/function-sdk-go/response"
	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	ec2v1alpha1 "github.com/Oded-B/ec2offering-crossplane-provider/apis/ec2/v1alpha1"
)

// Function returns whatever response you ask it to.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer

	log logging.Logger
}

// This function checks if a specific instance type exists in InstanceTypeOffering object
func doesInstanceTypeExists(instanceType string, offering *ec2v1alpha1.InstanceTypeOffering) bool {
	if offering == nil || offering.Status.AtProvider.InstanceTypeOfferings == nil {
		return false
	}
	for _, offeringItem := range offering.Status.AtProvider.InstanceTypeOfferings {
		if offeringItem.InstanceType == instanceType {
			return true
		}
	}
	return false
}

// getInstanceTypeOfferingFromExtraResources extracts InstanceTypeOffering from extra resources
func (f *Function) getInstanceTypeOfferingFromExtraResources(req *fnv1.RunFunctionRequest) (*ec2v1alpha1.InstanceTypeOffering, error) {
	// extraResources, err := request.GetExtraResources(req)
	extraResources, err := request.GetRequiredResources(req)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get extra resources from %T", req)
	}

	if len(extraResources) == 0 {
		return nil, errors.New("no extra resources found")
	}

	for name, extras := range extraResources {
		// Iterate through the slice of Extra resources
		for _, extra := range extras {
			if extra.Resource == nil {
				continue
			}

			// Check if this resource is of kind InstanceTypeOffering
			jsonBytes, err := json.Marshal(extra.Resource.Object)
			if err != nil {
				f.log.Info("Failed to marshal resource to JSON", "name", name, "error", err)
				continue
			}

			// Try to unmarshal as InstanceTypeOffering to check if it's the right type
			offering := &ec2v1alpha1.InstanceTypeOffering{}
			err = json.Unmarshal(jsonBytes, offering)
			if err != nil {
				f.log.Info("Failed to unmarshal resource to InstanceTypeOffering", "name", name, "error", err)
				continue
			}

			// Verify that this is actually an InstanceTypeOffering by checking the kind
			if offering.Kind == "InstanceTypeOffering" {
				f.log.Info("Found InstanceTypeOffering resource", "name", name)
				return offering, nil
			}
		}
	}
	return nil, errors.New("no InstanceTypeOffering resource found in extra resources")
}

// determineInstanceCategories determines which instance categories to use based on availability
func (f *Function) determineInstanceCategories(instanceOffering *ec2v1alpha1.InstanceTypeOffering, awsRegion string) []string {
	usedInstanceCategories := []string{"m"}
	checkInstanceType := "c8g.16xlarge"

	if doesInstanceTypeExists(checkInstanceType, instanceOffering) {
		f.log.Info(checkInstanceType + " instance type is available in " + awsRegion)
		usedInstanceCategories = append(usedInstanceCategories, "c")
	} else {
		f.log.Info(checkInstanceType + " instance type is not available in " + awsRegion + ", using default")
	}

	return usedInstanceCategories
}

// getResourceLimits returns CPU and memory limits based on environment
func getResourceLimits(cxEnv string) (string, string) {
	if cxEnv == "production" {
		return "2000m", "2000Mi"
	}
	return "1000m", "1000Mi"
}

//	func getRequiredResource[R any](rsp *fnv1.RunFunctionResponse, req *fnv1.RunFunctionRequest, selector *fnv1.ResourceSelector) (*R, error) {
//		key := fmt.Sprintf("%s/%s", selector.GetKind(), selector.GetMatchName())
//
//		if rsp.GetRequirements() == nil {
//			rsp.Requirements = &fnv1.Requirements{}
//		}
//		if rsp.GetRequirements().GetResources() == nil {
//			rsp.Requirements.Resources = make(map[string]*fnv1.ResourceSelector)
//		}
//		rsp.Requirements.Resources[key] = selector
//
//		requiredResources, err := request.GetRequiredResources(req)
//		if err != nil {
//			return nil, errors.Wrapf(err, "cannot get requiredResources resources with secret")
//		}
//		rr, ok := requiredResources[key]
//		if !ok {
//			return nil, nil
//		}
//
//		if len(rr) > 1 {
//			return nil, errors.Errorf("Too many resources returned")
//		}
//
//		var rs R
//		if err = runtime.DefaultUnstructuredConverter.
//			FromUnstructured(rr[0].Resource.Object, &rs); err != nil {
//			return nil, errors.Wrapf(err, "cannot convert Secret")
//		}
//		return &rs, nil
//	}
//
// RunFunction runs the Function.
func (f *Function) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	f.log.Info("Running function", "tag", req.GetMeta().GetTag())

	rsp := response.To(req, response.DefaultTTL)

	in := &v1beta1.Input{}
	if err := request.GetInput(req, in); err != nil {
		// You can set a custom status condition on the claim. This allows you to
		// communicate with the user. See the link below for status condition
		// guidance.
		// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
		response.ConditionFalse(rsp, "FunctionSuccess", "InternalError").
			WithMessage("Something went wrong.").
			TargetCompositeAndClaim()

		// You can emit an event regarding the claim. This allows you to communicate
		// with the user. Note that events should be used sparingly and are subject
		// to throttling; see the issue below for more information.
		// https://github.com/crossplane/crossplane/issues/5802
		response.Warning(rsp, errors.New("something went wrong")).
			TargetCompositeAndClaim()

		response.Fatal(rsp, errors.Wrapf(err, "cannot get Function input from %T", req))
		return rsp, nil
	}

	// TODO: Add your Function logic here!
	response.Normalf(rsp, "I was run with input %q!", in.Example)
	f.log.Info("I was run!", "input", in.Example)

	// Get desired composed resources and add the NodePool
	desired, err := request.GetDesiredComposedResources(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get desired resources from %T", req))
		return rsp, nil
	}

	xr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get observed composite resource from %T", req))
		return rsp, nil
	}

	// Get InstanceTypeOffering from extra resources
	instanceOffering, err := f.getInstanceTypeOfferingFromExtraResources(req)
	if err != nil {
		response.Fatal(rsp, err)
		return rsp, nil
	}

	cxEnv, err := xr.Resource.GetString("spec.CxEnv")
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot read spec.CxEnv field of %s", xr.Resource.GetKind()))
		return rsp, nil
	}

	xrName, err := xr.Resource.GetString("metadata.name")
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot read metadata.name field of %s", xr.Resource.GetKind()))
		return rsp, nil
	}

	awsRegion, err := xr.Resource.GetString("spec.AwsRegion")
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot read spec.AwsRegion field of %s", xr.Resource.GetKind()))
		return rsp, nil
	}

	// Determine instance categories based on availability
	usedInstanceCategories := f.determineInstanceCategories(instanceOffering, awsRegion)

	// Set resource limits based on cxEnv from XR
	cpuLimit, memoryLimit := getResourceLimits(cxEnv)

	// Create NodePool using Karpenter struct
	nodePool := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: xrName,
		},
		Spec: karpenterv1.NodePoolSpec{
			Limits: karpenterv1.Limits{
				corev1.ResourceCPU:    k8sresource.MustParse(cpuLimit),
				corev1.ResourceMemory: k8sresource.MustParse(memoryLimit),
			},
			Disruption: karpenterv1.Disruption{
				ConsolidationPolicy: karpenterv1.ConsolidationPolicyWhenEmptyOrUnderutilized,
			},
			Template: karpenterv1.NodeClaimTemplate{
				Spec: karpenterv1.NodeClaimTemplateSpec{
					NodeClassRef: &karpenterv1.NodeClassReference{
						Group: "karpenter.sh",
						Kind:  "EC2NodeClass",
						Name:  "default2",
					},
					Requirements: []karpenterv1.NodeSelectorRequirementWithMinValues{
						{
							NodeSelectorRequirement: corev1.NodeSelectorRequirement{
								Key:      "karpenter.k8s.aws/instance-category",
								Operator: "In",
								Values:   usedInstanceCategories,
							},
						},
					},
				},
			},
		},
	}

	karpenterSchemeGroupVersion := schema.GroupVersion{
		Group:   "karpenter.sh",
		Version: "v1",
	}

	composed.Scheme.AddKnownTypes(karpenterSchemeGroupVersion, &karpenterv1.NodePool{})
	// Convert NodePool to composed.Unstructured
	nodePoolResource, err := composed.From(nodePool)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot convert %T to %T", nodePool, &composed.Unstructured{}))
		return rsp, nil
	}

	// Add the NodePool to desired composed resources
	desired[resource.Name("nodepool")] = &resource.DesiredComposed{Resource: nodePoolResource}

	// Set the desired composed resources in the response
	if err := response.SetDesiredComposedResources(rsp, desired); err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot set desired composed resources in %T", rsp))
		return rsp, nil
	}

	// You can set a custom status condition on the claim. This allows you to
	// communicate with the user. See the link below for status condition
	// guidance.
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties
	response.ConditionTrue(rsp, "FunctionSuccess", "Success").
		TargetCompositeAndClaim()

	return rsp, nil
}
