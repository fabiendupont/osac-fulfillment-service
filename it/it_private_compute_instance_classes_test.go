/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package it

import (
	"context"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var _ = Describe("Private compute instance classes", func() {
	var (
		ctx    context.Context
		client privatev1.ComputeInstanceClassesClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewComputeInstanceClassesClient(tool.AdminConn())
	})

	It("Creates a bare-metal class", func() {
		response, err := client.Create(ctx, privatev1.ComputeInstanceClassesCreateRequest_builder{
			Object: privatev1.ComputeInstanceClass_builder{
				Title:       "4x B200 GPU Tray",
				Description: "Dedicated compute tray with 4 NVIDIA B200 GPUs",
				Backend:     "baremetal",
				Capabilities: privatev1.ComputeInstanceClassCapabilities_builder{
					CoresFixed:    proto.Int32(96),
					MemoryGibFixed: proto.Int32(480),
					Gpus: privatev1.ComputeInstanceClassGPU_builder{
						Count: 4,
						Model: "B200",
					}.Build(),
					Storage: privatev1.ComputeInstanceClassStorage_builder{
						BootDiskGibFixed: proto.Int32(960),
					}.Build(),
				}.Build(),
				Templates: []*privatev1.ComputeInstanceClassTemplateRef{
					privatev1.ComputeInstanceClassTemplateRef_builder{
						Name: "metal3-b200-paris",
						Site: "paris",
					}.Build(),
				},
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		object := response.GetObject()
		Expect(object).ToNot(BeNil())
		Expect(object.GetId()).ToNot(BeEmpty())
		Expect(object.GetTitle()).To(Equal("4x B200 GPU Tray"))
		Expect(object.GetBackend()).To(Equal("baremetal"))
		Expect(object.GetStatus().GetState()).To(Equal(
			privatev1.ComputeInstanceClassState_COMPUTE_INSTANCE_CLASS_STATE_READY))
	})

	It("Creates a virtual class with ranges", func() {
		response, err := client.Create(ctx, privatev1.ComputeInstanceClassesCreateRequest_builder{
			Object: privatev1.ComputeInstanceClass_builder{
				Title:   "Small VM",
				Backend: "virtual",
				Capabilities: privatev1.ComputeInstanceClassCapabilities_builder{
					CoresMin:    proto.Int32(2),
					CoresMax:    proto.Int32(16),
					MemoryGibMin: proto.Int32(4),
					MemoryGibMax: proto.Int32(64),
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		object := response.GetObject()
		Expect(object.GetBackend()).To(Equal("virtual"))
		caps := object.GetCapabilities()
		Expect(caps.GetCoresMin()).To(Equal(int32(2)))
		Expect(caps.GetCoresMax()).To(Equal(int32(16)))
	})

	It("Rejects creation without backend", func() {
		_, err := client.Create(ctx, privatev1.ComputeInstanceClassesCreateRequest_builder{
			Object: privatev1.ComputeInstanceClass_builder{
				Title: "No backend",
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
	})

	It("Rejects backend change on update", func() {
		createResp, err := client.Create(ctx, privatev1.ComputeInstanceClassesCreateRequest_builder{
			Object: privatev1.ComputeInstanceClass_builder{
				Title:   "Test class",
				Backend: "virtual",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResp.GetObject().GetId()

		_, err = client.Update(ctx, privatev1.ComputeInstanceClassesUpdateRequest_builder{
			Object: privatev1.ComputeInstanceClass_builder{
				Id:      id,
				Title:   "Test class",
				Backend: "baremetal",
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
	})

	It("Lists and gets classes", func() {
		_, err := client.Create(ctx, privatev1.ComputeInstanceClassesCreateRequest_builder{
			Object: privatev1.ComputeInstanceClass_builder{
				Title:   "List test class",
				Backend: "virtual",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())

		listResp, err := client.List(ctx, privatev1.ComputeInstanceClassesListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.GetItems()).ToNot(BeEmpty())
	})

	It("Deletes a class", func() {
		createResp, err := client.Create(ctx, privatev1.ComputeInstanceClassesCreateRequest_builder{
			Object: privatev1.ComputeInstanceClass_builder{
				Title:   "Delete test",
				Backend: "virtual",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResp.GetObject().GetId()

		_, err = client.Delete(ctx, privatev1.ComputeInstanceClassesDeleteRequest_builder{
			Id: id,
		}.Build())
		Expect(err).ToNot(HaveOccurred())
	})
})
