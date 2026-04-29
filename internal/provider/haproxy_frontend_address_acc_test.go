//go:build acc
// +build acc

package provider

import (
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccHaproxyFrontendAddress_builtinSelectorsImportApply(t *testing.T) {
	testAccPreCheck(t)

	frontendName := testAccResourceName(t, "frontend_addr")
	basePort := testAccPort(frontendName, 0)
	addresses := []struct {
		resourceName string
		extaddr      string
		port         int
	}{
		{resourceName: "any_ipv4", extaddr: "any_ipv4", port: basePort},
		{resourceName: "any_ipv6", extaddr: "any_ipv6", port: basePort + 1},
		{resourceName: "localhost_ipv4", extaddr: "localhost_ipv4", port: basePort + 2},
		{resourceName: "localhost_ipv6", extaddr: "localhost_ipv6", port: basePort + 3},
	}

	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttr("pfsense_haproxy_frontend.test", "name", frontendName),
		resource.TestCheckResourceAttr("pfsense_haproxy_frontend.test", "status", "disabled"),
		resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
	}
	for _, address := range addresses {
		resourceAddress := "pfsense_haproxy_frontend_address." + address.resourceName
		checks = append(checks,
			resource.TestCheckResourceAttr(resourceAddress, "id", fmt.Sprintf("%s/%s/-/%d", frontendName, address.extaddr, address.port)),
			resource.TestCheckResourceAttr(resourceAddress, "frontend_name", frontendName),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr", address.extaddr),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr_custom", ""),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr_port", fmt.Sprintf("%d", address.port)),
		)
	}

	steps := []resource.TestStep{
		{
			Config: testAccHaproxyFrontendAddressBuiltinConfig(frontendName, addresses),
			Check:  resource.ComposeAggregateTestCheckFunc(checks...),
		},
	}
	for _, address := range addresses {
		steps = append(steps, resource.TestStep{
			ResourceName:      "pfsense_haproxy_frontend_address." + address.resourceName,
			ImportState:       true,
			ImportStateId:     fmt.Sprintf("%s/%s/-/%d", frontendName, address.extaddr, address.port),
			ImportStateVerify: true,
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    steps,
	})
}

func TestAccHaproxyFrontendAddress_customIPv4IPv6ImportApply(t *testing.T) {
	testAccPreCheck(t)

	customIPv4 := strings.TrimSpace(os.Getenv("PFSENSE_TEST_CUSTOM_IPV4"))
	customIPv6 := strings.TrimSpace(os.Getenv("PFSENSE_TEST_CUSTOM_IPV6"))
	if customIPv4 == "" || customIPv6 == "" {
		t.Skip("PFSENSE_TEST_CUSTOM_IPV4 and PFSENSE_TEST_CUSTOM_IPV6 are required for custom frontend address acceptance tests")
	}
	parsedIPv4, err := netip.ParseAddr(customIPv4)
	if err != nil || !parsedIPv4.Is4() {
		t.Fatalf("PFSENSE_TEST_CUSTOM_IPV4 must be a valid IPv4 address, got %q", customIPv4)
	}
	parsedIPv6, err := netip.ParseAddr(customIPv6)
	if err != nil || !parsedIPv6.Is6() {
		t.Fatalf("PFSENSE_TEST_CUSTOM_IPV6 must be a valid IPv6 address, got %q", customIPv6)
	}

	frontendName := testAccResourceName(t, "frontend_custom_addr")
	basePort := testAccPort(frontendName, 10)
	addresses := []struct {
		resourceName string
		custom       string
		port         int
	}{
		{resourceName: "custom_ipv4", custom: parsedIPv4.String(), port: basePort},
		{resourceName: "custom_ipv6", custom: parsedIPv6.String(), port: basePort + 1},
	}

	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttr("pfsense_haproxy_frontend.test", "name", frontendName),
		resource.TestCheckResourceAttr("pfsense_haproxy_frontend.test", "status", "disabled"),
		resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
	}
	for _, address := range addresses {
		resourceAddress := "pfsense_haproxy_frontend_address." + address.resourceName
		importID := fmt.Sprintf("%s/custom/%s/%d", frontendName, address.custom, address.port)
		checks = append(checks,
			resource.TestCheckResourceAttr(resourceAddress, "id", importID),
			resource.TestCheckResourceAttr(resourceAddress, "frontend_name", frontendName),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr", "custom"),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr_custom", address.custom),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr_port", fmt.Sprintf("%d", address.port)),
		)
	}

	steps := []resource.TestStep{
		{
			Config: testAccHaproxyFrontendAddressCustomConfig(frontendName, addresses),
			Check:  resource.ComposeAggregateTestCheckFunc(checks...),
		},
	}
	for _, address := range addresses {
		resourceAddress := "pfsense_haproxy_frontend_address." + address.resourceName
		importID := fmt.Sprintf("%s/custom/%s/%d", frontendName, address.custom, address.port)
		steps = append(steps, resource.TestStep{
			ResourceName:      resourceAddress,
			ImportState:       true,
			ImportStateId:     importID,
			ImportStateVerify: true,
		})
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    steps,
	})
}

