/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package networkclass

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"

	privatev1 "github.com/osac-project/fulfillment-service/internal/api/osac/private/v1"
	"github.com/osac-project/fulfillment-service/internal/controllers"
	"github.com/osac-project/fulfillment-service/internal/controllers/finalizers"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/annotations"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/gvks"
	"github.com/osac-project/fulfillment-service/internal/kubernetes/labels"
	"github.com/osac-project/fulfillment-service/internal/masks"
)

// objectPrefix is the prefix that will be used in the `generateName` field of the resources created in the hub.
const objectPrefix = "networkclass-"

// FunctionBuilder contains the data and logic needed to build a function that reconciles network classes.
type FunctionBuilder struct {
	logger     *slog.Logger
	connection *grpc.ClientConn
	hubCache   controllers.HubCache
}

type function struct {
	logger               *slog.Logger
	hubCache             controllers.HubCache
	networkClassesClient privatev1.NetworkClassesClient
	hubsClient           privatev1.HubsClient
	maskCalculator       *masks.Calculator
}

type task struct {
	r            *function
	networkClass *privatev1.NetworkClass
	hubId        string
	hubNamespace string
	hubClient    clnt.Client
}

// NewFunction creates a new builder that can then be used to create a new network class reconciler function.
func NewFunction() *FunctionBuilder {
	return &FunctionBuilder{}
}

// SetLogger sets the logger. This is mandatory.
func (b *FunctionBuilder) SetLogger(value *slog.Logger) *FunctionBuilder {
	b.logger = value
	return b
}

// SetConnection sets the gRPC client connection. This is mandatory.
func (b *FunctionBuilder) SetConnection(value *grpc.ClientConn) *FunctionBuilder {
	b.connection = value
	return b
}

// SetHubCache sets the cache of hubs. This is mandatory.
func (b *FunctionBuilder) SetHubCache(value controllers.HubCache) *FunctionBuilder {
	b.hubCache = value
	return b
}

// Build uses the information stored in the builder to create a new network class reconciler.
func (b *FunctionBuilder) Build() (result controllers.ReconcilerFunction[*privatev1.NetworkClass], err error) {
	// Check parameters:
	if b.logger == nil {
		err = errors.New("logger is mandatory")
		return
	}
	if b.connection == nil {
		err = errors.New("client is mandatory")
		return
	}
	if b.hubCache == nil {
		err = errors.New("hub cache is mandatory")
		return
	}

	// Create and populate the object:
	object := &function{
		logger:               b.logger,
		networkClassesClient: privatev1.NewNetworkClassesClient(b.connection),
		hubsClient:           privatev1.NewHubsClient(b.connection),
		hubCache:             b.hubCache,
		maskCalculator:       masks.NewCalculator().Build(),
	}
	result = object.run
	return
}

func (r *function) run(ctx context.Context, networkClass *privatev1.NetworkClass) error {
	oldNetworkClass := proto.Clone(networkClass).(*privatev1.NetworkClass)
	t := task{
		r:            r,
		networkClass: networkClass,
	}
	var err error
	if networkClass.HasMetadata() && networkClass.GetMetadata().HasDeletionTimestamp() {
		err = t.delete(ctx)
	} else {
		err = t.update(ctx)
	}
	if err != nil {
		return err
	}
	// Calculate which fields the reconciler actually modified and use a field mask
	// to update only those fields. This prevents overwriting concurrent user changes.
	updateMask := r.maskCalculator.Calculate(oldNetworkClass, networkClass)

	// Only send an update if there are actual changes
	_, err = r.networkClassesClient.Update(ctx, privatev1.NetworkClassesUpdateRequest_builder{
		Object:     networkClass,
		UpdateMask: updateMask,
	}.Build())

	return err
}

