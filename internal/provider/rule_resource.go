package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"terraform-provider-jira-automation/internal/client"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ruleResource{}
	_ resource.ResourceWithImportState = &ruleResource{}
)

type ruleResource struct {
	client *client.Client
}

type ruleResourceModel struct {
	ID             types.String         `tfsdk:"id"`
	Name           types.String         `tfsdk:"name"`
	Enabled        types.Bool           `tfsdk:"enabled"`
	State          types.String         `tfsdk:"state"`
	Scope          types.List           `tfsdk:"scope"`
	Labels         types.List           `tfsdk:"labels"`
	ProjectID      types.String         `tfsdk:"project_id"`
	Trigger        *triggerModel        `tfsdk:"trigger"`
	TriggerJSON    jsontypes.Normalized `tfsdk:"trigger_json"`
	ComponentsJSON jsontypes.Normalized `tfsdk:"components_json"`
}

func NewRuleResource() resource.Resource {
	return &ruleResource{}
}

func (r *ruleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rule"
}

func (r *ruleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Jira Automation rule. Note: the public API has no DELETE endpoint, so terraform destroy will disable the rule instead of deleting it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Rule UUID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Rule name.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the rule is enabled.",
			},
			"state": schema.StringAttribute{
				Computed:    true,
				Description: "Rule state (ENABLED or DISABLED).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"scope": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Rule scope ARIs assigned by the API.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"labels": schema.ListAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Description: "Rule labels. The provider auto-adds managed-by:terraform. Labels are add-only (never removed).",
				PlanModifiers: []planmodifier.List{
					labelsAddOnlyModifier{},
				},
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Description: "Jira project numeric ID. Used to scope event-based triggers to a project.",
			},
			"trigger": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Structured trigger configuration. Mutually exclusive with trigger_json.",
				Validators: []validator.Object{
					objectvalidator.ExactlyOneOf(path.MatchRoot("trigger_json")),
				},
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:    true,
						Description: "Trigger type (e.g. status_transition).",
					},
					"args": schema.MapAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Trigger arguments as key-value pairs.",
					},
				},
			},
			"trigger_json": schema.StringAttribute{
				Optional:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Trigger configuration as a JSON string. Mutually exclusive with trigger.",
			},
			"components_json": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Components (actions/conditions) as a JSON array string.",
			},
		},
	}
}

