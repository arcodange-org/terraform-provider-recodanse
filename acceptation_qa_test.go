package main

// acceptation_qa_test.go — LE FOURNISSEUR CONTRE LA QA DÉPLOYÉE
// (https://api-qa.recodanse.fr, émetteur https://connexion-qa.recodanse.fr),
// par un vrai binaire (OpenTofu ou Terraform), avec l'environnement « qa » —
// aucune adresse écrite à la main.
//
//	RECODANSE_ACCEPTATION_QA=1 RECODANSE_JETON=… \
//	  [RECODANSE_ESSAI_BINAIRES=/opt/homebrew/bin/tofu] \
//	  go test -count=1 -run TestLeFournisseurContreLaQA -v .
//
// Le jeton : un jeton de développeur `rdk_…` d'un COMPTE DE TEST de la QA
// (jamais le compte d'une vraie personne), ou le jeton d'accès qu'écrit
// `recodanse-connexion connexion --environnement qa` (la connexion OAuth device
// code n'est pas encore servie par la QA).
//
// Ce qu'il fait — et défait, quoi qu'il arrive : déclarer une application de
// TEST « Fixture fournisseur <horodatage> », la relire par la source de données,
// faire tourner son secret, poser un webhook, lire le catalogue, tout détruire,
// puis vérifier que l'API ne connaît plus l'application.
//
// RÉPÉTITION LOCALE : avec RECODANSE_ACCEPTATION_QA_REPETITION=<API locale, comptes
// de démonstration ouverts> (README, § essai local), le MÊME scénario se joue contre
// cette API — le jeton vient alors d'un compte de démonstration (un rdk_, révoqué à la fin) et
// RECODANSE_API y mène ; l'environnement reste « qa » dans la configuration.
//
// ⚠ SANS RECODANSE_ACCEPTATION_QA=1, IL SE SAUTE, ET LE DIT (saut déclaré : il
//   parle à un environnement DÉPLOYÉ, il ne joue pas en CI). Avec la variable
//   mais sans jeton, il ÉCHOUE : un essai demandé qui n'a pas lieu n'est pas un vert.

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const configurationQA = `
provider "recodanse" {
  environnement = "qa"
}

resource "recodanse_application" "fixture" {
  nom          = "Fixture fournisseur %[1]s"
  description  = "Application de TEST du fournisseur Terraform : déclarée, relue puis détruite par l'essai d'acceptation."
  categorie    = "connexion"
  portees      = ["openid"]
  redirections = ["https://example.com/recodanse/retour"]
  rotation     = { quand = "%[2]s" }
}

resource "recodanse_webhook" "fixture" {
  application = recodanse_application.fixture.id
  url         = "https://example.com/webhooks/recodanse"
  evenements  = ["autorisation.retiree", "compte.efface"]
}

data "recodanse_application" "lue" {
  id = recodanse_application.fixture.id
}

data "recodanse_catalogue" "c" {}

output "id"      { value = recodanse_application.fixture.id }
output "nom_lu"  { value = data.recodanse_application.lue.nom }
output "statut"  { value = data.recodanse_application.lue.statut }
output "especes" { value = join(",", data.recodanse_catalogue.c.especes) }
output "secret" {
  value     = recodanse_application.fixture.client_secret
  sensitive = true
}
output "webhook_secret" {
  value     = recodanse_webhook.fixture.secret
  sensitive = true
}
`

func binairesQA(t *testing.T) []string {
	var b []string
	for _, x := range strings.Split(os.Getenv("RECODANSE_ESSAI_BINAIRES"), ",") {
		if x = strings.TrimSpace(x); x != "" {
			b = append(b, x)
		}
	}
	if len(b) == 0 {
		for _, nom := range []string{"tofu", "terraform"} {
			if p, err := exec.LookPath(nom); err == nil {
				b = append(b, p)
			}
		}
	}
	if len(b) == 0 {
		t.Fatal("RECODANSE_ACCEPTATION_QA=1 mais ni tofu ni terraform : l'essai n'a PAS eu lieu")
	}
	return b
}

