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
	"errors"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/crypto/ssh"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type PrivateSSHKeysServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.SSHKeysServer = (*PrivateSSHKeysServer)(nil)

type PrivateSSHKeysServer struct {
	privatev1.UnimplementedSSHKeysServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.SSHKey]
}

func NewPrivateSSHKeysServer() *PrivateSSHKeysServerBuilder {
	return &PrivateSSHKeysServerBuilder{}
}

func (b *PrivateSSHKeysServerBuilder) SetLogger(value *slog.Logger) *PrivateSSHKeysServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateSSHKeysServerBuilder) SetNotifier(value *database.Notifier) *PrivateSSHKeysServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateSSHKeysServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateSSHKeysServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateSSHKeysServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateSSHKeysServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateSSHKeysServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateSSHKeysServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateSSHKeysServerBuilder) Build() (result *PrivateSSHKeysServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the generic server:
	generic, err := NewGenericServer[*privatev1.SSHKey]().
		SetLogger(b.logger).
		SetService(privatev1.SSHKeys_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateSSHKeysServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateSSHKeysServer) List(ctx context.Context,
	request *privatev1.SSHKeysListRequest) (response *privatev1.SSHKeysListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateSSHKeysServer) Get(ctx context.Context,
	request *privatev1.SSHKeysGetRequest) (response *privatev1.SSHKeysGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateSSHKeysServer) Create(ctx context.Context,
	request *privatev1.SSHKeysCreateRequest) (response *privatev1.SSHKeysCreateResponse, err error) {
	// Validate the SSH key:
	key := request.GetObject()
	err = s.validateSSHKey(ctx, key)
	if err != nil {
		return
	}

	// Compute the fingerprint from the public key:
	fingerprint, err := computeSSHFingerprint(key.GetPublicKey())
	if err != nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'public_key' is not a valid SSH public key: %v", err)
		return
	}
	key.SetFingerprint(fingerprint)

	// Clear any caller-provided ID so the DAO always generates a UUID.
	key.SetId("")

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateSSHKeysServer) Update(ctx context.Context,
	request *privatev1.SSHKeysUpdateRequest) (response *privatev1.SSHKeysUpdateResponse, err error) {
	// Validate the update:
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	// Get existing object:
	getRequest := &privatev1.SSHKeysGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.SSHKeysGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existingKey := getResponse.GetObject()
	updatedKey := request.GetObject()

	// If public_key changed, recompute fingerprint:
	newPublicKey := updatedKey.GetPublicKey()
	if newPublicKey != "" && newPublicKey != existingKey.GetPublicKey() {
		fingerprint, fpErr := computeSSHFingerprint(newPublicKey)
		if fpErr != nil {
			err = grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'public_key' is not a valid SSH public key: %v", fpErr)
			return
		}
		updatedKey.SetFingerprint(fingerprint)
	} else {
		// Preserve existing fingerprint:
		updatedKey.SetFingerprint(existingKey.GetFingerprint())
	}

	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateSSHKeysServer) Delete(ctx context.Context,
	request *privatev1.SSHKeysDeleteRequest) (response *privatev1.SSHKeysDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateSSHKeysServer) Signal(ctx context.Context,
	request *privatev1.SSHKeysSignalRequest) (response *privatev1.SSHKeysSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// validateSSHKey validates the SSHKey object.
func (s *PrivateSSHKeysServer) validateSSHKey(ctx context.Context,
	key *privatev1.SSHKey) error {

	if key == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "ssh key is mandatory")
	}

	// SK-VAL-01: public_key is required
	if key.GetPublicKey() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'public_key' is required")
	}

	return nil
}

// computeSSHFingerprint parses an SSH public key in authorized_keys format
// and returns its SHA256 fingerprint.
func computeSSHFingerprint(publicKey string) (string, error) {
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(pubKey), nil
}
