package environnement

import (
	"strings"
	"testing"
)

func TestLesAdressesParEnvironnement(t *testing.T) {
	for _, c := range []struct{ nom, api, emetteur string }{
		{"", "https://api.recodanse.fr", "https://connexion.recodanse.fr"},
		{"production", "https://api.recodanse.fr", "https://connexion.recodanse.fr"},
		{"qa", "https://api-qa.recodanse.fr", "https://connexion-qa.recodanse.fr"},
	} {
		a, err := Resoudre(c.nom, "", "")
		if err != nil || a.API != c.api || a.Emetteur != c.emetteur {
			t.Errorf("%q : %+v %v", c.nom, a, err)
		}
	}
}

func TestLesAdressesSeSurchargent(t *testing.T) {
	a, err := Resoudre("qa", "http://127.0.0.1:8481/", "https://connexion.example")
	if err != nil || a.API != "http://127.0.0.1:8481" || a.Emetteur != "https://connexion.example" || a.Environnement != "qa" {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestLeBacASableRefuseTantQuIlNAPasDAdresses(t *testing.T) {
	_, err := Resoudre("bac-a-sable", "", "")
	if err == nil || !strings.Contains(err.Error(), "pas encore confirmées") || !strings.Contains(err.Error(), "emetteur") {
		t.Fatalf("attendu un refus clair, reçu %v", err)
	}
	if _, err := Resoudre("bac-a-sable", "https://api.bac.example", ""); err == nil {
		t.Fatal("l'API seule ne suffit pas : l'émetteur manque")
	}
	if a, err := Resoudre("bac-a-sable", "https://api.bac.example", "https://connexion.bac.example"); err != nil || a.API == "" {
		t.Fatalf("avec les deux adresses, le bac à sable s'ouvre : %+v %v", a, err)
	}
}

func TestUnJetonNeVoyagePasEnClair(t *testing.T) {
	if _, err := Resoudre("qa", "http://api.example", ""); err == nil || !strings.Contains(err.Error(), "en clair") {
		t.Fatalf("http:// hors de la machine doit être refusé : %v", err)
	}
	if _, err := Resoudre("qa", "http://localhost:8481", "http://127.0.0.1:4444"); err != nil {
		t.Fatalf("http:// vers la machine même est admis (essais locaux) : %v", err)
	}
}

func TestUnNomInconnuEstRefuse(t *testing.T) {
	if _, err := Resoudre("prod", "", ""); err == nil || !strings.Contains(err.Error(), "bac-a-sable, production, qa") {
		t.Fatalf("%v", err)
	}
}
