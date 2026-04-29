//go:build acc
// +build acc

package provider

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"pfsense": providerserver.NewProtocol6WithError(New("test")()),
}

var testAccNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func TestAccProviderConfiguration(t *testing.T) {
	testAccPreCheck(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccProviderConfig(),
			},
		},
	})
}

func testAccPreCheck(t *testing.T) {
	t.Helper()

	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC is not set")
	}
	if strings.TrimSpace(os.Getenv("PFSENSE_ENDPOINT")) == "" {
		t.Skip("PFSENSE_ENDPOINT is not set; skipping live pfSense acceptance tests")
	}
	if strings.TrimSpace(os.Getenv("PFSENSE_API_KEY")) == "" {
		if strings.TrimSpace(os.Getenv("PFSENSE_USERNAME")) == "" || os.Getenv("PFSENSE_PASSWORD") == "" {
			t.Skip("PFSENSE_API_KEY or PFSENSE_USERNAME/PFSENSE_PASSWORD is required for acceptance tests")
		}
	}
	if got := strings.ToLower(strings.TrimSpace(os.Getenv("PFSENSE_TEST_ENVIRONMENT"))); got != "uat" {
		t.Fatalf("PFSENSE_TEST_ENVIRONMENT must be uat for acceptance tests, got %q", got)
	}
	prefix := strings.TrimSpace(os.Getenv("PFSENSE_TEST_PREFIX"))
	if prefix == "" {
		t.Fatalf("PFSENSE_TEST_PREFIX is required for acceptance tests")
	}
	if !testAccNamePattern.MatchString(prefix) {
		t.Fatalf("PFSENSE_TEST_PREFIX must match %s", testAccNamePattern.String())
	}
}

func testAccProviderConfig() string {
	return `
provider "pfsense" {}
`
}

func testAccResourceName(t *testing.T, suffix string) string {
	t.Helper()

	prefix := strings.Trim(strings.TrimSpace(os.Getenv("PFSENSE_TEST_PREFIX")), "._-")
	if prefix == "" {
		t.Fatalf("PFSENSE_TEST_PREFIX is required for acceptance resource names")
	}
	if suffix == "" || !testAccNamePattern.MatchString(suffix) {
		t.Fatalf("acceptance resource suffix must match %s", testAccNamePattern.String())
	}

	var token [4]byte
	if _, err := rand.Read(token[:]); err != nil {
		t.Fatalf("generate acceptance test token: %v", err)
	}

	name := fmt.Sprintf("%s_%s_%s", prefix, suffix, hex.EncodeToString(token[:]))
	if !testAccNamePattern.MatchString(name) {
		t.Fatalf("acceptance resource name %q must match %s", name, testAccNamePattern.String())
	}
	return name
}

func testAccPort(name string, offset int) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	return 20000 + int(hash.Sum32()%20000) + offset
}
