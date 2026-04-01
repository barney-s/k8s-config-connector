---
name: PR for mockgcp
description: Periodically find TF and DCL resources to be migrated to Direct and create a PR to create the mockgcp implementation.
schedule: "@daily"
skipPR: true
---

<!--
Copyright 2026 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
-->

# Role
You are a software development assistant for the Kubernetes Config Connector project.
You have access to the following tools:
- GitHub CLI (`gh`)
- git
- go
- bash

# Filter Criteria
This is the criteria to identify the resource Group and Kind that need a mock created.

Finding resources that meet the following criteria:
1. Run `./dev/migration-tracker/list_top_unmigrated.py -n 20 --step mocks` to get a topologically sorted list of up to 20 unmigrated resources (output is in `<Group>/<Kind>` format).
2. The resource CRD must be present in `config/crds/resources` directory.
3. Candidate resources can be identified by the presence of the label `cnrm.cloud.google.com/dcl2crd: "true"` OR `cnrm.cloud.google.com/tf2crd: "true"` in their metadata.
4. The resource to be migrated should be in `beta` (`spec.versions[].name` is `v1beta1`).

**Dependency Verification:**
The output of `./dev/migration-tracker/list_top_unmigrated.py` is already topologically sorted.

Identify the Group and Kind of the resource that meets the above criteria.

# Task
Use git and gh cli tools to create a github Pull Request.
In a single run create at most one PR to avoid overwhelming the team.

1. Run `./dev/migration-tracker/list_top_unmigrated.py -n 20 --step mocks` to get the prioritized list of resource Groups and Kinds.
2. Iterate through the output list.
3. For each Group and Kind:
    - Find the corresponding CRD file in `config/crds/resources`.
    - Check if it is a migration candidate (DCL or TF, Beta version).
    - Check if an open PR already exists for creating this mock.
    - Check if the mock already exists in `mockgcp/`.
    - Find the e2e test fixtures for this resource. Tests are usually in `pkg/test/resourcefixture/testdata/basic/<group>/<version>/<kind>/` or similar locations.
    - If an e2e test DOES NOT exist for this resource, record the response (log that the test doesn't exist) and SKIP the resource.
    - If a test DOES exist and the mock does NOT, pick this resource.
4. If a resource is selected, run the existing terraform controller to grab the httplogs.
   Use the following command to run the tests against real GCP and write the golden output (replace `<lowercase_kind>` with the resource's kind in lowercase):
   ```bash
   E2E_KUBE_TARGET=envtest RUN_E2E=1 E2E_GCP_TARGET=real GOLDEN_OBJECT_CHECKS=1 GOLDEN_REQUEST_CHECKS=1 WRITE_GOLDEN_OUTPUT=1 go test -test.count=1 -timeout 3600s -v ./tests/e2e -run TestAllInSeries/fixtures/<lowercase_kind> 2>&1 | tee log
   ```
5. Use the generated `_http.log` files to create a mock that conforms to the same.
   The instructions to create a mockgcp are:
   - **Add the Proto Definition**: Locate the relevant `.proto` file for the GCP service in the `googleapis` repository and add it to `mockgcp/Makefile`.
   - **Generate Go Code**: Run `make all` in `mockgcp/` to generate the necessary Go files from the proto definition.
   - **Create a Mock Service Directory**: Create a new directory named `mockgcp/mock<servicename>`.
   - **Implement the basic service**: In your service directory, create `service.go` and `normalize.go`. Register the service in `mockgcp/register.go` for both GRPC and HTTP.
   - **Implement the Service**: Implement the core CRUD methods in a `.go` file within the new directory based on the `_http.log` output.
   - **Normalization**: Ensure you explicitly scope the `Previsit` logic in `normalize.go` to the relevant service URL to avoid corrupting logs in other services.
6. Verify your mock implementation by running the tests against it:
   ```bash
   E2E_KUBE_TARGET=envtest RUN_E2E=1 E2E_GCP_TARGET=mock GOLDEN_OBJECT_CHECKS=1 GOLDEN_REQUEST_CHECKS=1 go test -test.count=1 -timeout 3600s -v ./tests/e2e -run TestAllInSeries/fixtures/<lowercase_kind> 2>&1 | tee log
   ```
   The tests should pass against your mock implementation.
7. Create a branch and commit the new mockgcp implementation.
8. Update `dev/migration-tracker/data.json`:
   - Set `"mocks": true` in the `"steps"` object for this resource.
   - Commit this change as well.
9. Push the branch and create a PR using the `gh` tool.

## PR Title

Title should be: `feat: create mockgcp for <Group> <Kind>`,
where `<Group>` and `<Kind>` are replaced with the actual Group and Kind.

## PR Body

The PR body should contain:
- A brief explanation that this PR creates the mockgcp implementation for `<Group> <Kind>`.
- Mention that the HTTP logs were generated by running the Terraform controller against real GCP.
- A link to the main epic: https://github.com/GoogleCloudPlatform/k8s-config-connector/issues/5954.