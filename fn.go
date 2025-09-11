package main

import (
	"context"

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
)

// Function returns whatever response you ask it to.
type Function struct {
	fnv1.UnimplementedFunctionRunnerServiceServer

	log logging.Logger
}

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

	// Set resource limits based on cxEnv
	var cpuLimit, memoryLimit string
	if in.CxEnv == "production" {
		cpuLimit = "2000m"
		memoryLimit = "2000Mi"
	} else {
		cpuLimit = "1000m"
		memoryLimit = "1000Mi"
	}

	// Create NodePool using Karpenter struct
	nodePool := &karpenterv1.NodePool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default",
		},
		Spec: karpenterv1.NodePoolSpec{
			Limits: karpenterv1.Limits{
				corev1.ResourceCPU:    k8sresource.MustParse(cpuLimit),
				corev1.ResourceMemory: k8sresource.MustParse(memoryLimit),
			},
			Disruption: karpenterv1.Disruption{
				ConsolidationPolicy: karpenterv1.ConsolidationPolicyWhenEmptyOrUnderutilized,
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
