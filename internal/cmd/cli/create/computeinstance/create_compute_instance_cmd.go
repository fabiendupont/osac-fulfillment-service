/*
Copyright (c) 2025 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file except in compliance with the
License. You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the License for the specific
language governing permissions and limitations under the License.
*/

package computeinstance

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/spf13/cobra"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	publicv1 "github.com/osac-project/fulfillment-service/internal/api/osac/public/v1"
	"github.com/osac-project/fulfillment-service/internal/config"
	"github.com/osac-project/fulfillment-service/internal/exit"
	"github.com/osac-project/fulfillment-service/internal/logging"
	"github.com/osac-project/fulfillment-service/internal/reflection"
	"github.com/osac-project/fulfillment-service/internal/terminal"
)

//go:embed templates
var templatesFS embed.FS

func Cmd() *cobra.Command {
	runner := &runnerContext{}
	result := &cobra.Command{
		Use:     "computeinstance [flags]",
		Aliases: []string{string(proto.MessageName((*publicv1.ComputeInstance)(nil)))},
		Short:   "Create a compute instance",
		RunE:    runner.run,
	}
	flags := result.Flags()
	flags.StringVarP(
		&runner.args.name,
		"name",
		"n",
		"",
		"Name of the compute instance.",
	)
	flags.StringVarP(
		&runner.args.template,
		"template",
		"t",
		"",
		"Template identifier or name",
	)
	flags.StringSliceVarP(
		&runner.args.templateParameterValues,
		"template-parameter",
		"p",
		[]string{},
		"Template parameter in the format 'name=value'.",
	)
	flags.StringSliceVarP(
		&runner.args.templateParameterFiles,
		"template-parameter-file",
		"f",
		[]string{},
		"Template parameter from file in the format 'name=filename'.",
	)
	flags.Int32Var(
		&runner.args.cores,
		"cores",
		0,
		"Number of CPU cores.",
	)
	flags.Int32Var(
		&runner.args.memoryGiB,
		"memory-gib",
		0,
		"Memory size in GiB.",
	)
	flags.StringVar(
		&runner.args.imageSourceRef,
		"image",
		"",
		"Image reference (e.g. OCI image URL).",
	)
	flags.StringVar(
		&runner.args.imageSourceType,
		"image-source-type",
		"registry",
		"Image source type.",
	)
	flags.StringVar(
		&runner.args.sshKey,
		"ssh-key",
		"",
		"SSH public key.",
	)
	flags.Int32Var(
		&runner.args.bootDiskSizeGiB,
		"boot-disk-size",
		0,
		"Boot disk size in GiB.",
	)
	flags.StringSliceVar(
		&runner.args.additionalDisks,
		"additional-disk",
		[]string{},
		"Additional disk size in GiB (e.g. '100'). Repeatable.",
	)
	flags.StringVar(
		&runner.args.runStrategy,
		"run-strategy",
		"",
		"Run strategy (e.g. 'Always' or 'Halted').",
	)
	flags.StringVar(
		&runner.args.userData,
		"user-data",
		"",
		"User data for the compute instance (e.g. cloud-init, ignition).",
	)
	flags.StringVarP(
		&runner.args.class,
		"class",
		"c",
		"",
		"ComputeInstanceClass name (alternative to --template).",
	)
	flags.StringVar(
		&runner.args.region,
		"region",
		"",
		"Region for template selection within the class.",
	)
	flags.StringVar(
		&runner.args.imageRef,
		"image-ref",
		"",
		"Image resource name (alternative to --image).",
	)
	flags.StringSliceVar(
		&runner.args.sshKeyRefs,
		"ssh-key-ref",
		[]string{},
		"SSHKey resource name. Repeatable.",
	)
	return result
}

type runnerContext struct {
	args struct {
		name                    string
		template                string
		templateParameterValues []string
		templateParameterFiles  []string
		cores                   int32
		memoryGiB               int32
		imageSourceRef          string
		imageSourceType         string
		sshKey                  string
		bootDiskSizeGiB         int32
		additionalDisks         []string
		runStrategy             string
		userData                string
		class                   string
		region                  string
		imageRef                string
		sshKeyRefs              []string
	}
	logger                 *slog.Logger
	console                *terminal.Console
	templatesClient        publicv1.ComputeInstanceTemplatesClient
	computeInstancesClient publicv1.ComputeInstancesClient
}

