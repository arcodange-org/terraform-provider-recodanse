package main

// acceptation_test.go — LE FOURNISSEUR, JOUÉ PAR DE VRAIS BINAIRES (Terraform ET
// OpenTofu) contre une VRAIE API et un VRAI Hydra.
//
// Un fournisseur se teste mal par ses fonctions : ce qui casse, c'est le
// dialogue plan → apply → relecture que seul le binaire mène (un attribut
// calculé qui change sans être annoncé fait échouer l'`apply` avec « Provider
// produced inconsistent result », que rien d'autre ne voit).
//
//	# l'API ReCoDanse en mémoire, comptes de démonstration ouverts, reliée à
//	# Hydra sur 4444/4445 (README, § essai local) :
//	RECODANSE_ESSAI_API=http://127.0.0.1:8481 \
//	RECODANSE_ESSAI_HYDRA=http://127.0.0.1:4444 \
//	RECODANSE_ESSAI_BINAIRES=/chemin/terraform,/chemin/tofu \
//	RECODANSE_ESSAI_OBLIGATOIRE=1 go test -count=1 -run TestLeFournisseur .
//
// ⚠ SANS CES VARIABLES, LE TEST SE SAUTE — sauf avec RECODANSE_ESSAI_OBLIGATOIRE=1,
//   qui le fait ÉCHOUER : un saut silencieux est la forme locale du défaut que
//   tout ce dépôt traque.
//
// Sabotages relevés : voir le README, § « Ce que les essais prouvent ».

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type essai struct {
	t       *testing.T
	binaire string
	dossier string
	env     []string
	api     string
	hydra   string
	jeton   string
}

func variablesDEssai(t *testing.T) (api, hydra string, binaires []string) {
	api, hydra = os.Getenv("RECODANSE_ESSAI_API"), os.Getenv("RECODANSE_ESSAI_HYDRA")
	for _, b := range strings.Split(os.Getenv("RECODANSE_ESSAI_BINAIRES"), ",") {
		if b = strings.TrimSpace(b); b != "" {
			binaires = append(binaires, b)
		}
	}
	if api == "" || hydra == "" || len(binaires) == 0 {
		if os.Getenv("RECODANSE_ESSAI_OBLIGATOIRE") == "1" {
			t.Fatal("RECODANSE_ESSAI_OBLIGATOIRE=1 mais RECODANSE_ESSAI_API, RECODANSE_ESSAI_HYDRA ou RECODANSE_ESSAI_BINAIRES manque : l'essai n'a PAS eu lieu")
		}
		t.Skip("essai du fournisseur sauté : RECODANSE_ESSAI_API, RECODANSE_ESSAI_HYDRA et RECODANSE_ESSAI_BINAIRES absents")
	}
	return api, hydra, binaires
}

// jetonDuPersona — se connecter comme un compte de démonstration (route
// `/v0/recette/session/<persona>` de l'API locale), puis un jeton de développeur créé
// comme l'écran le crée (POST /v0/tiers/jetons-de-developpeur).
func jetonDuPersona(t *testing.T, api, persona string) (string, func()) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	rep, err := hc.Post(api+"/v0/recette/session/"+persona, "application/json", nil)
	if err != nil || rep.StatusCode != http.StatusOK {
		t.Fatalf("connexion du compte de démonstration %s : %v %v (comptes de démonstration ouverts sur l'API locale ?)", persona, err, rep)
	}
	_ = rep.Body.Close()
	corps := fmt.Sprintf(`{"nom":"essai du fournisseur %d","jours":30}`, time.Now().UnixNano())
	rep, err = hc.Post(api+"/v0/tiers/jetons-de-developpeur", "application/json", strings.NewReader(corps))
	if err != nil {
		t.Fatal(err)
	}
	defer rep.Body.Close()
	var j struct {
		ID    string `json:"id"`
		Jeton string `json:"jeton"`
		Error string `json:"error"`
	}
	_ = json.NewDecoder(rep.Body).Decode(&j)
	if rep.StatusCode != http.StatusCreated || !strings.HasPrefix(j.Jeton, "rdk_") {
		t.Fatalf("créer le jeton : %d %s", rep.StatusCode, j.Error)
	}
	// Révoquer, comme l'écran le fait (par cookie). L'essai le joue aussi à la fin,
	// quoi qu'il arrive : cinq jetons actifs au plus par compte.
	revoquer := func() {
		req, _ := http.NewRequest(http.MethodDelete, api+"/v0/tiers/jetons-de-developpeur/"+j.ID, nil)
		if rep, err := hc.Do(req); err == nil {
			_ = rep.Body.Close()
		}
	}
	return j.Jeton, revoquer
}

