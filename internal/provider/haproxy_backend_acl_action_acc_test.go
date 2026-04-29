//go:build acc
// +build acc

package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/pfsense"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccHaproxyBackendACLAction_unattachedBackendReorderImportApply(t *testing.T) {
	testAccPreCheck(t)

	backendName := testAccResourceName(t, "backend_acl_action")
	serverName := "app01"
	hostACLName := "host_acl"
	pathACLName := "path_acl"
	routeActionKey := "route_app01"
	headerActionKey := "set_response_header"
	importedActionKey := "imported_header"
	serverPort := testAccPort(backendName, 40)
	backendResource := "pfsense_haproxy_backend.test"
	serverResource := "pfsense_haproxy_backend_server.test"
	hostACLResource := "pfsense_haproxy_backend_acl.host"
	pathACLResource := "pfsense_haproxy_backend_acl.path"
	routeActionResource := "pfsense_haproxy_backend_action.route"
	headerActionResource := "pfsense_haproxy_backend_action.header"
	importedActionResource := "pfsense_haproxy_backend_action.imported"

	initial := backendACLActionConfig{
		backendName:          backendName,
		serverName:           serverName,
		serverPort:           serverPort,
		hostACLName:          hostACLName,
		hostACLPosition:      0,
		pathACLName:          pathACLName,
		pathACLValue:         "/uat",
		pathACLPosition:      1,
		routeActionKey:       routeActionKey,
		routeActionPosition:  0,
		headerActionKey:      headerActionKey,
		headerActionFmt:      "backend",
		headerActionPosition: 1,
	}
	updated := initial
	updated.hostACLPosition = 1
	updated.pathACLValue = "/uat-v2"
	updated.pathACLPosition = 0
	updated.routeActionPosition = 1
	updated.headerActionFmt = "backend-v2"
	updated.headerActionPosition = 0

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccHaproxyBackendACLActionConfig(initial, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(backendResource, "id", backendName),
					resource.TestCheckResourceAttr(serverResource, "id", fmt.Sprintf("%s/%s", backendName, serverName)),
					resource.TestCheckResourceAttr(hostACLResource, "id", fmt.Sprintf("%s/%s", backendName, hostACLName)),
					resource.TestCheckResourceAttr(hostACLResource, "expression", "host_matches"),
					resource.TestCheckResourceAttr(hostACLResource, "value", fmt.Sprintf("%s.example.invalid", backendName)),
					resource.TestCheckResourceAttr(hostACLResource, "position", "0"),
					resource.TestCheckResourceAttr(pathACLResource, "id", fmt.Sprintf("%s/%s", backendName, pathACLName)),
					resource.TestCheckResourceAttr(pathACLResource, "expression", "path_starts_with"),
					resource.TestCheckResourceAttr(pathACLResource, "value", "/uat"),
					resource.TestCheckResourceAttr(pathACLResource, "position", "1"),
					resource.TestCheckResourceAttr(routeActionResource, "id", fmt.Sprintf("%s/%s", backendName, routeActionKey)),
					resource.TestCheckResourceAttr(routeActionResource, "action", "use_server"),
					resource.TestCheckResourceAttr(routeActionResource, "acl", hostACLName),
					resource.TestCheckResourceAttr(routeActionResource, "server", serverName),
					resource.TestCheckResourceAttr(routeActionResource, "position", "0"),
					resource.TestCheckResourceAttr(headerActionResource, "id", fmt.Sprintf("%s/%s", backendName, headerActionKey)),
					resource.TestCheckResourceAttr(headerActionResource, "action", "http-response_set-header"),
					resource.TestCheckResourceAttr(headerActionResource, "acl", pathACLName),
					resource.TestCheckResourceAttr(headerActionResource, "name", "X-TF-UAT"),
					resource.TestCheckResourceAttr(headerActionResource, "fmt", "backend"),
					resource.TestCheckResourceAttr(headerActionResource, "position", "1"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				Config: testAccHaproxyBackendACLActionConfig(updated, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(hostACLResource, "position", "1"),
					resource.TestCheckResourceAttr(pathACLResource, "value", "/uat-v2"),
					resource.TestCheckResourceAttr(pathACLResource, "position", "0"),
					resource.TestCheckResourceAttr(routeActionResource, "server", serverName),
					resource.TestCheckResourceAttr(routeActionResource, "position", "1"),
					resource.TestCheckResourceAttr(headerActionResource, "fmt", "backend-v2"),
					resource.TestCheckResourceAttr(headerActionResource, "position", "0"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				ResourceName:      hostACLResource,
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("%s/%s", backendName, hostACLName),
				ImportStateVerify: true,
			},
			{
				ResourceName:      pathACLResource,
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("%s/%s", backendName, pathACLName),
				ImportStateVerify: true,
			},
			{
				Config: testAccHaproxyBackendACLActionConfig(updated, false),
				Check:  testAccCreateUnmanagedBackendHeaderAction(backendName, pathACLName),
			},
			{
				Config:             testAccHaproxyBackendACLActionConfig(updated, true),
				ResourceName:       importedActionResource,
				ImportState:        true,
				ImportStateId:      fmt.Sprintf("%s/%s", backendName, importedActionKey),
				ImportStatePersist: true,
				ImportStateCheck:   testAccCheckBackendActionImportState(backendName, importedActionKey),
			},
			{
				Config: testAccHaproxyBackendACLActionConfig(updated, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(importedActionResource, "id", fmt.Sprintf("%s/%s", backendName, importedActionKey)),
					resource.TestCheckResourceAttr(importedActionResource, "action", "http-response_set-header"),
					resource.TestCheckResourceAttr(importedActionResource, "acl", pathACLName),
					resource.TestCheckResourceAttr(importedActionResource, "name", "X-TF-IMPORTED"),
					resource.TestCheckResourceAttr(importedActionResource, "fmt", "imported"),
					resource.TestCheckResourceAttr(importedActionResource, "position", "2"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				Config: testAccHaproxyBackendACLActionCleanupConfig(backendName, serverName, serverPort),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(backendResource, "id", backendName),
					resource.TestCheckResourceAttr(serverResource, "id", fmt.Sprintf("%s/%s", backendName, serverName)),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
					testAccCheckHaproxyApplyAfterDestroy(),
				),
			},
		},
	})
}

