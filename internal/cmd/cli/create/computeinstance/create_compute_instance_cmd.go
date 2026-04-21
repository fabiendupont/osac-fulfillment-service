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
		Use:                   "computeinstance [FLAG...]",
		Aliases:               []string{string(proto.MessageName((*publicv1.ComputeInstance)(nil)))},
		Short:                 shortHelp,
		Long:                  longHelp,
		DisableFlagsInUseLine: true,
		Args:                  cobra.NoArgs,
		RunE:                  runner.run,
	}
	flags := result.Flags()
	flags.StringVarP(
		&runner.args.name,
		"name",
		"n",
		"",
		nameFlagHelp,
	)
	flags.StringVarP(
		&runner.args.template,
		"template",
		"t",
		"",
		templateFlagHelp,
	)
	flags.StringVar(
		&runner.args.catalogItem,
		"catalog-item",
		"",
		catalogItemFlagHelp,
	)
	flags.StringSliceVarP(
		&runner.args.templateParameterValues,
		"template-parameter",
		"p",
		[]string{},
		templateParameterFlagHelp,
	)
	flags.StringSliceVarP(
		&runner.args.templateParameterFiles,
		"template-parameter-file",
		"f",
		[]string{},
		templateParameterFileFlagHelp,
	)
	flags.Int32Var(
		&runner.args.cores,
		"cores",
		0,
		coresFlagHelp,
	)
	flags.Int32Var(
		&runner.args.memoryGiB,
		"memory-gib",
		0,
		memoryFlagHelp,
	)
	flags.StringVar(
		&runner.args.imageSourceRef,
		"image",
		"",
		imageFlagHelp,
	)
	flags.StringVar(
		&runner.args.imageSourceType,
		"image-source-type",
		"registry",
		imageSourceTypeFlagHelp,
	)
	flags.StringVar(
		&runner.args.sshKey,
		"ssh-key",
		"",
		sshKeyFlagHelp,
	)
	flags.Int32Var(
		&runner.args.bootDiskSizeGiB,
		"boot-disk-size",
		0,
		bootDiskSizeFlagHelp,
	)
	flags.StringSliceVar(
		&runner.args.additionalDisks,
		"additional-disk",
		[]string{},
		additionalDiskFlagHelp,
	)
	flags.StringVar(
		&runner.args.runStrategy,
		"run-strategy",
		"",
		runStrategyFlagHelp,
	)
	flags.StringVar(
		&runner.args.userData,
		"user-data",
		"",
		userDataFlagHelp,
	)
	flags.StringVar(
		&runner.args.subnet,
		"subnet",
		"",
		subnetFlagHelp,
	)
	flags.StringSliceVar(
		&runner.args.securityGroups,
		"security-group",
		nil,
		securityGroupFlagHelp,
	)
	flags.StringArrayVar(
		&runner.args.networkAttachments,
		"network-attachment",
		nil,
		networkAttachmentFlagHelp,
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

	// Mark deprecated flags
	flags.MarkDeprecated("subnet", "use --network-attachment instead")
	flags.MarkDeprecated("security-group", "use --network-attachment instead")

	result.MarkFlagsMutuallyExclusive("catalog-item", "template")
	result.MarkFlagsOneRequired("catalog-item", "template")
	return result
}

