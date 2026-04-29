//go:build acc
// +build acc

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccHaproxyBackendServer_disabledParentServerImportApply(t *testing.T) {
	testAccPreCheck(t)

	backendName := testAccResourceName(t, "backend_server")
	serverName := "app01"
	serverPort := testAccPort(backendName, 30)
	backendResource := "pfsense_haproxy_backend.test"
	serverResource := "pfsense_haproxy_backend_server.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccHaproxyBackendServerConfig(backendName, serverName, serverPort, "roundrobin", 25),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(backendResource, "id", backendName),
					resource.TestCheckResourceAttr(backendResource, "name", backendName),
					resource.TestCheckResourceAttr(backendResource, "balance", "roundrobin"),
					resource.TestCheckResourceAttr(backendResource, "connection_timeout", "10000"),
					resource.TestCheckResourceAttr(backendResource, "server_timeout", "20000"),
					resource.TestCheckResourceAttr(backendResource, "check_type", "none"),
					resource.TestCheckResourceAttr(serverResource, "id", fmt.Sprintf("%s/%s", backendName, serverName)),
					resource.TestCheckResourceAttr(serverResource, "backend_name", backendName),
					resource.TestCheckResourceAttr(serverResource, "name", serverName),
					resource.TestCheckResourceAttr(serverResource, "address", "127.0.0.1"),
					resource.TestCheckResourceAttr(serverResource, "port", fmt.Sprintf("%d", serverPort)),
					resource.TestCheckResourceAttr(serverResource, "status", "disabled"),
					resource.TestCheckResourceAttr(serverResource, "weight", "25"),
					resource.TestCheckResourceAttr(serverResource, "ssl", "false"),
					resource.TestCheckResourceAttr(serverResource, "sslserververify", "false"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				Config: testAccHaproxyBackendServerConfig(backendName, serverName, serverPort, "leastconn", 75),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(backendResource, "id", backendName),
					resource.TestCheckResourceAttr(backendResource, "balance", "leastconn"),
					resource.TestCheckResourceAttr(serverResource, "id", fmt.Sprintf("%s/%s", backendName, serverName)),
					resource.TestCheckResourceAttr(serverResource, "weight", "75"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				ResourceName:      backendResource,
				ImportState:       true,
				ImportStateId:     backendName,
				ImportStateVerify: true,
			},
			{
				ResourceName:      serverResource,
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("%s/%s", backendName, serverName),
				ImportStateVerify: true,
			},
		},
	})
}

func testAccHaproxyBackendServerConfig(backendName string, serverName string, serverPort int, balance string, weight int) string {
	return fmt.Sprintf(`
%s

resource "pfsense_haproxy_backend" "test" {
  name               = %[2]q
  balance            = %[5]q
  connection_timeout = 10000
  server_timeout     = 20000
  check_type         = "none"
}

resource "pfsense_haproxy_backend_server" "test" {
  backend_name    = pfsense_haproxy_backend.test.name
  name            = %[3]q
  address         = "127.0.0.1"
  port            = %[4]d
  status          = "disabled"
  weight          = %[6]d
  ssl             = false
  sslserververify = false
}

resource "pfsense_haproxy_apply" "test" {
  depends_on = [
    pfsense_haproxy_backend.test,
    pfsense_haproxy_backend_server.test,
  ]

  triggers = {
    backend = sha1(jsonencode({
      name               = pfsense_haproxy_backend.test.name
      balance            = pfsense_haproxy_backend.test.balance
      connection_timeout = pfsense_haproxy_backend.test.connection_timeout
      server_timeout     = pfsense_haproxy_backend.test.server_timeout
      check_type         = pfsense_haproxy_backend.test.check_type
    }))
    server = sha1(jsonencode({
      id              = pfsense_haproxy_backend_server.test.id
      backend_name    = pfsense_haproxy_backend_server.test.backend_name
      name            = pfsense_haproxy_backend_server.test.name
      address         = pfsense_haproxy_backend_server.test.address
      port            = pfsense_haproxy_backend_server.test.port
      status          = pfsense_haproxy_backend_server.test.status
      weight          = pfsense_haproxy_backend_server.test.weight
      ssl             = pfsense_haproxy_backend_server.test.ssl
      sslserververify = pfsense_haproxy_backend_server.test.sslserververify
    }))
  }

  timeout       = "2m"
  poll_interval = "2s"
}
`, testAccProviderConfig(), backendName, serverName, serverPort, balance, weight)
}
