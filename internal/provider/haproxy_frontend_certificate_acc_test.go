//go:build acc
// +build acc

package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccHaproxyFrontendCertificate_existingReferenceImportApply(t *testing.T) {
	testAccPreCheck(t)

	certificateRef := strings.TrimSpace(os.Getenv("PFSENSE_TEST_CERTIFICATE_REF"))
	if certificateRef == "" {
		t.Skip("PFSENSE_TEST_CERTIFICATE_REF is required for frontend certificate acceptance tests")
	}
	if _, err := haproxyFrontendCertificateSSLCertificate(types.StringValue(certificateRef)); err != nil {
		t.Fatalf("PFSENSE_TEST_CERTIFICATE_REF is not a valid certificate reference: %v", err)
	}

	frontendName := testAccResourceName(t, "frontend_cert")
	port := testAccPort(frontendName, 20)
	certificateID := fmt.Sprintf("%s/%s", frontendName, certificateRef)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccHaproxyFrontendCertificateConfig(frontendName, port, certificateRef),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("pfsense_haproxy_frontend.test", "name", frontendName),
					resource.TestCheckResourceAttr("pfsense_haproxy_frontend.test", "status", "disabled"),
					resource.TestCheckResourceAttr("pfsense_haproxy_frontend_address.https", "frontend_name", frontendName),
					resource.TestCheckResourceAttr("pfsense_haproxy_frontend_address.https", "extaddr_ssl", "true"),
					resource.TestCheckResourceAttr("pfsense_haproxy_frontend_certificate.test", "id", certificateID),
					resource.TestCheckResourceAttr("pfsense_haproxy_frontend_certificate.test", "frontend_name", frontendName),
					resource.TestCheckResourceAttr("pfsense_haproxy_frontend_certificate.test", "ssl_certificate", certificateRef),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				ResourceName:      "pfsense_haproxy_frontend_certificate.test",
				ImportState:       true,
				ImportStateId:     certificateID,
				ImportStateVerify: true,
			},
		},
	})
}

func testAccHaproxyFrontendCertificateConfig(frontendName string, port int, certificateRef string) string {
	return fmt.Sprintf(`
%s

resource "pfsense_haproxy_frontend" "test" {
  name           = %[2]q
  type           = "tcp"
  descr          = "Disabled frontend certificate acceptance"
  status         = "disabled"
  client_timeout = 15000
}

resource "pfsense_haproxy_frontend_address" "https" {
  frontend_name = pfsense_haproxy_frontend.test.name
  extaddr       = "localhost_ipv4"
  extaddr_port  = %[3]d
  extaddr_ssl   = true
}

resource "pfsense_haproxy_frontend_certificate" "test" {
  frontend_name   = pfsense_haproxy_frontend.test.name
  ssl_certificate = %[4]q

  depends_on = [pfsense_haproxy_frontend_address.https]
}

resource "pfsense_haproxy_apply" "test" {
  depends_on = [
    pfsense_haproxy_frontend.test,
    pfsense_haproxy_frontend_address.https,
    pfsense_haproxy_frontend_certificate.test,
  ]

  triggers = {
    frontend    = pfsense_haproxy_frontend.test.id
    address     = pfsense_haproxy_frontend_address.https.id
    certificate = pfsense_haproxy_frontend_certificate.test.id
  }

  timeout       = "2m"
  poll_interval = "2s"
}
`, testAccProviderConfig(), frontendName, port, certificateRef)
}