func TestLeFournisseurContreLaQA(t *testing.T) {
	if os.Getenv("RECODANSE_ACCEPTATION_QA") != "1" {
		t.Skip("essai contre la QA sauté : RECODANSE_ACCEPTATION_QA=1 et RECODANSE_JETON absents (saut déclaré, README § « Ce que l'essai prouve »)")
	}
	api, repetition := "https://api-qa.recodanse.fr", os.Getenv("RECODANSE_ACCEPTATION_QA_REPETITION")
	jeton := os.Getenv("RECODANSE_JETON")
	if repetition != "" {
		api = repetition
		var revoquer func()
		jeton, revoquer = jetonDuPersona(t, api, "elodie")
		t.Cleanup(revoquer)
	}
	if jeton == "" {
		t.Fatal("RECODANSE_ACCEPTATION_QA=1 mais RECODANSE_JETON manque : l'essai n'a PAS eu lieu")
	}
	fournisseur := construireLeFournisseur(t)
	for _, binaire := range binairesQA(t) {
		t.Run(filepath.Base(binaire), func(t *testing.T) {
			e := nouvelEssai(t, binaire, fournisseur, api, "https://connexion-qa.recodanse.fr", jeton)
			// L'environnement seul doit mener à la QA : aucune adresse dans l'environnement du binaire.
			garde := e.env[:0]
			if repetition != "" {
				garde = append(garde, "RECODANSE_API="+repetition)
			}
			for _, v := range e.env {
				if !strings.HasPrefix(v, "RECODANSE_API=") && !strings.HasPrefix(v, "RECODANSE_EMETTEUR=") &&
					!strings.HasPrefix(v, "RECODANSE_ENVIRONNEMENT=") {
					garde = append(garde, v)
				}
			}
			e.env = garde
			horodatage := time.Now().UTC().Format("20060102-150405") + "-" + filepath.Base(binaire)
			ecrireQA := func(rotation string) {
				tete := fmt.Sprintf("terraform {\n  required_providers {\n    recodanse = { source = %q }\n  }\n}\n", Adresse)
				if err := os.WriteFile(filepath.Join(e.dossier, "main.tf"),
					[]byte(tete+fmt.Sprintf(configurationQA, horodatage, rotation)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				if code, s := e.lancer("destroy", "-auto-approve", "-input=false", "-no-color"); code != 0 {
					t.Errorf("⚠ LA FIXTURE A PU SURVIVRE (destroy → %d) : retire « Fixture fournisseur %s » dans « Mes applications »\n%s", code, horodatage, s)
				}
			})

			// 1. Déclarer, relire par la source de données, poser le webhook.
			ecrireQA("1")
			e.appliquer()
			o := e.sorties()
			if !strings.HasPrefix(o["id"], "appl_") || o["secret"] == "" || o["nom_lu"] != "Fixture fournisseur "+horodatage {
				t.Fatalf("après la déclaration : id %q, nom lu %q, secret présent %v", o["id"], o["nom_lu"], o["secret"] != "")
			}
			if o["statut"] != "developpement" || o["especes"] != "agent,connexion,service" {
				t.Fatalf("statut %q, espèces %q", o["statut"], o["especes"])
			}
			if !strings.HasPrefix(o["webhook_secret"], "whsec_") {
				t.Fatal("le webhook n'a pas rendu sa clé whsec_")
			}
			e.rienAChanger("après la déclaration")

			// 2. Faire tourner le secret.
			ancien := o["secret"]
			ecrireQA("2")
			e.appliquer()
			if o = e.sorties(); o["secret"] == ancien || o["secret"] == "" {
				t.Fatal("la rotation n'a pas changé le secret")
			}
			e.rienAChanger("après la rotation")

			// 3. Tout détruire : l'API ne connaît plus l'application.
			e.reussir("destroy", "-auto-approve", "-input=false", "-no-color")
			if code, _ := e.lireLApplication(o["id"]); code != http.StatusNotFound {
				t.Fatalf("%s survit à la destruction : %d", o["id"], code)
			}
		})
	}
}
