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

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-config-connector/dev/tools/controllerbuilder/pkg/controllerprofile"
	"github.com/GoogleCloudPlatform/k8s-config-connector/dev/tools/controllerbuilder/pkg/controllerprofile/shadow"
	"gopkg.in/yaml.v3"
)

type Result struct {
	Tier1 []string          `json:"tier1"`
	Tier2 map[string]string `json:"tier2"`
}

func main() {
	writeYaml := flag.Bool("yaml", true, "Write YAML trait profile for Tier 1 controllers")
	directDir := flag.String("dir", "pkg/controller/direct", "Path to direct controllers directory")
	flag.Parse()

	var controllerFiles []string
	err := filepath.WalkDir(*directDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, "_controller.go") && !strings.HasSuffix(path, "_test.go") {
			if strings.Contains(path, "/directbase/") || strings.Contains(path, "/common/") ||
				strings.Contains(path, "/registry/") || strings.Contains(path, "/register/") ||
				strings.Contains(path, "/mappings/") || strings.Contains(path, "/debug/") {
				return nil
			}
			controllerFiles = append(controllerFiles, path)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error walking directory: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Discovered %d controller files\n", len(controllerFiles))

	result := Result{
		Tier1: []string{},
		Tier2: make(map[string]string),
	}

	for i, file := range controllerFiles {
		origCode, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var profile *controllerprofile.ControllerProfile
		yamlFile := strings.TrimSuffix(file, ".go") + "_profile.yaml"
		if _, err := os.Stat(yamlFile); err == nil {
			profile, err = controllerprofile.LoadFromYAML(yamlFile)
			if err != nil {
				fmt.Printf("[-] %s: yaml load error: %v\n", yamlFile, err)
			}
		}
		if profile == nil {
			profile, err = shadow.ExtractProfileFromSource(file)
			if err != nil {
				result.Tier2[file] = fmt.Sprintf("extract error: %v", err)
				continue
			}
		}

		genBytes, err := controllerprofile.RenderController(profile)
		if err != nil {
			result.Tier2[file] = fmt.Sprintf("render error: %v", err)
			continue
		}

		// Write generated code temporarily
		if err := os.WriteFile(file, genBytes, 0644); err != nil {
			result.Tier2[file] = fmt.Sprintf("write error: %v", err)
			continue
		}

		// Test build package
		pkgDir := filepath.Dir(file)
		cmd := exec.Command("go", "build", "./"+pkgDir+"/...")
		out, buildErr := cmd.CombinedOutput()
		if buildErr == nil {
			// Tier 1 controller!
			result.Tier1 = append(result.Tier1, file)
			fmt.Printf("[%d/%d] [+] TIER 1: %s\n", i+1, len(controllerFiles), file)
			if *writeYaml {
				yamlBytes, _ := yaml.Marshal(profile)
				yamlFile := strings.TrimSuffix(file, ".go") + "_profile.yaml"
				_ = os.WriteFile(yamlFile, yamlBytes, 0644)
			}
		} else {
			// Tier 2 controller -> revert original code!
			actualErr := ""
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					actualErr = line
					break
				}
			}
			result.Tier2[file] = actualErr
			_ = os.WriteFile(file, origCode, 0644)
			fmt.Printf("[%d/%d] [-] Tier 2: %s (%s)\n", i+1, len(controllerFiles), file, actualErr)
		}
	}

	fmt.Printf("\n=== Evaluation Complete ===\n")
	fmt.Printf("Total Controllers Tested: %d\n", len(controllerFiles))
	fmt.Printf("Tier 1 (Generated & Compiling): %d\n", len(result.Tier1))
	fmt.Printf("Tier 2 (Specialized / Devised):  %d\n", len(result.Tier2))

	resData, _ := json.MarshalIndent(result, "", "  ")
	_ = os.WriteFile("tier1_report.json", resData, 0644)
}