func (c *runnerContext) run(cmd *cobra.Command, args []string) error {
	var err error

	// Get the context:
	ctx := cmd.Context()

	// Get the logger and console:
	c.logger = logging.LoggerFromContext(ctx)
	c.console = terminal.ConsoleFromContext(ctx)

	// Add the templates file system to the console:
	err = c.console.AddTemplates(templatesFS, "templates")
	if err != nil {
		return fmt.Errorf("failed to load templates: %w", err)
	}

	// Get the configuration:
	cfg, err := config.Load(ctx)
	if err != nil {
		return err
	}
	if cfg.Address == "" {
		return fmt.Errorf("there is no configuration, run the 'login' command")
	}

	if c.args.template == "" && c.args.class == "" {
		return fmt.Errorf("either --class or --template is required")
	}

	// Create the gRPC connection from the configuration:
	conn, err := cfg.Connect(ctx, cmd.Flags())
	if err != nil {
		return fmt.Errorf("failed to create gRPC connection: %w", err)
	}
	defer conn.Close()

	// Create the reflection helper:
	helper, err := reflection.NewHelper().
		SetLogger(c.logger).
		SetConnection(conn).
		AddPackages(cfg.Packages()).
		Build()
	if err != nil {
		return fmt.Errorf("failed to create reflection tool: %w", err)
	}
	c.console.SetHelper(helper)

	// Create the gRPC clients:
	c.templatesClient = publicv1.NewComputeInstanceTemplatesClient(conn)
	c.computeInstancesClient = publicv1.NewComputeInstancesClient(conn)

	var spec *publicv1.ComputeInstanceSpec

	if c.args.class != "" {
		spec, err = c.buildClassBasedSpec()
	} else {
		// Legacy template-based flow
		template, findErr := c.findTemplate(ctx)
		if findErr != nil {
			return findErr
		}
		if template == nil {
			return exit.Error(1)
		}
		templateParameterValues, templateParameterIssues := c.parseTemplateParameters(ctx, template)
		if len(templateParameterIssues) > 0 {
			validTemplateParameters := c.validTemplateParameters(template)
			c.console.Render(ctx, "template_parameter_issues.txt", map[string]any{
				"Template":   c.args.template,
				"Parameters": validTemplateParameters,
				"Issues":     templateParameterIssues,
			})
			return exit.Error(1)
		}
		spec, err = c.buildSpec(template.GetId(), templateParameterValues)
	}
	if err != nil {
		return err
	}

	// Prepare the compute instance:
	computeInstance := publicv1.ComputeInstance_builder{
		Metadata: publicv1.Metadata_builder{
			Name: c.args.name,
		}.Build(),
		Spec: spec,
	}.Build()

	// Create the compute instance:
	response, err := c.computeInstancesClient.Create(ctx, publicv1.ComputeInstancesCreateRequest_builder{
		Object: computeInstance,
	}.Build())
	if err != nil {
		return fmt.Errorf("failed to create compute instance: %w", err)
	}

	// Display the result:
	computeInstance = response.Object
	c.console.Infof(ctx, "Created compute instance '%s'.\n", computeInstance.Id)

	return nil
}

// findTemplate finds a compute instance template by identifier or name. It tries to find by identifier or name using a
// server-side filter. If there is exactly one match it returns it. If there are multiple matches it displays them to
// the user and returns an error. If there are no matches it displays available templates and returns an error.
func (c *runnerContext) findTemplate(ctx context.Context) (result *publicv1.ComputeInstanceTemplate, err error) {
	// Try to find the template by identifier or name using a filter:
	filter := fmt.Sprintf(
		"this.id == %[1]q || this.metadata.name == %[1]q",
		c.args.template,
	)
	response, err := c.templatesClient.List(ctx, publicv1.ComputeInstanceTemplatesListRequest_builder{
		Filter: proto.String(filter),
		Limit:  proto.Int32(10),
	}.Build())
	if err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}
	total := response.GetTotal()
	matches := response.GetItems()

	// If there is exactly one match, use it:
	if len(matches) == 1 {
		result = matches[0]
		return
	}

	// If there are multiple matches, display them and advise to use the identifier:
	if len(matches) > 1 {
		c.console.Render(ctx, "template_conflict.txt", map[string]any{
			"Matches": matches,
			"Ref":     c.args.template,
			"Total":   total,
		})
		err = exit.Error(1)
		return
	}

	// If we are here then no matches were found, we will show to the user some of the available templates:
	response, err = c.templatesClient.List(ctx, publicv1.ComputeInstanceTemplatesListRequest_builder{
		Limit: proto.Int32(10),
	}.Build())
	if err != nil {
		return nil, fmt.Errorf("failed to list templates: %w", err)
	}
	examples := response.GetItems()
	c.console.Render(ctx, "template_not_found.txt", map[string]any{
		"Examples": examples,
		"Ref":      c.args.template,
	})
	err = exit.Error(1)
	return
}

