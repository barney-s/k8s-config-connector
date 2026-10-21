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
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoogleCloudPlatform/k8s-config-connector/dev/tools/controllerbuilder/pkg/controllerprofile"
)

func TestBulkGenerateAllControllers(t *testing.T) {
	directDir := filepath.Join("..", "..", "..", "..", "..", "..", "pkg", "controller", "direct")
	
	var controllerFiles []string
	err := filepath.WalkDir(directDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, "_controller.go") && !strings.HasSuffix(path, "_test.go") {
			controllerFiles = append(controllerFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking direct dir: %v", err)
	}

	t.Logf("Found %d direct controller files to evaluate and generate", len(controllerFiles))

	successCount := 0
	failedCount := 0
	var failureMessages []string

	for _, file := range controllerFiles {
		profile, err := ExtractProfileFromSource(file)
		if err != nil {
			failedCount++
			failureMessages = append(failureMessages, fmt.Sprintf("%s: extract failed: %v", filepath.Base(file), err))
			continue
		}

		code, err := controllerprofile.RenderController(profile)
		if err != nil {
			failedCount++
			failureMessages = append(failureMessages, fmt.Sprintf("%s: render failed: %v", filepath.Base(file), err))
			continue
		}

		if len(code) == 0 {
			failedCount++
			failureMessages = append(failureMessages, fmt.Sprintf("%s: empty code rendered", filepath.Base(file)))
			continue
		}

		successCount++
	}

	t.Logf("=== Bulk Generation Results ===")
	t.Logf("Total Controllers: %d", len(controllerFiles))
	t.Logf("Successfully Generated: %d (%.1f%%)", successCount, float64(successCount)/float64(len(controllerFiles))*100.0)
	t.Logf("Failures: %d", failedCount)

	if len(failureMessages) > 0 {
		t.Logf("Sample Failures (up to 10):")
		limit := 10
		if len(failureMessages) < limit {
			limit = len(failureMessages)
		}
		for i := 0; i < limit; i++ {
			t.Logf("  - %s", failureMessages[i])
		}
	}
}