func (r *ruleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.Client, got: %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *ruleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ruleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	trigger, diags := r.resolveTriggerJSON(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	components, err := parseComponentsJSON(plan.ComponentsJSON.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid components_json", err.Error())
		return
	}

	createReq := client.CreateRuleRequest{
		Name:       plan.Name.ValueString(),
		Trigger:    trigger,
		Components: components,
	}

	uuid, err := r.client.CreateRule(createReq)
	if err != nil {
		resp.Diagnostics.AddError("Error creating rule", err.Error())
		return
	}

	// Set the rule state after creation if needed.
	enabled := plan.Enabled.ValueBool()
	if err := r.client.SetRuleState(uuid, enabled); err != nil {
		resp.Diagnostics.AddError("Error setting rule state after creation", err.Error())
		return
	}

	// Read back the created rule to populate all computed fields.
	diags = r.readIntoModel(ctx, uuid, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Sync labels via internal API.
	r.syncLabels(ctx, uuid, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-read after label sync to pick up the new labels.
	diags = r.readIntoModel(ctx, uuid, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ruleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ruleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags := r.readIntoModel(ctx, state.ID.ValueString(), &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ruleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ruleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ruleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	uuid := state.ID.ValueString()

	trigger, d := r.resolveTriggerJSON(ctx, &plan)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}

	components, err := parseComponentsJSON(plan.ComponentsJSON.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid components_json", err.Error())
		return
	}

	updateReq := client.UpdateRuleRequest{
		Name:       plan.Name.ValueString(),
		Trigger:    trigger,
		Components: components,
	}

	if err := r.client.UpdateRule(uuid, updateReq); err != nil {
		resp.Diagnostics.AddError("Error updating rule", err.Error())
		return
	}

	// Handle enabled state change.
	enabled := plan.Enabled.ValueBool()
	if err := r.client.SetRuleState(uuid, enabled); err != nil {
		resp.Diagnostics.AddError("Error setting rule state", err.Error())
		return
	}

	// Read back the updated rule.
	diags := r.readIntoModel(ctx, uuid, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Sync labels via internal API.
	r.syncLabels(ctx, uuid, plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// Re-read after label sync to pick up the new labels.
	diags = r.readIntoModel(ctx, uuid, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ruleResource) Delete(_ context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ruleResourceModel
	resp.Diagnostics.Append(req.State.Get(context.Background(), &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// No DELETE endpoint in the public API — disable the rule instead.
	uuid := state.ID.ValueString()
	if err := r.client.SetRuleState(uuid, false); err != nil {
		resp.Diagnostics.AddError("Error disabling rule on destroy",
			fmt.Sprintf("The Jira Automation API has no DELETE endpoint. Attempted to disable rule %s instead, but got error: %s", uuid, err.Error()))
		return
	}

	resp.Diagnostics.AddWarning("Rule disabled, not deleted",
		fmt.Sprintf("Rule %s was disabled because the Jira Automation API does not support deletion. You may want to manually remove it from the Jira UI.", uuid))
}

func (r *ruleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	uuid := req.ID

	var model ruleResourceModel
	diags := r.readIntoModel(ctx, uuid, &model)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// readIntoModel fetches a rule by UUID and populates the model.
func (r *ruleResource) readIntoModel(ctx context.Context, uuid string, model *ruleResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	rule, err := r.client.GetRule(uuid)
	if err != nil {
		diags.AddError("Error reading rule", err.Error())
		return diags
	}

	model.ID = types.StringValue(rule.UUID)
	model.Name = types.StringValue(rule.Name)
	model.State = types.StringValue(rule.State)
	model.Enabled = types.BoolValue(rule.State == "ENABLED")

	// Scope
	if len(rule.RuleScopeARIs) > 0 {
		scopeList, d := types.ListValueFrom(ctx, types.StringType, rule.RuleScopeARIs)
		diags.Append(d...)
		model.Scope = scopeList
	} else {
		model.Scope = types.ListNull(types.StringType)
	}

	// Labels
	if len(rule.Labels) > 0 {
		labelList, d := types.ListValueFrom(ctx, types.StringType, rule.Labels)
		diags.Append(d...)
		model.Labels = labelList
	} else {
		model.Labels = types.ListNull(types.StringType)
	}

	// Trigger — if the user used the structured trigger block, parse the API
	// response back into the trigger model. Otherwise, populate trigger_json.
	if model.Trigger != nil {
		triggerType, args, err := ParseTrigger(rule.Trigger)
		if err != nil {
			diags.AddError("Error parsing trigger from API", err.Error())
			return diags
		}
		argsMap, d := types.MapValueFrom(ctx, types.StringType, args)
		diags.Append(d...)
		model.Trigger = &triggerModel{
			Type: types.StringValue(triggerType),
			Args: argsMap,
		}
	} else {
		triggerNorm, err := normalizeRawJSON(rule.Trigger)
		if err != nil {
			diags.AddError("Error normalizing trigger", err.Error())
			return diags
		}
		model.TriggerJSON = jsontypes.NewNormalizedValue(triggerNorm)
	}

	// Components JSON — normalize through interface{} for canonical key ordering.
	componentsNorm, err := normalizeRawJSONArray(rule.Components)
	if err != nil {
		diags.AddError("Error normalizing components", err.Error())
		return diags
	}
	model.ComponentsJSON = jsontypes.NewNormalizedValue(componentsNorm)

	return diags
}

// resolveTriggerJSON returns the trigger JSON from either the structured trigger
// block or the raw trigger_json attribute.
func (r *ruleResource) resolveTriggerJSON(ctx context.Context, model *ruleResourceModel) (json.RawMessage, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Trigger != nil {
		triggerType := model.Trigger.Type.ValueString()

		args := make(map[string]string)
		if !model.Trigger.Args.IsNull() && !model.Trigger.Args.IsUnknown() {
			diags.Append(model.Trigger.Args.ElementsAs(ctx, &args, false)...)
			if diags.HasError() {
				return nil, diags
			}
		}

		projectID := model.ProjectID.ValueString()
		raw, err := BuildTriggerJSON(triggerType, args, r.client.CloudID, projectID)
		if err != nil {
			diags.AddError("Error building trigger JSON", err.Error())
			return nil, diags
		}
		return raw, diags
	}

	return json.RawMessage(model.TriggerJSON.ValueString()), diags
}

// Helper functions

func parseComponentsJSON(s string) ([]json.RawMessage, error) {
	var components []json.RawMessage
	if err := json.Unmarshal([]byte(s), &components); err != nil {
		return nil, fmt.Errorf("invalid components_json: %w", err)
	}
	return components, nil
}

// normalizeRawJSON round-trips raw JSON through interface{} for canonical output,
// stripping API-assigned fields (id, parentId, conditionParentId) that aren't
// part of the Terraform config.
func normalizeRawJSON(raw json.RawMessage) (string, error) {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	stripAPIFields(v)
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func normalizeRawJSONArray(raws []json.RawMessage) (string, error) {
	var arr []interface{}
	for _, raw := range raws {
		var v interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", err
		}
		stripAPIFields(v)
		arr = append(arr, v)
	}
	out, err := json.Marshal(arr)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// stripAPIFields recursively removes API-assigned fields from component JSON
// so the normalized output matches the Terraform config (which doesn't include them).
func stripAPIFields(v interface{}) {
	m, ok := v.(map[string]interface{})
	if !ok {
		return
	}

	delete(m, "id")
	delete(m, "parentId")
	delete(m, "conditionParentId")

	if children, ok := m["children"].([]interface{}); ok {
		for _, child := range children {
			stripAPIFields(child)
		}
	}
	if conditions, ok := m["conditions"].([]interface{}); ok {
		for _, cond := range conditions {
			stripAPIFields(cond)
		}
	}
}

func toStringSlice(ctx context.Context, list types.List) []string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var strs []string
	list.ElementsAs(ctx, &strs, false)
	return strs
}

// syncLabels ensures all desired labels (config labels + managed-by:terraform) are present
// on the rule via the internal API. Skips if the rule has no single-project scope.
func (r *ruleResource) syncLabels(ctx context.Context, uuid string, model ruleResourceModel, diags *diag.Diagnostics) {
	// Extract single project ID from scope.
	scopes := toStringSlice(ctx, model.Scope)
	if len(scopes) != 1 {
		return // Global or multi-project rule — skip label management.
	}

	projectID := client.ExtractProjectID(scopes[0])
	if projectID == "" {
		return
	}

	// Build desired label set: config labels + managed-by:terraform.
	desired := map[string]bool{"managed-by:terraform": true}
	for _, l := range toStringSlice(ctx, model.Labels) {
		desired[l] = true
	}

	// Check which labels already exist on the rule (from the read-back).
	existing := map[string]bool{}
	for _, l := range toStringSlice(ctx, model.Labels) {
		existing[l] = true
	}

	for label := range desired {
		if existing[label] {
			continue
		}
		if err := r.client.EnsureLabel(projectID, uuid, label); err != nil {
			diags.AddWarning("Error adding label",
				fmt.Sprintf("Could not add label %q to rule %s: %s", label, uuid, err.Error()))
		}
	}
}

// --- Labels plan modifier (add-only semantics) ---

type labelsAddOnlyModifier struct{}

func (m labelsAddOnlyModifier) Description(_ context.Context) string {
	return "Computes labels as the union of config, state, and managed-by:terraform (add-only)."
}

func (m labelsAddOnlyModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m labelsAddOnlyModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	// If config is null/unknown, preserve state (no diff).
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		if !req.StateValue.IsNull() {
			resp.PlanValue = req.StateValue
		}
		return
	}

	// Build union of config labels + state labels + managed-by:terraform.
	seen := map[string]bool{}
	var union []string

	addLabel := func(l string) {
		if !seen[l] {
			seen[l] = true
			union = append(union, l)
		}
	}

	// Config labels first (preserves user-specified order).
	var configLabels []string
	req.ConfigValue.ElementsAs(ctx, &configLabels, false)
	for _, l := range configLabels {
		addLabel(l)
	}

	// State labels (from previous apply or Jira UI additions).
	if !req.StateValue.IsNull() && !req.StateValue.IsUnknown() {
		var stateLabels []string
		req.StateValue.ElementsAs(ctx, &stateLabels, false)
		for _, l := range stateLabels {
			addLabel(l)
		}
	}

	// Always include managed-by:terraform.
	addLabel("managed-by:terraform")

	planned, diags := types.ListValueFrom(ctx, types.StringType, union)
	resp.Diagnostics.Append(diags...)
	resp.PlanValue = planned
}