type backendACLActionConfig struct {
	backendName          string
	serverName           string
	serverPort           int
	hostACLName          string
	hostACLPosition      int
	pathACLName          string
	pathACLValue         string
	pathACLPosition      int
	routeActionKey       string
	routeActionPosition  int
	headerActionKey      string
	headerActionFmt      string
	headerActionPosition int
}

func testAccHaproxyBackendACLActionConfig(config backendACLActionConfig, includeImportedAction bool) string {
	importedActionHCL := ""
	importedActionTrigger := ""
	importedActionDependency := ""
	if includeImportedAction {
		importedActionHCL = `

resource "pfsense_haproxy_backend_action" "imported" {
  backend_name = pfsense_haproxy_backend.test.name
  key          = "imported_header"
  action       = "http-response_set-header"
  acl          = pfsense_haproxy_backend_acl.path.name
  name         = "X-TF-IMPORTED"
  fmt          = "imported"
  position     = 2
}
`
		importedActionTrigger = `
    action_imported = sha1(jsonencode({
      id           = pfsense_haproxy_backend_action.imported.id
      backend_name = pfsense_haproxy_backend_action.imported.backend_name
      key          = pfsense_haproxy_backend_action.imported.key
      action       = pfsense_haproxy_backend_action.imported.action
      acl          = pfsense_haproxy_backend_action.imported.acl
      name         = pfsense_haproxy_backend_action.imported.name
      fmt          = pfsense_haproxy_backend_action.imported.fmt
      position     = pfsense_haproxy_backend_action.imported.position
    }))`
		importedActionDependency = `
    pfsense_haproxy_backend_action.imported,`
	}

	return testAccHaproxyBackendACLActionBaseConfig(config.backendName, config.serverName, config.serverPort, fmt.Sprintf(`
resource "pfsense_haproxy_backend_acl" "host" {
  backend_name  = pfsense_haproxy_backend.test.name
  name          = %[1]q
  expression    = "host_matches"
  value         = %[2]q
  casesensitive = false
  not           = false
  position      = %[3]d
}

resource "pfsense_haproxy_backend_acl" "path" {
  backend_name  = pfsense_haproxy_backend.test.name
  name          = %[4]q
  expression    = "path_starts_with"
  value         = %[5]q
  casesensitive = false
  not           = false
  position      = %[6]d
}

resource "pfsense_haproxy_backend_action" "route" {
  backend_name = pfsense_haproxy_backend.test.name
  key          = %[7]q
  action       = "use_server"
  acl          = pfsense_haproxy_backend_acl.host.name
  server       = pfsense_haproxy_backend_server.test.name
  position     = %[8]d
}

resource "pfsense_haproxy_backend_action" "header" {
  backend_name = pfsense_haproxy_backend.test.name
  key          = %[9]q
  action       = "http-response_set-header"
  acl          = pfsense_haproxy_backend_acl.path.name
  name         = "X-TF-UAT"
  fmt          = %[10]q
  position     = %[11]d
}
%[12]s
`, config.hostACLName, fmt.Sprintf("%s.example.invalid", config.backendName), config.hostACLPosition, config.pathACLName, config.pathACLValue, config.pathACLPosition, config.routeActionKey, config.routeActionPosition, config.headerActionKey, config.headerActionFmt, config.headerActionPosition, importedActionHCL), `
    acl_host = sha1(jsonencode({
      id            = pfsense_haproxy_backend_acl.host.id
      backend_name  = pfsense_haproxy_backend_acl.host.backend_name
      name          = pfsense_haproxy_backend_acl.host.name
      expression    = pfsense_haproxy_backend_acl.host.expression
      value         = pfsense_haproxy_backend_acl.host.value
      casesensitive = pfsense_haproxy_backend_acl.host.casesensitive
      not           = pfsense_haproxy_backend_acl.host.not
      position      = pfsense_haproxy_backend_acl.host.position
    }))
    acl_path = sha1(jsonencode({
      id            = pfsense_haproxy_backend_acl.path.id
      backend_name  = pfsense_haproxy_backend_acl.path.backend_name
      name          = pfsense_haproxy_backend_acl.path.name
      expression    = pfsense_haproxy_backend_acl.path.expression
      value         = pfsense_haproxy_backend_acl.path.value
      casesensitive = pfsense_haproxy_backend_acl.path.casesensitive
      not           = pfsense_haproxy_backend_acl.path.not
      position      = pfsense_haproxy_backend_acl.path.position
    }))
    action_route = sha1(jsonencode({
      id           = pfsense_haproxy_backend_action.route.id
      backend_name = pfsense_haproxy_backend_action.route.backend_name
      key          = pfsense_haproxy_backend_action.route.key
      action       = pfsense_haproxy_backend_action.route.action
      acl          = pfsense_haproxy_backend_action.route.acl
      server       = pfsense_haproxy_backend_action.route.server
      position     = pfsense_haproxy_backend_action.route.position
    }))
    action_header = sha1(jsonencode({
      id           = pfsense_haproxy_backend_action.header.id
      backend_name = pfsense_haproxy_backend_action.header.backend_name
      key          = pfsense_haproxy_backend_action.header.key
      action       = pfsense_haproxy_backend_action.header.action
      acl          = pfsense_haproxy_backend_action.header.acl
      name         = pfsense_haproxy_backend_action.header.name
      fmt          = pfsense_haproxy_backend_action.header.fmt
      position     = pfsense_haproxy_backend_action.header.position
    }))`+importedActionTrigger, `
    pfsense_haproxy_backend_acl.host,
    pfsense_haproxy_backend_acl.path,
    pfsense_haproxy_backend_action.route,
    pfsense_haproxy_backend_action.header,`+importedActionDependency)
}

