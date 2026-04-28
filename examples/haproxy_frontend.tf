resource "pfsense_haproxy_frontend" "app" {
  name                = "app_uat_http"
  type                = "http"
  descr               = "Application UAT HTTP frontend"
  status              = "active"
  backend_serverpool  = pfsense_haproxy_backend.app.name
  max_connections     = 2000
  socket_stats        = true
  dontlognull         = true
  dontlog_normal      = false
  log_separate_errors = true
  log_detailed        = false
  client_timeout      = 30000
  forwardfor          = true
  httpclose           = "http-server-close"
}

resource "pfsense_haproxy_frontend_address" "app_https" {
  frontend_name = pfsense_haproxy_frontend.app.name
  extaddr       = "any_ipv4"
  extaddr_port  = 443
  extaddr_ssl   = true
}

resource "pfsense_haproxy_frontend_certificate" "app_https" {
  frontend_name   = pfsense_haproxy_frontend.app.name
  ssl_certificate = "existing_uat_certificate_ref"
}

# Explicit apply after frontend changes. There is intentionally no hidden
# auto-apply in pfsense_haproxy_frontend, pfsense_haproxy_frontend_address,
# pfsense_haproxy_frontend_certificate, or other durable HAProxy resources.
#
# UAT note: this assumes GET /services/haproxy/frontends returns objects with
# stable names and transient pfSense IDs, and that POST/PATCH/DELETE frontend
# writes only mark HAProxy configuration pending until pfsense_haproxy_apply runs.
# Frontend address writes additionally assume child lookups by parent_id,
# extaddr, extaddr_custom, and extaddr_port. Frontend certificate writes assume
# child lookups by parent_id and ssl_certificate. Replace
# existing_uat_certificate_ref with an existing pfSense certificate reference;
# do not place PEM certificate or private key material in Terraform.
resource "pfsense_haproxy_apply" "app_frontend" {
  depends_on = [
    pfsense_haproxy_backend.app,
    pfsense_haproxy_frontend.app,
    pfsense_haproxy_frontend_address.app_https,
    pfsense_haproxy_frontend_certificate.app_https,
  ]

  triggers = {
    frontend = sha1(jsonencode({
      name                = pfsense_haproxy_frontend.app.name
      type                = pfsense_haproxy_frontend.app.type
      descr               = pfsense_haproxy_frontend.app.descr
      status              = pfsense_haproxy_frontend.app.status
      backend_serverpool  = pfsense_haproxy_frontend.app.backend_serverpool
      max_connections     = pfsense_haproxy_frontend.app.max_connections
      socket_stats        = pfsense_haproxy_frontend.app.socket_stats
      dontlognull         = pfsense_haproxy_frontend.app.dontlognull
      dontlog_normal      = pfsense_haproxy_frontend.app.dontlog_normal
      log_separate_errors = pfsense_haproxy_frontend.app.log_separate_errors
      log_detailed        = pfsense_haproxy_frontend.app.log_detailed
      client_timeout      = pfsense_haproxy_frontend.app.client_timeout
      forwardfor          = pfsense_haproxy_frontend.app.forwardfor
      httpclose           = pfsense_haproxy_frontend.app.httpclose
    }))
    frontend_addresses = sha1(jsonencode({
      https = {
        extaddr        = pfsense_haproxy_frontend_address.app_https.extaddr
        extaddr_custom = pfsense_haproxy_frontend_address.app_https.extaddr_custom
        extaddr_port   = pfsense_haproxy_frontend_address.app_https.extaddr_port
        extaddr_ssl    = pfsense_haproxy_frontend_address.app_https.extaddr_ssl
      }
    }))
    frontend_certificates = sha1(jsonencode({
      https = {
        ssl_certificate = pfsense_haproxy_frontend_certificate.app_https.ssl_certificate
      }
    }))
  }

  timeout       = "2m"
  poll_interval = "2s"
}
