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
	"context"
	"errors"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type SSHKeysServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ publicv1.SSHKeysServer = (*SSHKeysServer)(nil)

type SSHKeysServer struct {
	publicv1.UnimplementedSSHKeysServer

	logger    *slog.Logger
	delegate  privatev1.SSHKeysServer
	inMapper  *GenericMapper[*publicv1.SSHKey, *privatev1.SSHKey]
	outMapper *GenericMapper[*privatev1.SSHKey, *publicv1.SSHKey]
}

func NewSSHKeysServer() *SSHKeysServerBuilder {
	return &SSHKeysServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *SSHKeysServerBuilder) SetLogger(value *slog.Logger) *SSHKeysServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *SSHKeysServerBuilder) SetNotifier(value *database.Notifier) *SSHKeysServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is optional.
func (b *SSHKeysServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *SSHKeysServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *SSHKeysServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *SSHKeysServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *SSHKeysServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *SSHKeysServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *SSHKeysServerBuilder) Build() (result *SSHKeysServer, err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	// Create the mappers:
	inMapper, err := NewGenericMapper[*publicv1.SSHKey, *privatev1.SSHKey]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.SSHKey, *publicv1.SSHKey]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivateSSHKeysServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &SSHKeysServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *SSHKeysServer) List(ctx context.Context,
	request *publicv1.SSHKeysListRequest) (response *publicv1.SSHKeysListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.SSHKeysListRequest{}
	privateRequest.SetOffset(request.GetOffset())
	privateRequest.SetLimit(request.GetLimit())
	privateRequest.SetFilter(request.GetFilter())
	privateRequest.SetOrder(request.GetOrder())

	// Delegate to private server:
	privateResponse, err := s.delegate.List(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateItems := privateResponse.GetItems()
	publicItems := make([]*publicv1.SSHKey, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &publicv1.SSHKey{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private SSH key to public",
				slog.Any("error", err),
			)
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process SSH keys")
		}
		publicItems[i] = publicItem
	}

	// Create the public response:
	response = &publicv1.SSHKeysListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *SSHKeysServer) Get(ctx context.Context,
	request *publicv1.SSHKeysGetRequest) (response *publicv1.SSHKeysGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.SSHKeysGetRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateSSHKey := privateResponse.GetObject()
	publicSSHKey := &publicv1.SSHKey{}
	err = s.outMapper.Copy(ctx, privateSSHKey, publicSSHKey)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private SSH key to public",
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process SSH key")
	}

	// Create the public response:
	response = &publicv1.SSHKeysGetResponse{}
	response.SetObject(publicSSHKey)
	return
}

func (s *SSHKeysServer) Create(ctx context.Context,
	request *publicv1.SSHKeysCreateRequest) (response *publicv1.SSHKeysCreateResponse, err error) {
	// Map the public SSH key to private format:
	publicSSHKey := request.GetObject()
	if publicSSHKey == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateSSHKey := &privatev1.SSHKey{}
	err = s.inMapper.Copy(ctx, publicSSHKey, privateSSHKey)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public SSH key to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process SSH key")
		return
	}

	// Delegate to the private server:
	privateRequest := &privatev1.SSHKeysCreateRequest{}
	privateRequest.SetObject(privateSSHKey)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	createdPrivateSSHKey := privateResponse.GetObject()
	createdPublicSSHKey := &publicv1.SSHKey{}
	err = s.outMapper.Copy(ctx, createdPrivateSSHKey, createdPublicSSHKey)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private SSH key to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process SSH key")
		return
	}

	// Create the public response:
	response = &publicv1.SSHKeysCreateResponse{}
	response.SetObject(createdPublicSSHKey)
	return
}

func (s *SSHKeysServer) Update(ctx context.Context,
	request *publicv1.SSHKeysUpdateRequest) (response *publicv1.SSHKeysUpdateResponse, err error) {
	// Validate the request:
	publicSSHKey := request.GetObject()
	if publicSSHKey == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	id := publicSSHKey.GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	// Get the existing object from the private server:
	getRequest := &privatev1.SSHKeysGetRequest{}
	getRequest.SetId(id)
	getResponse, err := s.delegate.Get(ctx, getRequest)
	if err != nil {
		return nil, err
	}
	existingPrivateSSHKey := getResponse.GetObject()

	// Map the public changes to the existing private object (preserving private data):
	err = s.inMapper.Copy(ctx, publicSSHKey, existingPrivateSSHKey)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public SSH key to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process SSH key")
		return
	}

	// Delegate to the private server with the merged object:
	privateRequest := &privatev1.SSHKeysUpdateRequest{}
	privateRequest.SetObject(existingPrivateSSHKey)
	privateRequest.SetUpdateMask(request.GetUpdateMask())
	privateRequest.SetLock(request.GetLock())
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	updatedPrivateSSHKey := privateResponse.GetObject()
	updatedPublicSSHKey := &publicv1.SSHKey{}
	err = s.outMapper.Copy(ctx, updatedPrivateSSHKey, updatedPublicSSHKey)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private SSH key to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process SSH key")
		return
	}

	// Create the public response:
	response = &publicv1.SSHKeysUpdateResponse{}
	response.SetObject(updatedPublicSSHKey)
	return
}

func (s *SSHKeysServer) Delete(ctx context.Context,
	request *publicv1.SSHKeysDeleteRequest) (response *publicv1.SSHKeysDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.SSHKeysDeleteRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	_, err = s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Create the public response:
	response = &publicv1.SSHKeysDeleteResponse{}
	return
}
