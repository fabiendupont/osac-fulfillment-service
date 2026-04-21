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

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var _ = Describe("Private compute instance groups", func() {
	var (
		ctx    context.Context
		client privatev1.ComputeInstanceGroupsClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewComputeInstanceGroupsClient(tool.AdminConn())
	})

	It("Creates a compute instance group", func() {
		response, err := client.Create(ctx, privatev1.ComputeInstanceGroupsCreateRequest_builder{
			Object: privatev1.ComputeInstanceGroup_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "test-training-pool",
				}.Build(),
				Spec: privatev1.ComputeInstanceGroupSpec_builder{
					ComputeInstanceClass: "gpu-b200-4",
					Replicas:             4,
					ImageRef:             stringPtr("rhel-9.6-gpu"),
					SshKeyRefs:           []string{"my-workstation-key"},
					Region:               stringPtr("eu-west"),
					PlacementPolicy: privatev1.ComputeInstanceGroupPlacementPolicy_builder{
						Strategy:    "pack",
						AffinityKey: stringPtr("nvlink_domain"),
					}.Build(),
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		object := response.GetObject()
		Expect(object).ToNot(BeNil())
		Expect(object.GetId()).ToNot(BeEmpty())
		Expect(object.GetSpec().GetComputeInstanceClass()).To(Equal("gpu-b200-4"))
		Expect(object.GetSpec().GetReplicas()).To(Equal(int32(4)))
		Expect(object.GetStatus().GetState()).To(Equal(
			privatev1.ComputeInstanceGroupState_COMPUTE_INSTANCE_GROUP_STATE_PENDING))
	})

	It("Rejects creation without compute_instance_class", func() {
		_, err := client.Create(ctx, privatev1.ComputeInstanceGroupsCreateRequest_builder{
			Object: privatev1.ComputeInstanceGroup_builder{
				Spec: privatev1.ComputeInstanceGroupSpec_builder{
					Replicas: 2,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
	})

	It("Rejects negative replicas", func() {
		_, err := client.Create(ctx, privatev1.ComputeInstanceGroupsCreateRequest_builder{
			Object: privatev1.ComputeInstanceGroup_builder{
				Spec: privatev1.ComputeInstanceGroupSpec_builder{
					ComputeInstanceClass: "test-class",
					Replicas:             -1,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
	})

	It("Updates replicas", func() {
		createResp, err := client.Create(ctx, privatev1.ComputeInstanceGroupsCreateRequest_builder{
			Object: privatev1.ComputeInstanceGroup_builder{
				Spec: privatev1.ComputeInstanceGroupSpec_builder{
					ComputeInstanceClass: "scale-test-class",
					Replicas:             2,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResp.GetObject().GetId()

		updateResp, err := client.Update(ctx, privatev1.ComputeInstanceGroupsUpdateRequest_builder{
			Object: privatev1.ComputeInstanceGroup_builder{
				Id: id,
				Spec: privatev1.ComputeInstanceGroupSpec_builder{
					ComputeInstanceClass: "scale-test-class",
					Replicas:             8,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(updateResp.GetObject().GetSpec().GetReplicas()).To(Equal(int32(8)))
	})

	It("Rejects class change on update", func() {
		createResp, err := client.Create(ctx, privatev1.ComputeInstanceGroupsCreateRequest_builder{
			Object: privatev1.ComputeInstanceGroup_builder{
				Spec: privatev1.ComputeInstanceGroupSpec_builder{
					ComputeInstanceClass: "immutable-class",
					Replicas:             1,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResp.GetObject().GetId()

		_, err = client.Update(ctx, privatev1.ComputeInstanceGroupsUpdateRequest_builder{
			Object: privatev1.ComputeInstanceGroup_builder{
				Id: id,
				Spec: privatev1.ComputeInstanceGroupSpec_builder{
					ComputeInstanceClass: "different-class",
					Replicas:             1,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
	})

	It("Lists, gets, and deletes groups", func() {
		createResp, err := client.Create(ctx, privatev1.ComputeInstanceGroupsCreateRequest_builder{
			Object: privatev1.ComputeInstanceGroup_builder{
				Spec: privatev1.ComputeInstanceGroupSpec_builder{
					ComputeInstanceClass: "crud-test-class",
					Replicas:             1,
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResp.GetObject().GetId()

		getResp, err := client.Get(ctx, privatev1.ComputeInstanceGroupsGetRequest_builder{Id: id}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.GetObject().GetSpec().GetComputeInstanceClass()).To(Equal("crud-test-class"))

		listResp, err := client.List(ctx, privatev1.ComputeInstanceGroupsListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.GetItems()).ToNot(BeEmpty())

		_, err = client.Delete(ctx, privatev1.ComputeInstanceGroupsDeleteRequest_builder{Id: id}.Build())
		Expect(err).ToNot(HaveOccurred())
	})
})
