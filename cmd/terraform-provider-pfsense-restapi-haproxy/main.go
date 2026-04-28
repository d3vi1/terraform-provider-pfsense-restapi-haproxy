package main

import (
	"context"
	"log"

	"github.com/d3vi1/terraform-provider-pfsense-restapi-haproxy/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

var version = "dev"

func main() {
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/d3vi1/pfsense-restapi-haproxy",
	})
	if err != nil {
		log.Fatal(err)
	}
}