type runnerContext struct {
	args struct {
		name                    string
		template                string
		catalogItem             string
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
		subnet                  string
		securityGroups          []string
		networkAttachments      []string
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

	// Reject template parameters when using catalog item (per D-04):
	if c.args.catalogItem != "" {
		if len(c.args.templateParameterValues) > 0 || len(c.args.templateParameterFiles) > 0 {
			return fmt.Errorf(
				"--template-parameter and --template-parameter-file are not supported with --catalog-item",
			)
		}
	}

	// Deprecation warning for --template (per D-03):
	if c.args.template != "" {
		fmt.Fprintf(os.Stderr, "Warning: --template is deprecated, use --catalog-item instead\n")
	}

	// Get the configuration:
	cfg := config.SettingsFromContext(ctx)
	if !cfg.Armed() {
		return fmt.Errorf("there is no configuration, run the 'login' command")
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

	if c.args.catalogItem != "" {
		// Catalog item path: skip template lookup entirely (per D-04).
		specResult, specErr := c.buildSpecFromCatalogItem(c.args.catalogItem)
		if specErr != nil {
			return specErr
		}

		computeInstance := publicv1.ComputeInstance_builder{
			Metadata: publicv1.Metadata_builder{
				Name: c.args.name,
			}.Build(),
			Spec: specResult,
		}.Build()

		response, err := c.computeInstancesClient.Create(ctx, publicv1.ComputeInstancesCreateRequest_builder{
			Object: computeInstance,
		}.Build())
		if err != nil {
			return fmt.Errorf("failed to create compute instance: %w", err)
		}

		computeInstance = response.Object
		c.console.Infof(ctx, "Created compute instance '%s'.\n", computeInstance.Id)
		return nil
	}

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
		Filter: new(filter),
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
		spec.Cores = new(c.args.cores)
	}
	if c.args.memoryGiB > 0 {
		spec.MemoryGib = new(c.args.memoryGiB)
	}
	if c.args.sshKey != "" {
		spec.SshKey = new(c.args.sshKey)
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
		spec.RunStrategy = new(c.args.runStrategy)
	}
	if c.args.userData != "" {
		spec.UserData = new(c.args.userData)
	}
	if err := c.applyNetworkingFlags(&spec); err != nil {
		return nil, err
	}
	return spec.Build(), nil
}

// applyNetworkingFlags sets spec.network_attachments or deprecated subnet / security_groups from CLI flags.
func (c *runnerContext) applyNetworkingFlags(spec *publicv1.ComputeInstanceSpec_builder) error {
	hasAttachments := len(c.args.networkAttachments) > 0
	hasLegacy := c.args.subnet != "" || len(c.args.securityGroups) > 0
	if hasAttachments && hasLegacy {
		return fmt.Errorf("do not combine --network-attachment with --subnet or --security-group")
	}
	if hasAttachments {
		attachments := make([]*publicv1.NetworkAttachment, 0, len(c.args.networkAttachments))
		for _, raw := range c.args.networkAttachments {
			na, err := parseNetworkAttachmentFlag(raw)
			if err != nil {
				return err
			}
			attachments = append(attachments, na)
		}
		spec.NetworkAttachments = attachments
		return nil
	}
	if c.args.subnet != "" {
		spec.Subnet = new(c.args.subnet)
	}
	if len(c.args.securityGroups) > 0 {
		spec.SecurityGroups = append([]string(nil), c.args.securityGroups...)
	}
	return nil
}

// extractSecurityGroupListSuffix returns the substring before a trailing "security-groups=" or "security_groups="
// clause (case-insensitive) and the list parsed from the remainder of the string after that clause.
// Works entirely on the lowercase copy to avoid Unicode byte-offset issues.
func extractSecurityGroupListSuffix(s string) (prefix string, groups []string, ok bool) {
	lower := strings.ToLower(s)

	// Try both "security-groups=" and "security_groups="
	for _, marker := range []string{"security-groups=", "security_groups="} {
		if i := strings.Index(lower, marker); i >= 0 {
			// Work entirely on lowercase copy to avoid Unicode byte/rune offset mismatches
			prefix = strings.TrimSpace(strings.TrimSuffix(lower[:i], ","))
			rest := strings.TrimSpace(lower[i+len(marker):])
			for _, id := range strings.Split(rest, ",") {
				id = strings.TrimSpace(id)
				if id != "" {
					groups = append(groups, id)
				}
			}
			return prefix, groups, true
		}
	}
	return s, nil, false
}

// parseMainSubnetOnly parses the subnet portion of --network-attachment (no security-groups clause): either a bare id
// or exactly subnet=<id>, optionally with a single comma between other parts only if we add more keys later.
func parseMainSubnetOnly(main string) (string, error) {
	main = strings.TrimSpace(strings.TrimSuffix(main, ","))
	if main == "" {
		return "", fmt.Errorf("--network-attachment must include a subnet or subnet=...")
	}
	if !strings.Contains(main, "=") {
		return main, nil
	}
	var subnet string
	for _, fragment := range strings.Split(main, ",") {
		fragment = strings.TrimSpace(fragment)
		if fragment == "" {
			continue
		}
		key, val, ok := strings.Cut(fragment, "=")
		if !ok {
			return "", fmt.Errorf("invalid --network-attachment fragment %q (expected key=value)", fragment)
		}
		key = strings.TrimSpace(strings.ToLower(key))
		val = strings.TrimSpace(val)
		if val == "" {
			return "", fmt.Errorf("invalid --network-attachment fragment %q (value is empty)", fragment)
		}
		if key != "subnet" {
			return "", fmt.Errorf("unknown key %q before security-groups (use subnet)", key)
		}
		if subnet != "" {
			return "", fmt.Errorf("subnet appears more than once in --network-attachment %q", main)
		}
		subnet = val
	}
	if subnet == "" {
		return "", fmt.Errorf("--network-attachment %q must include subnet=... or be a bare subnet id", main)
	}
	return subnet, nil
}

// parseNetworkAttachmentFlag parses one --network-attachment value: a bare subnet id, or subnet=<id> with optional
// security-groups=/security_groups= suffix (commas allowed in the group list).
func parseNetworkAttachmentFlag(s string) (*publicv1.NetworkAttachment, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty --network-attachment value")
	}
	prefix, securityGroups, hadGroups := extractSecurityGroupListSuffix(s)
	subnet, err := parseMainSubnetOnly(prefix)
	if err != nil {
		return nil, err
	}
	if !hadGroups && !strings.Contains(s, "=") {
		return publicv1.NetworkAttachment_builder{Subnet: s}.Build(), nil
	}
	return publicv1.NetworkAttachment_builder{
		Subnet:         subnet,
		SecurityGroups: securityGroups,
	}.Build(), nil
}

