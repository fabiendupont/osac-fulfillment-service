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

type PrivateComputeInstanceGroupsServerBuilder struct {
	logger            *slog.Logger
	notifier          *database.Notifier
	attributionLogic  auth.AttributionLogic
	tenancyLogic      auth.TenancyLogic
	metricsRegisterer prometheus.Registerer
}

var _ privatev1.ComputeInstanceGroupsServer = (*PrivateComputeInstanceGroupsServer)(nil)

type PrivateComputeInstanceGroupsServer struct {
	privatev1.UnimplementedComputeInstanceGroupsServer

	logger  *slog.Logger
	generic *GenericServer[*privatev1.ComputeInstanceGroup]
}

func NewPrivateComputeInstanceGroupsServer() *PrivateComputeInstanceGroupsServerBuilder {
	return &PrivateComputeInstanceGroupsServerBuilder{}
}

func (b *PrivateComputeInstanceGroupsServerBuilder) SetLogger(value *slog.Logger) *PrivateComputeInstanceGroupsServerBuilder {
	b.logger = value
	return b
}

func (b *PrivateComputeInstanceGroupsServerBuilder) SetNotifier(value *database.Notifier) *PrivateComputeInstanceGroupsServerBuilder {
	b.notifier = value
	return b
}

func (b *PrivateComputeInstanceGroupsServerBuilder) SetAttributionLogic(value auth.AttributionLogic) *PrivateComputeInstanceGroupsServerBuilder {
	b.attributionLogic = value
	return b
}

func (b *PrivateComputeInstanceGroupsServerBuilder) SetTenancyLogic(value auth.TenancyLogic) *PrivateComputeInstanceGroupsServerBuilder {
	b.tenancyLogic = value
	return b
}

func (b *PrivateComputeInstanceGroupsServerBuilder) SetMetricsRegisterer(value prometheus.Registerer) *PrivateComputeInstanceGroupsServerBuilder {
	b.metricsRegisterer = value
	return b
}

func (b *PrivateComputeInstanceGroupsServerBuilder) Build() (result *PrivateComputeInstanceGroupsServer, err error) {
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.tenancyLogic == nil {
		err = errors.New("tenancy logic is mandatory")
		return
	}

	generic, err := NewGenericServer[*privatev1.ComputeInstanceGroup]().
		SetLogger(b.logger).
		SetService(privatev1.ComputeInstanceGroups_ServiceDesc.ServiceName).
		SetNotifier(b.notifier).
		SetAttributionLogic(b.attributionLogic).
		SetTenancyLogic(b.tenancyLogic).
		SetMetricsRegisterer(b.metricsRegisterer).
		Build()
	if err != nil {
		return
	}

	result = &PrivateComputeInstanceGroupsServer{
		logger:  b.logger,
		generic: generic,
	}
	return
}

