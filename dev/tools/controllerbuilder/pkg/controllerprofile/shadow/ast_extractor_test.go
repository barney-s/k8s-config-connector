// Copyright 2026 Google LLC
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

package shadow

import (
	"path/filepath"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-config-connector/dev/tools/controllerbuilder/pkg/controllerprofile"
)

func TestExtractProfileFromSource(t *testing.T) {
	// Find root of repo
	testFile := filepath.Join("..", "..", "..", "..", "..", "..", "pkg", "controller", "direct", "securesourcemanager", "securesourcemanagerinstance_controller.go")
	profile, err := ExtractProfileFromSource(testFile)
	if err != nil {
		t.Fatalf("ExtractProfileFromSource failed: %v", err)
	}

	if profile.Kind != "SecureSourceManagerInstance" {
		t.Errorf("got Kind = %s, want SecureSourceManagerInstance", profile.Kind)
	}
	if profile.ProtoResource != "Instance" {
		t.Errorf("got ProtoResource = %s, want Instance", profile.ProtoResource)
	}
	if profile.Lifecycle.UpdateStyle != controllerprofile.UpdateStyleUnsupported {
		t.Errorf("got UpdateStyle = %s, want UpdateStyleUnsupported", profile.Lifecycle.UpdateStyle)
	}
	if !profile.Execution.Create.IsLRO {
		t.Errorf("expected Create.IsLRO to be true")
	}

	code, err := controllerprofile.RenderController(profile)
	if err != nil {
		t.Fatalf("RenderController failed: %v", err)
	}
	if len(code) == 0 {
		t.Fatalf("rendered code is empty")
	}
}

func TestExtractProfile_NetworkSecurityURLList(t *testing.T) {
	testFile := filepath.Join("..", "..", "..", "..", "..", "..", "pkg", "controller", "direct", "networksecurity", "networksecurityurllist_controller.go")
	profile, err := ExtractProfileFromSource(testFile)
	if err != nil {
		t.Fatalf("ExtractProfileFromSource failed: %v", err)
	}

	if profile.Kind != "NetworkSecurityURLList" {
		t.Errorf("got Kind = %s, want NetworkSecurityURLList", profile.Kind)
	}
	if profile.ProtoResource != "UrlList" {
		t.Errorf("got ProtoResource = %s, want UrlList", profile.ProtoResource)
	}
	if profile.Lifecycle.UpdateStyle != controllerprofile.UpdateStyleMask {
		t.Errorf("got UpdateStyle = %s, want UpdateStyleMask", profile.Lifecycle.UpdateStyle)
	}
	if !profile.Execution.Create.IsLRO {
		t.Errorf("expected Create.IsLRO to be true")
	}

	code, err := controllerprofile.RenderController(profile)
	if err != nil {
		t.Fatalf("RenderController failed: %v", err)
	}
	if len(code) == 0 {
		t.Fatalf("rendered code is empty")
	}
}