func (t *task) update(ctx context.Context) error {
	// Add the finalizer and return immediately if it was added. This ensures the finalizer is persisted before any
	// other work is done, reducing the chance of the object being deleted before the finalizer is saved.
	if t.addFinalizer() {
		return nil
	}

	// Set the default values:
	t.setDefaults()

	// Validate that exactly one tenant is assigned:
	if err := t.validateTenant(); err != nil {
		return err
	}

	// Select the hub:
	if err := t.selectHub(ctx); err != nil {
		return err
	}

	// Get the K8S object:
	object, err := t.getKubeObject(ctx)
	if err != nil {
		return err
	}

	// Prepare the changes to the spec:
	spec := t.buildSpec()

	// Create or update the Kubernetes object:
	if object == nil {
		object := &unstructured.Unstructured{}
		object.SetGroupVersionKind(gvks.NetworkClass)
		object.SetNamespace(t.hubNamespace)
		object.SetGenerateName(objectPrefix)
		object.SetLabels(map[string]string{
			labels.NetworkClassUuid: t.networkClass.GetId(),
		})
		object.SetAnnotations(map[string]string{
			annotations.Tenant: t.networkClass.GetMetadata().GetTenants()[0],
		})
		err = unstructured.SetNestedField(object.Object, spec, "spec")
		if err != nil {
			return err
		}
		err = t.hubClient.Create(ctx, object)
		if err != nil {
			return err
		}
		t.r.logger.DebugContext(
			ctx,
			"Created network class",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	} else {
		update := object.DeepCopy()
		err = unstructured.SetNestedField(update.Object, spec, "spec")
		if err != nil {
			return err
		}
		err = t.hubClient.Patch(ctx, update, clnt.MergeFrom(object))
		if err != nil {
			return err
		}
		t.r.logger.DebugContext(
			ctx,
			"Updated network class",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	return err
}

func (t *task) setDefaults() {
	if !t.networkClass.HasStatus() {
		t.networkClass.SetStatus(&privatev1.NetworkClassStatus{})
	}
	if t.networkClass.GetStatus().GetState() == privatev1.NetworkClassState_NETWORK_CLASS_STATE_UNSPECIFIED {
		t.networkClass.GetStatus().SetState(privatev1.NetworkClassState_NETWORK_CLASS_STATE_PENDING)
	}
}

func (t *task) validateTenant() error {
	if !t.networkClass.HasMetadata() || len(t.networkClass.GetMetadata().GetTenants()) != 1 {
		return errors.New("network class must have exactly one tenant assigned")
	}
	return nil
}

func (t *task) delete(ctx context.Context) (err error) {
	// Do nothing if we don't know the hub yet:
	t.hubId = t.networkClass.GetStatus().GetHub()
	if t.hubId == "" {
		// No hub assigned, nothing to clean up on K8s side.
		t.removeFinalizer()
		return nil
	}
	err = t.getHub(ctx)
	if err != nil {
		return
	}

	// Check if the K8S object still exists:
	object, err := t.getKubeObject(ctx)
	if err != nil {
		return
	}
	if object == nil {
		// K8s object is fully gone (all K8s finalizers processed).
		// Safe to remove our DB finalizer and allow archiving.
		t.r.logger.DebugContext(
			ctx,
			"Network class doesn't exist",
			slog.String("id", t.networkClass.GetId()),
		)
		t.removeFinalizer()
		return
	}

	// Initiate K8s deletion if not already in progress:
	if object.GetDeletionTimestamp() == nil {
		err = t.hubClient.Delete(ctx, object)
		if err != nil {
			return
		}
		t.r.logger.DebugContext(
			ctx,
			"Deleted network class",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	} else {
		t.r.logger.DebugContext(
			ctx,
			"Network class is still being deleted, waiting for K8s finalizers",
			slog.String("namespace", object.GetNamespace()),
			slog.String("name", object.GetName()),
		)
	}

	// Don't remove finalizer — K8s object still exists with finalizers being processed.
	return
}

func (t *task) selectHub(ctx context.Context) error {
	t.hubId = t.networkClass.GetStatus().GetHub()
	if t.hubId == "" {
		response, err := t.r.hubsClient.List(ctx, privatev1.HubsListRequest_builder{}.Build())
		if err != nil {
			return err
		}
		if len(response.Items) == 0 {
			return errors.New("there are no hubs")
		}
		t.hubId = response.Items[rand.IntN(len(response.Items))].GetId()
	}
	t.r.logger.DebugContext(
		ctx,
		"Selected hub",
		slog.String("id", t.hubId),
	)
	hubEntry, err := t.r.hubCache.Get(ctx, t.hubId)
	if err != nil {
		return err
	}
	t.hubNamespace = hubEntry.Namespace
	t.hubClient = hubEntry.Client
	// Save the selected hub in the private data of the network class:
	t.networkClass.GetStatus().SetHub(t.hubId)
	return nil
}

func (t *task) getHub(ctx context.Context) error {
	t.hubId = t.networkClass.GetStatus().GetHub()
	hubEntry, err := t.r.hubCache.Get(ctx, t.hubId)
	if err != nil {
		return err
	}
	t.hubNamespace = hubEntry.Namespace
	t.hubClient = hubEntry.Client
	return nil
}

func (t *task) getKubeObject(ctx context.Context) (result *unstructured.Unstructured, err error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvks.NetworkClassList)
	err = t.hubClient.List(
		ctx, list,
		clnt.InNamespace(t.hubNamespace),
		clnt.MatchingLabels{
			labels.NetworkClassUuid: t.networkClass.GetId(),
		},
	)
	if err != nil {
		return
	}
	items := list.Items
	count := len(items)
	if count > 1 {
		err = fmt.Errorf(
			"expected at most one network class with identifier '%s' but found %d",
			t.networkClass.GetId(), count,
		)
		return
	}
	if count > 0 {
		result = &items[0]
	}
	return
}

// addFinalizer adds the controller finalizer if it is not already present. Returns true if the finalizer was added,
// false if it was already present.
func (t *task) addFinalizer() bool {
	if !t.networkClass.HasMetadata() {
		t.networkClass.SetMetadata(&privatev1.Metadata{})
	}
	list := t.networkClass.GetMetadata().GetFinalizers()
	if !slices.Contains(list, finalizers.Controller) {
		list = append(list, finalizers.Controller)
		t.networkClass.GetMetadata().SetFinalizers(list)
		return true
	}
	return false
}

func (t *task) removeFinalizer() {
	if !t.networkClass.HasMetadata() {
		return
	}
	list := t.networkClass.GetMetadata().GetFinalizers()
	if slices.Contains(list, finalizers.Controller) {
		list = slices.DeleteFunc(list, func(item string) bool {
			return item == finalizers.Controller
		})
		t.networkClass.GetMetadata().SetFinalizers(list)
	}
}

// buildSpec constructs the spec map for the Kubernetes NetworkClass object based on the
// network class from the database.
func (t *task) buildSpec() map[string]any {
	spec := map[string]any{
		"title":                   t.networkClass.GetTitle(),
		"description":             t.networkClass.GetDescription(),
		"implementation_strategy": t.networkClass.GetImplementationStrategy(),
	}

	// Add constraints if present:
	if t.networkClass.HasConstraints() {
		// For now, constraints is an empty message but we include it for future extension
		spec["constraints"] = map[string]any{}
	}

	// Add capabilities if present:
	if t.networkClass.HasCapabilities() {
		caps := t.networkClass.GetCapabilities()
		spec["capabilities"] = map[string]any{
			"supports_ipv4":       caps.GetSupportsIpv4(),
			"supports_ipv6":       caps.GetSupportsIpv6(),
			"supports_dual_stack": caps.GetSupportsDualStack(),
		}
	}

	return spec
}
