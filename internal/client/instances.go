package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// Instances (OpenStack VMs): POST/GET/PUT/DELETE /api/instances[/{id}], plus /resize and
// /security-groups. Shapes come from a customer's reads and CLI dry runs (2026-10-27), MTN's
// FastCreateLinuxInstance.sh and the API reference: see dev-docs/api-research.md. An instance
// starts "provisioning" and ends "running", or "failed" / "error".

// Instance is an OpenStack instance as the API returns it.
type Instance struct {
	ID            int64
	Name          string
	Description   string
	HostName      string
	Status        string
	StatusMessage string // the platform's own words for a failure, when it gives some

	GroupName   string
	CloudName   string
	ProjectID   int64 // the resource pool, from config.resourcePoolId ("pool-<id>")
	ImageCode   string
	ImageName   string
	PlanID      int64
	PlanName    string
	LayoutID    int64  // the layout it was made from; resize plans are listed per layout
	Environment string // the environment code, for example "production"; "" for none
	Labels      []string
	Tags        map[string]string
	// EnvVars holds every variable the API lists, the platform's own included: those start with
	// EnvPrefix, for example "MTN_UBUNTU_SERVER_24_04_3LTS_IP".
	EnvVars         map[string]string
	EnvPrefix       string
	NetworkDomainID int64
	NoAgent         bool
	LayoutSize      int64

	Volumes    []Volume    // root first, then data volumes in the API's order
	Interfaces []Interface // primary first
	// ContainerIDs are the instance's VMs (one per scale_factor); floating IPs attach to these.
	ContainerIDs []int64
	PrivateIP    string
	PublicIP     string

	// Lifecycle: the days set at create (0 for none) and the dates they lead to ("" for none).
	ExpireDays, ShutdownDays int64
	ExpireDate, ShutdownDate string
}

// Volume is a disk of an instance. DatastoreID is "auto" (the project default) or a datastore ID.
type Volume struct {
	ID          int64
	Name        string
	Size        int64 // GB
	Root        bool
	DatastoreID string
}

// Interface is a network card of an instance.
type Interface struct {
	ID        string // the API sends a number or "network-<id>"; kept as sent for resize
	NetworkID int64
}

// InstanceInput is an instance to create. Empty strings and zero IDs are left out of the body, so
// the platform's own defaults (naming template, hostname policy) apply.
type InstanceInput struct {
	Name, Description, HostName string
	Placement                   Placement
	ProjectID                   int64
	ImageCode                   string
	LayoutID, PlanID            int64
	Environment                 string // code
	Labels                      []string
	Tags, EnvVars               map[string]string
	NetworkDomainID             int64
	SecurityGroupIDs            []int64
	Volumes                     []VolumeSpec // root first
	NetworkIDs                  []int64      // primary first
	NoAgent, CreateUser         bool
	LayoutSize                  int64
	ExpireDays, ShutdownDays    int64 // 0: none (or the policy's)
}

// VolumeSpec is a disk to create (ID 0) or to keep (its ID) in a create or resize.
type VolumeSpec struct {
	ID          int64
	Root        bool
	Name        string
	Size        int64
	DatastoreID string // "auto" or a datastore ID
}

// ResizeInput is a reconfigure: the plan, and the full lists of volumes and network cards, kept ones
// with their IDs and new ones without.
type ResizeInput struct {
	PlanID     int64
	Volumes    []VolumeSpec
	Interfaces []Interface // ID "" for a new card
}

// InstanceUpdate holds what PUT /api/instances/{id} changes in place.
type InstanceUpdate struct {
	Name, Description, Environment string
	Labels                         []string
	Tags                           map[string]string
}