func testAccHaproxyBackendACLActionCleanupConfig(backendName string, serverName string, serverPort int) string {
	return testAccHaproxyBackendACLActionBaseConfig(backendName, serverName, serverPort, "", `
    cleanup = "backend-acl-action-removed"`, "")
}

func testAccHaproxyBackendACLActionBaseConfig(backendName string, serverName string, serverPort int, childrenHCL string, childTriggers string, childDependencies string) string {
	return fmt.Sprintf(`
%s

resource "pfsense_haproxy_backend" "test" {
  name               = %[2]q
  balance            = "roundrobin"
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
  weight          = 50
  ssl             = false
  sslserververify = false
}
%[5]s

resource "pfsense_haproxy_apply" "test" {
  depends_on = [
    pfsense_haproxy_backend.test,
    pfsense_haproxy_backend_server.test,%[7]s
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
    }))%[6]s
  }

  timeout       = "2m"
  poll_interval = "2s"
}
`, testAccProviderConfig(), backendName, serverName, serverPort, childrenHCL, childTriggers, childDependencies)
}

func testAccCheckHaproxyApplyAfterDestroy() resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		client, err := testAccPfsenseClientFromEnv()
		if err != nil {
			return err
		}

		_, err = applyAndWaitForHaproxy(context.Background(), client, defaultHaproxyApplyTimeout, defaultHaproxyApplyPollInterval)
		return err
	}
}

