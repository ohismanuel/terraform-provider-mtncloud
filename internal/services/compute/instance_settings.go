package compute

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/mtn/terraform-provider-mtncloud/internal/client"
)

var (
	_ resource.ResourceWithConfigure   = (*instanceSettingsResource)(nil)
	_ resource.ResourceWithImportState = (*instanceSettingsResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*instanceSettingsResource)(nil)
)

func newInstanceSettings() resource.Resource { return &instanceSettingsResource{} }

// instanceSettingsResource manages day-2 settings of an instance something else owns, such as a
// catalog order's VM (B-028). It adopts the instance and never deletes it. This first part manages the
// plan only; the other B-028 settings (disks, NICs, power, security groups, floating IP, labels) follow.
type instanceSettingsResource struct{ base }

type instanceSettingsModel struct {
	ID         types.String   `tfsdk:"id"`
	InstanceID types.String   `tfsdk:"instance_id"`
	Plan       types.String   `tfsdk:"plan"`
	Name       types.String   `tfsdk:"name"`
	Status     types.String   `tfsdk:"status"`
	Timeouts   timeouts.Value `tfsdk:"timeouts"`
}

func (r *instanceSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_settings"
}

func (r *instanceSettingsResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "Day-2 settings of an instance that something else owns, such as a VM a catalog order made " +
			"(`instance_ids` of `mtncloud_wireguard_vpn`, `mtncloud_postgresql` and the other orders). It adopts the instance " +
			"and changes it in place; destroy only stops tracking it, and the owner still deletes it. For now it manages the plan " +
			"(size); a setting left out isn't managed.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The instance's ID, the same as `instance_id`.", PlanModifiers: keep},
			"instance_id": schema.StringAttribute{
				Required: true,
				Description: "ID of the instance to manage, for example `mtncloud_wireguard_vpn.vpn.instance_ids[0]`. Changing it " +
					"stops managing the old instance and adopts the new one; neither is deleted.",
				Validators:    []validator.String{stringvalidator.RegexMatches(digits, "must be an instance ID")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"plan": schema.StringAttribute{
				Optional: true,
				Description: "Size of the VM, for example `G2S4`, from the plans the instance's image offers (checked at plan time). " +
					"**Changing it restarts the VM**, in place. Disks keep their size: MTN Cloud can't grow a root disk of an " +
					"existing instance. Left out, the size isn't managed.",
			},
			"name": schema.StringAttribute{Computed: true, Description: "The instance's name.", PlanModifiers: keep},
			// Kept from state; ModifyPlan marks it unknown when a resize (a restart) is planned.
			"status": schema.StringAttribute{Computed: true, Description: "The instance's status, for example `running`.", PlanModifiers: keep},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true, Update: true,
				CreateDescription: "How long adopting may take, a resize included (default 30m).",
				UpdateDescription: "How long a resize may take (default 30m).",
			}),
		},
	}
}

// ModifyPlan checks the plan name against the instance's own plan list, so a typo fails at plan time.
func (r *instanceSettingsResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || r.client == nil {
		return // destroy, or validation before the provider is configured
	}
	var plan, state instanceSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	hasState := !req.State.Raw.IsNull()
	if hasState {
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	}
	if resp.Diagnostics.HasError() || !known(plan.InstanceID) || !known(plan.Plan) {
		return
	}
	id, _ := strconv.ParseInt(plan.InstanceID.ValueString(), 10, 64)
	inst, err := r.client.GetInstance(ctx, id)
	if client.IsNotFound(err) {
		resp.Diagnostics.AddAttributeError(path.Root("instance_id"), "Instance not found",
			fmt.Sprintf("There is no instance %d in your tenant.", id))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read instance", err.Error())
		return
	}
	if _, err := r.planFor(ctx, inst, plan.Plan.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("plan"), "Unknown plan", err.Error())
		return
	}
	// The same plan in another case is no change: the state's spelling stays.
	if hasState && strings.EqualFold(plan.Plan.ValueString(), state.Plan.ValueString()) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("plan"), state.Plan)...)
		return
	}
	if !strings.EqualFold(plan.Plan.ValueString(), inst.PlanName) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("status"), types.StringUnknown())...)
	}
}

// planFor finds a plan by name among the plans the instance's layout offers in its project.
func (r *instanceSettingsResource) planFor(ctx context.Context, inst *client.Instance, name string) (client.Plan, error) {
	p, err := r.client.PlacementForGroup(ctx, inst.GroupName)
	if err != nil {
		return client.Plan{}, err
	}
	return r.client.PlanByName(ctx, p, inst.ProjectID, inst.LayoutID, name)
}