type wireInstance struct {
	Containers        []int64  `json:"containers"`
	ExpireDays        *int64   `json:"expireDays"`
	ShutdownDays      *int64   `json:"shutdownDays"`
	ExpireDate        *string  `json:"expireDate"`
	ShutdownDate      *string  `json:"shutdownDate"`
	ID                int64    `json:"id"`
	Name              string   `json:"name"`
	Description       *string  `json:"description"`
	HostName          *string  `json:"hostName"`
	Status            string   `json:"status"`
	StatusMessage     *string  `json:"statusMessage"`
	ErrorMessage      *string  `json:"errorMessage"`
	InstanceContext   *string  `json:"instanceContext"`
	EnvironmentPrefix string   `json:"environmentPrefix"`
	Group             *wireRef `json:"group"`
	Cloud             *wireRef `json:"cloud"`
	InstanceType      *struct {
		Code string `json:"code"`
		Name string `json:"name"`
	} `json:"instanceType"`
	Plan          *wireRef  `json:"plan"`
	Layout        *wireRef  `json:"layout"`
	NetworkDomain *wireRef  `json:"networkDomain"`
	Labels        []string  `json:"labels"`
	Tags          []wireTag `json:"tags"`
	Evars         []struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	} `json:"evars"`
	Config struct {
		ResourcePoolID any   `json:"resourcePoolId"`
		NoAgent        *bool `json:"noAgent"`
		LayoutSize     any   `json:"layoutSize"`
	} `json:"config"`
	Volumes []struct {
		ID          int64  `json:"id"`
		Name        string `json:"name"`
		Size        int64  `json:"size"`
		RootVolume  bool   `json:"rootVolume"`
		DatastoreID any    `json:"datastoreId"`
	} `json:"volumes"`
	Interfaces []struct {
		ID      any      `json:"id"`
		Network *wireRef `json:"network"`
		Primary *bool    `json:"primaryInterface"`
	} `json:"interfaces"`
	ConnectionInfo []struct {
		IP string `json:"ip"`
	} `json:"connectionInfo"`
}

type wireTag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (w wireInstance) instance() *Instance {
	in := &Instance{
		ID: w.ID, Name: w.Name, Description: deref(w.Description), HostName: deref(w.HostName),
		Status: w.Status, Environment: deref(w.InstanceContext), Labels: w.Labels, EnvPrefix: w.EnvironmentPrefix,
		Tags: map[string]string{}, EnvVars: map[string]string{},
	}
	in.ContainerIDs = w.Containers
	in.ExpireDate, in.ShutdownDate = deref(w.ExpireDate), deref(w.ShutdownDate)
	if w.ExpireDays != nil {
		in.ExpireDays = *w.ExpireDays
	}
	if w.ShutdownDays != nil {
		in.ShutdownDays = *w.ShutdownDays
	}
	if in.StatusMessage = deref(w.StatusMessage); in.StatusMessage == "" {
		in.StatusMessage = deref(w.ErrorMessage)
	}
	if w.Group != nil {
		in.GroupName = w.Group.Name
	}
	if w.Cloud != nil {
		in.CloudName = w.Cloud.Name
	}
	if w.InstanceType != nil {
		in.ImageCode, in.ImageName = w.InstanceType.Code, w.InstanceType.Name
	}
	if w.Plan != nil {
		in.PlanID, in.PlanName = w.Plan.ID, w.Plan.Name
	}
	if w.Layout != nil {
		in.LayoutID = w.Layout.ID
	}
	if w.NetworkDomain != nil {
		in.NetworkDomainID = w.NetworkDomain.ID
	}
	for _, t := range w.Tags {
		in.Tags[t.Name] = t.Value
	}
	for _, e := range w.Evars {
		in.EnvVars[e.Name] = fmt.Sprint(e.Value)
	}
	if in.EnvPrefix == "" {
		in.EnvPrefix = derivedEnvPrefix(in.EnvVars, in.ImageName)
	}
	in.ProjectID = idFrom(w.Config.ResourcePoolID, "pool-")
	in.NoAgent = w.Config.NoAgent != nil && *w.Config.NoAgent
	if in.LayoutSize = idFrom(w.Config.LayoutSize, ""); in.LayoutSize == 0 {
		in.LayoutSize = 1
	}
	for _, v := range w.Volumes {
		ds := "auto"
		if v.DatastoreID != nil {
			ds = idString(v.DatastoreID)
		}
		vol := Volume{ID: v.ID, Name: v.Name, Size: v.Size, Root: v.RootVolume, DatastoreID: ds}
		if vol.Root {
			in.Volumes = slices.Insert(in.Volumes, 0, vol)
		} else {
			in.Volumes = append(in.Volumes, vol)
		}
	}
	for _, i := range w.Interfaces {
		if i.Network == nil {
			continue
		}
		nic := Interface{ID: idString(i.ID), NetworkID: i.Network.ID}
		if i.Primary != nil && *i.Primary {
			in.Interfaces = slices.Insert(in.Interfaces, 0, nic)
		} else {
			in.Interfaces = append(in.Interfaces, nic)
		}
	}
	// The instance doesn't carry its addresses and customers can't read /api/servers (403), but
	// the platform always sets <PREFIX>_IP to the private address (captured 2026-10-27).
	if in.EnvPrefix != "" {
		in.PrivateIP = in.EnvVars[in.EnvPrefix+"_IP"]
	} else {
		// Instances made in the console can have no prefix (captured 2026-10-27): use the one
		// <PREFIX>_IP variable there is, for example APACHE_WEB_SERVER_UBUNTU22_IP.
		var ips []string
		for k, v := range in.EnvVars {
			if strings.HasSuffix(k, "_IP") {
				ips = append(ips, v)
			}
		}
		if len(ips) == 1 {
			in.PrivateIP = ips[0]
		}
	}
	for _, ci := range w.ConnectionInfo {
		if ci.IP != "" && ci.IP != in.PrivateIP {
			in.PublicIP = ci.IP // a floating IP
			break
		}
	}
	return in
}

