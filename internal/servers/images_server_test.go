/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package servers

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/protobuf/proto"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/database"
	"github.com/osac-project/fulfillment-service/internal/database/dao"
)

var _ = Describe("Images server", func() {
	var (
		ctx context.Context
		tx  database.Tx
	)

	BeforeEach(func() {
		var err error

		// Create a context:
		ctx = context.Background()

		// Prepare the database pool:
		db := server.MakeDatabase()
		DeferCleanup(db.Close)
		pool, err := pgxpool.New(ctx, db.MakeURL())
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(pool.Close)

		// Create the transaction manager:
		tm, err := database.NewTxManager().
			SetLogger(logger).
			SetPool(pool).
			Build()
		Expect(err).ToNot(HaveOccurred())

		// Start a transaction and add it to the context:
		tx, err = tm.Begin(ctx)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() {
			err := tm.End(ctx, tx)
			Expect(err).ToNot(HaveOccurred())
		})
		ctx = database.TxIntoContext(ctx, tx)

		// Create the tables:
		err = dao.CreateTables[*publicv1.Image](ctx)
		Expect(err).ToNot(HaveOccurred())
	})

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

			// Create the public server:
			publicServer, err = NewImagesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create a private server for test data setup:
			privateServer, err = NewPrivateImagesServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		// createImage creates an Image via the private server and returns the created object.
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
			// Create a few objects via the private server:
			const count = 10
			for range count {
				createImage()
			}

			// List the objects via public server:
			response, err := publicServer.List(ctx, publicv1.ImagesListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects via the private server:
			const count = 10
			for range count {
				createImage()
			}

			// List the objects via public server:
			response, err := publicServer.List(ctx, publicv1.ImagesListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects via the private server:
			const count = 10
			for range count {
				createImage()
			}

			// List the objects via public server:
			response, err := publicServer.List(ctx, publicv1.ImagesListRequest_builder{
				Offset: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", count-1))
		})

		It("List objects with filter", func() {
			// Create a few objects via the private server:
			const count = 10
			var ids []string
			for range count {
				obj := createImage()
				ids = append(ids, obj.GetId())
			}

			// List the objects via public server:
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
			// Create the object via the private server:
			privateObj := createImage()

			// Get it via public server:
			getResponse, err := publicServer.Get(ctx, publicv1.ImagesGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			publicObj := getResponse.GetObject()
			Expect(publicObj.GetId()).To(Equal(privateObj.GetId()))
			Expect(publicObj.GetTitle()).To(Equal(privateObj.GetTitle()))
		})

		It("Update object", func() {
			// Create the object via the private server:
			privateObj := createImage()

			// Update the object via public server:
			updateResponse, err := publicServer.Update(ctx, publicv1.ImagesUpdateRequest_builder{
				Object: publicv1.Image_builder{
					Id:          privateObj.GetId(),
					Title:       "Updated RHEL 9.4",
					Description: "Updated description.",
					SourceType:  "registry",
					SourceRef:   "quay.io/rhel/rhel9:9.4-updated",
					BootMethod:  "cloud-init",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(updateResponse.GetObject().GetTitle()).To(Equal("Updated RHEL 9.4"))
			Expect(updateResponse.GetObject().GetDescription()).To(Equal("Updated description."))

			// Get and verify via public server:
			getResponse, err := publicServer.Get(ctx, publicv1.ImagesGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(getResponse.GetObject().GetTitle()).To(Equal("Updated RHEL 9.4"))
			Expect(getResponse.GetObject().GetDescription()).To(Equal("Updated description."))
		})

		It("Delete object", func() {
			// Create the object via the private server:
			privateObj := createImage()

			// Add a finalizer so the object is not immediately archived:
			_, err := tx.Exec(
				ctx,
				`update images set finalizers = '{"a"}' where id = $1`,
				privateObj.GetId(),
			)
			Expect(err).ToNot(HaveOccurred())

			// Delete the object via public server:
			_, err = publicServer.Delete(ctx, publicv1.ImagesDeleteRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify via public server:
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
