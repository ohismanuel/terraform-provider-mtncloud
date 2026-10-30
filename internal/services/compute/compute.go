// Package compute holds the instance resource. It doesn't import other service packages: a network
// is referenced by its ID, which any configuration can take from mtncloud_network.
package compute

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/mtn/terraform-provider-mtncloud/internal/client"
	"github.com/mtn/terraform-provider-mtncloud/internal/planmodifiers"
)

// Resources lists every resource in this package, for the provider to register.
func Resources() []func() resource.Resource {
	return []func() resource.Resource{newInstance, newInstanceSettings}
}

// base gives every resource its client.
type base struct {
	client *client.Client
}

func (b *base) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return // validate or an unconfigured provider; CRUD checks ready()
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("expected *client.Client, got %T. This is a provider bug.", req.ProviderData))
		return
	}
	b.client = c
}

// ready reports whether the provider is configured; see lookup.base.ready for when it isn't.
func (b *base) ready(diags *diag.Diagnostics) bool {
	if b.client == nil {
		diags.AddError("MTN Cloud provider is not configured yet",
			"The provider block uses values that are only known after apply. Use values known at plan time, or apply the resources they come from first.")
		return false
	}
	return true
}

// fillDefaults fills the attributes the configuration leaves out from the provider-wide defaults
// (the provider block, or env), in resp.Plan and in req.Plan, which the rest of ModifyPlan reads.
func (b *base) fillDefaults(ctx context.Context, req *resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse, attrs ...string) {
	d := b.client.DefaultsFor(ctx)
	values := map[string][2]string{
		"project": {d.Project, "MTNCLOUD_PROJECT"}, "group": {d.Group, "MTNCLOUD_GROUP"}, "cloud": {d.Cloud, "MTNCLOUD_CLOUD"},
	}
	for _, a := range attrs {
		resp.Diagnostics.Append(planmodifiers.FillDefault(ctx, req.Config, &resp.Plan, a, values[a][1], values[a][0])...)
	}
	req.Plan = resp.Plan
}
