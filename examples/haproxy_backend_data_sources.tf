data "pfsense_haproxy_backend" "existing" {
  name = "app_uat"
}

data "pfsense_haproxy_backend_server" "existing_app01" {
  backend_name = data.pfsense_haproxy_backend.existing.name
  name         = "app01"
}

output "existing_backend_server" {
  value = {
    backend = data.pfsense_haproxy_backend.existing.name
    name    = data.pfsense_haproxy_backend_server.existing_app01.name
    address = data.pfsense_haproxy_backend_server.existing_app01.address
    port    = data.pfsense_haproxy_backend_server.existing_app01.port
    status  = data.pfsense_haproxy_backend_server.existing_app01.status
  }
}
