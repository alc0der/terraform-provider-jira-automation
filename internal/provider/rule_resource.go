package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"terraform-provider-jira-automation/internal/client"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
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
				Optional:    true,
				ElementType: types.StringType,
				Description: "Rule scope ARIs (e.g. ari:cloud:jira:<cloudId>:project/<projectId>).",
			},
			"labels": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Rule labels.",
			},
			"trigger_json": schema.StringAttribute{
				Required:    true,
				CustomType:  jsontypes.NormalizedType{},
				Description: "Trigger configuration as a JSON string.",
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

	trigger := json.RawMessage(plan.TriggerJSON.ValueString())
	components, err := parseComponentsJSON(plan.ComponentsJSON.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid components_json", err.Error())
		return
	}

	createReq := client.CreateRuleRequest{
		Name:          plan.Name.ValueString(),
		Trigger:       trigger,
		Components:    components,
		RuleScopeARIs: toStringSlice(ctx, plan.Scope),
		Labels:        toStringSlice(ctx, plan.Labels),
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
	diags := r.readIntoModel(ctx, uuid, &plan)
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

	trigger := json.RawMessage(plan.TriggerJSON.ValueString())
	components, err := parseComponentsJSON(plan.ComponentsJSON.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid components_json", err.Error())
		return
	}

	updateReq := client.UpdateRuleRequest{
		Name:          plan.Name.ValueString(),
		Trigger:       trigger,
		Components:    components,
		RuleScopeARIs: toStringSlice(ctx, plan.Scope),
		Labels:        toStringSlice(ctx, plan.Labels),
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

	// Trigger JSON — normalize through interface{} for canonical key ordering.
	triggerNorm, err := normalizeRawJSON(rule.Trigger)
	if err != nil {
		diags.AddError("Error normalizing trigger", err.Error())
		return diags
	}
	model.TriggerJSON = jsontypes.NewNormalizedValue(triggerNorm)

	// Components JSON — normalize through interface{} for canonical key ordering.
	componentsNorm, err := normalizeRawJSONArray(rule.Components)
	if err != nil {
		diags.AddError("Error normalizing components", err.Error())
		return diags
	}
	model.ComponentsJSON = jsontypes.NewNormalizedValue(componentsNorm)

	return diags
}

// Helper functions

func parseComponentsJSON(s string) ([]json.RawMessage, error) {
	var components []json.RawMessage
	if err := json.Unmarshal([]byte(s), &components); err != nil {
		return nil, fmt.Errorf("invalid components_json: %w", err)
	}
	return components, nil
}

// normalizeRawJSON round-trips raw JSON through interface{} for canonical output.
func normalizeRawJSON(raw json.RawMessage) (string, error) {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
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
		arr = append(arr, v)
	}
	out, err := json.Marshal(arr)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func toStringSlice(ctx context.Context, list types.List) []string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var strs []string
	list.ElementsAs(ctx, &strs, false)
	return strs
}