// idFrom reads an ID the API sends as a number, a numeric string or "<prefix><id>".
// idString is an ID the API sends as a number or a string ("5742", "network-501"), as text.
func idString(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case string:
		return x
	}
	return ""
}

func idFrom(v any, prefix string) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(strings.TrimPrefix(x, prefix), 10, 64)
		return n
	}
	return 0
}

// derivedEnvPrefix finds the prefix of the platform's own variables when the API leaves
// environmentPrefix null (instances made in the console, and through the API, captured
// 2026-10-28). The platform names them after the image ("MTN Ubuntu Server 24.04.3LTS" gives
// MTN_UBUNTU_SERVER_24_04_3LTS_IP), so that name is tried first: a user's own <P>_IP and <P>_HOST
// can't be taken for it. Otherwise it is the one P with both P_IP and P_HOST set; with none, or
// several, it is "".
func derivedEnvPrefix(vars map[string]string, image string) string {
	if p := envName(image); p != "" {
		if _, ok := vars[p+"_IP"]; ok {
			return p
		}
	}
	found := ""
	for k := range vars {
		p, ok := strings.CutSuffix(k, "_IP")
		if !ok || p == "" {
			continue
		}
		if _, host := vars[p+"_HOST"]; !host {
			continue
		}
		if found != "" {
			return ""
		}
		found = p
	}
	return found
}

// envName is a name as the platform writes it in variable names: upper case, with every other
// character than a letter or digit as "_".
func envName(s string) string {
	return strings.Trim(strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		}
		return '_'
	}, s), "_")
}

// IsPlatformEnvVar reports a variable the platform sets on every instance (its address, host and
// ports, all named "<EnvPrefix>_..."), as opposed to one a user added.
func (in *Instance) IsPlatformEnvVar(name string) bool {
	return in.EnvPrefix != "" && strings.HasPrefix(name, in.EnvPrefix+"_")
}

// InstanceFailed reports an instance the platform marks as broken.
func InstanceFailed(status string) bool {
	switch strings.ToLower(status) {
	case "failed", "error":
		return true
	}
	return false
}

// instanceSettled reports a status that a create or reconfigure ends in. Anything else
// ("provisioning", "pending", "resizing", "restarting", ...) is still in progress.
func instanceSettled(status string) bool {
	switch strings.ToLower(status) {
	case "running", "stopped", "suspended", "warning":
		return true
	}
	return InstanceFailed(status)
}

func volumesBody(vs []VolumeSpec) []map[string]any {
	out := make([]map[string]any, 0, len(vs))
	for _, v := range vs {
		id := v.ID
		if id == 0 {
			id = -1
		}
		ds := v.DatastoreID
		if ds == "" {
			ds = "auto"
		}
		m := map[string]any{"id": id, "rootVolume": v.Root, "size": v.Size, "storageType": 11, "datastoreId": ds}
		if v.Name != "" {
			m["name"] = v.Name
		}
		out = append(out, m)
	}
	return out
}

