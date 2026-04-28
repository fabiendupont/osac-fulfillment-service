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

var _ = Describe("Private images", func() {
	var (
		ctx    context.Context
		client privatev1.ImagesClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewImagesClient(tool.AdminConn())
	})

	It("Creates an image with ignition boot method", func() {
		response, err := client.Create(ctx, privatev1.ImagesCreateRequest_builder{
			Object: privatev1.Image_builder{
				Title:        "RHEL 9.6 GPU",
				Description:  "RHEL 9.6 with NVIDIA drivers",
				SourceType:   "registry",
				SourceRef:    "quay.io/images/rhel-bootc:9.6-cuda12.8",
				Os:           stringPtr("rhel"),
				Version:      stringPtr("9.6"),
				Architecture: stringPtr("aarch64"),
				BootMethod:   "ignition",
				Compatibility: []string{"gpu-b200-4"},
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		object := response.GetObject()
		Expect(object).ToNot(BeNil())
		Expect(object.GetId()).ToNot(BeEmpty())
		Expect(object.GetTitle()).To(Equal("RHEL 9.6 GPU"))
		Expect(object.GetBootMethod()).To(Equal("ignition"))
		Expect(object.GetCompatibility()).To(ConsistOf("gpu-b200-4"))
	})

	It("Creates an image with cloud-init boot method", func() {
		response, err := client.Create(ctx, privatev1.ImagesCreateRequest_builder{
			Object: privatev1.Image_builder{
				Title:      "Ubuntu 24.04",
				SourceType: "registry",
				SourceRef:  "quay.io/images/ubuntu:24.04",
				BootMethod: "cloud-init",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response.GetObject().GetBootMethod()).To(Equal("cloud-init"))
	})

	It("Rejects creation without boot_method", func() {
		_, err := client.Create(ctx, privatev1.ImagesCreateRequest_builder{
			Object: privatev1.Image_builder{
				Title:      "No boot method",
				SourceType: "registry",
				SourceRef:  "quay.io/test:latest",
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("boot_method"))
	})

	It("Rejects creation without source_type", func() {
		_, err := client.Create(ctx, privatev1.ImagesCreateRequest_builder{
			Object: privatev1.Image_builder{
				Title:      "No source type",
				SourceRef:  "quay.io/test:latest",
				BootMethod: "ignition",
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("source_type"))
	})

	It("Lists, gets, and deletes images", func() {
		createResp, err := client.Create(ctx, privatev1.ImagesCreateRequest_builder{
			Object: privatev1.Image_builder{
				Title:      "CRUD test image",
				SourceType: "registry",
				SourceRef:  "quay.io/test:crud",
				BootMethod: "cloud-init",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResp.GetObject().GetId()

		getResp, err := client.Get(ctx, privatev1.ImagesGetRequest_builder{Id: id}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.GetObject().GetTitle()).To(Equal("CRUD test image"))

		listResp, err := client.List(ctx, privatev1.ImagesListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.GetItems()).ToNot(BeEmpty())

		_, err = client.Delete(ctx, privatev1.ImagesDeleteRequest_builder{Id: id}.Build())
		Expect(err).ToNot(HaveOccurred())
	})
})

func stringPtr(s string) *string {
	return &s
}