func (r *instanceSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan instanceSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	timeout, d := plan.Timeouts.Create(ctx, instanceTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	plan.ID = plan.InstanceID
	r.apply(ctx, &plan, "create", &resp.Diagnostics)
	if resp.Diagnostics.HasError() && plan.Name.IsUnknown() {
		return // not adopted: nothing to record
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *instanceSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan instanceSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	timeout, d := plan.Timeouts.Update(ctx, instanceTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() || !r.ready(&resp.Diagnostics) {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	r.apply(ctx, &plan, "update", &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// apply resizes the instance when its plan differs from the configured one, then records what it reads.
func (r *instanceSettingsResource) apply(ctx context.Context, m *instanceSettingsModel, op string, diags *diag.Diagnostics) {
	id, _ := strconv.ParseInt(m.InstanceID.ValueString(), 10, 64)
	inst, err := r.client.GetInstance(ctx, id)
	if err != nil {
		diags.AddError("Unable to read instance", err.Error())
		return
	}
	if known(m.Plan) && !strings.EqualFold(m.Plan.ValueString(), inst.PlanName) {
		pl, err := r.planFor(ctx, inst, m.Plan.ValueString())
		if err != nil {
			diags.AddAttributeError(path.Root("plan"), "Unknown plan", err.Error())
			r.record(m, inst)
			return
		}
		// Every disk goes with its ID and size, so nothing is added or removed; no cards are sent.
		resize := client.ResizeInput{PlanID: pl.ID}
		for _, v := range inst.Volumes {
			resize.Volumes = append(resize.Volumes, client.VolumeSpec{ID: v.ID, Root: v.Root, Name: v.Name, Size: v.Size, DatastoreID: v.DatastoreID})
		}
		if err := r.client.ResizeInstance(ctx, id, resize); err != nil {
			diags.AddError("Unable to reconfigure instance", err.Error())
			r.record(m, inst)
			return
		}
		got, err := r.client.WaitForResize(ctx, id, pl.ID, len(inst.Volumes), 0)
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			diags.AddError("Timed out waiting for the instance",
				fmt.Sprintf("Instance %d hasn't settled on plan %s after the %s timeout. Check it in the console, or raise `timeouts { %s = \"60m\" }`.",
					id, pl.Name, op, op))
		case err != nil:
			diags.AddError("Unable to read instance", err.Error())
		case client.InstanceFailed(got.Status):
			diags.AddError("Instance resize failed", fmt.Sprintf("MTN Cloud reports instance %d as %q: %s.", id, got.Status, got.StatusMessage))
		}
		if got != nil {
			inst = got
		}
	}
	r.record(m, inst)
}

// record copies what the instance reads into the model. A configured plan keeps its spelling while
// it names the instance's plan; one that differs (resized in the console) reads as the real plan.
func (r *instanceSettingsResource) record(m *instanceSettingsModel, inst *client.Instance) {
	m.ID = types.StringValue(strconv.FormatInt(inst.ID, 10))
	m.InstanceID = m.ID
	m.Name = types.StringValue(inst.Name)
	m.Status = types.StringValue(inst.Status)
	if !m.Plan.IsNull() && !strings.EqualFold(m.Plan.ValueString(), inst.PlanName) {
		m.Plan = types.StringValue(inst.PlanName)
	}
}

func (r *instanceSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if !r.ready(&resp.Diagnostics) {
		return
	}
	var state instanceSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := strconv.ParseInt(state.InstanceID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid instance ID", fmt.Sprintf("%q is not an instance ID: %s", state.InstanceID.ValueString(), err))
		return
	}
	inst, err := r.client.GetInstance(ctx, id)
	if client.IsNotFound(err) || (err == nil && strings.EqualFold(inst.Status, "pendingRemoval")) {
		resp.State.RemoveResource(ctx) // its owner deleted it
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read instance", err.Error())
		return
	}
	r.record(&state, inst)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Delete deletes nothing: the instance belongs to whatever made it.
func (r *instanceSettingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state instanceSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.AddWarning("Instance kept",
		fmt.Sprintf("Instance %s is no longer managed here but still exists, with the settings it has now. Its owner (the order, "+
			"or the console) deletes it.", state.InstanceID.ValueString()))
}

func (r *instanceSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if !digits.MatchString(req.ID) {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Import by the instance's numeric ID, for example 9701. Got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), req.ID)...)
}
