package main

import (
	"context"
	"testing"

	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/resource/composed"
	"github.com/crossplane/function-sdk-go/response"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	karpenterv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

// testLogSink implements logr.LogSink for testing
type testLogSink struct {
	t *testing.T
}

func (s *testLogSink) Init(info logr.RuntimeInfo) {}
func (s *testLogSink) Enabled(level int) bool     { return true }
func (s *testLogSink) Info(level int, msg string, keysAndValues ...interface{}) {
	s.t.Logf("[FUNCTION] %s %v", msg, keysAndValues)
}

func (s *testLogSink) Error(err error, msg string, keysAndValues ...interface{}) {
	s.t.Logf("[FUNCTION ERROR] %s: %v %v", msg, err, keysAndValues)
}

func (s *testLogSink) WithValues(keysAndValues ...interface{}) logr.LogSink {
	return s
}

func (s *testLogSink) WithName(name string) logr.LogSink {
	return s
}

func TestRunFunction(t *testing.T) {
	type args struct {
		ctx context.Context
		req *fnv1.RunFunctionRequest
	}
	type want struct {
		rsp *fnv1.RunFunctionResponse
		err error
	}

	// TODO seperate region and env tests

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"ResponseIsReturned": {
			reason: "The Function should return a fatal result if no input was specified",
			args: args{
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "template.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"example": "Hello, world"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
                "apiVersion": "example.crossplane.io/v1alpha1",
                "kind": "XNodePool",
                "metadata": {
                  "name": "np1"
                },
                "spec": {
                  "CxEnv": "development",
                  "AwsRegion": "af-south-1"
                }
              }`),
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  "I was run with input \"Hello, world\"!",
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Desired: func() *fnv1.State {
						// Create NodePool using Karpenter struct
						nodePool := &karpenterv1.NodePool{
							ObjectMeta: metav1.ObjectMeta{
								Name: "np1",
							},
							Spec: karpenterv1.NodePoolSpec{
								Limits: karpenterv1.Limits{
									corev1.ResourceCPU:    k8sresource.MustParse("1000m"),
									corev1.ResourceMemory: k8sresource.MustParse("1000Mi"),
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
													Values:   []string{"m"},
												},
											},
										},
									},
								},
							},
						}

						schemeGroupVersion := schema.GroupVersion{
							Group:   "karpenter.sh",
							Version: "v1",
						}

						composed.Scheme.AddKnownTypes(schemeGroupVersion, &karpenterv1.NodePool{})
						// Convert NodePool to composed.Unstructured
						nodePoolResource, err := composed.From(nodePool)
						if err != nil {
							t.Fatalf("cannot convert %T to %T: %v", nodePool, &composed.Unstructured{}, err)
						}

						// Convert to structpb.Struct for the test
						nodePoolStruct, err := resource.AsStruct(nodePoolResource)
						if err != nil {
							t.Fatalf("cannot convert %T to structpb.Struct: %v", nodePoolResource, err)
						}

						return &fnv1.State{
							Resources: map[string]*fnv1.Resource{
								"nodepool": {
									Resource: nodePoolStruct,
								},
							},
						}
					}(),
				},
			},
		},
		"ProductionEnvironment": {
			reason: "The Function should use production resource limits when cxEnv is production",
			args: args{
				req: &fnv1.RunFunctionRequest{
					Meta: &fnv1.RequestMeta{Tag: "hello"},
					Input: resource.MustStructJSON(`{
						"apiVersion": "template.fn.crossplane.io/v1beta1",
						"kind": "Input",
						"example": "Hello, world"
					}`),
					Observed: &fnv1.State{
						Composite: &fnv1.Resource{
							Resource: resource.MustStructJSON(`{
                "apiVersion": "example.crossplane.io/v1alpha1",
                "kind": "XNodePool",
                "metadata": {
                  "name": "np1"
                },
                "spec": {
                  "CxEnv": "production",
                  "AwsRegion": "us-east-1"
                }
              }`),
						},
					},
				},
			},
			want: want{
				rsp: &fnv1.RunFunctionResponse{
					Meta: &fnv1.ResponseMeta{Tag: "hello", Ttl: durationpb.New(response.DefaultTTL)},
					Results: []*fnv1.Result{
						{
							Severity: fnv1.Severity_SEVERITY_NORMAL,
							Message:  "I was run with input \"Hello, world\"!",
							Target:   fnv1.Target_TARGET_COMPOSITE.Enum(),
						},
					},
					Conditions: []*fnv1.Condition{
						{
							Type:   "FunctionSuccess",
							Status: fnv1.Status_STATUS_CONDITION_TRUE,
							Reason: "Success",
							Target: fnv1.Target_TARGET_COMPOSITE_AND_CLAIM.Enum(),
						},
					},
					Desired: func() *fnv1.State {
						// Create NodePool using Karpenter struct with production limits
						nodePool := &karpenterv1.NodePool{
							ObjectMeta: metav1.ObjectMeta{
								Name: "np1",
							},
							Spec: karpenterv1.NodePoolSpec{
								Limits: karpenterv1.Limits{
									corev1.ResourceCPU:    k8sresource.MustParse("2000m"),
									corev1.ResourceMemory: k8sresource.MustParse("2000Mi"),
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
													Values:   []string{"m", "c"},
												},
											},
										},
									},
								},
							},
						}

						// - key: karpenter.k8s.aws/instance-category
						//   operator: In
						//   values:
						//   - c
						//   - m

						schemeGroupVersion := schema.GroupVersion{
							Group:   "karpenter.sh",
							Version: "v1",
						}

						composed.Scheme.AddKnownTypes(schemeGroupVersion, &karpenterv1.NodePool{})
						// Convert NodePool to composed.Unstructured
						nodePoolResource, err := composed.From(nodePool)
						if err != nil {
							t.Fatalf("cannot convert %T to %T: %v", nodePool, &composed.Unstructured{}, err)
						}

						// Convert to structpb.Struct for the test
						nodePoolStruct, err := resource.AsStruct(nodePoolResource)
						if err != nil {
							t.Fatalf("cannot convert %T to structpb.Struct: %v", nodePoolResource, err)
						}

						return &fnv1.State{
							Resources: map[string]*fnv1.Resource{
								"nodepool": {
									Resource: nodePoolStruct,
								},
							},
						}
					}(),
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Create a verbose logger for testing
			logger := logr.New(&testLogSink{t: t})
			f := &Function{log: logging.NewLogrLogger(logger)}
			ctx := context.Background()
			rsp, err := f.RunFunction(ctx, tc.args.req)

			if diff := cmp.Diff(tc.want.rsp, rsp, protocmp.Transform()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want rsp, +got rsp:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("%s\nf.RunFunction(...): -want err, +got err:\n%s", tc.reason, diff)
			}
		})
	}
}
