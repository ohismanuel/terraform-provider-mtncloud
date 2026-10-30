"""Generate the features lists: templates/guides/features.md.tmpl (the registry guide: what a customer
login can use, nothing else) and dev-docs/features.md (the internal tracker: every type, master-only
or not, verification status, and what is coming next).

Usage: go run ./tools/listtypes | python3 scripts/gen-features.py
(run by `make docs`, before tfplugindocs renders the template into docs/guides/features.md)

The category map below is the only hand-written part. The script fails if a registered type is in
no category, or a category names a type that isn't registered, so the guide can't drift from the
provider. Planned (not yet built) types live in PLANNED and are shown separately.
"""
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "templates" / "guides" / "features.md.tmpl"  # the registry guide, customer types only
INTERNAL = ROOT / "dev-docs" / "features.md"  # the internal tracker

# (title, intro, [type names without the mtncloud_ prefix])
RESOURCES = [
    ("Tenancy, users and access",
     "Users, roles, groups, identity sources and environments.",
     ["user", "user_role", "user_group", "active_directory_identity_source",
      "saml_identity_source", "contact", "credential", "group", "environment"]),
    ("Wiki",
     "Wiki pages for runbooks and notes.",
     ["wiki_page"]),
    ("Forms and inputs",
     "Forms, option lists and option types: the inputs used by workflows and forms.",
     ["form"]),
    ("Inputs: option lists and option types",
     "The inputs used by forms and workflows.",
     ["api_option_list", "manual_option_list", "rest_option_list", "checkbox_option_type",
      "hidden_option_type", "number_option_type", "password_option_type", "radio_list_option_type",
      "select_list_option_type", "text_option_type", "textarea_option_type",
      "typeahead_option_type"]),
    ("Automation: tasks",
     "Building blocks for workflows.",
     ["ai_task", "ansible_playbook_task", "ansible_tower_task", "chef_bootstrap_task", "conditional_workflow_task",
      "dynamic_wallet_collection_task", "email_task", "http_task", "powerscale_storage_collection_task",
      "puppet_agent_install_task", "reverse_text_task",
      "library_script_task", "library_template_task",
      "nested_workflow_task", "powershell_script_task", "restart_task",
      "shell_script_task", "vro_task", "write_attributes_task"]),
    ("Automation: workflows, jobs and schedules",
     "Workflows that chain tasks, and jobs that run them on a schedule.",
     ["operational_workflow", "provisioning_workflow", "task_job", "workflow_job",
      "execute_schedule"]),
    ("Projects",
     "Projects (resource pools), ordered from MTN's catalog.",
     ["project"]),
    ("Catalog: databases and web stacks",
     "Ordered from MTN's catalog: each deploys VMs and installs the service. Inputs are fixed once ordered; delete them in the console.",
     ["mysql", "postgresql", "mariadb", "mongodb", "lamp_stack", "lemp_stack", "wordpress", "apache_web_server", "nginx_web_server"]),
    ("Catalog: managed services",
     "Managed services ordered from MTN's catalog. They show under Apps in the console.",
     ["managed_database", "managed_mq", "managed_streaming"]),
    ("Catalog: self-managed Kubernetes",
     "A Kubernetes cluster MTN Cloud builds and you run. It shows under Apps in the console.",
     ["self_managed_kubernetes"]),
    ("Catalog: network and security appliances",
     "VPNs, firewalls, bastion hosts and a SIEM server ordered from MTN's catalog (BYOL: bring your own license).",
     ["vpn_service", "wireguard_vpn", "bastion_host", "siem", "fortinet_firewall", "fortiweb", "fortiadc", "fortimanager", "fortianalyzer"]),
    ("Catalog: file shares and certificates",
     "NFS and SMB shares (created, updated and deleted by orders) and Let's Encrypt certificates for instances.",
     ["nfs_share", "smb_share", "letsencrypt_certificate"]),
    ("Compute",
     "Instances (virtual machines from MTN's OS images), day-2 settings of instances an order made, and Kubernetes "
     "and Docker clusters from MTN's layouts.",
     ["instance", "instance_settings", "cluster"]),
    ("Networking",
     "MTN Networks and MTN Routers, security groups (instance firewalls) and their rules, IP pools "
     "(static IP ranges for a network) and DNS domains (instance hostnames and domain join).",
     ["network", "router", "security_group", "security_group_rule", "ipv4_ip_pool", "network_domain"]),
    ("Load balancers",
     "Load balancers on an MTN Network, and their listeners, pools, members and health monitors, each its own resource.",
     ["load_balancer", "load_balancer_listener", "load_balancer_pool", "load_balancer_member", "load_balancer_health_monitor",
      "load_balancer_instance_member", "load_balancer_l7_policy", "load_balancer_floating_ip"]),
    ("MOS (MTN Object Storage)",
     "S3-compatible buckets, listed in the console, with their access, CORS, lifecycle, access logging and "
     "object lock settings, and the objects in them (with retention).",
     ["mos_bucket", "mos_bucket_acl", "mos_bucket_cors", "mos_bucket_lifecycle", "mos_bucket_logging",
      "mos_bucket_object_lock", "mos_object", "mos_object_acl"]),
    ("Archives",
     "File stores (Tools > Archives) whose files are kept in a MOS bucket, their files, and download links to them.",
     ["archive_bucket", "archive_file", "archive_file_link"]),
    ("Secrets and keys",
     "Cypher secrets, SSH key pairs and SSL certificates you upload.",
     ["cypher_key", "cypher_password", "cypher_secret", "cypher_tfvars", "cypher_uuid", "key_pair", "ssl_certificate"]),
    ("Scaling",
     "Autoscaling thresholds.",
     ["scale_threshold"]),
    ("Deprecated names",
     "Old names kept for now; each page says what replaces it and how to move without replacing anything.",
     ["managed_kubernetes"]),
]

