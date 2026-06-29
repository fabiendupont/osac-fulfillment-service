/*
Copyright (c) 2026 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
)

var _ = Describe("Images server", func() {
	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewImagesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewImagesServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewImagesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var (
			publicServer  *ImagesServer
			privateServer *PrivateImagesServer
		)

		BeforeEach(func() {
			var err error

			publicServer, err = NewImagesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			privateServer, err = NewPrivateImagesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		createImage := func() *privatev1.Image {
			response, err := privateServer.Create(ctx, privatev1.ImagesCreateRequest_builder{
				Object: privatev1.Image_builder{
					Title:      "RHEL 9.4",
					SourceType: "registry",
					SourceRef:  "quay.io/rhel/rhel9:9.4",
					BootMethod: "cloud-init",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			return response.GetObject()
		}

		It("List objects", func() {
			const count = 10
			for range count {
				createImage()
			}

			response, err := publicServer.List(ctx, publicv1.ImagesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			const count = 10
			for range count {
				createImage()
			}

			response, err := publicServer.List(ctx, publicv1.ImagesListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			const count = 10
			for range count {
				createImage()
			}

			response, err := publicServer.List(ctx, publicv1.ImagesListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			const count = 10
			var ids []string
			for range count {
				obj := createImage()
				ids = append(ids, obj.GetId())
			}

			for _, id := range ids {
				response, err := publicServer.List(ctx, publicv1.ImagesListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", id)),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(id))
			}
		})

		It("Get object", func() {
			privateObj := createImage()

			getResponse, err := publicServer.Get(ctx, publicv1.ImagesGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			publicObj := getResponse.GetObject()
			Expect(publicObj.GetId()).To(Equal(privateObj.GetId()))
			Expect(publicObj.GetTitle()).To(Equal(privateObj.GetTitle()))
		})

		It("Update object", func() {
			privateObj := createImage()

			updateResponse, err := publicServer.Update(ctx, publicv1.ImagesUpdateRequest_builder{
				Object: publicv1.Image_builder{
					Id:          privateObj.GetId(),
					Title:       "Updated RHEL 9.4",
					Description: "Updated description.",
					SourceType:  "registry",
					SourceRef:   "quay.io/rhel/rhel9:9.4",
					BootMethod:  "cloud-init",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetTitle()).To(Equal("Updated RHEL 9.4"))
			Expect(updateResponse.GetObject().GetDescription()).To(Equal("Updated description."))

			getResponse, err := publicServer.Get(ctx, publicv1.ImagesGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetTitle()).To(Equal("Updated RHEL 9.4"))
			Expect(getResponse.GetObject().GetDescription()).To(Equal("Updated description."))
		})

		It("Delete object", func() {
			privateObj := createImage()

			tx, err := database.TxFromContext(ctx)
			Expect(err).ToNot(HaveOccurred())
			_, err = tx.Exec(
				ctx,
				`update images set finalizers = '{"a"}' where id = $1`,
				privateObj.GetId(),
			)
			Expect(err).ToNot(HaveOccurred())

			_, err = publicServer.Delete(ctx, publicv1.ImagesDeleteRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			getResponse, err := publicServer.Get(ctx, publicv1.ImagesGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := getResponse.GetObject()
			Expect(object.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("Generates UUID for id ignoring caller-provided value", func() {
			callerProvidedId := "my-custom-id"
			response, err := privateServer.Create(ctx, privatev1.ImagesCreateRequest_builder{
				Object: privatev1.Image_builder{
					Id:         callerProvidedId,
					Title:      "Test Image",
					SourceType: "registry",
					SourceRef:  "quay.io/test/image:latest",
					BootMethod: "ignition",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetObject().GetId()).ToNot(Equal(callerProvidedId))
			Expect(response.GetObject().GetId()).ToNot(BeEmpty())
		})

		It("Sets status to READY on creation", func() {
			privateObj := createImage()
			Expect(privateObj.GetStatus()).ToNot(BeNil())
			Expect(privateObj.GetStatus().GetState()).To(Equal(privatev1.ImageState_IMAGE_STATE_READY))
		})

		It("Rejects create without title", func() {
			_, err := privateServer.Create(ctx, privatev1.ImagesCreateRequest_builder{
				Object: privatev1.Image_builder{
					SourceType: "registry",
					SourceRef:  "quay.io/test/image:latest",
					BootMethod: "ignition",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("title"))
		})

		It("Rejects create without source_type", func() {
			_, err := privateServer.Create(ctx, privatev1.ImagesCreateRequest_builder{
				Object: privatev1.Image_builder{
					Title:      "Test",
					SourceRef:  "quay.io/test/image:latest",
					BootMethod: "ignition",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("source_type"))
		})

		It("Rejects create without source_ref", func() {
			_, err := privateServer.Create(ctx, privatev1.ImagesCreateRequest_builder{
				Object: privatev1.Image_builder{
					Title:      "Test",
					SourceType: "registry",
					BootMethod: "ignition",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("source_ref"))
		})

		It("Rejects create without boot_method", func() {
			_, err := privateServer.Create(ctx, privatev1.ImagesCreateRequest_builder{
				Object: privatev1.Image_builder{
					Title:      "Test",
					SourceType: "registry",
					SourceRef:  "quay.io/test/image:latest",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("boot_method"))
		})

		It("Rejects create with invalid boot_method", func() {
			_, err := privateServer.Create(ctx, privatev1.ImagesCreateRequest_builder{
				Object: privatev1.Image_builder{
					Title:      "Test",
					SourceType: "registry",
					SourceRef:  "quay.io/test/image:latest",
					BootMethod: "invalid",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("boot_method"))
		})

		It("Rejects update changing source_type", func() {
			privateObj := createImage()

			_, err := privateServer.Update(ctx, privatev1.ImagesUpdateRequest_builder{
				Object: privatev1.Image_builder{
					Id:         privateObj.GetId(),
					Title:      "Test",
					SourceType: "s3",
					SourceRef:  "quay.io/test/image:latest",
					BootMethod: "cloud-init",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("source_type"))
			Expect(err.Error()).To(ContainSubstring("immutable"))
		})

		It("Rejects update changing boot_method", func() {
			privateObj := createImage()

			_, err := privateServer.Update(ctx, privatev1.ImagesUpdateRequest_builder{
				Object: privatev1.Image_builder{
					Id:         privateObj.GetId(),
					Title:      "Test",
					SourceType: "registry",
					SourceRef:  "quay.io/test/image:latest",
					BootMethod: "kickstart",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("boot_method"))
			Expect(err.Error()).To(ContainSubstring("immutable"))
		})
	})
})