func construireLeFournisseur(t *testing.T) string {
	t.Helper()
	dossier := t.TempDir()
	cmd := exec.Command("go", "build", "-o", filepath.Join(dossier, "terraform-provider-recodanse"), ".")
	if sortie, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("construire le fournisseur : %v\n%s", err, sortie)
	}
	return dossier
}

func nouvelEssai(t *testing.T, binaire, fournisseur, api, hydra, jeton string) *essai {
	dossier := t.TempDir()
	cli := filepath.Join(dossier, "cli.tfrc")
	conf := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    %q = %q\n  }\n  direct {}\n}\n", Adresse, fournisseur)
	if err := os.WriteFile(cli, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "TF_CLI_CONFIG_FILE="+cli, "TF_IN_AUTOMATION=1", "CHECKPOINT_DISABLE=1",
		"RECODANSE_API="+api, "RECODANSE_JETON="+jeton)
	return &essai{t: t, binaire: binaire, dossier: dossier, env: env, api: api, hydra: hydra, jeton: jeton}
}

// changerDeJeton — la suite de l'essai présente ce jeton-là (au fournisseur et aux relectures).
func (e *essai) changerDeJeton(jeton string) {
	e.jeton = jeton
	for i, v := range e.env {
		if strings.HasPrefix(v, "RECODANSE_JETON=") {
			e.env[i] = "RECODANSE_JETON=" + jeton
		}
	}
}