func (c *runnerContext) parseTemplateParameters(ctx context.Context,
	template *publicv1.ComputeInstanceTemplate) (result map[string]*anypb.Any, issues []string) {
	result = map[string]*anypb.Any{}
	return
}

// buildSpec constructs the ComputeInstanceSpec from template info and CLI flags.
func (c *runnerContext) buildSpec(templateID string,
	templateParams map[string]*anypb.Any) (*publicv1.ComputeInstanceSpec, error) {
	spec := publicv1.ComputeInstanceSpec_builder{
		Template:           templateID,
		TemplateParameters: templateParams,
	}
	if c.args.imageSourceRef != "" {
		spec.Image = publicv1.ComputeInstanceImage_builder{
			SourceType: c.args.imageSourceType,
			SourceRef:  c.args.imageSourceRef,
		}.Build()
	}
	if c.args.cores > 0 {
		spec.Cores = proto.Int32(c.args.cores)
	}
	if c.args.memoryGiB > 0 {
		spec.MemoryGib = proto.Int32(c.args.memoryGiB)
	}
	if c.args.sshKey != "" {
		spec.SshKey = proto.String(c.args.sshKey)
	}
	if c.args.bootDiskSizeGiB > 0 {
		spec.BootDisk = publicv1.ComputeInstanceDisk_builder{
			SizeGib: c.args.bootDiskSizeGiB,
		}.Build()
	}
	if len(c.args.additionalDisks) > 0 {
		disks, err := parseAdditionalDisks(c.args.additionalDisks)
		if err != nil {
			return nil, err
		}
		spec.AdditionalDisks = disks
	}
	if c.args.runStrategy != "" {
		spec.RunStrategy = proto.String(c.args.runStrategy)
	}
	if c.args.userData != "" {
		spec.UserData = proto.String(c.args.userData)
	}
	return spec.Build(), nil
}

func (c *runnerContext) buildClassBasedSpec() (*publicv1.ComputeInstanceSpec, error) {
	spec := publicv1.ComputeInstanceSpec_builder{
		ComputeInstanceClass: proto.String(c.args.class),
	}
	if c.args.region != "" {
		spec.Region = proto.String(c.args.region)
	}
	if c.args.imageRef != "" {
		spec.ImageRef = proto.String(c.args.imageRef)
	} else if c.args.imageSourceRef != "" {
		spec.Image = publicv1.ComputeInstanceImage_builder{
			SourceType: c.args.imageSourceType,
			SourceRef:  c.args.imageSourceRef,
		}.Build()
	}
	if len(c.args.sshKeyRefs) > 0 {
		spec.SshKeyRefs = c.args.sshKeyRefs
	} else if c.args.sshKey != "" {
		spec.SshKey = proto.String(c.args.sshKey)
	}
	if c.args.cores > 0 {
		spec.Cores = proto.Int32(c.args.cores)
	}
	if c.args.memoryGiB > 0 {
		spec.MemoryGib = proto.Int32(c.args.memoryGiB)
	}
	if c.args.bootDiskSizeGiB > 0 {
		spec.BootDisk = publicv1.ComputeInstanceDisk_builder{
			SizeGib: c.args.bootDiskSizeGiB,
		}.Build()
	}
	if len(c.args.additionalDisks) > 0 {
		disks, err := parseAdditionalDisks(c.args.additionalDisks)
		if err != nil {
			return nil, err
		}
		spec.AdditionalDisks = disks
	}
	if c.args.runStrategy != "" {
		spec.RunStrategy = proto.String(c.args.runStrategy)
	}
	if c.args.userData != "" {
		spec.UserData = proto.String(c.args.userData)
	}
	return spec.Build(), nil
}

// parseAdditionalDisks parses disk sizes in GiB.
// Example: "100"
func parseAdditionalDisks(diskArgs []string) ([]*publicv1.ComputeInstanceDisk, error) {
	disks := make([]*publicv1.ComputeInstanceDisk, 0, len(diskArgs))
	for _, arg := range diskArgs {
		sizeGiB, err := strconv.ParseInt(arg, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid disk size '%s': expected an integer number of GiB", arg)
		}
		disks = append(disks, publicv1.ComputeInstanceDisk_builder{
			SizeGib: int32(sizeGiB),
		}.Build())
	}
	return disks, nil
}

// validTemplateParameter contains the information about a valid template parameter, for use in the error messages that
// display them.
type validTemplateParameter struct {
	// Name is the name of the parameter.
	Name string

	// Type is the type of the parameter.
	Type string

	// Title is the title of the parameter.
	Title string
}

func (c *runnerContext) validTemplateParameters(template *publicv1.ComputeInstanceTemplate) []validTemplateParameter {
	return []validTemplateParameter{}
}
