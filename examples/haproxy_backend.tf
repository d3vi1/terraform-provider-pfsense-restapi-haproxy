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

# Explicit apply after backend changes. There is intentionally no hidden
# auto-apply in pfsense_haproxy_backend or other durable HAProxy resources.
#
# UAT note: this assumes GET /services/haproxy/backends returns objects with
# stable names and transient pfSense IDs, and that POST/PATCH/DELETE backend
# writes only mark HAProxy configuration pending until pfsense_haproxy_apply runs.
resource "pfsense_haproxy_apply" "app_backend" {
  depends_on = [pfsense_haproxy_backend.app]

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
  }

  timeout       = "2m"
  poll_interval = "2s"
}
