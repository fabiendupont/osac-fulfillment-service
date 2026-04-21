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
	"strings"

	. "github.com/onsi/ginkgo/v2/dsl/core"
	. "github.com/onsi/gomega"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
)

var _ = Describe("Private SSH keys", func() {
	var (
		ctx    context.Context
		client privatev1.SSHKeysClient
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = privatev1.NewSSHKeysClient(tool.AdminConn())
	})

	It("Creates an SSH key and computes fingerprint", func() {
		response, err := client.Create(ctx, privatev1.SSHKeysCreateRequest_builder{
			Object: privatev1.SSHKey_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "test-workstation-key",
				}.Build(),
				PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl test@workstation",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(response).ToNot(BeNil())
		object := response.GetObject()
		Expect(object).ToNot(BeNil())
		Expect(object.GetId()).ToNot(BeEmpty())
		Expect(object.GetFingerprint()).ToNot(BeEmpty())
		Expect(strings.HasPrefix(object.GetFingerprint(), "SHA256:")).To(BeTrue())
	})

	It("Rejects creation without public_key", func() {
		_, err := client.Create(ctx, privatev1.SSHKeysCreateRequest_builder{
			Object: privatev1.SSHKey_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "no-key",
				}.Build(),
			}.Build(),
		}.Build())
		Expect(err).To(HaveOccurred())
	})

	It("Updates public_key and recomputes fingerprint", func() {
		createResp, err := client.Create(ctx, privatev1.SSHKeysCreateRequest_builder{
			Object: privatev1.SSHKey_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "update-test-key",
				}.Build(),
				PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl original@host",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResp.GetObject().GetId()
		originalFingerprint := createResp.GetObject().GetFingerprint()

		updateResp, err := client.Update(ctx, privatev1.SSHKeysUpdateRequest_builder{
			Object: privatev1.SSHKey_builder{
				Id:        id,
				PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIG7FRGCPMsLqLwB+wVDqoX4GRhMNovGHPRvavU1VpMiL different@host",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		newFingerprint := updateResp.GetObject().GetFingerprint()
		Expect(newFingerprint).ToNot(Equal(originalFingerprint))
	})

	It("Lists, gets, and deletes SSH keys", func() {
		createResp, err := client.Create(ctx, privatev1.SSHKeysCreateRequest_builder{
			Object: privatev1.SSHKey_builder{
				Metadata: privatev1.Metadata_builder{
					Name: "crud-test-key",
				}.Build(),
				PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl crud@host",
			}.Build(),
		}.Build())
		Expect(err).ToNot(HaveOccurred())
		id := createResp.GetObject().GetId()

		getResp, err := client.Get(ctx, privatev1.SSHKeysGetRequest_builder{Id: id}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(getResp.GetObject().GetFingerprint()).ToNot(BeEmpty())

		listResp, err := client.List(ctx, privatev1.SSHKeysListRequest_builder{}.Build())
		Expect(err).ToNot(HaveOccurred())
		Expect(listResp.GetItems()).ToNot(BeEmpty())

		_, err = client.Delete(ctx, privatev1.SSHKeysDeleteRequest_builder{Id: id}.Build())
		Expect(err).ToNot(HaveOccurred())
	})
})