DATA_SOURCES = [
    ("Pick by name: what can I deploy?",
     ["projects", "project", "images", "plans", "datastores", "datastore", "security_groups",
      "security_group", "external_networks", "registries", "cluster_layouts",
      "catalog_items"]),
    ("Tenancy, users and access",
     ["user", "users", "user_role", "user_roles", "user_group", "user_groups", "identity_source",
      "identity_sources",
      "permission_set", "contact", "credential", "group", "groups", "environment", "environments"]),
    ("Networking",
     ["network", "networks", "network_group", "network_subnet", "network_subnets", "domain", "domains",
      "routers", "router", "floating_ips", "ssl_certificate", "ssl_certificates"]),
    ("Load balancers",
     ["load_balancers", "load_balancer", "load_balancer_listeners", "load_balancer_listener",
      "load_balancer_pools", "load_balancer_pool", "load_balancer_members", "load_balancer_member",
      "load_balancer_l7_policies", "load_balancer_l7_policy",
      "load_balancer_health_monitors", "load_balancer_health_monitor", "load_balancer_floating_ip_pools"]),
    ("Compute",
     ["instance", "instances", "app", "apps", "cluster", "clusters"]),
    ("Images",
     ["image", "virtual_image",
      "virtual_images"]),
    ("MOS (MTN Object Storage)",
     ["mos_credentials", "mos_buckets", "mos_bucket", "mos_bucket_acl", "mos_bucket_cors", "mos_bucket_lifecycle",
      "mos_bucket_logging", "mos_bucket_object_lock", "mos_objects", "mos_object", "mos_object_acl"]),
    ("Archives",
     ["archive_buckets", "archive_bucket", "archive_files"]),
    ("Storage",
     ["storage_bucket", "storage_buckets", "storage_volume", "storage_volumes", "storage_volume_type"]),
    ("Catalog, forms and inputs",
     ["catalog_item_type", "catalog_item", "catalog_order", "catalog_orders", "form", "forms", "option_list",
      "option_type", "vdi_pool"]),
    ("Automation and integrations",
     ["task", "tasks", "workflow", "workflows", "job", "jobs", "execute_schedule", "power_schedule", "integration", "integrations",
      "git_integration", "ansible_tower_inventory", "ansible_tower_job_template", "chef_server",
      "servicenow_workflow", "vro_workflow"]),
    ("Budgets",
     ["budget"]),
    ("Secrets and keys",
     ["key_pair", "key_pairs", "credentials", "cypher", "cyphers"]),
]

# Who can use each type with a customer (sub-tenant) login. Evidence: dev-docs/customer-permissions.md
# (read-only probe as a Customer Admin User). Master-only types also say so in their description.
MASTER_ONLY = {"resource": set(), "data": set()}  # removed (D-024): customers can't use them
# Built for customers on the endpoints the console's own wizard uses (D-016).
FOR_CUSTOMERS = {"resource": {"project", "instance", "network", "router", "security_group", "security_group_rule"},
                 "data": {"projects", "project", "images", "plans", "datastores", "security_groups", "security_group", "router",
                          "external_networks", "catalog_items", "routers", "instances", "instance"}}
