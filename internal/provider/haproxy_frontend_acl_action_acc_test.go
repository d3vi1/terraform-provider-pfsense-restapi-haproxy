//go:build acc
// +build acc

package provider

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccHaproxyFrontendACLAction_disabledFrontendReorderImportApply(t *testing.T) {
	testAccPreCheck(t)

	backendName := testAccResourceName(t, "frontend_acl_action_backend")
	frontendName := testAccResourceName(t, "frontend_acl_action")
	hostACLName := "host_acl"
	pathACLName := "path_acl"
	routeActionKey := "route_backend"
	headerActionKey := "set_request_header"
	importedActionKey := "imported_header"
	backendResource := "pfsense_haproxy_backend.test"
	frontendResource := "pfsense_haproxy_frontend.test"
	hostACLResource := "pfsense_haproxy_frontend_acl.host"
	pathACLResource := "pfsense_haproxy_frontend_acl.path"
	routeActionResource := "pfsense_haproxy_frontend_action.route"
	headerActionResource := "pfsense_haproxy_frontend_action.header"
	importedActionResource := "pfsense_haproxy_frontend_action.imported"

	initial := frontendACLActionConfig{
		backendName:          backendName,
		frontendName:         frontendName,
		hostACLName:          hostACLName,
		hostACLPosition:      0,
		pathACLName:          pathACLName,
		pathACLValue:         "/uat",
		pathACLPosition:      1,
		routeActionKey:       routeActionKey,
		routeActionPosition:  0,
		headerActionKey:      headerActionKey,
		headerActionFmt:      "frontend",
		headerActionPosition: 1,
	}
	updated := initial
	updated.hostACLPosition = 1
	updated.pathACLValue = "/uat-v2"
	updated.pathACLPosition = 0
	updated.routeActionPosition = 1
	updated.headerActionFmt = "frontend-v2"
	updated.headerActionPosition = 0

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccHaproxyFrontendACLActionConfig(initial, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(backendResource, "id", backendName),
					resource.TestCheckResourceAttr(frontendResource, "id", frontendName),
					resource.TestCheckResourceAttr(frontendResource, "status", "disabled"),
					resource.TestCheckResourceAttr(hostACLResource, "id", fmt.Sprintf("%s/%s", frontendName, hostACLName)),
					resource.TestCheckResourceAttr(hostACLResource, "expression", "host_matches"),
					resource.TestCheckResourceAttr(hostACLResource, "value", fmt.Sprintf("%s.example.invalid", frontendName)),
					resource.TestCheckResourceAttr(hostACLResource, "position", "0"),
					resource.TestCheckResourceAttr(pathACLResource, "id", fmt.Sprintf("%s/%s", frontendName, pathACLName)),
					resource.TestCheckResourceAttr(pathACLResource, "expression", "path_starts_with"),
					resource.TestCheckResourceAttr(pathACLResource, "value", "/uat"),
					resource.TestCheckResourceAttr(pathACLResource, "position", "1"),
					resource.TestCheckResourceAttr(routeActionResource, "id", fmt.Sprintf("%s/%s", frontendName, routeActionKey)),
					resource.TestCheckResourceAttr(routeActionResource, "action", "use_backend"),
					resource.TestCheckResourceAttr(routeActionResource, "acl", hostACLName),
					resource.TestCheckResourceAttr(routeActionResource, "backend", backendName),
					resource.TestCheckResourceAttr(routeActionResource, "position", "0"),
					resource.TestCheckResourceAttr(headerActionResource, "id", fmt.Sprintf("%s/%s", frontendName, headerActionKey)),
					resource.TestCheckResourceAttr(headerActionResource, "action", "http-request_set-header"),
					resource.TestCheckResourceAttr(headerActionResource, "acl", pathACLName),
					resource.TestCheckResourceAttr(headerActionResource, "name", "X-TF-UAT"),
					resource.TestCheckResourceAttr(headerActionResource, "fmt", "frontend"),
					resource.TestCheckResourceAttr(headerActionResource, "position", "1"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				Config: testAccHaproxyFrontendACLActionConfig(updated, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(hostACLResource, "position", "1"),
					resource.TestCheckResourceAttr(pathACLResource, "value", "/uat-v2"),
					resource.TestCheckResourceAttr(pathACLResource, "position", "0"),
					resource.TestCheckResourceAttr(routeActionResource, "backend", backendName),
					resource.TestCheckResourceAttr(routeActionResource, "position", "1"),
					resource.TestCheckResourceAttr(headerActionResource, "fmt", "frontend-v2"),
					resource.TestCheckResourceAttr(headerActionResource, "position", "0"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				ResourceName:      hostACLResource,
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("%s/%s", frontendName, hostACLName),
				ImportStateVerify: true,
			},
			{
				ResourceName:      pathACLResource,
				ImportState:       true,
				ImportStateId:     fmt.Sprintf("%s/%s", frontendName, pathACLName),
				ImportStateVerify: true,
			},
			{
				Config: testAccHaproxyFrontendACLActionConfig(updated, false),
				Check:  testAccCreateUnmanagedFrontendHeaderAction(frontendName, pathACLName),
			},
			{
				Config:             testAccHaproxyFrontendACLActionConfig(updated, true),
				ResourceName:       importedActionResource,
				ImportState:        true,
				ImportStateId:      fmt.Sprintf("%s/%s", frontendName, importedActionKey),
				ImportStatePersist: true,
				ImportStateCheck:   testAccCheckFrontendActionImportState(frontendName, importedActionKey),
			},
			{
				Config: testAccHaproxyFrontendACLActionConfig(updated, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(importedActionResource, "id", fmt.Sprintf("%s/%s", frontendName, importedActionKey)),
					resource.TestCheckResourceAttr(importedActionResource, "action", "http-request_set-header"),
					resource.TestCheckResourceAttr(importedActionResource, "acl", pathACLName),
					resource.TestCheckResourceAttr(importedActionResource, "name", "X-TF-IMPORTED"),
					resource.TestCheckResourceAttr(importedActionResource, "fmt", "imported"),
					resource.TestCheckResourceAttr(importedActionResource, "position", "2"),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
				),
			},
			{
				Config: testAccHaproxyFrontendACLActionCleanupConfig(backendName, frontendName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(backendResource, "id", backendName),
					resource.TestCheckResourceAttr(frontendResource, "id", frontendName),
					resource.TestCheckResourceAttr("pfsense_haproxy_apply.test", "status", "done"),
					testAccCheckHaproxyApplyAfterDestroy(),
				),
			},
		},
	})
}

