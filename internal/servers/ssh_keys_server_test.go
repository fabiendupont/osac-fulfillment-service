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

// testSSHPublicKey is a valid SSH ed25519 public key for testing.
const testSSHPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@example.com"

// testSSHPublicKey2 is a second valid SSH ed25519 public key for testing key updates.
const testSSHPublicKey2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGrDHKRhPSoxo3aN5oAK5pUR7fFJMAN0MwBkbyf4vrFn test2@example.com"

var _ = Describe("SSH keys server", func() {
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
		err = dao.CreateTables[*publicv1.SSHKey](ctx)
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("Creation", func() {
		It("Can be built if all the required parameters are set", func() {
			server, err := NewSSHKeysServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
			Expect(server).ToNot(BeNil())
		})

		It("Fails if logger is not set", func() {
			server, err := NewSSHKeysServer().
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(server).To(BeNil())
		})

		It("Fails if tenancy logic is not set", func() {
			server, err := NewSSHKeysServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				Build()
			Expect(err).To(MatchError("tenancy logic is mandatory"))
			Expect(server).To(BeNil())
		})
	})

	Describe("Behaviour", func() {
		var (
			publicServer  *SSHKeysServer
			privateServer *PrivateSSHKeysServer
		)

		BeforeEach(func() {
			var err error

			// Create the public server:
			publicServer, err = NewSSHKeysServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())

			// Create a private server for test data setup:
			privateServer, err = NewPrivateSSHKeysServer().
				SetLogger(logger).
				SetAttributionLogic(attribution).
				SetTenancyLogic(tenancy).
				Build()
			Expect(err).ToNot(HaveOccurred())
		})

		// createSSHKey creates an SSHKey via the private server and returns the created object.
		createSSHKey := func() *privatev1.SSHKey {
			response, err := privateServer.Create(ctx, privatev1.SSHKeysCreateRequest_builder{
				Object: privatev1.SSHKey_builder{
					PublicKey: testSSHPublicKey,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			return response.GetObject()
		}

		It("List objects", func() {
			// Create a few objects via the private server:
			const count = 10
			for range count {
				createSSHKey()
			}

			// List the objects via public server:
			response, err := publicServer.List(ctx, publicv1.SSHKeysListRequest_builder{}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response).ToNot(BeNil())
			items := response.GetItems()
			Expect(items).To(HaveLen(count))
		})

		It("List objects with limit", func() {
			// Create a few objects via the private server:
			const count = 10
			for range count {
				createSSHKey()
			}

			// List the objects via public server:
			response, err := publicServer.List(ctx, publicv1.SSHKeysListRequest_builder{
				Limit: proto.Int32(1),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetSize()).To(BeNumerically("==", 1))
		})

		It("List objects with offset", func() {
			// Create a few objects via the private server:
			const count = 10
			for range count {
				createSSHKey()
			}

			// List the objects via public server:
			response, err := publicServer.List(ctx, publicv1.SSHKeysListRequest_builder{
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
				obj := createSSHKey()
				ids = append(ids, obj.GetId())
			}

			// List the objects via public server:
			for _, id := range ids {
				response, err := publicServer.List(ctx, publicv1.SSHKeysListRequest_builder{
					Filter: proto.String(fmt.Sprintf("this.id == '%s'", id)),
				}.Build())
				Expect(err).ToNot(HaveOccurred())
				Expect(response.GetSize()).To(BeNumerically("==", 1))
				Expect(response.GetItems()[0].GetId()).To(Equal(id))
			}
		})

		It("Get object", func() {
			// Create the object via the private server:
			privateObj := createSSHKey()

			// Get it via public server:
			getResponse, err := publicServer.Get(ctx, publicv1.SSHKeysGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			publicObj := getResponse.GetObject()
			Expect(publicObj.GetId()).To(Equal(privateObj.GetId()))
			Expect(publicObj.GetPublicKey()).To(Equal(privateObj.GetPublicKey()))
			Expect(publicObj.GetFingerprint()).To(Equal(privateObj.GetFingerprint()))
		})

		It("Delete object", func() {
			// Create the object via the private server:
			privateObj := createSSHKey()

			// Add a finalizer so the object is not immediately archived:
			_, err := tx.Exec(
				ctx,
				`update ssh_keys set finalizers = '{"a"}' where id = $1`,
				privateObj.GetId(),
			)
			Expect(err).ToNot(HaveOccurred())

			// Delete the object via public server:
			_, err = publicServer.Delete(ctx, publicv1.SSHKeysDeleteRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			// Get and verify via public server:
			getResponse, err := publicServer.Get(ctx, publicv1.SSHKeysGetRequest_builder{
				Id: privateObj.GetId(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			object := getResponse.GetObject()
			Expect(object.GetMetadata().GetDeletionTimestamp()).ToNot(BeNil())
		})

		It("Generates UUID for id ignoring caller-provided value", func() {
			callerProvidedId := "my-custom-id"
			response, err := privateServer.Create(ctx, privatev1.SSHKeysCreateRequest_builder{
				Object: privatev1.SSHKey_builder{
					Id:        callerProvidedId,
					PublicKey: testSSHPublicKey,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			Expect(response.GetObject().GetId()).ToNot(Equal(callerProvidedId))
			Expect(response.GetObject().GetId()).ToNot(BeEmpty())
		})

		It("Computes fingerprint on create", func() {
			privateObj := createSSHKey()
			Expect(privateObj.GetFingerprint()).ToNot(BeEmpty())
			Expect(privateObj.GetFingerprint()).To(HavePrefix("SHA256:"))
		})

		It("Recomputes fingerprint when public_key changes on update", func() {
			// Create the object:
			privateObj := createSSHKey()
			originalFingerprint := privateObj.GetFingerprint()

			// Update with a different key:
			updateResponse, err := privateServer.Update(ctx, privatev1.SSHKeysUpdateRequest_builder{
				Object: privatev1.SSHKey_builder{
					Id:        privateObj.GetId(),
					PublicKey: testSSHPublicKey2,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			updatedObj := updateResponse.GetObject()
			Expect(updatedObj.GetFingerprint()).ToNot(BeEmpty())
			Expect(updatedObj.GetFingerprint()).To(HavePrefix("SHA256:"))
			Expect(updatedObj.GetFingerprint()).ToNot(Equal(originalFingerprint))
		})

		It("Preserves fingerprint when public_key does not change on update", func() {
			// Create the object:
			privateObj := createSSHKey()
			originalFingerprint := privateObj.GetFingerprint()

			// Update without changing the key:
			updateResponse, err := privateServer.Update(ctx, privatev1.SSHKeysUpdateRequest_builder{
				Object: privatev1.SSHKey_builder{
					Id:        privateObj.GetId(),
					PublicKey: testSSHPublicKey,
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())

			updatedObj := updateResponse.GetObject()
			Expect(updatedObj.GetFingerprint()).To(Equal(originalFingerprint))
		})

		It("Rejects create without public_key", func() {
			_, err := privateServer.Create(ctx, privatev1.SSHKeysCreateRequest_builder{
				Object: privatev1.SSHKey_builder{}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("public_key"))
		})

		It("Rejects create with invalid public_key", func() {
			_, err := privateServer.Create(ctx, privatev1.SSHKeysCreateRequest_builder{
				Object: privatev1.SSHKey_builder{
					PublicKey: "not-a-valid-ssh-key",
				}.Build(),
			}.Build())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("public_key"))
		})

		It("Client-provided fingerprint is ignored on create", func() {
			response, err := privateServer.Create(ctx, privatev1.SSHKeysCreateRequest_builder{
				Object: privatev1.SSHKey_builder{
					PublicKey:   testSSHPublicKey,
					Fingerprint: "SHA256:fakefingerprint",
				}.Build(),
			}.Build())
			Expect(err).ToNot(HaveOccurred())
			// The fingerprint should be computed from the key, not the client-provided value:
			Expect(response.GetObject().GetFingerprint()).ToNot(Equal("SHA256:fakefingerprint"))
			Expect(response.GetObject().GetFingerprint()).To(HavePrefix("SHA256:"))
		})
	})
})
