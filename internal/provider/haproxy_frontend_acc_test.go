//go:build acc
// +build acc

package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccHaproxyFrontend_disabledHTTPToTCPImportApply(t *testing.T) {
	testAccPreCheck(t)

	frontendName := testAccResourceName(t, "frontend")
	resourceName := "pfsense_haproxy_frontend.test"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccHaproxyFrontendConfig(frontendName, "http", "Disabled HTTP frontend", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "id", frontendName),
					resource.TestCheckResourceAttr(resourceName, "name", frontendName),
					resource.TestCheckResourceAttr(resourceName, "type", "http"),
					resource.TestCheckResourceAttr(resourceName, "status", "disabled"),
					resource.TestCheckResourceAttr(resourceName, "forwardfor", "true"),
					resource.TestCheckResourceAttr(resourceName, "httpclose", "http-server-close"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				Config: testAccHaproxyFrontendConfig(frontendName, "tcp", "Disabled TCP frontend", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "id", frontendName),
					resource.TestCheckResourceAttr(resourceName, "name", frontendName),
					resource.TestCheckResourceAttr(resourceName, "type", "tcp"),
					resource.TestCheckResourceAttr(resourceName, "status", "disabled"),
					resource.TestCheckResourceAttr(resourceName, "descr", "Disabled TCP frontend"),
					resource.TestCheckResourceAttr(resourceName, "client_timeout", "15000"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateId:     frontendName,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccHaproxyFrontend_tcpRejectsHTTPOnlyFields(t *testing.T) {
	testAccPreCheck(t)

	frontendName := testAccResourceName(t, "frontend_tcp_invalid")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      testAccHaproxyFrontendInvalidTCPConfig(frontendName),
				ExpectError: regexp.MustCompile(`forwardfor is only valid when type is http|httpclose is only valid when type is http`),
			},
		},
	})
}

func testAccHaproxyFrontendConfig(name string, frontendType string, description string, includeHTTPFields bool) string {
	httpFields := ""
	if includeHTTPFields {
		httpFields = `
  forwardfor = true
  httpclose  = "http-server-close"
`
	}

	return fmt.Sprintf(`
%s

resource "pfsense_haproxy_frontend" "test" {
  name           = %[2]q
  type           = %[3]q
  descr          = %[4]q
  status         = "disabled"
  client_timeout = 15000
%[5]s
}

resource "pfsense_haproxy_apply" "test" {
  depends_on = [pfsense_haproxy_frontend.test]

  triggers = {
    frontend = sha1(jsonencode({
      name           = pfsense_haproxy_frontend.test.name
      type           = pfsense_haproxy_frontend.test.type
      descr          = pfsense_haproxy_frontend.test.descr
      status         = pfsense_haproxy_frontend.test.status
      client_timeout = pfsense_haproxy_frontend.test.client_timeout
    }))
  }

  timeout       = "2m"
  poll_interval = "2s"
}
`, testAccProviderConfig(), name, frontendType, description, httpFields)
}

func testAccHaproxyFrontendInvalidTCPConfig(name string) string {
	return fmt.Sprintf(`
%s

resource "pfsense_haproxy_frontend" "test" {
  name       = %[2]q
  type       = "tcp"
  status     = "disabled"
  forwardfor = true
}
`, testAccProviderConfig(), name)
}
