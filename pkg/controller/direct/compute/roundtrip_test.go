// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package compute

import (
	"math/rand"
	"testing"

	pb "cloud.google.com/go/compute/apiv1/computepb"

	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/controller/direct"
	"github.com/GoogleCloudPlatform/k8s-config-connector/pkg/test/fuzz"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/testing/protocmp"
	"k8s.io/apimachinery/pkg/util/sets"
)

func FuzzComputeForwardingRuleSpec(f *testing.F) {
	f.Fuzz(func(t *testing.T, seed int64) {
		randStream := rand.New(rand.NewSource(seed))

		p1 := &pb.ForwardingRule{}
		fuzz.FillWithRandom(t, randStream, p1)

		// We don't expect output fields to round-trip
		outputFields := sets.New(".etag")

		// A few fields are not implemented yet in KRM, don't test them
		unimplementedFields := sets.New[string]()

		// Status fields
		unimplementedFields.Insert(".creation_timestamp")
		unimplementedFields.Insert(".fingerprint")
		unimplementedFields.Insert(".label_fingerprint")
		unimplementedFields.Insert(".psc_connection_id")
		unimplementedFields.Insert(".psc_connection_status")
		unimplementedFields.Insert(".self_link")
		unimplementedFields.Insert(".service_name")
		// Not supported in KRM
		unimplementedFields.Insert(".base_forwarding_rule")
		unimplementedFields.Insert(".id")
		unimplementedFields.Insert(".ip_collection")
		unimplementedFields.Insert(".kind")
		unimplementedFields.Insert(".labels")
		unimplementedFields.Insert(".name")
		unimplementedFields.Insert(".service_directory_registrations", ".service_directory_region")
		// KCC uses `location` instead of `region`
		unimplementedFields.Insert(".region")

		// Skip target field in fuzz test.
		// ComputeForwardingRuleSpec_Target_FromProto verifies the conversion of input strings into specified outputs.
		// This fuzz test uses randomly generated strings as inputs, which may result the test to fail.
		// We can verify this field by fixtures tests.
		unimplementedFields.Insert(".target")

		// Remove any output only or known-unimplemented fields
		clearFields := &fuzz.ClearFields{
			Paths: unimplementedFields.Union(outputFields),
		}
		fuzz.Visit("", p1.ProtoReflect(), nil, clearFields)

		r := &fuzz.ReplaceFields{}
		r.Func = func(path string, val protoreflect.Value) (protoreflect.Value, bool) {
			// TODO: Any values that must follow a pattern
			return protoreflect.Value{}, false
		}
		fuzz.Visit("", p1.ProtoReflect(), nil, r)

		ctx := &direct.MapContext{}
		k := ComputeForwardingRuleSpec_FromProto(ctx, p1)
		if ctx.Err() != nil {
			t.Fatalf("error mapping from proto to krm: %v", ctx.Err())
		}

		p2 := ComputeForwardingRuleSpec_ToProto(ctx, k)
		if ctx.Err() != nil {
			t.Fatalf("error mapping from krm to proto: %v", ctx.Err())
		}

		if diff := cmp.Diff(p1, p2, protocmp.Transform()); diff != "" {
			t.Logf("p1 = %v", prototext.Format(p1))
			t.Logf("p2 = %v", prototext.Format(p2))
			t.Errorf("roundtrip failed; diff:\n%s", diff)
		}
	})
}