func (s *PrivateComputeInstanceGroupsServer) List(ctx context.Context,
	request *privatev1.ComputeInstanceGroupsListRequest) (response *privatev1.ComputeInstanceGroupsListResponse, err error) {
	err = s.generic.List(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceGroupsServer) Get(ctx context.Context,
	request *privatev1.ComputeInstanceGroupsGetRequest) (response *privatev1.ComputeInstanceGroupsGetResponse, err error) {
	err = s.generic.Get(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceGroupsServer) Create(ctx context.Context,
	request *privatev1.ComputeInstanceGroupsCreateRequest) (response *privatev1.ComputeInstanceGroupsCreateResponse, err error) {
	err = s.validateComputeInstanceGroup(ctx, request.GetObject(), nil)
	if err != nil {
		return
	}

	cig := request.GetObject()
	if cig.Status == nil {
		cig.Status = &privatev1.ComputeInstanceGroupStatus{}
	}
	cig.Status.SetState(privatev1.ComputeInstanceGroupState_COMPUTE_INSTANCE_GROUP_STATE_PENDING)
	cig.SetId("")

	err = s.generic.Create(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceGroupsServer) Update(ctx context.Context,
	request *privatev1.ComputeInstanceGroupsUpdateRequest) (response *privatev1.ComputeInstanceGroupsUpdateResponse, err error) {
	id := request.GetObject().GetId()
	if id == "" {
		err = grpcstatus.Errorf(grpccodes.InvalidArgument, "object identifier is mandatory")
		return
	}

	getRequest := &privatev1.ComputeInstanceGroupsGetRequest{}
	getRequest.SetId(id)
	var getResponse *privatev1.ComputeInstanceGroupsGetResponse
	err = s.generic.Get(ctx, getRequest, &getResponse)
	if err != nil {
		return
	}

	existingCIG := getResponse.GetObject()
	merged := proto.Clone(existingCIG).(*privatev1.ComputeInstanceGroup)
	applyComputeInstanceGroupUpdate(merged, request.GetObject(), request.GetUpdateMask())

	err = s.validateComputeInstanceGroup(ctx, merged, existingCIG)
	if err != nil {
		return
	}

	err = s.generic.Update(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceGroupsServer) Delete(ctx context.Context,
	request *privatev1.ComputeInstanceGroupsDeleteRequest) (response *privatev1.ComputeInstanceGroupsDeleteResponse, err error) {
	err = s.generic.Delete(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceGroupsServer) Signal(ctx context.Context,
	request *privatev1.ComputeInstanceGroupsSignalRequest) (response *privatev1.ComputeInstanceGroupsSignalResponse, err error) {
	err = s.generic.Signal(ctx, request, &response)
	return
}

func (s *PrivateComputeInstanceGroupsServer) validateComputeInstanceGroup(ctx context.Context,
	newCIG *privatev1.ComputeInstanceGroup, existingCIG *privatev1.ComputeInstanceGroup) error {

	if newCIG == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "compute instance group is mandatory")
	}

	spec := newCIG.GetSpec()
	if spec == nil {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "spec is mandatory")
	}

	if spec.GetComputeInstanceClass() == "" {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.compute_instance_class' is required")
	}

	if spec.GetReplicas() < 0 {
		return grpcstatus.Errorf(grpccodes.InvalidArgument, "field 'spec.replicas' must be non-negative")
	}

	if existingCIG != nil {
		if spec.GetComputeInstanceClass() != existingCIG.GetSpec().GetComputeInstanceClass() {
			return grpcstatus.Errorf(grpccodes.InvalidArgument,
				"field 'spec.compute_instance_class' is immutable and cannot be changed from '%s' to '%s'",
				existingCIG.GetSpec().GetComputeInstanceClass(), spec.GetComputeInstanceClass())
		}
	}

	return nil
}

// applyComputeInstanceGroupUpdate applies the update fields onto the base object, respecting the field mask.
// If no mask is provided, all fields from the update are applied.
func applyComputeInstanceGroupUpdate(base, update *privatev1.ComputeInstanceGroup, mask *fieldmaskpb.FieldMask) {
	if mask == nil || len(mask.GetPaths()) == 0 {
		proto.Merge(base, update)
		return
	}
	for _, path := range mask.GetPaths() {
		switch path {
		case "spec.replicas":
			if base.Spec == nil {
				base.Spec = &privatev1.ComputeInstanceGroupSpec{}
			}
			base.Spec.SetReplicas(update.GetSpec().GetReplicas())
		case "spec.placement_policy":
			if base.Spec == nil {
				base.Spec = &privatev1.ComputeInstanceGroupSpec{}
			}
			base.Spec.SetPlacementPolicy(update.GetSpec().GetPlacementPolicy())
		case "status.state":
			if base.Status == nil {
				base.Status = &privatev1.ComputeInstanceGroupStatus{}
			}
			base.Status.SetState(update.GetStatus().GetState())
		case "status.ready_replicas":
			if base.Status == nil {
				base.Status = &privatev1.ComputeInstanceGroupStatus{}
			}
			base.Status.SetReadyReplicas(update.GetStatus().GetReadyReplicas())
		case "status.instances":
			if base.Status == nil {
				base.Status = &privatev1.ComputeInstanceGroupStatus{}
			}
			base.Status.SetInstances(update.GetStatus().GetInstances())
		}
	}
}
