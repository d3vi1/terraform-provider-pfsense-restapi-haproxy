resource "pfsense_haproxy_backend" "app" {
  name                = "app_uat"
  balance             = "roundrobin"
  connection_timeout  = 30000
  server_timeout      = 30000
  check_type          = "HTTP"
  checkinter          = 2000
  log_health_checks   = true
  httpcheck_method    = "GET"
  monitor_uri         = "/health"
  monitor_httpversion = "HTTP/1.1"
}

resource "pfsense_haproxy_backend_server" "app01" {
  backend_name = pfsense_haproxy_backend.app.name
  name         = "app01"
  address      = "10.30.10.21"
  port         = 8080
  status       = "active"
  weight       = 10
}

# Explicit apply after backend changes. There is intentionally no hidden
# auto-apply in pfsense_haproxy_backend, pfsense_haproxy_backend_server, or
# other durable HAProxy resources.
#
# UAT note: this assumes GET /services/haproxy/backends returns objects with
# stable names and transient pfSense IDs, and that POST/PATCH/DELETE backend
# writes only mark HAProxy configuration pending until pfsense_haproxy_apply runs.
# Backend server writes additionally assume child lookups by parent_id and name.
resource "pfsense_haproxy_apply" "app_backend" {
  depends_on = [
    pfsense_haproxy_backend.app,
    pfsense_haproxy_backend_server.app01,
  ]

  triggers = {
    backend = sha1(jsonencode({
      name                = pfsense_haproxy_backend.app.name
      balance             = pfsense_haproxy_backend.app.balance
      connection_timeout  = pfsense_haproxy_backend.app.connection_timeout
      server_timeout      = pfsense_haproxy_backend.app.server_timeout
      check_type          = pfsense_haproxy_backend.app.check_type
      checkinter          = pfsense_haproxy_backend.app.checkinter
      log_health_checks   = pfsense_haproxy_backend.app.log_health_checks
      httpcheck_method    = pfsense_haproxy_backend.app.httpcheck_method
      monitor_uri         = pfsense_haproxy_backend.app.monitor_uri
      monitor_httpversion = pfsense_haproxy_backend.app.monitor_httpversion
    }))
    backend_servers = sha1(jsonencode({
      app01 = {
        name            = pfsense_haproxy_backend_server.app01.name
        address         = pfsense_haproxy_backend_server.app01.address
        port            = pfsense_haproxy_backend_server.app01.port
        status          = pfsense_haproxy_backend_server.app01.status
        weight          = pfsense_haproxy_backend_server.app01.weight
        ssl             = pfsense_haproxy_backend_server.app01.ssl
        sslserververify = pfsense_haproxy_backend_server.app01.sslserververify
      }
    }))
  }

  timeout       = "2m"
  poll_interval = "2s"
}
