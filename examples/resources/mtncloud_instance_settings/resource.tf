resource "mtncloud_wireguard_vpn" "vpn" {
  name             = "wg-1"
  project          = "web-prod"
  plan             = "G1S2"
  root_volume_size = 20
  networks         = ["app-net"]
  floating_ip_pool = "public-network-01"
}

# The order's inputs can't change; its VM's size can, here.
resource "mtncloud_instance_settings" "vpn" {
  instance_id = mtncloud_wireguard_vpn.vpn.instance_ids[0]
  plan        = "G2S4"
}