func (e *essai) ecrire(hcl string) {
	e.t.Helper()
	tete := fmt.Sprintf("terraform {\n  required_providers {\n    recodanse = { source = %q }\n  }\n}\nprovider \"recodanse\" {}\n", Adresse)
	if err := os.WriteFile(filepath.Join(e.dossier, "main.tf"), []byte(tete+hcl), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

// lancer — la commande, son code de sortie et sa sortie complète (jamais tronquée).
func (e *essai) lancer(args ...string) (int, string) {
	e.t.Helper()
	cmd := exec.Command(e.binaire, args...)
	cmd.Dir, cmd.Env = e.dossier, e.env
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	code := 0
	if x, ok := err.(*exec.ExitError); ok {
		code = x.ExitCode()
	} else if err != nil {
		e.t.Fatalf("%s %v : %v", e.binaire, args, err)
	}
	return code, b.String()
}

func (e *essai) reussir(args ...string) string {
	e.t.Helper()
	code, s := e.lancer(args...)
	if code != 0 {
		e.t.Fatalf("%s %s → %d\n%s", filepath.Base(e.binaire), strings.Join(args, " "), code, s)
	}
	return s
}

func (e *essai) appliquer() {
	e.t.Helper()
	e.reussir("apply", "-auto-approve", "-input=false", "-no-color")
}

// rienAChanger — `plan -detailed-exitcode` rend 0 : le fournisseur relit ce qu'il a écrit.
func (e *essai) rienAChanger(etape string) {
	e.t.Helper()
	if code, s := e.lancer("plan", "-detailed-exitcode", "-input=false", "-no-color"); code != 0 {
		e.t.Fatalf("%s : le plan d'après devait être vide (code %d)\n%s", etape, code, s)
	}
}

func (e *essai) sorties() map[string]string {
	e.t.Helper()
	var brut map[string]struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal([]byte(e.reussir("output", "-json")), &brut); err != nil {
		e.t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range brut {
		out[k] = fmt.Sprint(v.Value)
	}
	return out
}

// lireLApplication — ce que l'API dit, relu SANS le fournisseur.
func (e *essai) lireLApplication(id string) (int, map[string]any) {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, e.api+"/v1/applications/"+id, nil)
	req.Header.Set("Authorization", "Bearer "+e.jeton)
	rep, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rep.Body.Close()
	var a map[string]any
	_ = json.NewDecoder(rep.Body).Decode(&a)
	return rep.StatusCode, a
}

// jetonDeService — Hydra délivre-t-il un jeton à ce couple ? (la preuve d'une rotation)
func (e *essai) jetonDeService(clientID, secret string) int {
	e.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.hydra+"/oauth2/token",
		strings.NewReader(url.Values{"grant_type": {"client_credentials"}, "scope": {"evenements.publier"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(url.QueryEscape(clientID)+":"+url.QueryEscape(secret))))
	rep, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	_ = rep.Body.Close()
	return rep.StatusCode
}

func conditionsEnVigueur(t *testing.T, api, jeton string) string {
	req, _ := http.NewRequest(http.MethodGet, api+"/v1/applications", nil)
	req.Header.Set("Authorization", "Bearer "+jeton)
	rep, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer rep.Body.Close()
	var l struct {
		Version string `json:"conditions_en_vigueur"`
	}
	_ = json.NewDecoder(rep.Body).Decode(&l)
	if l.Version == "" {
		t.Fatal("l'API ne dit pas la version des conditions en vigueur")
	}
	return l.Version
}

const configuration = `
resource "recodanse_application" "studio" {
  nom       = "Planning du studio %[1]s"
  categorie = "service"
  portees   = [%[2]s]
  rotation  = { quand = "%[3]s" }
  soumettre = %[4]s
  %[5]s
}

resource "recodanse_application" "site" {
  nom          = "Site du festival %[1]s"
  categorie    = "connexion"
  portees      = ["openid", "profile"]
  redirections = ["https://festival.example/retour", "http://127.0.0.1:8080/retour"]
  publique     = true
}

resource "recodanse_webhook" "site" {
  application = recodanse_application.site.id
  url         = "https://festival.example/webhooks/recodanse"
  evenements  = ["autorisation.retiree", "compte.efface"]
}

data "recodanse_application" "studio" {
  id = recodanse_application.studio.id
}

data "recodanse_applications" "toutes" {
  depends_on = [recodanse_application.studio, recodanse_application.site]
}

data "recodanse_catalogue" "c" {}

output "studio_id"      { value = recodanse_application.studio.id }
output "studio_nom_lu"  { value = data.recodanse_application.studio.nom }
output "nombre_lues"    { value = length(data.recodanse_applications.toutes.applications) }
output "especes"        { value = join(",", data.recodanse_catalogue.c.especes) }
output "studio_client"  { value = recodanse_application.studio.client_id }
output "studio_secret" {
  value     = recodanse_application.studio.client_secret
  sensitive = true
}
output "studio_statut"  { value = recodanse_application.studio.statut }
output "site_id"        { value = recodanse_application.site.id }
output "site_secret" {
  value     = coalesce(recodanse_application.site.client_secret, "nul")
  sensitive = true
}
output "webhook_secret" {
  value     = recodanse_webhook.site.secret
  sensitive = true
}
`

func (e *essai) config(suffixe, portees, rotation, soumettre, acceptation string) {
	e.ecrire(fmt.Sprintf(configuration, suffixe, portees, rotation, soumettre, acceptation))
}

func TestLeFournisseurJoueParTerraformEtOpenTofu(t *testing.T) {
	api, hydra, binaires := variablesDEssai(t)
	fournisseur := construireLeFournisseur(t)
	personas := []string{"lea", "karim", "elodie"}
	for i, binaire := range binaires {
		binaire, persona := binaire, personas[i%len(personas)]
		t.Run(filepath.Base(binaire), func(t *testing.T) {
			jeton, revoquer := jetonDuPersona(t, api, persona)
			e := nouvelEssai(t, binaire, fournisseur, api, hydra, jeton)
			// Un essai raté ne laisse rien derrière lui (dix applications, cinq jetons au
			// plus par compte) : détruire avec le jeton COURANT, puis révoquer.
			aRevoquer := []func(){revoquer}
			t.Cleanup(func() {
				e.lancer("destroy", "-auto-approve", "-input=false", "-no-color")
				for _, r := range aRevoquer {
					r()
				}
			})
			s := filepath.Base(binaire)

			// 1. Déclarer : un compte de service (secret) et une connexion publique (sans secret), un webhook.
			e.config(s, `"evenements.publier"`, "1", "false", "")
			e.appliquer()
			o := e.sorties()
			if !strings.HasPrefix(o["studio_id"], "appl_") || o["studio_secret"] == "" || o["studio_statut"] != "developpement" {
				t.Fatalf("après la déclaration : %v", o)
			}
			if o["site_secret"] != "nul" {
				t.Fatalf("un client public n'a pas de secret : %q", o["site_secret"])
			}
			if !strings.HasPrefix(o["webhook_secret"], "whsec_") {
				t.Fatalf("le secret du webhook : %q", o["webhook_secret"])
			}
			// Les sources de données : relire par l'identifiant, lister, le catalogue.
			if o["studio_nom_lu"] != "Planning du studio "+s || o["nombre_lues"] < "2" || o["especes"] != "agent,connexion,service" {
				t.Fatalf("sources de données : nom lu %q, lues %q, espèces %q", o["studio_nom_lu"], o["nombre_lues"], o["especes"])
			}
			if code := e.jetonDeService(o["studio_client"], o["studio_secret"]); code != http.StatusOK {
				t.Fatalf("Hydra refuse le secret tout neuf : %d", code)
			}
			e.rienAChanger("après la déclaration")

			// 2. La rotation : l'ancien secret cesse de valoir, le nouveau vaut.
			ancien := o["studio_secret"]
			e.config(s, `"evenements.publier"`, "2", "false", "")
			e.appliquer()
			o = e.sorties()
			if o["studio_secret"] == ancien || o["studio_secret"] == "" {
				t.Fatal("la rotation n'a pas changé le secret")
			}
			if code := e.jetonDeService(o["studio_client"], ancien); code == http.StatusOK {
				t.Fatal("l'ancien secret vaut encore après la rotation")
			}
			if code := e.jetonDeService(o["studio_client"], o["studio_secret"]); code != http.StatusOK {
				t.Fatalf("le nouveau secret ne vaut pas : %d", code)
			}
			e.rienAChanger("après la rotation")

			// 3. Une portée de plus, demandée par le code : l'API la porte.
			e.config(s, `"evenements.publier", "creneaux.publier"`, "2", "false", "")
			e.appliquer()
			if _, a := e.lireLApplication(o["studio_id"]); fmt.Sprint(a["portees"]) != "[creneaux.publier evenements.publier]" {
				t.Fatalf("portées relues par l'API : %v", a["portees"])
			}
			if o2 := e.sorties(); o2["studio_secret"] != o["studio_secret"] {
				t.Fatal("corriger les portées a changé le secret")
			}
			e.rienAChanger("après les portées")

			// 4. Une portée hors de sa catégorie : refusée AU PLAN, sans appel.
			e.config(s, `"evenements.publier", "videos.lire"`, "2", "false", "")
			if code, sortie := e.lancer("plan", "-input=false", "-no-color"); code == 0 || !strings.Contains(sortie, "videos.lire") {
				t.Fatalf("une portée interdite devait être refusée au plan (code %d)\n%s", code, sortie)
			}

			// 5. Soumettre : une version périmée des conditions est refusée au plan,
			//    et le message donne celle en vigueur ; la bonne fait passer en revue.
			version := conditionsEnVigueur(t, api, jeton)
			e.config(s, `"evenements.publier", "creneaux.publier"`, "2", "true", `accepte_les_conditions = "1999-01"`)
			if code, sortie := e.lancer("plan", "-input=false", "-no-color"); code == 0 || !strings.Contains(sortie, version) {
				t.Fatalf("une version périmée devait être refusée au plan en nommant %q (code %d)\n%s", version, code, sortie)
			}
			e.config(s, `"evenements.publier", "creneaux.publier"`, "2", "true", fmt.Sprintf("accepte_les_conditions = %q", version))
			e.appliquer()
			if o = e.sorties(); o["studio_statut"] != "en_revue" {
				t.Fatalf("après la soumission : %v", o["studio_statut"])
			}
			e.rienAChanger("après la soumission")

			// 6. L'import : l'application connexion, oubliée puis réimportée, ne demande rien.
			e.reussir("state", "rm", "recodanse_application.site")
			e.reussir("import", "-input=false", "-no-color", "recodanse_application.site", o["site_id"])
			e.rienAChanger("après l'import")

			// 7. Révoquer le jeton : le plan suivant est refusé (il relit l'API).
			revoquer()
			if code, sortie := e.lancer("plan", "-input=false", "-no-color"); code == 0 || !strings.Contains(sortie, "jeton_invalide") {
				t.Fatalf("après la révocation, le plan devait être refusé (jeton_invalide), code %d\n%s", code, sortie)
			}
			neuf, revoquerLeNeuf := jetonDuPersona(t, api, persona)
			aRevoquer = append(aRevoquer, revoquerLeNeuf)
			e.changerDeJeton(neuf)

			// 8. Détruire : l'API ne connaît plus ni l'une ni l'autre.
			e.reussir("destroy", "-auto-approve", "-input=false", "-no-color")
			for _, id := range []string{o["studio_id"], o["site_id"]} {
				if code, _ := e.lireLApplication(id); code != http.StatusNotFound {
					t.Fatalf("%s survit à la destruction : %d", id, code)
				}
			}
		})
	}
}