func kvList(m map[string]string) []map[string]string {
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	})
	out := make([]map[string]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]string{"name": k, "value": m[k]})
	}
	return out
}

// CreateInstance provisions an instance and returns its ID. It is still provisioning:
// WaitForInstance.
func (c *Client) CreateInstance(ctx context.Context, in InstanceInput) (int64, error) {
	if in.ProjectID == 0 || in.LayoutID == 0 || in.PlanID == 0 || len(in.NetworkIDs) == 0 {
		return 0, fmt.Errorf("an instance needs a project, an image, a plan and a network")
	}
	inst := map[string]any{
		"site":         map[string]any{"id": in.Placement.GroupID},
		"type":         in.ImageCode,
		"instanceType": map[string]any{"code": in.ImageCode},
		"layout":       map[string]any{"id": in.LayoutID},
		"plan":         map[string]any{"id": in.PlanID},
	}
	for k, v := range map[string]string{"name": in.Name, "description": in.Description, "hostName": in.HostName, "instanceContext": in.Environment} {
		if v != "" {
			inst[k] = v
		}
	}
	if len(in.Labels) > 0 {
		inst["labels"] = in.Labels
	}
	if in.NetworkDomainID != 0 {
		inst["networkDomain"] = map[string]any{"id": in.NetworkDomainID}
	}
	// Only at create: afterwards the platform offers only "extend" and "cancel".
	if in.ExpireDays > 0 {
		inst["expireDays"] = in.ExpireDays
	}
	if in.ShutdownDays > 0 {
		inst["shutdownDays"] = in.ShutdownDays
	}
	nics := make([]map[string]any, len(in.NetworkIDs))
	for i, id := range in.NetworkIDs {
		// No ipMode: the network's own DHCP setting applies (FastCreateLinuxInstance.sh).
		nics[i] = map[string]any{"primaryInterface": i == 0, "network": map[string]any{"id": "network-" + itoa(id)}}
	}
	sgs := make([]map[string]any, len(in.SecurityGroupIDs))
	for i, id := range in.SecurityGroupIDs {
		sgs[i] = map[string]any{"id": id}
	}
	layoutSize := in.LayoutSize
	if layoutSize == 0 {
		layoutSize = 1
	}
	body := map[string]any{
		"zoneId":   in.Placement.CloudID,
		"instance": inst,
		"config": map[string]any{
			"resourcePoolId": "pool-" + itoa(in.ProjectID),
			"createUser":     in.CreateUser,
			"noAgent":        in.NoAgent,
		},
		"volumes":           volumesBody(in.Volumes),
		"networkInterfaces": nics,
		"securityGroups":    sgs,
		"layoutSize":        layoutSize,
		"copies":            1,
	}
	if len(in.Tags) > 0 {
		body["tags"] = kvList(in.Tags)
	}
	if len(in.EnvVars) > 0 {
		body["evars"] = kvList(in.EnvVars)
	}
	var resp struct {
		Instance struct {
			ID int64 `json:"id"`
		} `json:"instance"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/instances", nil, body, &resp); err != nil {
		return 0, err
	}
	if resp.Instance.ID == 0 {
		return 0, fmt.Errorf("MTN Cloud API POST /api/instances: the answer has no instance ID")
	}
	return resp.Instance.ID, nil
}

// GetInstance reads an instance. A missing instance is an APIError with IsNotFound.
func (c *Client) GetInstance(ctx context.Context, id int64) (*Instance, error) {
	var resp struct {
		Instance wireInstance `json:"instance"`
	}
	if err := c.get(ctx, "/api/instances/"+itoa(id), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Instance.instance(), nil
}

// FindInstances returns the instances with exactly this name in a project.
func (c *Client) FindInstances(ctx context.Context, name string, projectID int64) ([]*Instance, error) {
	items, err := listAll[wireInstance](ctx, c, "/api/instances", "instances", url.Values{"name": {name}})
	if err != nil {
		return nil, err
	}
	var out []*Instance
	for _, w := range items {
		if in := w.instance(); in.Name == name && in.ProjectID == projectID {
			out = append(out, in)
		}
	}
	return out, nil
}

// ListInstances returns every instance the caller can see.
func (c *Client) ListInstances(ctx context.Context) ([]*Instance, error) {
	items, err := listAll[wireInstance](ctx, c, "/api/instances", "instances", nil)
	if err != nil {
		return nil, err
	}
	out := make([]*Instance, len(items))
	for i, w := range items {
		out[i] = w.instance()
	}
	return out, nil
}

// ListInstancesNamed returns the instances with exactly this name, in any project. The server
// filters by name (loosely), so only those are fetched.
func (c *Client) ListInstancesNamed(ctx context.Context, name string) ([]*Instance, error) {
	items, err := listAll[wireInstance](ctx, c, "/api/instances", "instances", url.Values{"name": {name}})
	if err != nil {
		return nil, err
	}
	var out []*Instance
	for _, w := range items {
		if in := w.instance(); in.Name == name {
			out = append(out, in)
		}
	}
	return out, nil
}

// UpdateInstance changes the name, description, environment, labels and tags in place. Tags are
// sent as the full list.
func (c *Client) UpdateInstance(ctx context.Context, id int64, u InstanceUpdate) error {
	inst := map[string]any{
		"description":     u.Description,
		"instanceContext": u.Environment,
		"labels":          append([]string{}, u.Labels...),
		"tags":            kvList(u.Tags),
	}
	if u.Name != "" {
		inst["name"] = u.Name
	}
	return c.do(ctx, http.MethodPut, "/api/instances/"+itoa(id), nil, map[string]any{"instance": inst}, nil)
}

// ResizeInstance reconfigures an instance: a new plan restarts the VM; new volumes and network
// cards are attached. Kept volumes and cards are sent with their IDs, so nothing is removed.
func (c *Client) ResizeInstance(ctx context.Context, id int64, r ResizeInput) error {
	nics := make([]map[string]any, len(r.Interfaces))
	for i, n := range r.Interfaces {
		nics[i] = map[string]any{"network": map[string]any{"id": "network-" + itoa(n.NetworkID)}}
		if n.ID != "" {
			nics[i]["id"] = n.ID
		}
	}
	body := map[string]any{
		"instance":              map[string]any{"id": id, "plan": map[string]any{"id": r.PlanID}},
		"volumes":               volumesBody(r.Volumes),
		"deleteOriginalVolumes": false,
	}
	// Only when a card is added: the platform adds every card sent without an ID (live QA
	// 2026-10-28), and the CLI's own resize sends none.
	if len(nics) > 0 {
		body["networkInterfaces"] = nics
	}
	return c.do(ctx, http.MethodPut, "/api/instances/"+itoa(id)+"/resize", nil, body, nil)
}

// InstanceSecurityGroups lists the security groups applied to an instance.
func (c *Client) InstanceSecurityGroups(ctx context.Context, id int64) ([]Option, error) {
	var resp struct {
		SecurityGroups []*wireRef `json:"securityGroups"`
	}
	if err := c.get(ctx, "/api/instances/"+itoa(id)+"/security-groups", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Option, 0, len(resp.SecurityGroups))
	for _, g := range resp.SecurityGroups {
		if g != nil {
			out = append(out, Option{ID: g.ID, Name: g.Name})
		}
	}
	return out, nil
}

// SetInstanceSecurityGroups replaces the security groups applied to an instance.
func (c *Client) SetInstanceSecurityGroups(ctx context.Context, id int64, groupIDs []int64) error {
	ids := append([]int64{}, groupIDs...)
	slices.Sort(ids)
	return c.do(ctx, http.MethodPost, "/api/instances/"+itoa(id)+"/security-groups", nil, map[string]any{"securityGroupIds": ids}, nil)
}

// DeleteInstance deletes an instance. One that is already gone counts as deleted.
func (c *Client) DeleteInstance(ctx context.Context, id int64) error {
	return ignoreNotFound(c.do(ctx, http.MethodDelete, "/api/instances/"+itoa(id), nil, nil, nil))
}

// Power actions, sent with PowerInstance.
const (
	PowerStart   = "start"
	PowerStop    = "stop"
	PowerSuspend = "suspend"
	PowerRestart = "restart"
)

// PowerInstance starts, stops, suspends or restarts an instance's server (PUT
// /api/instances/{id}/<action>). Start, stop and restart act on the server, not only the service
// (server=true); start also resumes a suspended server.
func (c *Client) PowerInstance(ctx context.Context, id int64, action string) error {
	var q url.Values
	if action != PowerSuspend {
		q = url.Values{"server": {"true"}}
	}
	return c.do(ctx, http.MethodPut, "/api/instances/"+itoa(id)+"/"+action, q, nil, nil)
}

// PowerOf is the power state an instance status shows: "running", "stopped" or "suspended", or ""
// while it is changing. "warning" (up, with a failing check) is running.
func PowerOf(status string) string {
	switch s := strings.ToLower(status); s {
	case "running", "stopped", "suspended":
		return s
	case "warning":
		return "running"
	}
	return ""
}

// WaitForInstanceStatus polls until the instance's power state (PowerOf) is power, or it has failed.
func (c *Client) WaitForInstanceStatus(ctx context.Context, id int64, power string) (*Instance, error) {
	return poll(ctx, c, func(ctx context.Context) (*Instance, error) { return c.GetInstance(ctx, id) },
		func(in *Instance, err error) (bool, error) {
			if err != nil {
				return false, err
			}
			return PowerOf(in.Status) == power || InstanceFailed(in.Status), nil
		})
}

// ErrPowerIgnored is returned when a power call was accepted but the instance hasn't changed state
// within Config.PowerGrace: MTN Cloud answers success to a suspend some VMs don't support.
var ErrPowerIgnored = errors.New("MTN Cloud accepted the call, but the instance hasn't changed state")

// WaitForPowerChange waits for an instance to leave power state from (for up to Config.PowerGrace, else
// ErrPowerIgnored), then to reach to.
func (c *Client) WaitForPowerChange(ctx context.Context, id int64, from, to string) (*Instance, error) {
	leave, cancel := context.WithTimeout(ctx, c.cfg.PowerGrace)
	in, err := poll(leave, c, func(ctx context.Context) (*Instance, error) { return c.GetInstance(ctx, id) },
		func(in *Instance, err error) (bool, error) {
			if err != nil {
				return false, err
			}
			return PowerOf(in.Status) != from, nil
		})
	cancel()
	if err != nil {
		// Ignored only when the instance was read and still shows the old state: a grace spent on
		// failed reads is the API's problem, not the VM's.
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil && in != nil && PowerOf(in.Status) == from {
			return in, ErrPowerIgnored
		}
		return in, err
	}
	return c.WaitForInstanceStatus(ctx, id, to)
}

// WaitForRestart waits for a restarted instance to leave "running" (for up to Config.PowerGrace; one that reboots
// too fast to be seen doing it is taken as restarted), then
// to run again. Straight after the call it still reads "running", so waiting for that alone would
// end before the reboot starts.
func (c *Client) WaitForRestart(ctx context.Context, id int64) (*Instance, error) {
	leave, cancel := context.WithTimeout(ctx, c.cfg.PowerGrace)
	_, err := poll(leave, c, func(ctx context.Context) (*Instance, error) { return c.GetInstance(ctx, id) },
		func(in *Instance, err error) (bool, error) {
			if err != nil {
				return false, err
			}
			return PowerOf(in.Status) != "running", nil
		})
	cancel()
	if err != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return c.WaitForInstanceStatus(ctx, id, "running")
}

// WaitForInstance polls an instance until its status settles (running, or failed / error) and
// returns it. A 404 just after the create is retried (rule 13). A failed instance is returned with
// no error: the caller reports the status and keeps the instance in state.
func (c *Client) WaitForInstance(ctx context.Context, id int64) (*Instance, error) {
	return poll(ctx, c, func(ctx context.Context) (*Instance, error) { return c.GetInstance(ctx, id) },
		func(in *Instance, err error) (bool, error) {
			switch {
			case IsNotFound(err):
				return false, nil
			case err != nil:
				return false, err
			}
			return instanceSettled(in.Status), nil
		})
}

// WaitForResize polls until a reconfigured instance has settled with the plan and at least the
// volumes and network cards asked for. Straight after the call the instance still reads as it
// was ("running", old plan), so a plain WaitForInstance returns too early (live QA 2026-10-28).
func (c *Client) WaitForResize(ctx context.Context, id, planID int64, volumes, interfaces int) (*Instance, error) {
	return poll(ctx, c, func(ctx context.Context) (*Instance, error) { return c.GetInstance(ctx, id) },
		func(in *Instance, err error) (bool, error) {
			switch {
			case IsNotFound(err):
				return false, nil
			case err != nil:
				return false, err
			}
			if InstanceFailed(in.Status) {
				return true, nil
			}
			return instanceSettled(in.Status) && in.PlanID == planID && len(in.Volumes) >= volumes && len(in.Interfaces) >= interfaces, nil
		})
}

// WaitForInstanceGone polls until a deleted instance reads as 404. Under a delayed-delete policy
// the platform keeps it as "pendingRemoval" instead: it is gone for the customer, so that counts.
func (c *Client) WaitForInstanceGone(ctx context.Context, id int64) error {
	_, err := poll(ctx, c, func(ctx context.Context) (*Instance, error) { return c.GetInstance(ctx, id) },
		func(in *Instance, err error) (bool, error) {
			if IsNotFound(err) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			return strings.EqualFold(in.Status, "pendingRemoval"), nil
		})
	return err
}

// Threshold is an instance's scaling: how many VMs (min to max, by increment) and the usage, in
// percent, that adds (above max) or removes (below min) one. A nil range is not watched.
type Threshold struct {
	AutoUp, AutoDown              bool
	MinCount, MaxCount, Increment int64
	CPU, Memory, Disk             *Range
}

// Range is a low and high usage mark, in percent.
type Range struct{ Min, Max float64 }

// Set reports whether any scaling is configured; a new instance reads as all off (captured 2026-10-27).
func (t *Threshold) Set() bool {
	return t.AutoUp || t.AutoDown || t.CPU != nil || t.Memory != nil || t.Disk != nil
}

type wireThreshold struct {
	AutoUp         bool     `json:"autoUp"`
	AutoDown       bool     `json:"autoDown"`
	MinCount       *int64   `json:"minCount"`
	MaxCount       *int64   `json:"maxCount"`
	ScaleIncrement *int64   `json:"scaleIncrement"`
	CPUEnabled     bool     `json:"cpuEnabled"`
	MinCPU         *float64 `json:"minCpu"`
	MaxCPU         *float64 `json:"maxCpu"`
	MemoryEnabled  bool     `json:"memoryEnabled"`
	MinMemory      *float64 `json:"minMemory"`
	MaxMemory      *float64 `json:"maxMemory"`
	DiskEnabled    bool     `json:"diskEnabled"`
	MinDisk        *float64 `json:"minDisk"`
	MaxDisk        *float64 `json:"maxDisk"`
}

func rangeOf(enabled bool, lo, hi *float64) *Range {
	if !enabled {
		return nil
	}
	r := &Range{}
	if lo != nil {
		r.Min = *lo
	}
	if hi != nil {
		r.Max = *hi
	}
	return r
}

func orZero(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// InstanceThreshold reads an instance's scaling. The answer's key is "instanceThreshold" (captured
// 2026-10-27), not the "instance.threshold" of the API reference.
func (c *Client) InstanceThreshold(ctx context.Context, id int64) (*Threshold, error) {
	var resp struct {
		InstanceThreshold *wireThreshold `json:"instanceThreshold"`
	}
	if err := c.get(ctx, "/api/instances/"+itoa(id)+"/threshold", nil, &resp); err != nil {
		return nil, err
	}
	w := resp.InstanceThreshold
	if w == nil {
		return &Threshold{MinCount: 1, Increment: 1}, nil
	}
	return &Threshold{
		AutoUp: w.AutoUp, AutoDown: w.AutoDown,
		MinCount: orZero(w.MinCount), MaxCount: orZero(w.MaxCount), Increment: orZero(w.ScaleIncrement),
		CPU:    rangeOf(w.CPUEnabled, w.MinCPU, w.MaxCPU),
		Memory: rangeOf(w.MemoryEnabled, w.MinMemory, w.MaxMemory),
		Disk:   rangeOf(w.DiskEnabled, w.MinDisk, w.MaxDisk),
	}, nil
}

// SetInstanceThreshold replaces an instance's scaling. A zero Threshold turns scaling off.
func (c *Client) SetInstanceThreshold(ctx context.Context, id int64, t Threshold) error {
	body := map[string]any{
		"autoUp": t.AutoUp, "autoDown": t.AutoDown,
		"minCount": max(t.MinCount, 1), "scaleIncrement": max(t.Increment, 1),
	}
	if t.MaxCount > 0 {
		body["maxCount"] = t.MaxCount
	}
	for name, r := range map[string]*Range{"Cpu": t.CPU, "Memory": t.Memory, "Disk": t.Disk} {
		key := strings.ToLower(name[:1]) + name[1:]
		body[key+"Enabled"] = r != nil
		if r != nil {
			body["min"+name], body["max"+name] = r.Min, r.Max
		}
	}
	return c.do(ctx, http.MethodPut, "/api/instances/"+itoa(id)+"/threshold", nil, map[string]any{"instanceThreshold": body}, nil)
}

// FloatingIPPools lists the floating IP pools a VM can take an address from, for example
// {pool-43, "public-network-02"}. The list needs the VM's container: without one the option source
// answers 500 (captured 2026-10-27). Pools have their own IDs, not the external networks' IDs.
func (c *Client) FloatingIPPools(ctx context.Context, containerID int64) ([]Option, error) {
	var resp struct {
		Data []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"data"`
	}
	if err := c.get(ctx, "/api/options/openStack/openstackFloatingIpOptions", url.Values{"containerId": {itoa(containerID)}}, &resp); err != nil {
		return nil, err
	}
	var out []Option
	for _, d := range resp.Data {
		// The list also holds headings ("Pools", "Available Ips", value "") and free addresses ("ip-<id>").
		if v, ok := d.Value.(string); ok && strings.HasPrefix(v, "pool-") {
			if id := idFrom(v, "pool-"); id != 0 {
				out = append(out, Option{ID: id, Name: d.Name})
			}
		}
	}
	return out, nil
}

