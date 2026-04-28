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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type PrivateImagesServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.ImagesServer = (*PrivateImagesServer)(nil)

type PrivateImagesServer struct {
	privatev1.UnimplementedImagesServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.Image]
}

func NewPrivateImagesServer() *PrivateImagesServerBuilder {
	return &PrivateImagesServerBuilder{}
}

func (b *PrivateImagesServerBuilder) SetLogger(value *slog.Logger) *PrivateImagesServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateImagesServerBuilder) SetNotifier(value *database.Notifier) *PrivateImagesServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateImagesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateImagesServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateImagesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateImagesServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateImagesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateImagesServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateImagesServerBuilder) Build() (result *PrivateImagesServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.Image]().
		SetLogger(b.logger).
		SetService(privatev1.Images_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateImagesServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateImagesServer) List(ctx context.Context,
	request *privatev1.ImagesListRequest) (response *privatev1.ImagesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateImagesServer) Get(ctx context.Context,
	request *privatev1.ImagesGetRequest) (response *privatev1.ImagesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateImagesServer) Create(ctx context.Context,
	request *privatev1.ImagesCreateRequest) (response *privatev1.ImagesCreateResponse, err error) {
	// Validate before creating:
	err = s.validateImage(ctx, request.GetObject(), nil)
	if err != nil {
		return
	}

	// Set status to READY on creation since Image has no backend provisioning.
	img := request.GetObject()
	if img.Status == nil {
		img.Status = &privatev1.ImageStatus{}
	}
	img.Status.SetState(privatev1.ImageState_IMAGE_STATE_READY)

	// Clear any caller-provided ID so the DAO always generates a UUID.
	img.SetId("")

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateImagesServer) Update(ctx context.Context,
	request *privatev1.ImagesUpdateRequest) (response *privatev1.ImagesUpdateResponse, err error) {
	// Get existing object for immutability validation:
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.ImagesGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.ImagesGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existingImage := getResponse.GetObject()

	// Merge the update into the existing object so that required-field
	// validation works correctly for partial updates (field mask).
	merged := cloneImage(existingImage)
	applyImageUpdate(merged, request.GetObject(), request.GetUpdateMask())

	// Validate the merged result against the original for immutability checks:
	err = s.validateImage(ctx, merged, existingImage)
	if err != nil {
		return
	}

	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateImagesServer) Delete(ctx context.Context,
	request *privatev1.ImagesDeleteRequest) (response *privatev1.ImagesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateImagesServer) Signal(ctx context.Context,
	request *privatev1.ImagesSignalRequest) (response *privatev1.ImagesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// validBootMethods contains the set of valid boot methods for images.
var validBootMethods = map[string]bool{
	"ignition":   true,
	"cloud-init": true,
	"kickstart":  true,
}

// validateImage validates the Image object.
func (s *PrivateImagesServer) validateImage(ctx context.Context,
	newImage *privatev1.Image, existingImage *privatev1.Image) error {

	if newImage == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "image is mandatory")
	}

	// IMG-VAL-01: title is required
	if newImage.GetTitle() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'title' is required")
	}

	// IMG-VAL-02: source_type is required
	if newImage.GetSourceType() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'source_type' is required")
	}

	// IMG-VAL-03: source_ref is required
	if newImage.GetSourceRef() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'source_ref' is required")
	}

	// IMG-VAL-04: boot_method is required and must be valid
	bootMethod := newImage.GetBootMethod()
	if bootMethod == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'boot_method' is required")
	}
	if !validBootMethods[bootMethod] {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'boot_method' must be one of 'ignition', 'cloud-init', 'kickstart', got '%s'",
			bootMethod)
	}

	// IMG-VAL-05: Check immutable fields (only on Update)
	if existingImage != nil {
		// source_type is immutable
		if newImage.GetSourceType() != existingImage.GetSourceType() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'source_type' is immutable and cannot be changed from '%s' to '%s'",
				existingImage.GetSourceType(), newImage.GetSourceType())
		}
		// boot_method is immutable
		if newImage.GetBootMethod() != existingImage.GetBootMethod() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'boot_method' is immutable and cannot be changed from '%s' to '%s'",
				existingImage.GetBootMethod(), newImage.GetBootMethod())
		}
		// source_ref is immutable
		if newImage.GetSourceRef() != existingImage.GetSourceRef() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'source_ref' is immutable and cannot be changed from '%s' to '%s'",
				existingImage.GetSourceRef(), newImage.GetSourceRef())
		}
	}

	return nil
}

// cloneImage creates a deep copy of an Image.
func cloneImage(img *privatev1.Image) *privatev1.Image {
	return proto.Clone(img).(*privatev1.Image)
}

// applyImageUpdate applies the update fields onto the base object,
// respecting the field mask. If no mask is provided, all fields from the
// update are applied.
func applyImageUpdate(base, update *privatev1.Image, mask *fieldmaskpb.FieldMask) {
	if mask == nil || len(mask.GetPaths()) == 0 {
		proto.Merge(base, update)
		return
	}
	for _, path := range mask.GetPaths() {
		switch path {
		case "title":
			base.SetTitle(update.GetTitle())
		case "description":
			base.SetDescription(update.GetDescription())
		case "source_type":
			base.SetSourceType(update.GetSourceType())
		case "source_ref":
			base.SetSourceRef(update.GetSourceRef())
		case "os":
			base.SetOs(update.GetOs())
		case "version":
			base.SetVersion(update.GetVersion())
		case "architecture":
			base.SetArchitecture(update.GetArchitecture())
		case "boot_method":
			base.SetBootMethod(update.GetBootMethod())
		case "compatibility":
			base.SetCompatibility(update.GetCompatibility())
		case "checksum":
			base.SetChecksum(update.GetChecksum())
		case "status.state":
			if base.Status == nil {
				base.Status = &privatev1.ImageStatus{}
			}
			base.Status.SetState(update.GetStatus().GetState())
		case "status.message":
			if base.Status == nil {
				base.Status = &privatev1.ImageStatus{}
			}
			base.Status.SetMessage(update.GetStatus().GetMessage())
		default:
			// For unknown paths, fall through - the generic handler will
			// reject invalid paths if needed.
		}
	}
}
