//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
Copyright 2025 Flant JSC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Code generated by deepcopy-gen. DO NOT EDIT.

package v1alpha1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NFSStorageClass) DeepCopyInto(out *NFSStorageClass) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	if in.Status != nil {
		in, out := &in.Status, &out.Status
		*out = new(NFSStorageClassStatus)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NFSStorageClass.
func (in *NFSStorageClass) DeepCopy() *NFSStorageClass {
	if in == nil {
		return nil
	}
	out := new(NFSStorageClass)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NFSStorageClass) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NFSStorageClassConnection) DeepCopyInto(out *NFSStorageClassConnection) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NFSStorageClassConnection.
func (in *NFSStorageClassConnection) DeepCopy() *NFSStorageClassConnection {
	if in == nil {
		return nil
	}
	out := new(NFSStorageClassConnection)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NFSStorageClassList) DeepCopyInto(out *NFSStorageClassList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]NFSStorageClass, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NFSStorageClassList.
func (in *NFSStorageClassList) DeepCopy() *NFSStorageClassList {
	if in == nil {
		return nil
	}
	out := new(NFSStorageClassList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NFSStorageClassList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NFSStorageClassMountOptions) DeepCopyInto(out *NFSStorageClassMountOptions) {
	*out = *in
	if in.ReadOnly != nil {
		in, out := &in.ReadOnly, &out.ReadOnly
		*out = new(bool)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NFSStorageClassMountOptions.
func (in *NFSStorageClassMountOptions) DeepCopy() *NFSStorageClassMountOptions {
	if in == nil {
		return nil
	}
	out := new(NFSStorageClassMountOptions)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NFSStorageClassSpec) DeepCopyInto(out *NFSStorageClassSpec) {
	*out = *in
	if in.Connection != nil {
		in, out := &in.Connection, &out.Connection
		*out = new(NFSStorageClassConnection)
		**out = **in
	}
	if in.MountOptions != nil {
		in, out := &in.MountOptions, &out.MountOptions
		*out = new(NFSStorageClassMountOptions)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NFSStorageClassSpec.
func (in *NFSStorageClassSpec) DeepCopy() *NFSStorageClassSpec {
	if in == nil {
		return nil
	}
	out := new(NFSStorageClassSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NFSStorageClassStatus) DeepCopyInto(out *NFSStorageClassStatus) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NFSStorageClassStatus.
func (in *NFSStorageClassStatus) DeepCopy() *NFSStorageClassStatus {
	if in == nil {
		return nil
	}
	out := new(NFSStorageClassStatus)
	in.DeepCopyInto(out)
	return out
}