func testAccHaproxyFrontendAddressBuiltinConfig(frontendName string, addresses []struct {
	resourceName string
	extaddr      string
	port         int
}) string {
	addressHCL := ""
	triggerEntries := ""
	for _, address := range addresses {
		addressHCL += fmt.Sprintf(`
resource "pfsense_haproxy_frontend_address" %[1]q {
  frontend_name = pfsense_haproxy_frontend.test.name
  extaddr       = %[2]q
  extaddr_port  = %[3]d
  extaddr_ssl   = false
}
`, address.resourceName, address.extaddr, address.port)
		triggerEntries += fmt.Sprintf(`
      %[1]s = {
        id   = pfsense_haproxy_frontend_address.%[1]s.id
        port = pfsense_haproxy_frontend_address.%[1]s.extaddr_port
      }`, address.resourceName)
	}

	return testAccHaproxyFrontendAddressBaseConfig(frontendName, addressHCL, triggerEntries)
}

func testAccHaproxyFrontendAddressCustomConfig(frontendName string, addresses []struct {
	resourceName string
	custom       string
	port         int
}) string {
	addressHCL := ""
	triggerEntries := ""
	for _, address := range addresses {
		addressHCL += fmt.Sprintf(`
resource "pfsense_haproxy_frontend_address" %[1]q {
  frontend_name  = pfsense_haproxy_frontend.test.name
  extaddr        = "custom"
  extaddr_custom = %[2]q
  extaddr_port   = %[3]d
  extaddr_ssl    = false
}
`, address.resourceName, address.custom, address.port)
		triggerEntries += fmt.Sprintf(`
      %[1]s = {
        id   = pfsense_haproxy_frontend_address.%[1]s.id
        port = pfsense_haproxy_frontend_address.%[1]s.extaddr_port
      }`, address.resourceName)
	}

	return testAccHaproxyFrontendAddressBaseConfig(frontendName, addressHCL, triggerEntries)
}

func testAccHaproxyFrontendAddressBaseConfig(frontendName string, addressHCL string, triggerEntries string) string {
	return fmt.Sprintf(`
%s

resource "pfsense_haproxy_frontend" "test" {
  name           = %[2]q
  type           = "tcp"
  descr          = "Disabled frontend address acceptance"
  status         = "disabled"
  client_timeout = 15000
}
%[3]s

resource "pfsense_haproxy_apply" "test" {
  depends_on = [
    pfsense_haproxy_frontend.test,
  ]

  triggers = {
    frontend = pfsense_haproxy_frontend.test.id
    addresses = sha1(jsonencode({%[4]s
    }))
  }

  timeout       = "2m"
  poll_interval = "2s"
}
`, testAccProviderConfig(), frontendName, addressHCL, triggerEntries)
}
