package fournisseur

// Le bac à sable joué par le protocole contre la doublure d'API, sur le contrat
// SUPPOSÉ `/v1/bac-a-sable/*` (README, « Le bac à sable »). Ce qui compte :
// créer DÉCLENCHE une fois, détruire n'appelle RIEN, et hors du bac à sable
// tout se refuse au plan, sans un appel.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/essai"
)

// auBacASable — un fournisseur configuré « bac-a-sable », adresses données.
func auBacASable(t *testing.T, a *essai.API, environnement string) *joueur {
	t.Helper()
	f := filepath.Join(t.TempDir(), "jeton")
	if err := os.WriteFile(f, []byte("rdk_du_bac_a_sable"), 0o600); err != nil {
		t.Fatal(err)
	}
	j := nouveauJoueur(t, map[string]string{"RECODANSE_JETON_FICHIER": f}, "")
	if e, _ := j.configurer(map[string]tftypes.Value{"environnement": chaine(environnement), "api": chaine(a.URL()),
		"emetteur": chaine("https://connexion.bac.example")}); len(e) != 0 {
		t.Fatalf("configurer : %v", e)
	}
	return j
}

func TestLesComptesFictifsSeLisentAuBacASable(t *testing.T) {
	a := essai.NouvelleAPI(t, "rdk_du_bac_a_sable")
	a.BacASable = true
	a.ComptesFictifs = []map[string]string{{"cle": "lea", "prenom": "Léa", "titre": "Danseuse de salsa"}}
	j := auBacASable(t, a, "bac-a-sable")
	m, ds := j.lire("recodanse_comptes_fictifs", nil)
	if m == nil {
		t.Fatalf("%v", ds)
	}
	var l []tftypes.Value
	if err := m["comptes"].As(&l); err != nil || len(l) != 1 {
		t.Fatalf("%v %v", l, err)
	}
	var c map[string]tftypes.Value
	_ = l[0].As(&c)
	if texte(c["cle"]) != "lea" || texte(c["prenom"]) != "Léa" {
		t.Fatalf("%v", c)
	}
}

func TestHorsDuBacASableLesComptesFictifsSeRefusentSansAppel(t *testing.T) {
	a := essai.NouvelleAPI(t, "rdk_du_bac_a_sable")
	a.BacASable = true
	j := auBacASable(t, a, "qa")
	_, ds := j.lire("recodanse_comptes_fictifs", nil)
	if e := messages(ds, tfprotov6.DiagnosticSeverityError); len(e) == 0 || !strings.Contains(e[0], "Bac à sable seulement") {
		t.Fatalf("%v", e)
	}
	if v := a.Vues("/v1/bac-a-sable/comptes"); len(v) != 0 {
		t.Fatalf("aucun appel hors du bac à sable : %v", v)
	}
}

func TestUneAPISansBacASableLeDit(t *testing.T) {
	a := essai.NouvelleAPI(t, "rdk_du_bac_a_sable") // BacASable faux : 404
	j := auBacASable(t, a, "bac-a-sable")
	_, ds := j.lire("recodanse_comptes_fictifs", nil)
	if e := messages(ds, tfprotov6.DiagnosticSeverityError); len(e) == 0 || !strings.Contains(e[0], "pas de bac à sable actif") {
		t.Fatalf("%v", e)
	}
}

const typeEvenement = "recodanse_evenement_du_bac_a_sable"

func (j *joueur) typeRessource(nom string) tftypes.Object {
	return j.schema.ResourceSchemas[nom].ValueType().(tftypes.Object)
}

// planifierEvenement — PlanResourceChange d'une création ; les erreurs.
func (j *joueur) planifierEvenement(attrs map[string]tftypes.Value) []string {
	j.t.Helper()
	typ := j.typeRessource(typeEvenement)
	prior, _ := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, nil))
	conf := valeurDe(j.t, typ, attrs)
	rep, err := j.serveur.PlanResourceChange(context.Background(), &tfprotov6.PlanResourceChangeRequest{
		TypeName: typeEvenement, PriorState: &prior, ProposedNewState: conf, Config: conf})
	if err != nil {
		j.t.Fatal(err)
	}
	return messages(rep.Diagnostics, tfprotov6.DiagnosticSeverityError)
}

