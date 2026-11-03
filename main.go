// Le fournisseur Terraform / OpenTofu de ReCoDanse :
// les applications OpenID et les webhooks d'un compte développeur, pilotés par un
// jeton de développeur. Voir README.md.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/fournisseur"
)

// version — posée à la construction (`-ldflags "-X main.version=0.1.0"`).
var version = "dev"

// Adresse — la source du fournisseur, telle qu'un `required_providers` la nomme.
// L'hôte est celui du portail des développeurs : c'est lui qui servira le
// protocole de registre le jour où le fournisseur sera publié (README).
const Adresse = "registry.terraform.io/arcodange-org/recodanse"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "lancer le fournisseur pour un débogueur (delve)")
	flag.Parse()
	err := providerserver.Serve(context.Background(), fournisseur.Nouveau(version), providerserver.ServeOpts{
		Address: Adresse,
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
