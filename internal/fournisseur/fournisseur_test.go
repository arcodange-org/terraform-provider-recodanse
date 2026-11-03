package fournisseur

// Ce que le fournisseur refuse AVANT d'appeler l'API, joué par le protocole lui-même
// (tfprotov6, comme le binaire terraform l'appelle) — la moitié de l'essai à vrais
// binaires (`acceptation_test.go`) qui tourne aussi en CI, où ni Terraform ni
// OpenTofu ne sont installés.
//
// Sabotages relevés (`go test ./internal/fournisseur/`, paquet compilable) :
//   - la vérification de catégorie retirée de ValidateConfig (`if false && …`) →
//     « une portée de service sur une connexion » rougit ;
//   - le refus d'une rotation sur un client public retiré → « une rotation sans
//     secret » rougit ;
//   - `accepte_les_conditions` plus exigé pour soumettre → « soumettre sans les
//     conditions » rougit.

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func serveur(t *testing.T) (tfprotov6.ProviderServer, *tfprotov6.GetProviderSchemaResponse) {
	t.Helper()
	s, err := providerserver.NewProtocol6WithError(Nouveau("essai")())()
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Diagnostics {
		t.Errorf("schéma : %s — %s", d.Summary, d.Detail)
	}
	return s, r
}

func TestLeSchemaSeChargeEtNommeSesDeuxRessources(t *testing.T) {
	_, r := serveur(t)
	for _, nom := range []string{"recodanse_application", "recodanse_webhook"} {
		if r.ResourceSchemas[nom] == nil {
			t.Errorf("ressource %s absente du schéma", nom)
		}
	}
	for nom, attr := range map[string]string{"recodanse_application": "client_secret", "recodanse_webhook": "secret"} {
		for _, a := range r.ResourceSchemas[nom].Block.Attributes {
			if a.Name == attr && !a.Sensitive {
				t.Errorf("%s.%s doit être sensible", nom, attr)
			}
		}
	}
}

// valider — ValidateResourceConfig de recodanse_application avec ces attributs
// (les autres nuls), et les résumés des diagnostics rendus.
func valider(t *testing.T, attrs map[string]tftypes.Value) []string {
	t.Helper()
	s, r := serveur(t)
	typ := r.ResourceSchemas["recodanse_application"].ValueType().(tftypes.Object)
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
	rep, err := s.ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{TypeName: "recodanse_application", Config: &dv})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range rep.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			out = append(out, d.Summary+" : "+d.Detail)
		}
	}
	return out
}

func chaine(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }

func ensemble(vs ...string) tftypes.Value {
	var l []tftypes.Value
	for _, v := range vs {
		l = append(l, chaine(v))
	}
	return tftypes.NewValue(tftypes.Set{ElementType: tftypes.String}, l)
}

func connexion(extra map[string]tftypes.Value) map[string]tftypes.Value {
	m := map[string]tftypes.Value{
		"nom": chaine("Site du festival"), "categorie": chaine("connexion"),
		"portees": ensemble("openid", "profile"), "redirections": ensemble("https://festival.example/retour"),
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func contient(erreurs []string, mot string) bool {
	for _, e := range erreurs {
		if strings.Contains(e, mot) {
			return true
		}
	}
	return false
}

func TestUneConfigurationJusteNeRendAucuneErreur(t *testing.T) {
	if e := valider(t, connexion(nil)); len(e) != 0 {
		t.Fatalf("témoin : une connexion juste est refusée : %v", e)
	}
}

func TestUnePorteeDeServiceSurUneConnexionEstRefuseeAuPlan(t *testing.T) {
	e := valider(t, connexion(map[string]tftypes.Value{"portees": ensemble("openid", "creneaux.publier")}))
	if !contient(e, "creneaux.publier") {
		t.Fatalf("la portée hors catégorie devait être refusée en la nommant : %v", e)
	}
}

func TestUneRotationSansSecretEstRefusee(t *testing.T) {
	e := valider(t, connexion(map[string]tftypes.Value{
		"publique": tftypes.NewValue(tftypes.Bool, true),
		"rotation": tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, map[string]tftypes.Value{"quand": chaine("1")}),
	}))
	if !contient(e, "Rotation sans objet") {
		t.Fatalf("une rotation sur un client public devait être refusée : %v", e)
	}
}

func TestSoumettreSansLesConditionsEstRefuse(t *testing.T) {
	e := valider(t, connexion(map[string]tftypes.Value{"soumettre": tftypes.NewValue(tftypes.Bool, true)}))
	if !contient(e, "Conditions des développeurs") {
		t.Fatalf("soumettre sans accepter les conditions devait être refusé : %v", e)
	}
}

func TestUnServiceNAPasDAdresseDeRetour(t *testing.T) {
	e := valider(t, map[string]tftypes.Value{
		"nom": chaine("Planning du studio"), "categorie": chaine("service"),
		"portees": ensemble("evenements.publier"), "redirections": ensemble("https://studio.example/retour"),
	})
	if !contient(e, "Adresses de retour inutiles") {
		t.Fatalf("un service avec adresses de retour devait être refusé : %v", e)
	}
}