// FloatingIPPoolByName finds a VM's floating IP pool by name (case ignored), listing the valid ones
// when there is none.
func (c *Client) FloatingIPPoolByName(ctx context.Context, containerID int64, name string) (Option, error) {
	pools, err := c.FloatingIPPools(ctx, containerID)
	if err != nil {
		return Option{}, err
	}
	names := make([]string, len(pools))
	for i, p := range pools {
		if strings.EqualFold(p.Name, name) {
			return p, nil
		}
		names[i] = p.Name
	}
	return Option{}, fmt.Errorf("no floating IP pool named %q. Available pools: %s. %s", name, strings.Join(names, ", "), SyncHint)
}

// AttachFloatingIP gives a VM an address from a floating IP pool. It works only when the VM's
// network is on an MTN Router with an external network.
func (c *Client) AttachFloatingIP(ctx context.Context, containerID, poolID int64) error {
	body := map[string]any{"config": map[string]any{"osExternalNetworkId": "pool-" + itoa(poolID)}}
	return c.do(ctx, http.MethodPut, "/api/containers/"+itoa(containerID)+"/attach-floating-ip", nil, body, nil)
}

// DetachFloatingIP takes a VM's floating IP away.
func (c *Client) DetachFloatingIP(ctx context.Context, containerID int64) error {
	return c.do(ctx, http.MethodPut, "/api/containers/"+itoa(containerID)+"/detach-floating-ip", nil, map[string]any{}, nil)
}

// WaitForPublicIP polls until the instance shows a floating IP (want) or none (!want), so an attach
// or detach is known to have worked before the apply ends.
func (c *Client) WaitForPublicIP(ctx context.Context, id int64, want bool) (*Instance, error) {
	return poll(ctx, c, func(ctx context.Context) (*Instance, error) { return c.GetInstance(ctx, id) },
		func(in *Instance, err error) (bool, error) {
			if err != nil {
				return false, err
			}
			return (in.PublicIP != "") == want, nil
		})
}