func testAccCreateUnmanagedBackendHeaderAction(backendName string, aclName string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		client, err := testAccPfsenseClientFromEnv()
		if err != nil {
			return err
		}

		_, parentID, found, err := findHaproxyBackendByName(context.Background(), client, backendName)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("backend %q not found before creating unmanaged imported action", backendName)
		}

		keys := haproxyBackendActionKeys{backendName: backendName, key: "imported_header"}
		payload := haproxyBackendActionPayload{
			action: "http-response_set-header",
			acl:    aclName,
			fields: map[string]string{
				"name": "X-TF-IMPORTED",
				"fmt":  "imported",
			},
		}
		_, _, found, err = findHaproxyBackendAction(context.Background(), client, parentID, keys, payload)
		if err != nil {
			return err
		}
		if found {
			return nil
		}

		request := haproxyBackendActionPayloadToAPI(payload)
		request["parent_id"] = parentID
		request["placement"] = int64(2)
		return client.Post(context.Background(), haproxyBackendActionPath, request, nil)
	}
}

func testAccCheckBackendActionImportState(backendName string, key string) resource.ImportStateCheckFunc {
	return func(states []*terraform.InstanceState) error {
		expectedID := fmt.Sprintf("%s/%s", backendName, key)
		var attributes map[string]string
		for _, state := range states {
			if state.Attributes["id"] != expectedID {
				continue
			}
			if state.Attributes["backend_name"] != backendName || state.Attributes["key"] != key {
				continue
			}
			attributes = state.Attributes
			break
		}
		if attributes == nil {
			return fmt.Errorf("imported backend action state %q not found in %d states", expectedID, len(states))
		}

		for name, expected := range map[string]string{
			"id":           expectedID,
			"backend_name": backendName,
			"key":          key,
		} {
			if got := attributes[name]; got != expected {
				return fmt.Errorf("imported backend action %s = %q, want %q", name, got, expected)
			}
		}

		return nil
	}
}

func testAccPfsenseClientFromEnv() (*pfsense.Client, error) {
	resolved, diags := resolveConfig(providerConfig{
		Endpoint:    types.StringNull(),
		APIKey:      types.StringNull(),
		Username:    types.StringNull(),
		Password:    types.StringNull(),
		InsecureTLS: types.BoolNull(),
		Timeout:     types.StringNull(),
	})
	if diags.HasError() {
		return nil, fmt.Errorf("resolve provider config for acceptance helper: %s", diagnosticsText(diags))
	}

	return pfsense.NewClient(pfsense.Config{
		Endpoint:    resolved.Endpoint,
		APIKey:      resolved.APIKey,
		Username:    resolved.Username,
		Password:    resolved.Password,
		InsecureTLS: resolved.InsecureTLS,
		Timeout:     resolved.Timeout,
	}), nil
}