type frontendACLActionConfig struct {
	backendName          string
	frontendName         string
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

func testAccHaproxyFrontendACLActionConfig(config frontendACLActionConfig, includeImportedAction bool) string {
	importedActionHCL := ""
	importedActionTrigger := ""
	importedActionDependency := ""
	if includeImportedAction {
		importedActionHCL = `

resource "pfsense_haproxy_frontend_action" "imported" {
  frontend_name = pfsense_haproxy_frontend.test.name
  key           = "imported_header"
  action        = "http-request_set-header"
  acl           = pfsense_haproxy_frontend_acl.path.name
  name          = "X-TF-IMPORTED"
  fmt           = "imported"
  position      = 2
}
`
		importedActionTrigger = `
    action_imported = sha1(jsonencode({
      id            = pfsense_haproxy_frontend_action.imported.id
      frontend_name = pfsense_haproxy_frontend_action.imported.frontend_name
      key           = pfsense_haproxy_frontend_action.imported.key
      action        = pfsense_haproxy_frontend_action.imported.action
      acl           = pfsense_haproxy_frontend_action.imported.acl
      name          = pfsense_haproxy_frontend_action.imported.name
      fmt           = pfsense_haproxy_frontend_action.imported.fmt
      position      = pfsense_haproxy_frontend_action.imported.position
    }))`
		importedActionDependency = `
    pfsense_haproxy_frontend_action.imported,`
	}

	return testAccHaproxyFrontendACLActionBaseConfig(config.backendName, config.frontendName, fmt.Sprintf(`
resource "pfsense_haproxy_frontend_acl" "host" {
  frontend_name = pfsense_haproxy_frontend.test.name
  name          = %[1]q
  expression    = "host_matches"
  value         = %[2]q
  casesensitive = false
  not           = false
  position      = %[3]d
}

resource "pfsense_haproxy_frontend_acl" "path" {
  frontend_name = pfsense_haproxy_frontend.test.name
  name          = %[4]q
  expression    = "path_starts_with"
  value         = %[5]q
  casesensitive = false
  not           = false
  position      = %[6]d
}

resource "pfsense_haproxy_frontend_action" "route" {
  frontend_name = pfsense_haproxy_frontend.test.name
  key           = %[7]q
  action        = "use_backend"
  acl           = pfsense_haproxy_frontend_acl.host.name
  backend       = pfsense_haproxy_backend.test.name
  position      = %[8]d
}

resource "pfsense_haproxy_frontend_action" "header" {
  frontend_name = pfsense_haproxy_frontend.test.name
  key           = %[9]q
  action        = "http-request_set-header"
  acl           = pfsense_haproxy_frontend_acl.path.name
  name          = "X-TF-UAT"
  fmt           = %[10]q
  position      = %[11]d
}
%[12]s
`, config.hostACLName, fmt.Sprintf("%s.example.invalid", config.frontendName), config.hostACLPosition, config.pathACLName, config.pathACLValue, config.pathACLPosition, config.routeActionKey, config.routeActionPosition, config.headerActionKey, config.headerActionFmt, config.headerActionPosition, importedActionHCL), `
    acl_host = sha1(jsonencode({
      id            = pfsense_haproxy_frontend_acl.host.id
      frontend_name = pfsense_haproxy_frontend_acl.host.frontend_name
      name          = pfsense_haproxy_frontend_acl.host.name
      expression    = pfsense_haproxy_frontend_acl.host.expression
      value         = pfsense_haproxy_frontend_acl.host.value
      casesensitive = pfsense_haproxy_frontend_acl.host.casesensitive
      not           = pfsense_haproxy_frontend_acl.host.not
      position      = pfsense_haproxy_frontend_acl.host.position
    }))
    acl_path = sha1(jsonencode({
      id            = pfsense_haproxy_frontend_acl.path.id
      frontend_name = pfsense_haproxy_frontend_acl.path.frontend_name
      name          = pfsense_haproxy_frontend_acl.path.name
      expression    = pfsense_haproxy_frontend_acl.path.expression
      value         = pfsense_haproxy_frontend_acl.path.value
      casesensitive = pfsense_haproxy_frontend_acl.path.casesensitive
      not           = pfsense_haproxy_frontend_acl.path.not
      position      = pfsense_haproxy_frontend_acl.path.position
    }))
    action_route = sha1(jsonencode({
      id            = pfsense_haproxy_frontend_action.route.id
      frontend_name = pfsense_haproxy_frontend_action.route.frontend_name
      key           = pfsense_haproxy_frontend_action.route.key
      action        = pfsense_haproxy_frontend_action.route.action
      acl           = pfsense_haproxy_frontend_action.route.acl
      backend       = pfsense_haproxy_frontend_action.route.backend
      position      = pfsense_haproxy_frontend_action.route.position
    }))
    action_header = sha1(jsonencode({
      id            = pfsense_haproxy_frontend_action.header.id
      frontend_name = pfsense_haproxy_frontend_action.header.frontend_name
      key           = pfsense_haproxy_frontend_action.header.key
      action        = pfsense_haproxy_frontend_action.header.action
      acl           = pfsense_haproxy_frontend_action.header.acl
      name          = pfsense_haproxy_frontend_action.header.name
      fmt           = pfsense_haproxy_frontend_action.header.fmt
      position      = pfsense_haproxy_frontend_action.header.position
    }))`+importedActionTrigger, `
    pfsense_haproxy_frontend_acl.host,
    pfsense_haproxy_frontend_acl.path,
    pfsense_haproxy_frontend_action.route,
    pfsense_haproxy_frontend_action.header,`+importedActionDependency)
}

func testAccHaproxyFrontendACLActionCleanupConfig(backendName string, frontendName string) string {
	return testAccHaproxyFrontendACLActionBaseConfig(backendName, frontendName, "", `
    cleanup = "frontend-acl-action-removed"`, "")
}

func testAccHaproxyFrontendACLActionBaseConfig(backendName string, frontendName string, childrenHCL string, childTriggers string, childDependencies string) string {
	return fmt.Sprintf(`
%s

resource "pfsense_haproxy_backend" "test" {
  name               = %[2]q
  balance            = "roundrobin"
  connection_timeout = 10000
  server_timeout     = 20000
  check_type         = "none"
}

resource "pfsense_haproxy_frontend" "test" {
  name           = %[3]q
  type           = "http"
  descr          = "Disabled frontend ACL action acceptance"
  status         = "disabled"
  client_timeout = 15000
}
%[4]s

resource "pfsense_haproxy_apply" "test" {
  depends_on = [
    pfsense_haproxy_backend.test,
    pfsense_haproxy_frontend.test,%[6]s
  ]

  triggers = {
    backend = sha1(jsonencode({
      name               = pfsense_haproxy_backend.test.name
      balance            = pfsense_haproxy_backend.test.balance
      connection_timeout = pfsense_haproxy_backend.test.connection_timeout
      server_timeout     = pfsense_haproxy_backend.test.server_timeout
      check_type         = pfsense_haproxy_backend.test.check_type
    }))
    frontend = sha1(jsonencode({
      name           = pfsense_haproxy_frontend.test.name
      type           = pfsense_haproxy_frontend.test.type
      descr          = pfsense_haproxy_frontend.test.descr
      status         = pfsense_haproxy_frontend.test.status
      client_timeout = pfsense_haproxy_frontend.test.client_timeout
    }))%[5]s
  }

  timeout       = "2m"
  poll_interval = "2s"
}
`, testAccProviderConfig(), backendName, frontendName, childrenHCL, childTriggers, childDependencies)
}

func testAccCreateUnmanagedFrontendHeaderAction(frontendName string, aclName string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		client, err := testAccPfsenseClientFromEnv()
		if err != nil {
			return err
		}

		_, parentID, found, err := findHaproxyFrontendByName(context.Background(), client, frontendName)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("frontend %q not found before creating unmanaged imported action", frontendName)
		}

		keys := haproxyFrontendActionKeys{frontendName: frontendName, key: "imported_header"}
		payload := haproxyFrontendActionPayload{
			action: "http-request_set-header",
			acl:    aclName,
			fields: map[string]string{
				"name": "X-TF-IMPORTED",
				"fmt":  "imported",
			},
		}
		_, _, found, err = findHaproxyFrontendAction(context.Background(), client, parentID, keys, payload)
		if err != nil {
			return err
		}
		if found {
			return nil
		}

		request := haproxyFrontendActionPayloadToAPI(payload)
		request["parent_id"] = parentID
		request["placement"] = int64(2)
		return client.Post(context.Background(), haproxyFrontendActionPath, request, nil)
	}
}

func testAccCheckFrontendActionImportState(frontendName string, key string) resource.ImportStateCheckFunc {
	return func(states []*terraform.InstanceState) error {
		expectedID := fmt.Sprintf("%s/%s", frontendName, key)
		var attributes map[string]string
		for _, state := range states {
			if state.Attributes["id"] != expectedID {
				continue
			}
			if state.Attributes["frontend_name"] != frontendName || state.Attributes["key"] != key {
				continue
			}
			attributes = state.Attributes
			break
		}
		if attributes == nil {
			return fmt.Errorf("imported frontend action state %q not found in %d states", expectedID, len(states))
		}

		for name, expected := range map[string]string{
			"id":            expectedID,
			"frontend_name": frontendName,
			"key":           key,
		} {
			if got := attributes[name]; got != expected {
				return fmt.Errorf("imported frontend action %s = %q, want %q", name, got, expected)
			}
		}

		return nil
	}
}