// buildSpecFromCatalogItem builds the spec for catalog-item-based creation.
func (c *runnerContext) buildSpecFromCatalogItem(catalogItemID string) (*publicv1.ComputeInstanceSpec, error) {
	spec := publicv1.ComputeInstanceSpec_builder{
		CatalogItem: catalogItemID,
	}
	if c.args.imageSourceRef != "" {
		spec.Image = publicv1.ComputeInstanceImage_builder{
			SourceType: c.args.imageSourceType,
			SourceRef:  c.args.imageSourceRef,
		}.Build()
	}
	if c.args.cores > 0 {
		spec.Cores = new(c.args.cores)
	}
	if c.args.memoryGiB > 0 {
		spec.MemoryGib = new(c.args.memoryGiB)
	}
	if c.args.sshKey != "" {
		spec.SshKey = new(c.args.sshKey)
	}
	if c.args.bootDiskSizeGiB > 0 {
		spec.BootDisk = publicv1.ComputeInstanceDisk_builder{
			SizeGib: c.args.bootDiskSizeGiB,
		}.Build()
	}
	if len(c.args.additionalDisks) > 0 {
		disks, diskErr := parseAdditionalDisks(c.args.additionalDisks)
		if diskErr != nil {
			return nil, diskErr
		}
		spec.AdditionalDisks = disks
	}
	if c.args.runStrategy != "" {
		spec.RunStrategy = new(c.args.runStrategy)
	}
	if c.args.userData != "" {
		spec.UserData = new(c.args.userData)
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

const shortHelp = `Create a compute instance.`

const longHelp = `
Create a compute instance.
`

const nameFlagHelp = `
_NAME_ - Name of the compute instance.
`

const templateFlagHelp = `
_TEMPLATE_ - Template identifier or name. Mutually exclusive with
{{ bt }}--catalog-item{{ bt }}.
`

const catalogItemFlagHelp = `
_ID_ - Catalog item identifier. Mutually exclusive with
{{ bt }}--template{{ bt }}.
`

const templateParameterFlagHelp = `
_NAME=VALUE_ - Template parameter in the format
{{ bt }}name=value{{ bt }}. Can be specified multiple times.
`

const templateParameterFileFlagHelp = `
_NAME=FILE_ - Template parameter whose value is read from a file, in the
format {{ bt }}name=filename{{ bt }}. Can be specified multiple
times.
`

const coresFlagHelp = `
_COUNT_ - Number of CPU cores.
`

const memoryFlagHelp = `
_SIZE_ - Memory size in GiB.
`

const imageFlagHelp = `
_URL_ - Image reference, for example an OCI image URL.
`

const imageSourceTypeFlagHelp = `
_TYPE_ - Image source type.
`

const sshKeyFlagHelp = `
_KEY_ - SSH public key.
`

const bootDiskSizeFlagHelp = `
_SIZE_ - Boot disk size in GiB.
`

const additionalDiskFlagHelp = `
_SIZE_ - Additional disk size in GiB. Can be specified multiple times to add
more than one disk.
`

const runStrategyFlagHelp = `
_STRATEGY_ - Run strategy, for example {{ bt }}Always{{ bt }} or
{{ bt }}Halted{{ bt }}.
`

const userDataFlagHelp = `
_DATA_ - User data for the compute instance, for example cloud-init or
ignition configuration.
`

const subnetFlagHelp = `
_ID_ - Subnet ID for the primary NIC.

This flag is deprecated. Use {{ bt }}--network-attachment{{ bt }}
instead.
`

const securityGroupFlagHelp = `
_ID_ - Security group ID applied together with
{{ bt }}--subnet{{ bt }}. Can be specified multiple times.

This flag is deprecated. Use {{ bt }}--network-attachment{{ bt }}
instead.
`

const networkAttachmentFlagHelp = `
_SPEC_ - Per-NIC network attachment. The value can be a plain subnet ID, or a
comma-separated specification in the format
{{ bt }}subnet=ID[,security-groups=ID,ID...]{{ bt }}. Can be
specified multiple times to attach multiple NICs.

This flag is incompatible with the deprecated
{{ bt }}--subnet{{ bt }} and
{{ bt }}--security-group{{ bt }} flags.
`
