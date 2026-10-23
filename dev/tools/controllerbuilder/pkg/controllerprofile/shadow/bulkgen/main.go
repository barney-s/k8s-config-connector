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
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoogleCloudPlatform/k8s-config-connector/dev/tools/controllerbuilder/pkg/controllerprofile"
	"github.com/GoogleCloudPlatform/k8s-config-connector/dev/tools/controllerbuilder/pkg/controllerprofile/shadow"
	"gopkg.in/yaml.v3"
)

func main() {
	writeFlag := flag.Bool("write", false, "Write generated controllers in place")
	yamlFlag := flag.Bool("yaml", false, "Write declarative YAML profile for traits")
	directDir := flag.String("dir", "pkg/controller/direct", "Path to direct controllers directory")
	serviceFilter := flag.String("service", "", "Filter to specific service name (optional)")
	targetFile := flag.String("file", "", "Specific controller file to generate (optional)")
	flag.Parse()

	var controllerFiles []string
	if *targetFile != "" {
		controllerFiles = append(controllerFiles, *targetFile)
	} else {
		err := filepath.WalkDir(*directDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, "_controller.go") && !strings.HasSuffix(path, "_test.go") {
				// Skip infrastructure packages that are not resource reconcilers
				if strings.Contains(path, "/directbase/") || strings.Contains(path, "/common/") ||
					strings.Contains(path, "/registry/") || strings.Contains(path, "/register/") ||
					strings.Contains(path, "/mappings/") || strings.Contains(path, "/debug/") {
					return nil
				}
				if *serviceFilter == "" || strings.Contains(path, "/"+*serviceFilter+"/") {
					controllerFiles = append(controllerFiles, path)
				}
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "error walking directory: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("Discovered %d controller files\n", len(controllerFiles))

	success := 0
	failed := 0

	for _, file := range controllerFiles {
		var err error
		var profile *controllerprofile.ControllerProfile
		yamlFile := strings.TrimSuffix(file, ".go") + "_profile.yaml"
		if _, statErr := os.Stat(yamlFile); statErr == nil {
			profile, err = controllerprofile.LoadFromYAML(yamlFile)
			if err != nil {
				fmt.Printf("[-] %s: yaml load error: %v\n", yamlFile, err)
			}
		}
		if profile == nil {
			profile, err = shadow.ExtractProfileFromSource(file)
			if err != nil {
				fmt.Printf("[-] %s: extract error: %v\n", file, err)
				failed++
				continue
			}
		}

		code, err := controllerprofile.RenderController(profile)
		if err != nil {
			fmt.Printf("[-] %s: render error: %v\n", file, err)
			failed++
			continue
		}

		if *writeFlag {
			if err := os.WriteFile(file, code, 0644); err != nil {
				fmt.Printf("[-] %s: write error: %v\n", file, err)
				failed++
				continue
			}
		}
		if *yamlFlag {
			yamlBytes, err := yaml.Marshal(profile)
			if err != nil {
				fmt.Printf("[-] %s: yaml marshal error: %v\n", file, err)
			} else {
				yamlFile := strings.TrimSuffix(file, ".go") + "_profile.yaml"
				if err := os.WriteFile(yamlFile, yamlBytes, 0644); err != nil {
					fmt.Printf("[-] %s: yaml write error: %v\n", file, err)
				}
			}
		}
		success++
	}

	fmt.Printf("\n=== Bulk Generation Completed ===\n")
	fmt.Printf("Total: %d | Success: %d | Failed: %d\n", len(controllerFiles), success, failed)
	if *writeFlag {
		fmt.Printf("Files updated in place!\n")
	} else {
		fmt.Printf("Dry run mode (use --write to overwrite files in place)\n")
	}
}
