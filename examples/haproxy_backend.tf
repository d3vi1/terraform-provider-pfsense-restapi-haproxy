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

resource "pfsense_haproxy_backend_acl" "app_host" {
  backend_name = pfsense_haproxy_backend.app.name
  name         = "app_host"
  expression   = "host_matches"
  value        = "app-uat.example.com"
  position     = 0
}

resource "pfsense_haproxy_backend_action" "app_route" {
  backend_name = pfsense_haproxy_backend.app.name
  key          = "route_app01"
  action       = "use_server"
  acl          = pfsense_haproxy_backend_acl.app_host.name
  server       = pfsense_haproxy_backend_server.app01.name
  position     = 0
}

resource "pfsense_haproxy_backend_action" "app_response_header" {
  backend_name = pfsense_haproxy_backend.app.name
  key          = "set_response_env"
  action       = "http-response_set-header"
  acl          = pfsense_haproxy_backend_acl.app_host.name
  name         = "X-Environment"
  fmt          = "uat"
  position     = 1
}

# Explicit apply after backend changes. There is intentionally no hidden
# auto-apply in pfsense_haproxy_backend, pfsense_haproxy_backend_server,
# pfsense_haproxy_backend_acl, pfsense_haproxy_backend_action, or other durable
# HAProxy resources.
#
# UAT note: this assumes GET /services/haproxy/backends returns objects with
# stable names and transient pfSense IDs, and that POST/PATCH/DELETE backend
# writes only mark HAProxy configuration pending until pfsense_haproxy_apply runs.
# Backend child writes additionally assume server/ACL child lookups by parent_id
# and natural name, action payload matching by unique normalized payload, and
# ordered child reads for position/placement semantics.
resource "pfsense_haproxy_apply" "app_backend" {
  depends_on = [
    pfsense_haproxy_backend.app,
    pfsense_haproxy_backend_server.app01,
    pfsense_haproxy_backend_acl.app_host,
    pfsense_haproxy_backend_action.app_route,
    pfsense_haproxy_backend_action.app_response_header,
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
    backend_acls = sha1(jsonencode({
      host = {
        name       = pfsense_haproxy_backend_acl.app_host.name
        expression = pfsense_haproxy_backend_acl.app_host.expression
        value      = pfsense_haproxy_backend_acl.app_host.value
        position   = pfsense_haproxy_backend_acl.app_host.position
      }
    }))
    backend_actions = sha1(jsonencode({
      route = {
        key      = pfsense_haproxy_backend_action.app_route.key
        action   = pfsense_haproxy_backend_action.app_route.action
        acl      = pfsense_haproxy_backend_action.app_route.acl
        server   = pfsense_haproxy_backend_action.app_route.server
        position = pfsense_haproxy_backend_action.app_route.position
      }
      header = {
        key      = pfsense_haproxy_backend_action.app_response_header.key
        action   = pfsense_haproxy_backend_action.app_response_header.action
        acl      = pfsense_haproxy_backend_action.app_response_header.acl
        name     = pfsense_haproxy_backend_action.app_response_header.name
        fmt      = pfsense_haproxy_backend_action.app_response_header.fmt
        position = pfsense_haproxy_backend_action.app_response_header.position
      }
    }))
  }

  timeout       = "2m"
  poll_interval = "2s"
}