// creerEvenement — ApplyResourceChange d'une création (calculés inconnus).
func (j *joueur) creerEvenement(attrs map[string]tftypes.Value) (map[string]tftypes.Value, []string) {
	j.t.Helper()
	typ := j.typeRessource(typeEvenement)
	vals := map[string]tftypes.Value{}
	for k, v := range attrs {
		vals[k] = v
	}
	for _, k := range []string{"id", "sujet", "declenche_le"} {
		vals[k] = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
	}
	prior, _ := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, nil))
	rep, err := j.serveur.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: typeEvenement, PriorState: &prior, PlannedState: valeurDe(j.t, typ, vals), Config: valeurDe(j.t, typ, attrs)})
	if err != nil {
		j.t.Fatal(err)
	}
	if e := messages(rep.Diagnostics, tfprotov6.DiagnosticSeverityError); len(e) != 0 {
		return nil, e
	}
	v, _ := rep.NewState.Unmarshal(typ)
	var m map[string]tftypes.Value
	_ = v.As(&m)
	return m, nil
}

func evenement(compte string) map[string]tftypes.Value {
	return map[string]tftypes.Value{"application": chaine("appl_1"), "compte": chaine(compte), "evenement": chaine("compte.efface")}
}

func TestCreerLEvenementLeDeclencheUneFoisEtDetruireNAppelleRien(t *testing.T) {
	a := essai.NouvelleAPI(t, "rdk_du_bac_a_sable")
	a.BacASable, a.AutorisentApplication = true, map[string]bool{"lea": true}
	j := auBacASable(t, a, "bac-a-sable")
	if e := j.planifierEvenement(evenement("lea")); len(e) != 0 {
		t.Fatalf("témoin : au bac à sable le plan passe : %v", e)
	}
	m, e := j.creerEvenement(evenement("lea"))
	if e != nil {
		t.Fatal(e)
	}
	if texte(m["sujet"]) != "sujet-de-lea" || !strings.HasPrefix(texte(m["id"]), "appl_1/lea/compte.efface/") || texte(m["declenche_le"]) == "" {
		t.Fatalf("état : %v", m)
	}
	d := a.LesDeclenches()
	if len(d) != 1 || d[0]["application"] != "appl_1" || d[0]["compte"] != "lea" || d[0]["evenement"] != "compte.efface" {
		t.Fatalf("déclenchements : %v", d)
	}
	// Détruire : aucun appel.
	typ := j.typeRessource(typeEvenement)
	etat, _ := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, m))
	nul, _ := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, nil))
	rep, err := j.serveur.ApplyResourceChange(context.Background(), &tfprotov6.ApplyResourceChangeRequest{
		TypeName: typeEvenement, PriorState: &etat, PlannedState: &nul, Config: &nul})
	if err != nil || len(messages(rep.Diagnostics, tfprotov6.DiagnosticSeverityError)) != 0 {
		t.Fatalf("détruire : %v %v", err, rep.Diagnostics)
	}
	if d := a.LesDeclenches(); len(d) != 1 {
		t.Fatalf("détruire ne déclenche rien : %v", d)
	}
}

func TestUnCompteQuiNAPasAutoriseDitQuoiFaire(t *testing.T) {
	a := essai.NouvelleAPI(t, "rdk_du_bac_a_sable")
	a.BacASable = true
	j := auBacASable(t, a, "bac-a-sable")
	_, e := j.creerEvenement(evenement("karim"))
	if len(e) == 0 || !strings.Contains(e[0], "pas_autorisee") || !strings.Contains(e[0], "Continuer en tant que") {
		t.Fatalf("%v", e)
	}
}

func TestHorsDuBacASableLEvenementSeRefuseAuPlan(t *testing.T) {
	a := essai.NouvelleAPI(t, "rdk_du_bac_a_sable")
	a.BacASable = true
	j := auBacASable(t, a, "qa")
	if e := j.planifierEvenement(evenement("lea")); len(e) == 0 || !strings.Contains(e[0], "Bac à sable seulement") {
		t.Fatalf("%v", e)
	}
	if d := a.LesDeclenches(); len(d) != 0 {
		t.Fatalf("aucun appel au plan : %v", d)
	}
}

func TestUnEvenementNonDeclenchableEstRefuse(t *testing.T) {
	j := nouveauJoueur(t, nil, "")
	typ := j.typeRessource(typeEvenement)
	attrs := evenement("lea")
	attrs["evenement"] = chaine("publication.retiree")
	rep, err := j.serveur.ValidateResourceConfig(context.Background(), &tfprotov6.ValidateResourceConfigRequest{
		TypeName: typeEvenement, Config: valeurDe(t, typ, attrs)})
	if err != nil {
		t.Fatal(err)
	}
	if e := messages(rep.Diagnostics, tfprotov6.DiagnosticSeverityError); len(e) == 0 {
		t.Fatal("publication.retiree ne se déclenche pas au bac à sable")
	}
}
