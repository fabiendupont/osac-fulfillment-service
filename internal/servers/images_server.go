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
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type ImagesServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ publicv1.ImagesServer = (*ImagesServer)(nil)

type ImagesServer struct {
	publicv1.UnimplementedImagesServer

	logger    *slog.Logger
	delegate  privatev1.ImagesServer
	inMapper  *GenericMapper[*publicv1.Image, *privatev1.Image]
	outMapper *GenericMapper[*privatev1.Image, *publicv1.Image]
}

func NewImagesServer() *ImagesServerBuilder {
	return &ImagesServerBuilder{}
}

// SetLogger sets the logger to use. This is mandatory.
func (b *ImagesServerBuilder) SetLogger(value *slog.Logger) *ImagesServerBuilder {
	b.logger = value
	return b
}

// SetNotifier sets the notifier to use. This is optional.
func (b *ImagesServerBuilder) SetNotifier(value *database.Notifier) *ImagesServerBuilder {
	b.notifier = value
	return b
}

// SetAttributionLogic sets the attribution logic to use. This is optional.
func (b *ImagesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *ImagesServerBuilder {
	b.attributionLogic = value
	return b
}

// SetTenancyLogic sets the tenancy logic to use. This is mandatory.
func (b *ImagesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *ImagesServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *ImagesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *ImagesServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *ImagesServerBuilder) Build() (result *ImagesServer, err error) {
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
	inMapper, err := NewGenericMapper[*publicv1.Image, *privatev1.Image]().
		SetLogger(b.logger).
		SetStrict(true).
		Build()
	if err != nil {
		return
	}
	outMapper, err := NewGenericMapper[*privatev1.Image, *publicv1.Image]().
		SetLogger(b.logger).
		SetStrict(false).
		Build()
	if err != nil {
		return
	}

	// Create the private server to delegate to:
	delegate, err := NewPrivateImagesServer().
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
	result = &ImagesServer{
		logger:    b.logger,
		delegate:  delegate,
		inMapper:  inMapper,
		outMapper: outMapper,
	}
	return
}

func (s *ImagesServer) List(ctx context.Context,
	request *publicv1.ImagesListRequest) (response *publicv1.ImagesListResponse, err error) {
	// Create private request with same parameters:
	privateRequest := &privatev1.ImagesListRequest{}
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
	publicItems := make([]*publicv1.Image, len(privateItems))
	for i, privateItem := range privateItems {
		publicItem := &publicv1.Image{}
		err = s.outMapper.Copy(ctx, privateItem, publicItem)
		if err != nil {
			s.logger.ErrorContext(
				ctx,
				"Failed to map private image to public",
				slog.Any("error", err),
			)
			return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process images")
		}
		publicItems[i] = publicItem
	}

	// Create the public response:
	response = &publicv1.ImagesListResponse{}
	response.SetSize(privateResponse.GetSize())
	response.SetTotal(privateResponse.GetTotal())
	response.SetItems(publicItems)
	return
}

func (s *ImagesServer) Get(ctx context.Context,
	request *publicv1.ImagesGetRequest) (response *publicv1.ImagesGetResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.ImagesGetRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	privateResponse, err := s.delegate.Get(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map private response to public format:
	privateImage := privateResponse.GetObject()
	publicImage := &publicv1.Image{}
	err = s.outMapper.Copy(ctx, privateImage, publicImage)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private image to public",
			slog.Any("error", err),
		)
		return nil, grpcstatus.Errorf(grpccodes.Internal, "failed to process image")
	}

	// Create the public response:
	response = &publicv1.ImagesGetResponse{}
	response.SetObject(publicImage)
	return
}

func (s *ImagesServer) Create(ctx context.Context,
	request *publicv1.ImagesCreateRequest) (response *publicv1.ImagesCreateResponse, err error) {
	// Map the public image to private format:
	publicImage := request.GetObject()
	if publicImage == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	privateImage := &privatev1.Image{}
	err = s.inMapper.Copy(ctx, publicImage, privateImage)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public image to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process image")
		return
	}

	// Delegate to the private server:
	privateRequest := &privatev1.ImagesCreateRequest{}
	privateRequest.SetObject(privateImage)
	privateResponse, err := s.delegate.Create(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	createdPrivateImage := privateResponse.GetObject()
	createdPublicImage := &publicv1.Image{}
	err = s.outMapper.Copy(ctx, createdPrivateImage, createdPublicImage)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private image to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process image")
		return
	}

	// Create the public response:
	response = &publicv1.ImagesCreateResponse{}
	response.SetObject(createdPublicImage)
	return
}

func (s *ImagesServer) Update(ctx context.Context,
	request *publicv1.ImagesUpdateRequest) (response *publicv1.ImagesUpdateResponse, err error) {
	// Validate the request:
	publicImage := request.GetObject()
	if publicImage == nil {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object is mandatory")
		return
	}
	id := publicImage.GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	// Get the existing object from the private server:
	getRequest := &privatev1.ImagesGetRequest{}
	getRequest.SetId(id)
	getResponse, err := s.delegate.Get(ctx, getRequest)
	if err != nil {
		return nil, err
	}
	existingPrivateImage := getResponse.GetObject()

	// Map the public changes to the existing private object (preserving private data):
	err = s.inMapper.Copy(ctx, publicImage, existingPrivateImage)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map public image to private",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process image")
		return
	}

	// Delegate to the private server with the merged object:
	privateRequest := &privatev1.ImagesUpdateRequest{}
	privateRequest.SetObject(existingPrivateImage)
	privateRequest.SetLock(request.GetLock())
	privateResponse, err := s.delegate.Update(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Map the private response back to public format:
	updatedPrivateImage := privateResponse.GetObject()
	updatedPublicImage := &publicv1.Image{}
	err = s.outMapper.Copy(ctx, updatedPrivateImage, updatedPublicImage)
	if err != nil {
		s.logger.ErrorContext(
			ctx,
			"Failed to map private image to public",
			slog.Any("error", err),
		)
		err = grpcstatus.Errorf(grpccodes.Internal, "failed to process image")
		return
	}

	// Create the public response:
	response = &publicv1.ImagesUpdateResponse{}
	response.SetObject(updatedPublicImage)
	return
}

func (s *ImagesServer) Delete(ctx context.Context,
	request *publicv1.ImagesDeleteRequest) (response *publicv1.ImagesDeleteResponse, err error) {
	// Create private request:
	privateRequest := &privatev1.ImagesDeleteRequest{}
	privateRequest.SetId(request.GetId())

	// Delegate to private server:
	_, err = s.delegate.Delete(ctx, privateRequest)
	if err != nil {
		return nil, err
	}

	// Create the public response:
	response = &publicv1.ImagesDeleteResponse{}
	return
}
