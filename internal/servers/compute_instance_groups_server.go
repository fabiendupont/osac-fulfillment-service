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

type ComputeInstanceGroupsServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ publicv1.ComputeInstanceGroupsServer = (*ComputeInstanceGroupsServer)(nil)

type ComputeInstanceGroupsServer struct {
	publicv1.UnimplementedComputeInstanceGroupsServer

	logger    *slog.Logger
	delegate  privatev1.ComputeInstanceGroupsServer
	inMapper  *GenericMapper[*publicv1.ComputeInstanceGroup, *privatev1.ComputeInstanceGroup]
	outMapper *GenericMapper[*privatev1.ComputeInstanceGroup, *publicv1.ComputeInstanceGroup]
}

func NewComputeInstanceGroupsServer() *ComputeInstanceGroupsServerBuilder {
	return &ComputeInstanceGroupsServerBuilder{}
}

func (b *ComputeInstanceGroupsServerBuilder) SetLogger(value *slog.Logger) *ComputeInstanceGroupsServerBuilder {
	b.logger = value
	return b
}

func (b *ComputeInstanceGroupsServerBuilder) SetNotifier(value *database.Notifier) *ComputeInstanceGroupsServerBuilder {
	b.notifier = value
	return b
}

func (b *ComputeInstanceGroupsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *ComputeInstanceGroupsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *ComputeInstanceGroupsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *ComputeInstanceGroupsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *ComputeInstanceGroupsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *ComputeInstanceGroupsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *ComputeInstanceGroupsServerBuilder) Build() (result *ComputeInstanceGroupsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	inMapper, err := NewGenericMapper[*publicv1.ComputeInstanceGroup, *privatev1.ComputeInstanceGroup]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.ComputeInstanceGroup, *publicv1.ComputeInstanceGroup]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	delegate, err := NewPrivateComputeInstanceGroupsServer().
		SetLogger(b.logger).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	result = &ComputeInstanceGroupsServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *ComputeInstanceGroupsServer) List(ctx context.Context,
	request *publicv1.ComputeInstanceGroupsListRequest) (response *publicv1.ComputeInstanceGroupsListResponse, err error) {
	privateRequest := &privatev1.ComputeInstanceGroupsListRequest{}
	privateRequest.SetOffset(request.GetOffset())
	privateRequest.SetLimit(request.GetLimit())
	privateRequest.SetFilter(request.GetFilter())

	privateResponse, err := s.delegate.List(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	privateItems := privateResponse.GetItems()
	publicItems := make([]*publicv1.ComputeInstanceGroup, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &publicv1.ComputeInstanceGroup{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(ctx, "Failed to map private compute instance group to public", slog.Any("error", err))
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance groups")
		}
		publicItems[i] = publicItem
	}

	response = &publicv1.ComputeInstanceGroupsListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *ComputeInstanceGroupsServer) Get(ctx context.Context,
	request *publicv1.ComputeInstanceGroupsGetRequest) (response *publicv1.ComputeInstanceGroupsGetResponse, err error) {
	privateRequest := &privatev1.ComputeInstanceGroupsGetRequest{}
	privateRequest.SetId(request.GetId())

	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	publicCIG := &publicv1.ComputeInstanceGroup{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), publicCIG)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map private compute instance group to public", slog.Any("error", err))
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance group")
	}

	response = &publicv1.ComputeInstanceGroupsGetResponse{}
	response.SetObject(publicCIG)
	return
}

func (s *ComputeInstanceGroupsServer) Create(ctx context.Context,
	request *publicv1.ComputeInstanceGroupsCreateRequest) (response *publicv1.ComputeInstanceGroupsCreateResponse, err error) {
	publicCIG := request.GetObject()
	if publicCIG == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateCIG := &privatev1.ComputeInstanceGroup{}
	err = s.inMapper.Copy(ctx, publicCIG, privateCIG)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map public compute instance group to private", slog.Any("error", err))
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance group")
		return
	}

	privateRequest := &privatev1.ComputeInstanceGroupsCreateRequest{}
	privateRequest.SetObject(privateCIG)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	createdPublicCIG := &publicv1.ComputeInstanceGroup{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), createdPublicCIG)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map private compute instance group to public", slog.Any("error", err))
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance group")
		return
	}

	response = &publicv1.ComputeInstanceGroupsCreateResponse{}
	response.SetObject(createdPublicCIG)
	return
}

func (s *ComputeInstanceGroupsServer) Update(ctx context.Context,
	request *publicv1.ComputeInstanceGroupsUpdateRequest) (response *publicv1.ComputeInstanceGroupsUpdateResponse, err error) {
	publicCIG := request.GetObject()
	if publicCIG == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	id := publicCIG.GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.ComputeInstanceGroupsGetRequest{}
	getRequest.SetId(id)
	getResponse, err := s.delegate.Get(ctx, getRequest)
	if err != nil {
		return nil, err
	}
	existingPrivateCIG := getResponse.GetObject()

	err = s.inMapper.Copy(ctx, publicCIG, existingPrivateCIG)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map public compute instance group to private", slog.Any("error", err))
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance group")
		return
	}

	privateRequest := &privatev1.ComputeInstanceGroupsUpdateRequest{}
	privateRequest.SetObject(existingPrivateCIG)
	privateRequest.SetLock(request.GetLock())
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	updatedPublicCIG := &publicv1.ComputeInstanceGroup{}
	err = s.outMapper.Copy(ctx, privateResponse.GetObject(), updatedPublicCIG)
	if err != nil {
		s.logger.ErrorContext(ctx, "Failed to map private compute instance group to public", slog.Any("error", err))
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process compute instance group")
		return
	}

	response = &publicv1.ComputeInstanceGroupsUpdateResponse{}
	response.SetObject(updatedPublicCIG)
	return
}

func (s *ComputeInstanceGroupsServer) Delete(ctx context.Context,
	request *publicv1.ComputeInstanceGroupsDeleteRequest) (response *publicv1.ComputeInstanceGroupsDeleteResponse, err error) {
	privateRequest := &privatev1.ComputeInstanceGroupsDeleteRequest{}
	privateRequest.SetId(request.GetId())

	_, err = s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	response = &publicv1.ComputeInstanceGroupsDeleteResponse{}
	return
}
