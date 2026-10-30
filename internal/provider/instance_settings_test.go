package provider

import (
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

// seedOrderVM adds an instance as a catalog order leaves it (captured 2026-10-30, a WireGuard VPN):
// running, with its layout, one root disk, and no Terraform resource of its own.
func seedOrderVM(s *instStore, id int64, plan float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.insts[id] = map[string]any{
		"id": id, "name": "vpn-1", "status": "running",
		"group":        map[string]any{"id": 9101, "name": "MTNNG_CLOUD_AZ_1"},
		"cloud":        map[string]any{"id": 4, "name": "MTNNG_CLOUD_AZ_1"},
		"instanceType": map[string]any{"code": "MTN-WGVPN-01", "name": "MTN-WIREGUARD-SSL-VPN"},
		"layout":       map[string]any{"id": 364, "name": "MTN-WIREGUARD-SSL-VPN"},
		"plan":         map[string]any{"id": plan, "name": fakePlans[plan]},
		"config":       map[string]any{"resourcePoolId": "pool-9201"},
		"volumes":      []any{map[string]any{"id": id*10 + 1, "name": "root", "size": 20, "rootVolume": true, "storageType": 11, "datastoreId": "auto"}},
		"interfaces":   []any{}, "evars": []any{}, "connectionInfo": []any{},
	}
}

func settingsConfig(f *fakeAPI, attrs string) string {
	return f.providerConfig() + `
resource "mtncloud_instance_settings" "vpn" {
  instance_id = "9801"
` + attrs + `
}
`
}

func settingsPlan(action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("mtncloud_instance_settings.vpn", action)}}
}

func TestInstanceSettings_Lifecycle(t *testing.T) {
	f, s := newInstFakes(t)
	seedOrderVM(s, 9801, 9401)
	lock := func(fn func()) func() { return func() { s.mu.Lock(); defer s.mu.Unlock(); fn() } }
	plan := func(want string) statecheck.StateCheck {
		return statecheck.ExpectKnownValue("mtncloud_instance_settings.vpn", tfjsonpath.New("plan"), knownvalue.StringExact(want))
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []resource.TestStep{
			{
				// Adopting at the plan it has: no resize.
				Config:            settingsConfig(f, `  plan = "G1S1"`),
				ConfigPlanChecks:  settingsPlan(plancheck.ResourceActionCreate),
				ConfigStateChecks: []statecheck.StateCheck{plan("G1S1")},
				Check: func(*terraform.State) error {
					if len(s.resizes) != 0 {
						t.Errorf("%d resizes on adopt at the same plan, want none", len(s.resizes))
					}
					return nil
				},
			},
			{Config: settingsConfig(f, `  plan = "g1s1"`), ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}},
			{
				// A new plan resizes in place, sending the root disk with its ID and size.
				Config:           settingsConfig(f, `  plan = "G2S4"`),
				ConfigPlanChecks: settingsPlan(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{plan("G2S4"),
					statecheck.ExpectKnownValue("mtncloud_instance_settings.vpn", tfjsonpath.New("status"), knownvalue.StringExact("running"))},
				Check: func(*terraform.State) error {
					if len(s.resizes) != 1 {
						t.Fatalf("%d resizes, want 1", len(s.resizes))
					}
					vols := s.resizes[0]["volumes"].([]any)
					if v := vols[0].(map[string]any); len(vols) != 1 || v["id"] != 98011.0 || v["size"] != 20.0 {
						t.Errorf("resize sent volumes %v, want the root disk 98011 at 20 GB only", vols)
					}
					if _, ok := s.resizes[0]["networkInterfaces"]; ok {
						t.Error("resize sent network cards, want none")
					}
					return nil
				},
			},
			{
				// Resized in the console: the next plan puts it back.
				PreConfig: lock(func() { s.insts[9801]["plan"] = map[string]any{"id": 9401.0, "name": "G1S1"} }),
				Config:    settingsConfig(f, `  plan = "G2S4"`), ConfigPlanChecks: settingsPlan(plancheck.ResourceActionUpdate),
			},
			{
				// Left out, the plan isn't managed.
				Config: settingsConfig(f, ""), ConfigPlanChecks: settingsPlan(plancheck.ResourceActionUpdate),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue("mtncloud_instance_settings.vpn", tfjsonpath.New("plan"), knownvalue.Null()),
				},
			},
			{ResourceName: "mtncloud_instance_settings.vpn", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
			{
				// Destroy keeps the instance.
				Config: f.providerConfig(),
				Check: func(*terraform.State) error {
					if s.count() != 1 {
						t.Errorf("%d instances after destroy, want the adopted one kept", s.count())
					}
					return nil
				},
			},
		},
	})
}

func TestInstanceSettings_Refusals(t *testing.T) {
	f, s := newInstFakes(t)
	seedOrderVM(s, 9801, 9401)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []resource.TestStep{
			{Config: settingsConfig(f, `  plan = "G9S99"`), PlanOnly: true, ExpectError: regexp.MustCompile(`(?s)Unknown plan.*G1S1,\s+G2S4`)},
			{Config: strings.Replace(settingsConfig(f, `  plan = "G1S1"`), `"9801"`, `"9899"`, 1), PlanOnly: true,
				ExpectError: regexp.MustCompile(`Instance not found`)},
			{Config: strings.Replace(settingsConfig(f, ""), `"9801"`, `"vpn"`, 1), PlanOnly: true, ExpectError: regexp.MustCompile(`must be an instance ID`)},
			{ResourceName: "mtncloud_instance_settings.vpn", ImportState: true, ImportStateId: "vpn", Config: settingsConfig(f, ""),
				ExpectError: regexp.MustCompile(`Invalid import ID`)},
		},
	})
}

// The owner deleting the instance leaves nothing to manage: the next plan adopts again and fails cleanly.
func TestInstanceSettings_OwnerDeleted(t *testing.T) {
	f, s := newInstFakes(t)
	seedOrderVM(s, 9801, 9401)
	lock := func(fn func()) func() { return func() { s.mu.Lock(); defer s.mu.Unlock(); fn() } }
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories(),
		Steps: []resource.TestStep{
			{Config: settingsConfig(f, `  plan = "G1S1"`)},
			{
				PreConfig: lock(func() { delete(s.insts, 9801) }),
				Config:    settingsConfig(f, `  plan = "G1S1"`), PlanOnly: true, ExpectError: regexp.MustCompile(`Instance not found`),
			},
			{Config: f.providerConfig()},
		},
	})
}