# Need an integration first; the probed tenant had none.
NOT_CHECKED = {"data": {"ansible_tower_inventory", "ansible_tower_job_template", "chef_server",
                        "servicenow_workflow", "vro_workflow"}}
NO_API = {"data": {"permission_set"}}

LABELS = {
    "for": "Yes, built for customers",
    "read": "Yes (read verified)",
    "master": "No: master tenant only",
    "unchecked": "Not checked yet",
    "noapi": "Yes (no API call)",
}


def access(kind, name):
    for key, table in (("master", MASTER_ONLY), ("for", FOR_CUSTOMERS), ("unchecked", NOT_CHECKED), ("noapi", NO_API)):
        if name in table.get(kind, ()):
            return key
    return "read"


# New MTN-specific types: designed, not yet built. Kept here so the guide shows the roadmap.
# (type, status, what it does). Status: Next, Planned or On hold.
PLANNED = [
    ("Resources", [
        ("mtncloud_app", "Planned", "Apps from blueprints: several instances or containers deployed together."),
        ("mtncloud_datastore", "Planned", "Datastores: the storage that volumes are created on."),
    ]),
    ("Data sources", []),
]


def main():
    registered = {"resource": set(), "data": set()}
    described_master = {"resource": set(), "data": set()}  # description starts "Master tenant only"
    for line in sys.stdin:
        kind, name, flag = line.split()
        name = name.removeprefix("mtncloud_")
        registered[kind].add(name)
        if flag == "master":
            described_master[kind].add(name)

    errors = []
    for kind, groups in (("resource", [g[2] for g in RESOURCES]), ("data", [g[1] for g in DATA_SOURCES])):
        listed = [n for g in groups for n in g]
        dupes = {n for n in listed if listed.count(n) > 1}
        if dupes:
            errors.append(f"{kind}: listed twice: {sorted(dupes)}")
        missing = registered[kind] - set(listed)
        unknown = set(listed) - registered[kind]
        if missing:
            errors.append(f"{kind}: registered but in no category (add it to scripts/gen-features.py): {sorted(missing)}")
        if unknown:
            errors.append(f"{kind}: in a category but not registered: {sorted(unknown)}")
    for table in (MASTER_ONLY, FOR_CUSTOMERS, NOT_CHECKED, NO_API):
        for kind, names in table.items():
            if names - registered[kind]:
                errors.append(f"{kind}: access map names unregistered types: {sorted(names - registered[kind])}")
    for kind in registered:
        # The guide and each type's own docs must agree on master-only.
        if MASTER_ONLY[kind] - described_master[kind]:
            errors.append(f"{kind}: master-only here but the description doesn't start with "
                          f"'Master tenant only': {sorted(MASTER_ONLY[kind] - described_master[kind])}")
        if described_master[kind] - MASTER_ONLY[kind]:
            errors.append(f"{kind}: description says master-only but MASTER_ONLY doesn't: "
                          f"{sorted(described_master[kind] - MASTER_ONLY[kind])}")
    if not registered["resource"] or not registered["data"]:
        errors.append("no input: pipe `go run ./tools/listtypes` into this script")
    if errors:
        sys.exit("gen-features: " + "\n".join(errors))

    write(OUT, public(registered))
    write(INTERNAL, internal(registered))


def write(path, lines):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(lines), encoding="utf-8", newline="\n")
    print(f"gen-features: wrote {path.relative_to(ROOT)}")


def for_customers(kind, name):
    return access(kind, name) in ("for", "read", "noapi")


def public(registered):
    """The registry guide: only what a customer login can use. No master tenant, no verification
    status, no roadmap: those live in the internal tracker."""
    res = [(t, i, [n for n in names if for_customers("resource", n)]) for t, i, names in RESOURCES]
    data = [(t, [n for n in names if for_customers("data", n)]) for t, names in DATA_SOURCES]
    nr = sum(len(n) for _, _, n in res)
    nd = sum(len(n) for _, n in data)
    out = [
        "---",
        'page_title: "Features"',
        "description: |-",
        "  Everything you can manage on MTN Cloud with this provider.",
        "---",
        "",
        "# Features",
        "",
        "<!-- Generated by scripts/gen-features.py from the provider's registered types. Do not edit. -->",
        "",
        f"Everything you can manage on MTN Cloud with your account: {nr} resources and {nd} data sources. "
        "The cloud (`MTNNG_CLOUD_AZ_1`) is found automatically, so you never configure it.",
        "",
        "MTN Cloud's own names are used throughout: **MTN Network**, **MTN Router** and **MOS** "
        "(MTN Object Storage).",
        "",
        "## Resources",
        "",
    ]
    for title, intro, names in res:
        if names:
            out += [f"### {title}", "", intro, ""] + links("resources", names)
    out += ["## Data sources", ""]
    for title, names in data:
        if names:
            out += [f"### {title}", ""] + links("data-sources", names)
    return out


