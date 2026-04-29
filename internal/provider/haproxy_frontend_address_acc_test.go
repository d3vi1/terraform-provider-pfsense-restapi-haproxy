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

type testAccHaproxyFrontendAddressBuiltin struct {
	resourceName string
	extaddr      string
	port         int
	extaddrSSL   bool
}

type testAccHaproxyFrontendAddressCustom struct {
	resourceName    string
	customInput     string
	customCanonical string
	port            int
	extaddrSSL      bool
}

func TestAccHaproxyFrontendAddress_builtinSelectorsImportApply(t *testing.T) {
	testAccPreCheck(t)

	frontendName := testAccResourceName(t, "frontend_addr")
	basePort := testAccPort(frontendName, 0)
	addresses := []testAccHaproxyFrontendAddressBuiltin{
		{resourceName: "any_ipv4", extaddr: "any_ipv4", port: basePort, extaddrSSL: false},
		{resourceName: "any_ipv6", extaddr: "any_ipv6", port: basePort + 1, extaddrSSL: false},
		{resourceName: "localhost_ipv4", extaddr: "localhost_ipv4", port: basePort + 2, extaddrSSL: false},
		{resourceName: "localhost_ipv6", extaddr: "localhost_ipv6", port: basePort + 3, extaddrSSL: false},
	}
	updatedAddresses := make([]testAccHaproxyFrontendAddressBuiltin, len(addresses))
	copy(updatedAddresses, addresses)
	for index := range updatedAddresses {
		updatedAddresses[index].extaddrSSL = true
	}

	steps := []resource.TestStep{
		{
			Config: testAccHaproxyFrontendAddressBuiltinConfig(frontendName, addresses),
			Check:  testAccHaproxyFrontendAddressBuiltinChecks(frontendName, addresses),
		},
		{
			Config: testAccHaproxyFrontendAddressBuiltinConfig(frontendName, updatedAddresses),
			Check:  testAccHaproxyFrontendAddressBuiltinChecks(frontendName, updatedAddresses),
		},
	}
	for _, address := range updatedAddresses {
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
	addresses := []testAccHaproxyFrontendAddressCustom{
		{resourceName: "custom_ipv4", customInput: customIPv4, customCanonical: parsedIPv4.String(), port: basePort, extaddrSSL: false},
		{resourceName: "custom_ipv6", customInput: testAccExpandedIPv6(parsedIPv6), customCanonical: parsedIPv6.String(), port: basePort + 1, extaddrSSL: false},
	}
	updatedAddresses := make([]testAccHaproxyFrontendAddressCustom, len(addresses))
	copy(updatedAddresses, addresses)
	for index := range updatedAddresses {
		updatedAddresses[index].extaddrSSL = true
	}

	steps := []resource.TestStep{
		{
			Config: testAccHaproxyFrontendAddressCustomConfig(frontendName, addresses),
			Check:  testAccHaproxyFrontendAddressCustomChecks(frontendName, addresses),
		},
		{
			Config: testAccHaproxyFrontendAddressCustomConfig(frontendName, updatedAddresses),
			Check:  testAccHaproxyFrontendAddressCustomChecks(frontendName, updatedAddresses),
		},
	}
	for _, address := range updatedAddresses {
		resourceAddress := "pfsense_haproxy_frontend_address." + address.resourceName
		importID := fmt.Sprintf("%s/custom/%s/%d", frontendName, address.customCanonical, address.port)
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

func testAccHaproxyFrontendAddressBuiltinChecks(frontendName string, addresses []testAccHaproxyFrontendAddressBuiltin) resource.TestCheckFunc {
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
			resource.TestCheckResourceAttr(resourceAddress, "extaddr_ssl", fmt.Sprintf("%t", address.extaddrSSL)),
		)
	}

	return resource.ComposeAggregateTestCheckFunc(checks...)
}

func testAccHaproxyFrontendAddressCustomChecks(frontendName string, addresses []testAccHaproxyFrontendAddressCustom) resource.TestCheckFunc {
	checks := []resource.TestCheckFunc{
		resource.TestCheckResourceAttr("pfsense_haproxy_frontend.test", "name", frontendName),
		resource.TestCheckResourceAttr("pfsense_haproxy_frontend.test", "status", "disabled"),
		resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
	}
	for _, address := range addresses {
		resourceAddress := "pfsense_haproxy_frontend_address." + address.resourceName
		importID := fmt.Sprintf("%s/custom/%s/%d", frontendName, address.customCanonical, address.port)
		checks = append(checks,
			resource.TestCheckResourceAttr(resourceAddress, "id", importID),
			resource.TestCheckResourceAttr(resourceAddress, "frontend_name", frontendName),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr", "custom"),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr_custom", address.customCanonical),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr_port", fmt.Sprintf("%d", address.port)),
			resource.TestCheckResourceAttr(resourceAddress, "extaddr_ssl", fmt.Sprintf("%t", address.extaddrSSL)),
		)
	}

	return resource.ComposeAggregateTestCheckFunc(checks...)
}

func testAccHaproxyFrontendAddressBuiltinConfig(frontendName string, addresses []testAccHaproxyFrontendAddressBuiltin) string {
	addressHCL := ""
	triggerEntries := ""
	for _, address := range addresses {
		addressHCL += fmt.Sprintf(`
resource "pfsense_haproxy_frontend_address" %[1]q {
  frontend_name = pfsense_haproxy_frontend.test.name
  extaddr       = %[2]q
  extaddr_port  = %[3]d
  extaddr_ssl   = %[4]t
}
`, address.resourceName, address.extaddr, address.port, address.extaddrSSL)
		triggerEntries += fmt.Sprintf(`
      %[1]s = {
        id   = pfsense_haproxy_frontend_address.%[1]s.id
        port = pfsense_haproxy_frontend_address.%[1]s.extaddr_port
        ssl  = pfsense_haproxy_frontend_address.%[1]s.extaddr_ssl
      }`, address.resourceName)
	}

	return testAccHaproxyFrontendAddressBaseConfig(frontendName, addressHCL, triggerEntries)
}

func testAccHaproxyFrontendAddressCustomConfig(frontendName string, addresses []testAccHaproxyFrontendAddressCustom) string {
	addressHCL := ""
	triggerEntries := ""
	for _, address := range addresses {
		addressHCL += fmt.Sprintf(`
resource "pfsense_haproxy_frontend_address" %[1]q {
  frontend_name  = pfsense_haproxy_frontend.test.name
  extaddr        = "custom"
  extaddr_custom = %[2]q
  extaddr_port   = %[3]d
  extaddr_ssl    = %[4]t
}
`, address.resourceName, address.customInput, address.port, address.extaddrSSL)
		triggerEntries += fmt.Sprintf(`
      %[1]s = {
        id   = pfsense_haproxy_frontend_address.%[1]s.id
        port = pfsense_haproxy_frontend_address.%[1]s.extaddr_port
        ssl  = pfsense_haproxy_frontend_address.%[1]s.extaddr_ssl
      }`, address.resourceName)
	}

	return testAccHaproxyFrontendAddressBaseConfig(frontendName, addressHCL, triggerEntries)
}

func testAccExpandedIPv6(address netip.Addr) string {
	bytes := address.As16()
	groups := make([]string, 8)
	for index := range groups {
		groups[index] = fmt.Sprintf("%04X", uint16(bytes[index*2])<<8|uint16(bytes[index*2+1]))
	}

	return strings.Join(groups, ":")
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
