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
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-config-connector/dev/tools/controllerbuilder/pkg/controllerprofile"
)

// ExtractProfileFromSource parses an existing direct controller file and constructs
// a faithful ControllerProfile capturing its exact types, SDK clients, and traits.
func ExtractProfileFromSource(filePath string) (*controllerprofile.ControllerProfile, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", filePath, err)
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing file %s: %w", filePath, err)
	}

	rawText := string(content)

	// Determine service from path (e.g. pkg/controller/direct/securesourcemanager/...)
	relParts := strings.Split(filepath.ToSlash(filePath), "/")
	kccService := ""
	for i, part := range relParts {
		if part == "direct" && i+1 < len(relParts) {
			kccService = relParts[i+1]
			break
		}
	}
	if kccService == "" {
		kccService = file.Name.Name
	}

	profile := &controllerprofile.ControllerProfile{
		Package:    file.Name.Name,
		KCCService: kccService,
		KCCVersion: "v1beta1", // default, may be overridden by krm import
	}

	// 1. Inspect Imports
	var krmImport, gcpImport, pbImport string
	var pbAlias string

	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		alias := ""
		if imp.Name != nil {
			alias = imp.Name.Name
		}

		if strings.Contains(importPath, "apis/") {
			if alias == "krm" || strings.HasSuffix(alias, "krm") || krmImport == "" {
				krmImport = importPath
				// Extract version if present, e.g. apis/securesourcemanager/v1beta1
				parts := strings.Split(importPath, "/")
				if len(parts) >= 2 {
					last := parts[len(parts)-1]
					if strings.HasPrefix(last, "v1") {
						profile.KCCVersion = last
					}
				}
			}
		} else if strings.Contains(importPath, "cloud.google.com/go/") {
			if strings.HasSuffix(importPath, "pb") {
				pbImport = importPath
				if alias != "" {
					pbAlias = alias
				} else {
					parts := strings.Split(importPath, "/")
					pbAlias = parts[len(parts)-1]
				}
			} else {
				gcpImport = importPath
			}
		}
	}

	if gcpImport == "" && pbImport != "" {
		lastSlash := strings.LastIndex(pbImport, "/")
		if lastSlash != -1 {
			gcpImport = pbImport[:lastSlash]
		}
	}

	if krmImport == "" || gcpImport == "" || pbImport == "" {
		return nil, fmt.Errorf("non-GAPIC/proto controller (krm=%q, gcp=%q, pb=%q)", krmImport, gcpImport, pbImport)
	}

	profile.Transport.GCPClientImportPath = gcpImport
	profile.Transport.PBImportPath = pbImport
	if pbAlias == "" {
		pbAlias = "pb"
	}
	profile.Transport.PBPackageAlias = pbAlias

	// 2. Discover GVK and Kind
	reGVK := regexp.MustCompile(`RegisterModel\((?:krm\.)?([A-Za-z0-9]+)GVK`)
	if match := reGVK.FindStringSubmatch(rawText); len(match) > 1 {
		profile.Kind = match[1]
	} else {
		// Fallback from filename
		base := filepath.Base(filePath)
		base = strings.TrimSuffix(base, "_controller.go")
		profile.Kind = strings.Title(base)
	}

	// 3. Discover Client Type & Constructor
	reConstructor := regexp.MustCompile(`(gcp\.New[A-Za-z0-9]*(?:RESTClient|Client))\(`)
	if match := reConstructor.FindStringSubmatch(rawText); len(match) > 1 {
		profile.Transport.GCPClientConstructor = match[1]
	} else {
		profile.Transport.GCPClientConstructor = "gcp.NewRESTClient"
	}

	reClientType := regexp.MustCompile(`gcpClient\s+(\*gcp\.[A-Za-z0-9]+Client|\*gcp\.Client)`)
	if match := reClientType.FindStringSubmatch(rawText); len(match) > 1 {
		profile.Transport.GCPClientType = match[1]
	} else {
		profile.Transport.GCPClientType = "*gcp.Client"
	}

	// 4. Discover Identity Trait
	reIdentityType := regexp.MustCompile(`\(\*(krm\.[A-Za-z0-9]+Identity)\)`)
	if match := reIdentityType.FindStringSubmatch(rawText); len(match) > 1 {
		profile.Identity.TypeName = match[1]
	} else {
		reAdapterID := regexp.MustCompile(`id\s+\*([A-Za-z0-9_.]*Identity)`)
		if match := reAdapterID.FindStringSubmatch(rawText); len(match) > 1 {
			profile.Identity.TypeName = match[1]
		} else {
			profile.Identity.TypeName = "krm." + profile.Kind + "Identity"
		}
	}

	profile.Identity.Strategy = controllerprofile.IdentityClientField
	profile.Identity.NameField = "Name"
	profile.Identity.UseGetIdentity = strings.Contains(rawText, "GetIdentity(")
	profile.Identity.HasFromExternal = strings.Contains(rawText, ".FromExternal(")
	if strings.Contains(rawText, "a.id.Parent().String()") {
		profile.Identity.ParentStringExpr = "Parent().String()"
	} else {
		profile.Identity.ParentStringExpr = "ParentString()"
	}
	profile.Identity.HasProjectRef = strings.Contains(rawText, "Spec.ProjectRef")
	profile.Identity.HasLocation = strings.Contains(rawText, "Spec.Location")
	profile.Identity.HasResourceID = strings.Contains(rawText, "Spec.ResourceID")

	// Determine ProtoResource from Kind and method calls
	protoResource := strings.TrimPrefix(profile.Kind, profile.KCCService)
	if protoResource == "" {
		protoResource = profile.Kind
	}
	// Verify from Get/Create methods if found
	reGetMethod := regexp.MustCompile(`gcpClient\.(Get[A-Za-z0-9]+)\(`)
	if match := reGetMethod.FindStringSubmatch(rawText); len(match) > 1 {
		profile.Lifecycle.GetMethodName = match[1]
		protoResourceCandidate := strings.TrimPrefix(match[1], "Get")
		if protoResourceCandidate != "" {
			protoResource = protoResourceCandidate
		}
	}
	profile.ProtoResource = protoResource

	// Discover ID field on identity (e.g. a.id.Instance or a.id.ID())
	if strings.Contains(rawText, "a.id.ID()") {
		profile.Identity.IDExpr = "ID()"
	} else {
		reIDExpr := regexp.MustCompile(`a\.id\.([A-Za-z0-9]+)`)
		idMatches := reIDExpr.FindAllStringSubmatch(rawText, -1)
		for _, m := range idMatches {
			if len(m) > 1 && m[1] != "String" && m[1] != "ParentString" && m[1] != "Parent" && m[1] != "Project" && m[1] != "Location" && m[1] != "ID" {
				profile.Identity.IDExpr = m[1]
				break
			}
		}
		if profile.Identity.IDExpr == "" {
			profile.Identity.IDExpr = protoResource
		}
	}

	// 5. Discover Lifecycle & Execution Traits
	profile.Lifecycle.HasGet = strings.Contains(rawText, "func (a *") && strings.Contains(rawText, ") Find(")
	if profile.Lifecycle.GetMethodName == "" {
		profile.Lifecycle.GetMethodName = "Get" + protoResource
	}
	profile.Lifecycle.GetRequestType = profile.Lifecycle.GetMethodName + "Request"
	profile.Lifecycle.GetNameField = "Name"

	// Create
	profile.Lifecycle.CanCreate = strings.Contains(rawText, ") Create(")
	profile.Lifecycle.CreateMethodName = "Create" + protoResource
	profile.Lifecycle.CreateRequestType = "Create" + protoResource + "Request"
	profile.Lifecycle.CreateParentField = "Parent"
	profile.Lifecycle.CreateBodyField = protoResource
	profile.Lifecycle.CreateIDField = protoResource + "Id"

	// Discover specific ID field name in Create request if present
	reCreateID := regexp.MustCompile(`([A-Za-z0-9]+Id)\s*:\s*a\.id`)
	if match := reCreateID.FindStringSubmatch(rawText); len(match) > 1 {
		profile.Lifecycle.CreateIDField = match[1]
	}

	// Update Style
	if strings.Contains(rawText, "Update" + protoResource) {
		profile.Lifecycle.UpdateMethodName = "Update" + protoResource
		profile.Lifecycle.UpdateRequestType = "Update" + protoResource + "Request"
		profile.Lifecycle.UpdateBodyField = protoResource
		if strings.Contains(rawText, "UpdateMask") {
			profile.Lifecycle.UpdateStyle = controllerprofile.UpdateStyleMask
			profile.Lifecycle.UpdateMaskField = "UpdateMask"
		} else {
			profile.Lifecycle.UpdateStyle = controllerprofile.UpdateStyleFullReplacement
		}
	} else if strings.Contains(rawText, "is immutable") || strings.Contains(rawText, "cannot be updated") {
		profile.Lifecycle.UpdateStyle = controllerprofile.UpdateStyleUnsupported
	} else {
		profile.Lifecycle.UpdateStyle = controllerprofile.UpdateStyleUnsupported
	}

	// Delete Style
	if strings.Contains(rawText, "Delete"+protoResource) {
		profile.Lifecycle.DeleteStyle = controllerprofile.DeleteStyleStandard
		profile.Lifecycle.DeleteMethodName = "Delete" + protoResource
		profile.Lifecycle.DeleteRequestType = "Delete" + protoResource + "Request"
		profile.Lifecycle.DeleteNameField = "Name"
	} else {
		profile.Lifecycle.DeleteStyle = controllerprofile.DeleteStyleUnsupported
	}

	// Execution Traits (LRO vs Sync)
	createIsLRO := strings.Contains(rawText, "op, err := a.gcpClient.Create") || strings.Contains(rawText, "op.Wait(ctx)")
	deleteIsLRO := strings.Contains(rawText, "op, err := a.gcpClient.Delete") ||
		strings.Contains(rawText, "_, err := a.gcpClient.Delete") ||
		strings.Contains(rawText, "op.Wait(ctx)")
	profile.Execution.Create = controllerprofile.OpExecution{IsLRO: createIsLRO, IsEmpty: false}
	profile.Execution.Update = controllerprofile.OpExecution{IsLRO: createIsLRO, IsEmpty: false}
	profile.Execution.Delete = controllerprofile.OpExecution{IsLRO: deleteIsLRO, IsEmpty: true}

	// Transport Traits
	profile.Transport.HasReferences = strings.Contains(rawText, "NormalizeReferences")
	profile.Transport.HasObservedState = strings.Contains(rawText, "ObservedState")
	profile.Transport.HasLabels = strings.Contains(rawText, "Labels")
	profile.Transport.LabelDiffing = controllerprofile.LabelsNone
	profile.Transport.UseGRPCClient = strings.Contains(rawText, "GRPCClientOptions") || (!strings.HasSuffix(profile.Transport.GCPClientConstructor, "RESTClient") && strings.HasSuffix(profile.Transport.GCPClientConstructor, "Client"))

	reMapper := regexp.MustCompile(profile.Kind + `Spec(_[A-Za-z0-9]+)?_ToProto`)
	if match := reMapper.FindStringSubmatch(rawText); len(match) > 1 {
		profile.Transport.MapperVersionSuffix = match[1]
	}

	return profile, nil
}
