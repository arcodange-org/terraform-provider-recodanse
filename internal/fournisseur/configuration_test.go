package fournisseur

// La configuration du fournisseur et ses sources de données, jouées par le
// protocole (tfprotov6) comme le binaire terraform les appelle : ConfigureProvider
// puis ReadDataSource sur le MÊME serveur, contre une doublure d'API. Aucune
// variable du poste n'entre : `env` est une carte, le fichier par défaut un
// chemin temporaire.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/essai"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/jeton"
)

type joueur struct {
	t       *testing.T
	serveur tfprotov6.ProviderServer
	schema  *tfprotov6.GetProviderSchemaResponse
}

func nouveauJoueur(t *testing.T, env map[string]string, parDefaut string) *joueur {
	t.Helper()
	f := &fournisseur{version: "essai", env: func(k string) string { return env[k] }, parDefaut: parDefaut}
	s, err := providerserver.NewProtocol6WithError(provider.Provider(f))()
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return &joueur{t: t, serveur: s, schema: r}
}

// valeurDe — un objet du type `typ`, ces attributs posés, les autres nuls.
func valeurDe(t *testing.T, typ tftypes.Object, attrs map[string]tftypes.Value) *tfprotov6.DynamicValue {
	t.Helper()
	vals := map[string]tftypes.Value{}
	for nom, at := range typ.AttributeTypes {
		if v, ok := attrs[nom]; ok {
			vals[nom] = v
		} else {
			vals[nom] = tftypes.NewValue(at, nil)
		}
	}
	dv, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, vals))
	if err != nil {
		t.Fatal(err)
	}
	return &dv
}

func messages(ds []*tfprotov6.Diagnostic, severite tfprotov6.DiagnosticSeverity) []string {
	var out []string
	for _, d := range ds {
		if d.Severity == severite {
			out = append(out, d.Summary+" : "+d.Detail)
		}
	}
	return out
}

// configurer — ConfigureProvider ; les erreurs ET les avertissements, en texte.
func (j *joueur) configurer(attrs map[string]tftypes.Value) (erreurs []string, tout string) {
	j.t.Helper()
	typ := j.schema.Provider.ValueType().(tftypes.Object)
	rep, err := j.serveur.ConfigureProvider(context.Background(), &tfprotov6.ConfigureProviderRequest{
		TerraformVersion: "1.10.6", Config: valeurDe(j.t, typ, attrs)})
	if err != nil {
		j.t.Fatal(err)
	}
	for _, d := range rep.Diagnostics {
		tout += d.Summary + " " + d.Detail + "\n"
	}
	return messages(rep.Diagnostics, tfprotov6.DiagnosticSeverityError), tout
}

// lire — ReadDataSource ; l'état rendu (nil sur une erreur) et les diagnostics.
func (j *joueur) lire(source string, attrs map[string]tftypes.Value) (map[string]tftypes.Value, []*tfprotov6.Diagnostic) {
	j.t.Helper()
	typ := j.schema.DataSourceSchemas[source].ValueType().(tftypes.Object)
	rep, err := j.serveur.ReadDataSource(context.Background(), &tfprotov6.ReadDataSourceRequest{
		TypeName: source, Config: valeurDe(j.t, typ, attrs)})
	if err != nil {
		j.t.Fatal(err)
	}
	if rep.State == nil {
		return nil, rep.Diagnostics
	}
	v, err := rep.State.Unmarshal(typ)
	if err != nil {
		j.t.Fatal(err)
	}
	var m map[string]tftypes.Value
	if err := v.As(&m); err != nil {
		j.t.Fatal(err)
	}
	return m, rep.Diagnostics
}

func texte(v tftypes.Value) string {
	var s string
	_ = v.As(&s)
	return s
}

func chaines_(t *testing.T, v tftypes.Value) []string {
	t.Helper()
	var l []tftypes.Value
	if err := v.As(&l); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range l {
		out = append(out, texte(e))
	}
	return out
}

func TestLeSchemaNommeSesTroisSourcesDeDonnees(t *testing.T) {
	j := nouveauJoueur(t, nil, "")
	for _, nom := range []string{"recodanse_catalogue", "recodanse_application", "recodanse_applications"} {
		if j.schema.DataSourceSchemas[nom] == nil {
			t.Errorf("source de données %s absente du schéma", nom)
		}
	}
	for _, a := range j.schema.DataSourceSchemas["recodanse_application"].Block.Attributes {
		if strings.Contains(a.Name, "secret") {
			t.Errorf("une application LUE ne porte aucun secret : %s", a.Name)
		}
		if a.Name == "conditions_en_vigueur" {
			t.Error("la version EN VIGUEUR ne se lit pas dans une source de données (accepter sans lire)")
		}
	}
	for _, a := range j.schema.Provider.Block.Attributes {
		if a.Name == "jeton" && !a.Sensitive {
			t.Error("l'attribut jeton doit être sensible")
		}
	}
}