def links(folder, names):
    return [f"- [`mtncloud_{n}`](../{folder}/{n}.md)" for n in sorted(names)] + [""]


def internal(registered):
    """The internal tracker (dev-docs/): every type, who can use it, how far it is verified, and
    what is coming next. Never published."""
    nr, nd = len(registered["resource"]), len(registered["data"])
    mr, md = len(MASTER_ONLY["resource"]), len(MASTER_ONLY["data"])
    ur, ud = len(NOT_CHECKED.get("resource", ())), len(NOT_CHECKED.get("data", ()))
    out = [
        "# Features: internal tracker",
        "",
        "<!-- Generated by scripts/gen-features.py (make docs). Do not edit. Internal: never published; "
        "the registry shows docs/guides/features.md, which lists customer types only. -->",
        "",
        "The cloud (`MTNNG_CLOUD_AZ_1`) is found "
        "automatically: you never configure it.",
        "",
        "| | Resources | Data sources |",
        "|---|---|---|",
        f"| Available today | {nr} | {nd} |",
        f"| Usable with a customer (sub-tenant) login | {nr - mr - ur} | {nd - md - ud} |",
        f"| Master tenant only | {mr} | {md} |",
        f"| Not checked yet with a customer login | {ur} | {ud} |",
        f"| Coming next | {len(PLANNED[0][1])} | {len(PLANNED[1][1])} |",
        "",
        "## Who can use what",
        "",
        "MTN Cloud customers log in to a **sub-tenant**. The platform hides some things from them "
        "(clouds, hosts, prices, policies, other tenants), so the **Customer login** column says, "
        "for each type:",
        "",
        f"- **{LABELS['for']}**: made for customers. It uses the same calls as the console's own "
        "screens, checked with a customer login.",
        f"- **{LABELS['read']}**: a customer login can read what it manages. Creating and changing "
        "are confirmed type by type as acceptance tests are added.",
        f"- **{LABELS['master']}**: customer logins are refused (HTTP 403). Only the MTN Cloud master "
        "tenant can use it, and its description says so.",
        f"- **{LABELS['unchecked']}**: needs an integration to exist first; not yet tested with a "
        "customer login.",
        f"- **{LABELS['noapi']}**: builds a value locally and never calls MTN Cloud.",
        "",
        "Customer-facing names are used throughout: **MTN Network**, **MTN Router**, **MOS** "
        "(MTN Object Storage).",
        "",
        "## Resources",
        "",
    ]

    def table(kind, folder, names):
        rows = ["| Type | Customer login |", "|---|---|"]
        rows += [f"| [`mtncloud_{n}`](../docs/{folder}/{n}.md) | {LABELS[access(kind, n)]} |" for n in sorted(names)]
        return rows + [""]

    for title, intro, names in RESOURCES:
        out += [f"### {title}", "", intro, ""] + table("resource", "resources", names)
    out += ["## Data sources", ""]
    for title, names in DATA_SOURCES:
        out += [f"### {title}", ""] + table("data", "data-sources", names)
    out += [
        "## Master tenant only",
        "",
        "These need the MTN Cloud master tenant. A customer login gets HTTP 403.",
        "",
        "- **Resources:** " + ", ".join(f"`mtncloud_{n}`" for n in sorted(MASTER_ONLY["resource"])),
        "- **Data sources:** " + ", ".join(f"`mtncloud_{n}`" for n in sorted(MASTER_ONLY["data"])),
        "",
        "Customers use `mtncloud_projects` instead of `mtncloud_resource_pool`, `mtncloud_plans` "
        "instead of `mtncloud_plan`, and `mtncloud_datastores` instead of `mtncloud_cloud_datastore`. "
        "They don't need `mtncloud_cloud`: the provider finds the cloud.",
        "",
        "## Coming next",
        "",
        "Designed, **not available yet**, and all meant for customers.",
        "",
    ]
    for title, items in PLANNED:
        out += [f"### {title}", "", "| Type | Status | What it does |", "|---|---|---|"]
        out += [f"| `{n}` | {st} | {d} |" for n, st, d in items]
        out += [""]
    return out


main()
