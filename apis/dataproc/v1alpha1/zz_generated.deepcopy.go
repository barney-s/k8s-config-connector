//go:build !ignore_autogenerated

// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Code generated by controller-gen. DO NOT EDIT.

package v1alpha1

import (
	"github.com/GoogleCloudPlatform/k8s-config-connector/apis/refs/v1beta1"
	k8sv1alpha1 "github.com/GoogleCloudPlatform/k8s-config-connector/pkg/apis/k8s/v1alpha1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DataprocJob) DeepCopyInto(out *DataprocJob) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DataprocJob.
func (in *DataprocJob) DeepCopy() *DataprocJob {
	if in == nil {
		return nil
	}
	out := new(DataprocJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DataprocJob) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DataprocJobList) DeepCopyInto(out *DataprocJobList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]DataprocJob, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DataprocJobList.
func (in *DataprocJobList) DeepCopy() *DataprocJobList {
	if in == nil {
		return nil
	}
	out := new(DataprocJobList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *DataprocJobList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DataprocJobObservedState) DeepCopyInto(out *DataprocJobObservedState) {
	*out = *in
	if in.Placement != nil {
		in, out := &in.Placement, &out.Placement
		*out = new(JobPlacementObservedState)
		(*in).DeepCopyInto(*out)
	}
	if in.Status != nil {
		in, out := &in.Status, &out.Status
		*out = new(JobStatus)
		**out = **in
	}
	if in.StatusHistory != nil {
		in, out := &in.StatusHistory, &out.StatusHistory
		*out = make([]JobStatus, len(*in))
		copy(*out, *in)
	}
	if in.YarnApplications != nil {
		in, out := &in.YarnApplications, &out.YarnApplications
		*out = make([]YarnApplication, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.DriverOutputResourceURI != nil {
		in, out := &in.DriverOutputResourceURI, &out.DriverOutputResourceURI
		*out = new(string)
		**out = **in
	}
	if in.DriverControlFilesURI != nil {
		in, out := &in.DriverControlFilesURI, &out.DriverControlFilesURI
		*out = new(string)
		**out = **in
	}
	if in.JobUUid != nil {
		in, out := &in.JobUUid, &out.JobUUid
		*out = new(string)
		**out = **in
	}
	if in.Done != nil {
		in, out := &in.Done, &out.Done
		*out = new(bool)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DataprocJobObservedState.
func (in *DataprocJobObservedState) DeepCopy() *DataprocJobObservedState {
	if in == nil {
		return nil
	}
	out := new(DataprocJobObservedState)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DataprocJobParent) DeepCopyInto(out *DataprocJobParent) {
	*out = *in
	if in.ProjectRef != nil {
		in, out := &in.ProjectRef, &out.ProjectRef
		*out = new(v1beta1.ProjectRef)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DataprocJobParent.
func (in *DataprocJobParent) DeepCopy() *DataprocJobParent {
	if in == nil {
		return nil
	}
	out := new(DataprocJobParent)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DataprocJobSpec) DeepCopyInto(out *DataprocJobSpec) {
	*out = *in
	if in.Reference != nil {
		in, out := &in.Reference, &out.Reference
		*out = new(JobReference)
		(*in).DeepCopyInto(*out)
	}
	if in.Placement != nil {
		in, out := &in.Placement, &out.Placement
		*out = new(JobPlacement)
		(*in).DeepCopyInto(*out)
	}
	if in.HadoopJob != nil {
		in, out := &in.HadoopJob, &out.HadoopJob
		*out = new(HadoopJob)
		(*in).DeepCopyInto(*out)
	}
	if in.SparkJob != nil {
		in, out := &in.SparkJob, &out.SparkJob
		*out = new(SparkJob)
		(*in).DeepCopyInto(*out)
	}
	if in.PysparkJob != nil {
		in, out := &in.PysparkJob, &out.PysparkJob
		*out = new(PySparkJob)
		(*in).DeepCopyInto(*out)
	}
	if in.HiveJob != nil {
		in, out := &in.HiveJob, &out.HiveJob
		*out = new(HiveJob)
		(*in).DeepCopyInto(*out)
	}
	if in.PigJob != nil {
		in, out := &in.PigJob, &out.PigJob
		*out = new(PigJob)
		(*in).DeepCopyInto(*out)
	}
	if in.SparkRJob != nil {
		in, out := &in.SparkRJob, &out.SparkRJob
		*out = new(SparkRJob)
		(*in).DeepCopyInto(*out)
	}
	if in.SparkSQLJob != nil {
		in, out := &in.SparkSQLJob, &out.SparkSQLJob
		*out = new(SparkSQLJob)
		(*in).DeepCopyInto(*out)
	}
	if in.PrestoJob != nil {
		in, out := &in.PrestoJob, &out.PrestoJob
		*out = new(PrestoJob)
		(*in).DeepCopyInto(*out)
	}
	if in.TrinoJob != nil {
		in, out := &in.TrinoJob, &out.TrinoJob
		*out = new(TrinoJob)
		(*in).DeepCopyInto(*out)
	}
	if in.FlinkJob != nil {
		in, out := &in.FlinkJob, &out.FlinkJob
		*out = new(FlinkJob)
		(*in).DeepCopyInto(*out)
	}
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Scheduling != nil {
		in, out := &in.Scheduling, &out.Scheduling
		*out = new(JobScheduling)
		(*in).DeepCopyInto(*out)
	}
	if in.DriverSchedulingConfig != nil {
		in, out := &in.DriverSchedulingConfig, &out.DriverSchedulingConfig
		*out = new(DriverSchedulingConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.DataprocJobParent != nil {
		in, out := &in.DataprocJobParent, &out.DataprocJobParent
		*out = new(DataprocJobParent)
		(*in).DeepCopyInto(*out)
	}
	if in.ResourceID != nil {
		in, out := &in.ResourceID, &out.ResourceID
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DataprocJobSpec.
func (in *DataprocJobSpec) DeepCopy() *DataprocJobSpec {
	if in == nil {
		return nil
	}
	out := new(DataprocJobSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DataprocJobStatus) DeepCopyInto(out *DataprocJobStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]k8sv1alpha1.Condition, len(*in))
		copy(*out, *in)
	}
	if in.ObservedGeneration != nil {
		in, out := &in.ObservedGeneration, &out.ObservedGeneration
		*out = new(int64)
		**out = **in
	}
	if in.ExternalRef != nil {
		in, out := &in.ExternalRef, &out.ExternalRef
		*out = new(string)
		**out = **in
	}
	if in.ObservedState != nil {
		in, out := &in.ObservedState, &out.ObservedState
		*out = new(DataprocJobObservedState)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DataprocJobStatus.
func (in *DataprocJobStatus) DeepCopy() *DataprocJobStatus {
	if in == nil {
		return nil
	}
	out := new(DataprocJobStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DriverSchedulingConfig) DeepCopyInto(out *DriverSchedulingConfig) {
	*out = *in
	if in.MemoryMb != nil {
		in, out := &in.MemoryMb, &out.MemoryMb
		*out = new(int32)
		**out = **in
	}
	if in.Vcores != nil {
		in, out := &in.Vcores, &out.Vcores
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DriverSchedulingConfig.
func (in *DriverSchedulingConfig) DeepCopy() *DriverSchedulingConfig {
	if in == nil {
		return nil
	}
	out := new(DriverSchedulingConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *FlinkJob) DeepCopyInto(out *FlinkJob) {
	*out = *in
	if in.MainJarFileURI != nil {
		in, out := &in.MainJarFileURI, &out.MainJarFileURI
		*out = new(string)
		**out = **in
	}
	if in.MainClass != nil {
		in, out := &in.MainClass, &out.MainClass
		*out = new(string)
		**out = **in
	}
	if in.Args != nil {
		in, out := &in.Args, &out.Args
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.JarFileURIs != nil {
		in, out := &in.JarFileURIs, &out.JarFileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.SavepointURI != nil {
		in, out := &in.SavepointURI, &out.SavepointURI
		*out = new(string)
		**out = **in
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.LoggingConfig != nil {
		in, out := &in.LoggingConfig, &out.LoggingConfig
		*out = new(LoggingConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new FlinkJob.
func (in *FlinkJob) DeepCopy() *FlinkJob {
	if in == nil {
		return nil
	}
	out := new(FlinkJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HadoopJob) DeepCopyInto(out *HadoopJob) {
	*out = *in
	if in.MainJarFileURI != nil {
		in, out := &in.MainJarFileURI, &out.MainJarFileURI
		*out = new(string)
		**out = **in
	}
	if in.MainClass != nil {
		in, out := &in.MainClass, &out.MainClass
		*out = new(string)
		**out = **in
	}
	if in.Args != nil {
		in, out := &in.Args, &out.Args
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.JarFileURIs != nil {
		in, out := &in.JarFileURIs, &out.JarFileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.FileURIs != nil {
		in, out := &in.FileURIs, &out.FileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ArchiveURIs != nil {
		in, out := &in.ArchiveURIs, &out.ArchiveURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.LoggingConfig != nil {
		in, out := &in.LoggingConfig, &out.LoggingConfig
		*out = new(LoggingConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HadoopJob.
func (in *HadoopJob) DeepCopy() *HadoopJob {
	if in == nil {
		return nil
	}
	out := new(HadoopJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HiveJob) DeepCopyInto(out *HiveJob) {
	*out = *in
	if in.QueryFileURI != nil {
		in, out := &in.QueryFileURI, &out.QueryFileURI
		*out = new(string)
		**out = **in
	}
	if in.QueryList != nil {
		in, out := &in.QueryList, &out.QueryList
		*out = new(QueryList)
		(*in).DeepCopyInto(*out)
	}
	if in.ContinueOnFailure != nil {
		in, out := &in.ContinueOnFailure, &out.ContinueOnFailure
		*out = new(bool)
		**out = **in
	}
	if in.ScriptVariables != nil {
		in, out := &in.ScriptVariables, &out.ScriptVariables
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.JarFileURIs != nil {
		in, out := &in.JarFileURIs, &out.JarFileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HiveJob.
func (in *HiveJob) DeepCopy() *HiveJob {
	if in == nil {
		return nil
	}
	out := new(HiveJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobIdentity) DeepCopyInto(out *JobIdentity) {
	*out = *in
	if in.parent != nil {
		in, out := &in.parent, &out.parent
		*out = new(JobParent)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobIdentity.
func (in *JobIdentity) DeepCopy() *JobIdentity {
	if in == nil {
		return nil
	}
	out := new(JobIdentity)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobParent) DeepCopyInto(out *JobParent) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobParent.
func (in *JobParent) DeepCopy() *JobParent {
	if in == nil {
		return nil
	}
	out := new(JobParent)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobPlacement) DeepCopyInto(out *JobPlacement) {
	*out = *in
	if in.ClusterName != nil {
		in, out := &in.ClusterName, &out.ClusterName
		*out = new(string)
		**out = **in
	}
	if in.ClusterLabels != nil {
		in, out := &in.ClusterLabels, &out.ClusterLabels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobPlacement.
func (in *JobPlacement) DeepCopy() *JobPlacement {
	if in == nil {
		return nil
	}
	out := new(JobPlacement)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobPlacementObservedState) DeepCopyInto(out *JobPlacementObservedState) {
	*out = *in
	if in.ClusterUuid != nil {
		in, out := &in.ClusterUuid, &out.ClusterUuid
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobPlacementObservedState.
func (in *JobPlacementObservedState) DeepCopy() *JobPlacementObservedState {
	if in == nil {
		return nil
	}
	out := new(JobPlacementObservedState)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobRef) DeepCopyInto(out *JobRef) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobRef.
func (in *JobRef) DeepCopy() *JobRef {
	if in == nil {
		return nil
	}
	out := new(JobRef)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobReference) DeepCopyInto(out *JobReference) {
	*out = *in
	if in.ProjectID != nil {
		in, out := &in.ProjectID, &out.ProjectID
		*out = new(string)
		**out = **in
	}
	if in.JobID != nil {
		in, out := &in.JobID, &out.JobID
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobReference.
func (in *JobReference) DeepCopy() *JobReference {
	if in == nil {
		return nil
	}
	out := new(JobReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobScheduling) DeepCopyInto(out *JobScheduling) {
	*out = *in
	if in.MaxFailuresPerHour != nil {
		in, out := &in.MaxFailuresPerHour, &out.MaxFailuresPerHour
		*out = new(int32)
		**out = **in
	}
	if in.MaxFailuresTotal != nil {
		in, out := &in.MaxFailuresTotal, &out.MaxFailuresTotal
		*out = new(int32)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobScheduling.
func (in *JobScheduling) DeepCopy() *JobScheduling {
	if in == nil {
		return nil
	}
	out := new(JobScheduling)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobStatus) DeepCopyInto(out *JobStatus) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobStatus.
func (in *JobStatus) DeepCopy() *JobStatus {
	if in == nil {
		return nil
	}
	out := new(JobStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobStatusObservedState) DeepCopyInto(out *JobStatusObservedState) {
	*out = *in
	if in.State != nil {
		in, out := &in.State, &out.State
		*out = new(string)
		**out = **in
	}
	if in.Details != nil {
		in, out := &in.Details, &out.Details
		*out = new(string)
		**out = **in
	}
	if in.StateStartTime != nil {
		in, out := &in.StateStartTime, &out.StateStartTime
		*out = new(string)
		**out = **in
	}
	if in.Substate != nil {
		in, out := &in.Substate, &out.Substate
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new JobStatusObservedState.
func (in *JobStatusObservedState) DeepCopy() *JobStatusObservedState {
	if in == nil {
		return nil
	}
	out := new(JobStatusObservedState)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *LoggingConfig) DeepCopyInto(out *LoggingConfig) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new LoggingConfig.
func (in *LoggingConfig) DeepCopy() *LoggingConfig {
	if in == nil {
		return nil
	}
	out := new(LoggingConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PigJob) DeepCopyInto(out *PigJob) {
	*out = *in
	if in.QueryFileURI != nil {
		in, out := &in.QueryFileURI, &out.QueryFileURI
		*out = new(string)
		**out = **in
	}
	if in.QueryList != nil {
		in, out := &in.QueryList, &out.QueryList
		*out = new(QueryList)
		(*in).DeepCopyInto(*out)
	}
	if in.ContinueOnFailure != nil {
		in, out := &in.ContinueOnFailure, &out.ContinueOnFailure
		*out = new(bool)
		**out = **in
	}
	if in.ScriptVariables != nil {
		in, out := &in.ScriptVariables, &out.ScriptVariables
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.JarFileURIs != nil {
		in, out := &in.JarFileURIs, &out.JarFileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.LoggingConfig != nil {
		in, out := &in.LoggingConfig, &out.LoggingConfig
		*out = new(LoggingConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PigJob.
func (in *PigJob) DeepCopy() *PigJob {
	if in == nil {
		return nil
	}
	out := new(PigJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PrestoJob) DeepCopyInto(out *PrestoJob) {
	*out = *in
	if in.QueryFileURI != nil {
		in, out := &in.QueryFileURI, &out.QueryFileURI
		*out = new(string)
		**out = **in
	}
	if in.QueryList != nil {
		in, out := &in.QueryList, &out.QueryList
		*out = new(QueryList)
		(*in).DeepCopyInto(*out)
	}
	if in.ContinueOnFailure != nil {
		in, out := &in.ContinueOnFailure, &out.ContinueOnFailure
		*out = new(bool)
		**out = **in
	}
	if in.OutputFormat != nil {
		in, out := &in.OutputFormat, &out.OutputFormat
		*out = new(string)
		**out = **in
	}
	if in.ClientTags != nil {
		in, out := &in.ClientTags, &out.ClientTags
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.LoggingConfig != nil {
		in, out := &in.LoggingConfig, &out.LoggingConfig
		*out = new(LoggingConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PrestoJob.
func (in *PrestoJob) DeepCopy() *PrestoJob {
	if in == nil {
		return nil
	}
	out := new(PrestoJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PySparkJob) DeepCopyInto(out *PySparkJob) {
	*out = *in
	if in.MainPythonFileURI != nil {
		in, out := &in.MainPythonFileURI, &out.MainPythonFileURI
		*out = new(string)
		**out = **in
	}
	if in.Args != nil {
		in, out := &in.Args, &out.Args
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.PythonFileURIs != nil {
		in, out := &in.PythonFileURIs, &out.PythonFileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.JarFileURIs != nil {
		in, out := &in.JarFileURIs, &out.JarFileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.FileURIs != nil {
		in, out := &in.FileURIs, &out.FileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ArchiveURIs != nil {
		in, out := &in.ArchiveURIs, &out.ArchiveURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.LoggingConfig != nil {
		in, out := &in.LoggingConfig, &out.LoggingConfig
		*out = new(LoggingConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PySparkJob.
func (in *PySparkJob) DeepCopy() *PySparkJob {
	if in == nil {
		return nil
	}
	out := new(PySparkJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *QueryList) DeepCopyInto(out *QueryList) {
	*out = *in
	if in.Queries != nil {
		in, out := &in.Queries, &out.Queries
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new QueryList.
func (in *QueryList) DeepCopy() *QueryList {
	if in == nil {
		return nil
	}
	out := new(QueryList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ServiceRef) DeepCopyInto(out *ServiceRef) {
	*out = *in
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ServiceRef.
func (in *ServiceRef) DeepCopy() *ServiceRef {
	if in == nil {
		return nil
	}
	out := new(ServiceRef)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SparkJob) DeepCopyInto(out *SparkJob) {
	*out = *in
	if in.MainJarFileURI != nil {
		in, out := &in.MainJarFileURI, &out.MainJarFileURI
		*out = new(string)
		**out = **in
	}
	if in.MainClass != nil {
		in, out := &in.MainClass, &out.MainClass
		*out = new(string)
		**out = **in
	}
	if in.Args != nil {
		in, out := &in.Args, &out.Args
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.JarFileURIs != nil {
		in, out := &in.JarFileURIs, &out.JarFileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.FileURIs != nil {
		in, out := &in.FileURIs, &out.FileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ArchiveURIs != nil {
		in, out := &in.ArchiveURIs, &out.ArchiveURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.LoggingConfig != nil {
		in, out := &in.LoggingConfig, &out.LoggingConfig
		*out = new(LoggingConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SparkJob.
func (in *SparkJob) DeepCopy() *SparkJob {
	if in == nil {
		return nil
	}
	out := new(SparkJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SparkRJob) DeepCopyInto(out *SparkRJob) {
	*out = *in
	if in.MainRFileURI != nil {
		in, out := &in.MainRFileURI, &out.MainRFileURI
		*out = new(string)
		**out = **in
	}
	if in.Args != nil {
		in, out := &in.Args, &out.Args
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.FileURIs != nil {
		in, out := &in.FileURIs, &out.FileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.ArchiveURIs != nil {
		in, out := &in.ArchiveURIs, &out.ArchiveURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.LoggingConfig != nil {
		in, out := &in.LoggingConfig, &out.LoggingConfig
		*out = new(LoggingConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SparkRJob.
func (in *SparkRJob) DeepCopy() *SparkRJob {
	if in == nil {
		return nil
	}
	out := new(SparkRJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SparkSQLJob) DeepCopyInto(out *SparkSQLJob) {
	*out = *in
	if in.QueryFileURI != nil {
		in, out := &in.QueryFileURI, &out.QueryFileURI
		*out = new(string)
		**out = **in
	}
	if in.QueryList != nil {
		in, out := &in.QueryList, &out.QueryList
		*out = new(QueryList)
		(*in).DeepCopyInto(*out)
	}
	if in.ScriptVariables != nil {
		in, out := &in.ScriptVariables, &out.ScriptVariables
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.JarFileURIs != nil {
		in, out := &in.JarFileURIs, &out.JarFileURIs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.LoggingConfig != nil {
		in, out := &in.LoggingConfig, &out.LoggingConfig
		*out = new(LoggingConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SparkSQLJob.
func (in *SparkSQLJob) DeepCopy() *SparkSQLJob {
	if in == nil {
		return nil
	}
	out := new(SparkSQLJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *TrinoJob) DeepCopyInto(out *TrinoJob) {
	*out = *in
	if in.QueryFileURI != nil {
		in, out := &in.QueryFileURI, &out.QueryFileURI
		*out = new(string)
		**out = **in
	}
	if in.QueryList != nil {
		in, out := &in.QueryList, &out.QueryList
		*out = new(QueryList)
		(*in).DeepCopyInto(*out)
	}
	if in.ContinueOnFailure != nil {
		in, out := &in.ContinueOnFailure, &out.ContinueOnFailure
		*out = new(bool)
		**out = **in
	}
	if in.OutputFormat != nil {
		in, out := &in.OutputFormat, &out.OutputFormat
		*out = new(string)
		**out = **in
	}
	if in.ClientTags != nil {
		in, out := &in.ClientTags, &out.ClientTags
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Properties != nil {
		in, out := &in.Properties, &out.Properties
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.LoggingConfig != nil {
		in, out := &in.LoggingConfig, &out.LoggingConfig
		*out = new(LoggingConfig)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new TrinoJob.
func (in *TrinoJob) DeepCopy() *TrinoJob {
	if in == nil {
		return nil
	}
	out := new(TrinoJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *YarnApplication) DeepCopyInto(out *YarnApplication) {
	*out = *in
	if in.Name != nil {
		in, out := &in.Name, &out.Name
		*out = new(string)
		**out = **in
	}
	if in.State != nil {
		in, out := &in.State, &out.State
		*out = new(string)
		**out = **in
	}
	if in.Progress != nil {
		in, out := &in.Progress, &out.Progress
		*out = new(float32)
		**out = **in
	}
	if in.TrackingURL != nil {
		in, out := &in.TrackingURL, &out.TrackingURL
		*out = new(string)
		**out = **in
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new YarnApplication.
func (in *YarnApplication) DeepCopy() *YarnApplication {
	if in == nil {
		return nil
	}
	out := new(YarnApplication)
	in.DeepCopyInto(out)
	return out
}
