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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/auth"
	"github.com/osac-project/fulfillment-service/internal/database"
)

type PrivateComputeInstanceClassesServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.ComputeInstanceClassesServer = (*PrivateComputeInstanceClassesServer)(nil)

type PrivateComputeInstanceClassesServer struct {
	privatev1.UnimplementedComputeInstanceClassesServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.ComputeInstanceClass]
}

func NewPrivateComputeInstanceClassesServer() *PrivateComputeInstanceClassesServerBuilder {
	return &PrivateComputeInstanceClassesServerBuilder{}
}

func (b *PrivateComputeInstanceClassesServerBuilder) SetLogger(value *slog.Logger) *PrivateComputeInstanceClassesServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateComputeInstanceClassesServerBuilder) SetNotifier(value *database.Notifier) *PrivateComputeInstanceClassesServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateComputeInstanceClassesServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateComputeInstanceClassesServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateComputeInstanceClassesServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateComputeInstanceClassesServerBuilder {
	b.tenancyLogic = value
	return b
}

// SetMetricsRegisterer sets the Prometheus registerer used to register the metrics for the underlying database
// access objects. This is optional. If not set, no metrics will be recorded.
func (b *PrivateComputeInstanceClassesServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateComputeInstanceClassesServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateComputeInstanceClassesServerBuilder) Build() (result *PrivateComputeInstanceClassesServer, err error) {
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
	generic, err := NewGenericServer[*privatev1.ComputeInstanceClass]().
		SetLogger(b.logger).
		SetService(privatev1.ComputeInstanceClasses_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	// Create and populate the object:
	result = &PrivateComputeInstanceClassesServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateComputeInstanceClassesServer) List(ctx context.Context,
	request *privatev1.ComputeInstanceClassesListRequest) (response *privatev1.ComputeInstanceClassesListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceClassesServer) Get(ctx context.Context,
	request *privatev1.ComputeInstanceClassesGetRequest) (response *privatev1.ComputeInstanceClassesGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceClassesServer) Create(ctx context.Context,
	request *privatev1.ComputeInstanceClassesCreateRequest) (response *privatev1.ComputeInstanceClassesCreateResponse, err error) {
	// Validate before creating:
	err = s.validateComputeInstanceClass(ctx, request.GetObject(), nil)
	if err != nil {
		return
	}

	// Set status to READY on creation since ComputeInstanceClass has no backend provisioning.
	cic := request.GetObject()
	if cic.Status == nil {
		cic.Status = &privatev1.ComputeInstanceClassStatus{}
	}
	cic.Status.SetState(privatev1.ComputeInstanceClassState_COMPUTE_INSTANCE_CLASS_STATE_READY)

	// Clear any caller-provided ID so the DAO always generates a UUID.
	cic.SetId("")

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceClassesServer) Update(ctx context.Context,
	request *privatev1.ComputeInstanceClassesUpdateRequest) (response *privatev1.ComputeInstanceClassesUpdateResponse, err error) {
	// Get existing object for immutability validation:
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.ComputeInstanceClassesGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.ComputeInstanceClassesGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existingCIC := getResponse.GetObject()

	// Merge the update into the existing object so that required-field
	// validation works correctly for partial updates (field mask).
	merged := cloneComputeInstanceClass(existingCIC)
	applyComputeInstanceClassUpdate(merged, request.GetObject(), request.GetUpdateMask())

	// Validate the merged result against the original for immutability checks:
	err = s.validateComputeInstanceClass(ctx, merged, existingCIC)
	if err != nil {
		return
	}

	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceClassesServer) Delete(ctx context.Context,
	request *privatev1.ComputeInstanceClassesDeleteRequest) (response *privatev1.ComputeInstanceClassesDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceClassesServer) Signal(ctx context.Context,
	request *privatev1.ComputeInstanceClassesSignalRequest) (response *privatev1.ComputeInstanceClassesSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

// validateComputeInstanceClass validates the ComputeInstanceClass object.
func (s *PrivateComputeInstanceClassesServer) validateComputeInstanceClass(ctx context.Context,
	newCIC *privatev1.ComputeInstanceClass, existingCIC *privatev1.ComputeInstanceClass) error {

	if newCIC == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance class is mandatory")
	}

	// CIC-VAL-01: backend is required and must be a known value
	backend := newCIC.GetBackend()
	if backend == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'backend' is required")
	}
	if backend != "baremetal" && backend != "virtual" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument,
			"field 'backend' must be 'baremetal' or 'virtual', got '%s'", backend)
	}

	// CIC-VAL-02: title is required
	if newCIC.GetTitle() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'title' is required")
	}

	// CIC-VAL-03: Check immutable fields (only on Update)
	if existingCIC != nil {
		// backend is immutable
		if newCIC.GetBackend() != existingCIC.GetBackend() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'backend' is immutable and cannot be changed from '%s' to '%s'",
				existingCIC.GetBackend(), newCIC.GetBackend())
		}
	}

	return nil
}

// cloneComputeInstanceClass creates a deep copy of a ComputeInstanceClass.
func cloneComputeInstanceClass(cic *privatev1.ComputeInstanceClass) *privatev1.ComputeInstanceClass {
	return proto.Clone(cic).(*privatev1.ComputeInstanceClass)
}

// applyComputeInstanceClassUpdate applies the update fields onto the base object,
// respecting the field mask. If no mask is provided, all fields from the
// update are applied.
func applyComputeInstanceClassUpdate(base, update *privatev1.ComputeInstanceClass, mask *fieldmaskpb.FieldMask) {
	if mask == nil || len(mask.GetPaths()) == 0 {
		proto.Merge(base, update)
		return
	}
	for _, path := range mask.GetPaths() {
		switch path {
		case "status.state":
			if base.Status == nil {
				base.Status = &privatev1.ComputeInstanceClassStatus{}
			}
			base.Status.SetState(update.GetStatus().GetState())
		case "status.message":
			if base.Status == nil {
				base.Status = &privatev1.ComputeInstanceClassStatus{}
			}
			base.Status.SetMessage(update.GetStatus().GetMessage())
		case "title":
			base.SetTitle(update.GetTitle())
		case "description":
			base.SetDescription(update.GetDescription())
		case "backend":
			base.SetBackend(update.GetBackend())
		case "capabilities":
			base.SetCapabilities(update.GetCapabilities())
		case "templates":
			base.SetTemplates(update.GetTemplates())
		default:
			// For unknown paths, fall through - the generic handler will
			// reject invalid paths if needed.
		}
	}
}