func TestLeBacASableSansAdressesRefuseClairement(t *testing.T) {
	j := nouveauJoueur(t, map[string]string{"RECODANSE_JETON": "rdk_x"}, "")
	e, _ := j.configurer(map[string]tftypes.Value{"environnement": chaine("bac-a-sable")})
	if len(e) == 0 || !strings.Contains(e[0], "bac-a-sable") || !strings.Contains(e[0], "pas encore confirmées") {
		t.Fatalf("attendu un refus clair : %v", e)
	}
}

func TestSansJetonLeFournisseurDitCommentSeConnecter(t *testing.T) {
	j := nouveauJoueur(t, nil, filepath.Join(t.TempDir(), "absent"))
	e, _ := j.configurer(nil)
	if len(e) == 0 || !strings.Contains(e[0], "recodanse-connexion connexion") {
		t.Fatalf("%v", e)
	}
}

func TestJetonEtFichierEnsembleSontRefuses(t *testing.T) {
	j := nouveauJoueur(t, nil, "")
	e, tout := j.configurer(map[string]tftypes.Value{"jeton": chaine("rdk_secret_x"), "jeton_fichier": chaine("/tmp/x")})
	if len(e) == 0 || !strings.Contains(e[0], "l'un ou l'autre") || strings.Contains(tout, "rdk_secret_x") {
		t.Fatalf("%v", e)
	}
}

func TestApiEtAdresseEnsembleSontRefuses(t *testing.T) {
	j := nouveauJoueur(t, map[string]string{"RECODANSE_JETON": "rdk_x"}, "")
	e, _ := j.configurer(map[string]tftypes.Value{"api": chaine("https://a.example"), "adresse": chaine("https://b.example")})
	if len(e) == 0 || !strings.Contains(e[0], "ancien nom") {
		t.Fatalf("%v", e)
	}
}

func TestUnFichierDUnAutreEnvironnementNePartPasEtNeSAffichePas(t *testing.T) {
	f := filepath.Join(t.TempDir(), "jeton")
	if err := jeton.Ecrire(f, jeton.Rangement{Environnement: "qa", Emetteur: "https://connexion-qa.recodanse.fr",
		JetonDAcces: essai.JetonDAcces}); err != nil {
		t.Fatal(err)
	}
	// Le fichier par défaut, et le fournisseur en production (défaut).
	j := nouveauJoueur(t, nil, f)
	e, tout := j.configurer(nil)
	if len(e) == 0 || !strings.Contains(e[0], "ne change pas d'environnement") {
		t.Fatalf("%v", e)
	}
	if strings.Contains(tout, essai.JetonDAcces) {
		t.Fatalf("un diagnostic cite le jeton :\n%s", tout)
	}
	// Témoin : le même fichier, en qa, se lit (le refus venait bien de l'environnement).
	j = nouveauJoueur(t, map[string]string{"RECODANSE_ENVIRONNEMENT": "qa"}, f)
	if e, _ := j.configurer(nil); len(e) != 0 {
		t.Fatalf("témoin : en qa le fichier de la qa se lit : %v", e)
	}
}

