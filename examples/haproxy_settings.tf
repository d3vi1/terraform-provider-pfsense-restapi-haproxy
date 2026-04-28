data "pfsense_haproxy_settings" "current" {}

data "pfsense_haproxy_apply" "status" {}

# Import before managing:
# terraform import pfsense_haproxy_settings.global settings
#
# UAT note: this example uses scalar fields from the documented
# pfSense-pkg-RESTAPI HAProxySettings model. Validate against the approved UAT
# firewall before applying to shared or production HAProxy configuration.
resource "pfsense_haproxy_settings" "global" {
  enable               = true
  maxconn              = 2000
  nbthread             = 1
  hard_stop_after      = "15m"
  logfacility          = "local0"
  loglevel             = "warning"
  resolver_retries     = 3
  sslcompatibilitymode = "auto"
  ssldefaultdhparam    = 2048
}

# Explicit apply after settings changes. There is intentionally no hidden
# auto-apply in pfsense_haproxy_settings or other durable HAProxy resources.
#
# UAT note: this assumes GET /services/haproxy/apply reports an "applied"
# boolean and POST /services/haproxy/apply starts HAProxy config application.
resource "pfsense_haproxy_apply" "global" {
  depends_on = [pfsense_haproxy_settings.global]

  triggers = {
    settings = sha1(jsonencode({
      enable               = pfsense_haproxy_settings.global.enable
      maxconn              = pfsense_haproxy_settings.global.maxconn
      nbthread             = pfsense_haproxy_settings.global.nbthread
      hard_stop_after      = pfsense_haproxy_settings.global.hard_stop_after
      logfacility          = pfsense_haproxy_settings.global.logfacility
      loglevel             = pfsense_haproxy_settings.global.loglevel
      resolver_retries     = pfsense_haproxy_settings.global.resolver_retries
      sslcompatibilitymode = pfsense_haproxy_settings.global.sslcompatibilitymode
      ssldefaultdhparam    = pfsense_haproxy_settings.global.ssldefaultdhparam
    }))
  }

  timeout       = "2m"
  poll_interval = "2s"
}
