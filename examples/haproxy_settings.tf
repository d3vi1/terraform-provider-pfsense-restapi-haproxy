data "pfsense_haproxy_settings" "current" {}

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