// configureContre — un fournisseur configuré vers la doublure d'API, jeton OAuth en fichier.
func configureContre(t *testing.T, a *essai.API) *joueur {
	t.Helper()
	f := filepath.Join(t.TempDir(), "jeton")
	if err := os.WriteFile(f, []byte(essai.JetonDAcces+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := nouveauJoueur(t, map[string]string{"RECODANSE_ENVIRONNEMENT": "qa", "RECODANSE_JETON_FICHIER": f}, "")
	if e, _ := j.configurer(map[string]tftypes.Value{"api": chaine(a.URL())}); len(e) != 0 {
		t.Fatalf("configurer : %v", e)
	}
	return j
}

func toutesLesPortees() []string {
	vues := map[string]bool{}
	for _, l := range PorteesParCategorie {
		for _, p := range l {
			vues[p] = true
		}
	}
	for _, p := range PorteesDesAgents {
		vues[p] = true
	}
	var out []string
	for p := range vues {
		out = append(out, p)
	}
	return out
}

func TestLeCatalogueSeLitEtSeConfronteALAnnonceDeLAPI(t *testing.T) {
	a := essai.NouvelleAPI(t, essai.JetonDAcces)
	a.Annoncees = toutesLesPortees()
	j := configureContre(t, a)
	m, ds := j.lire("recodanse_catalogue", nil)
	if m == nil || len(messages(ds, tfprotov6.DiagnosticSeverityError)) != 0 {
		t.Fatalf("lecture : %v", ds)
	}
	if w := messages(ds, tfprotov6.DiagnosticSeverityWarning); len(w) != 0 {
		t.Fatalf("témoin : copie et annonce égales, aucun avertissement : %v", w)
	}
	if got := strings.Join(chaines_(t, m["especes"]), ","); got != "agent,connexion,service" {
		t.Fatalf("espèces : %s", got)
	}
	if got := strings.Join(chaines_(t, m["evenements_des_webhooks"]), ","); got != "autorisation.retiree,compte.efface,publication.retiree" {
		t.Fatalf("événements : %s", got)
	}
	var parEspece map[string]tftypes.Value
	if err := m["portees"].As(&parEspece); err != nil {
		t.Fatal(err)
	}
	if got := chaines_(t, parEspece["service"]); strings.Join(got, ",") != "evenements.publier,creneaux.publier" {
		t.Fatalf("portées du service : %v", got)
	}
	if v := a.Vues("/.well-known/oauth-protected-resource"); len(v) != 1 || v[0] != "" {
		t.Fatalf("la route publique ne reçoit AUCUN jeton : %q", v)
	}
}

func TestUnEcartAvecLAnnonceSAvertitEnNommantLaPortee(t *testing.T) {
	a := essai.NouvelleAPI(t, essai.JetonDAcces)
	a.Annoncees = append(toutesLesPortees(), "salles.publier")
	j := configureContre(t, a)
	_, ds := j.lire("recodanse_catalogue", nil)
	w := messages(ds, tfprotov6.DiagnosticSeverityWarning)
	if len(w) != 1 || !strings.Contains(w[0], "salles.publier") {
		t.Fatalf("attendu un avertissement qui nomme salles.publier : %v", w)
	}
}

func uneApplication() map[string]any {
	return map[string]any{"id": "appl_1", "client_id": "c-1", "nom": "Site du festival", "categorie": "connexion",
		"statut": "developpement", "portees": []string{"profile", "openid"}, "redirections": []string{"https://f.example/retour"},
		"publique": false, "cree_le": "2026-11-02T21:00:00Z", "client_secret": "NE-DOIT-PAS-ENTRER",
		"conditions": map[string]any{"version_acceptee": "2026-10-20", "a_jour": true}}
}

func TestUneApplicationSeLitParSonIdentifiant(t *testing.T) {
	a := essai.NouvelleAPI(t, essai.JetonDAcces)
	a.Applications = []map[string]any{uneApplication()}
	j := configureContre(t, a)
	m, ds := j.lire("recodanse_application", map[string]tftypes.Value{"id": chaine("appl_1")})
	if m == nil {
		t.Fatalf("%v", ds)
	}
	if texte(m["nom"]) != "Site du festival" || texte(m["conditions_acceptees"]) != "2026-10-20" ||
		strings.Join(chaines_(t, m["portees"]), ",") != "openid,profile" {
		t.Fatalf("lue : %v", m)
	}
	_, ds = j.lire("recodanse_application", map[string]tftypes.Value{"id": chaine("appl_absente")})
	if e := messages(ds, tfprotov6.DiagnosticSeverityError); len(e) == 0 || !strings.Contains(e[0], "introuvable") {
		t.Fatalf("une application absente : %v", e)
	}
}

func TestLesApplicationsSeLisentToutes(t *testing.T) {
	a := essai.NouvelleAPI(t, essai.JetonDAcces)
	deux := uneApplication()
	deux["id"], deux["nom"], deux["conditions"] = "appl_2", "Planning", map[string]any{"version_acceptee": nil, "a_jour": false}
	a.Applications = []map[string]any{uneApplication(), deux}
	j := configureContre(t, a)
	m, ds := j.lire("recodanse_applications", nil)
	if m == nil {
		t.Fatalf("%v", ds)
	}
	var l []tftypes.Value
	if err := m["applications"].As(&l); err != nil || len(l) != 2 {
		t.Fatalf("%v %v", l, err)
	}
	var second map[string]tftypes.Value
	_ = l[1].As(&second)
	if texte(second["id"]) != "appl_2" || !second["conditions_acceptees"].IsNull() {
		t.Fatalf("second : %v", second)
	}
	if v := a.Vues("/v1/applications"); len(v) != 1 || v[0] != "Bearer "+essai.JetonDAcces {
		t.Fatalf("le jeton du fichier part dans l'en-tête : %v", v)
	}
}

func TestUnJetonRefuseParLAPIEchoueALaLecture(t *testing.T) {
	a := essai.NouvelleAPI(t) // aucun jeton admis
	j := configureContre(t, a)
	_, ds := j.lire("recodanse_applications", nil)
	if e := messages(ds, tfprotov6.DiagnosticSeverityError); len(e) == 0 || !strings.Contains(e[0], "jeton_invalide") ||
		strings.Contains(e[0], essai.JetonDAcces) {
		t.Fatalf("%v", e)
	}
}
