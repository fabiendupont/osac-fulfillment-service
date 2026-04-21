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

type ComputeInstanceClassesServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ publicv1.ComputeInstanceClassesServer = (*ComputeInstanceClassesServer)(nil)

type ComputeInstanceClassesServer struct {
	publicv1.UnimplementedComputeInstanceClassesServer

	logger    *slog.Logger
	delegate  privatev1.ComputeInstanceClassesServer
	inMapper  *GenericMapper[*publicv1.ComputeInstanceClass, *privatev1.ComputeInstanceClass]
	outMapper *GenericMapper[*privatev1.ComputeInstanceClass, *publicv1.ComputeInstanceClass]
}

func NewComputeInstanceClassesServer() *ComputeInstanceClassesServerBuilder {
	return &ComputeInstanceClassesServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *ComputeInstanceClassesServerBuilder) SetLogger(value *slog.Logger) *ComputeInstanceClassesServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *ComputeInstanceClassesServerBuilder) SetNotifier(value *database.Notifier) *ComputeInstanceClassesServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is optional.
func (b *ComputeInstanceClassesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *ComputeInstanceClassesServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *ComputeInstanceClassesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *ComputeInstanceClassesServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *ComputeInstanceClassesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *ComputeInstanceClassesServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *ComputeInstanceClassesServerBuilder) Build() (result *ComputeInstanceClassesServer, err error) {
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
	inMapper, err := NewGenericMapper[*publicv1.ComputeInstanceClass, *privatev1.ComputeInstanceClass]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.ComputeInstanceClass, *publicv1.ComputeInstanceClass]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivateComputeInstanceClassesServer().
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
	result = &ComputeInstanceClassesServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *ComputeInstanceClassesServer) List(ctx context.Context,
	request *publicv1.ComputeInstanceClassesListRequest) (response *publicv1.ComputeInstanceClassesListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.ComputeInstanceClassesListRequest{}
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
	publicItems := make([]*publicv1.ComputeInstanceClass, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &publicv1.ComputeInstanceClass{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private compute instance class to public",
				slog.Any("error", err),
			)
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance classes")
		}
		publicItems[i] = publicItem
	}

	// Create the public response:
	response = &publicv1.ComputeInstanceClassesListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *ComputeInstanceClassesServer) Get(ctx context.Context,
	request *publicv1.ComputeInstanceClassesGetRequest) (response *publicv1.ComputeInstanceClassesGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.ComputeInstanceClassesGetRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateComputeInstanceClass := privateResponse.GetObject()
	publicComputeInstanceClass := &publicv1.ComputeInstanceClass{}
	err = s.outMapper.Copy(ctx, privateComputeInstanceClass, publicComputeInstanceClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private compute instance class to public",
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance class")
	}

	// Create the public response:
	response = &publicv1.ComputeInstanceClassesGetResponse{}
	response.SetObject(publicComputeInstanceClass)
	return
}

func (s *ComputeInstanceClassesServer) Create(ctx context.Context,
	request *publicv1.ComputeInstanceClassesCreateRequest) (response *publicv1.ComputeInstanceClassesCreateResponse, err error) {
	// Map the public compute instance class to private format:
	publicComputeInstanceClass := request.GetObject()
	if publicComputeInstanceClass == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateComputeInstanceClass := &privatev1.ComputeInstanceClass{}
	err = s.inMapper.Copy(ctx, publicComputeInstanceClass, privateComputeInstanceClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public compute instance class to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance class")
		return
	}

	// Delegate to the private server:
	privateRequest := &privatev1.ComputeInstanceClassesCreateRequest{}
	privateRequest.SetObject(privateComputeInstanceClass)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	createdPrivateComputeInstanceClass := privateResponse.GetObject()
	createdPublicComputeInstanceClass := &publicv1.ComputeInstanceClass{}
	err = s.outMapper.Copy(ctx, createdPrivateComputeInstanceClass, createdPublicComputeInstanceClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private compute instance class to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance class")
		return
	}

	// Create the public response:
	response = &publicv1.ComputeInstanceClassesCreateResponse{}
	response.SetObject(createdPublicComputeInstanceClass)
	return
}

func (s *ComputeInstanceClassesServer) Update(ctx context.Context,
	request *publicv1.ComputeInstanceClassesUpdateRequest) (response *publicv1.ComputeInstanceClassesUpdateResponse, err error) {
	// Validate the request:
	publicComputeInstanceClass := request.GetObject()
	if publicComputeInstanceClass == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	id := publicComputeInstanceClass.GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	// Get the existing object from the private server:
	getRequest := &privatev1.ComputeInstanceClassesGetRequest{}
	getRequest.SetId(id)
	getResponse, err := s.delegate.Get(ctx, getRequest)
	if err != nil {
		return nil, err
	}
	existingPrivateComputeInstanceClass := getResponse.GetObject()

	// Map the public changes to the existing private object (preserving private data):
	err = s.inMapper.Copy(ctx, publicComputeInstanceClass, existingPrivateComputeInstanceClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public compute instance class to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance class")
		return
	}

	// Delegate to the private server with the merged object:
	privateRequest := &privatev1.ComputeInstanceClassesUpdateRequest{}
	privateRequest.SetObject(existingPrivateComputeInstanceClass)
	privateRequest.SetLock(request.GetLock())
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	updatedPrivateComputeInstanceClass := privateResponse.GetObject()
	updatedPublicComputeInstanceClass := &publicv1.ComputeInstanceClass{}
	err = s.outMapper.Copy(ctx, updatedPrivateComputeInstanceClass, updatedPublicComputeInstanceClass)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private compute instance class to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance class")
		return
	}

	// Create the public response:
	response = &publicv1.ComputeInstanceClassesUpdateResponse{}
	response.SetObject(updatedPublicComputeInstanceClass)
	return
}

func (s *ComputeInstanceClassesServer) Delete(ctx context.Context,
	request *publicv1.ComputeInstanceClassesDeleteRequest) (response *publicv1.ComputeInstanceClassesDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.ComputeInstanceClassesDeleteRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	_, err = s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Create the public response:
	response = &publicv1.ComputeInstanceClassesDeleteResponse{}
	return
}
