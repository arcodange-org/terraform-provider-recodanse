package jeton

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/environnement"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/essai"
)

func carte(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLOrdreDesSourcesEtLesConflits(t *testing.T) {
	dossier := t.TempDir()
	parDefaut := filepath.Join(dossier, "jeton")
	cas := []struct {
		nom             string
		attrJ, attrF    string
		env             map[string]string
		defautExiste    bool
		valeur, fichier string
		erreur          string
	}{
		{nom: "attribut jeton", attrJ: "rdk_a", env: map[string]string{VariableJeton: "rdk_env"}, valeur: "rdk_a"},
		{nom: "attribut fichier", attrF: "/f", env: map[string]string{VariableJeton: "rdk_env"}, fichier: "/f"},
		{nom: "les deux attributs", attrJ: "rdk_a", attrF: "/f", erreur: "l'un ou l'autre"},
		{nom: "variable jeton", env: map[string]string{VariableJeton: "rdk_env"}, defautExiste: true, valeur: "rdk_env"},
		{nom: "variable fichier", env: map[string]string{VariableFichier: "/g"}, fichier: "/g"},
		{nom: "les deux variables", env: map[string]string{VariableJeton: "x", VariableFichier: "/g"}, erreur: "l'une ou l'autre"},
		{nom: "fichier par défaut", defautExiste: true, fichier: parDefaut},
		{nom: "rien", erreur: "recodanse-connexion connexion"},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			_ = os.Remove(parDefaut)
			if c.defautExiste {
				if err := os.WriteFile(parDefaut, []byte("rdk_defaut"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			s, err := Choisir(c.attrJ, c.attrF, carte(c.env), parDefaut)
			if c.erreur != "" {
				if err == nil || !strings.Contains(err.Error(), c.erreur) {
					t.Fatalf("attendu une erreur « %s », reçu %+v %v", c.erreur, s, err)
				}
				return
			}
			if err != nil || s.Valeur != c.valeur || s.Fichier != c.fichier {
				t.Fatalf("%+v %v", s, err)
			}
		})
	}
	if _, err := Choisir("", "", carte(nil), filepath.Join(dossier, "absent")); !errors.Is(err, ErrAucunJeton) {
		t.Fatalf("un fichier par défaut absent : %v", err)
	}
}

func qa() environnement.Adresses {
	a, _ := environnement.Resoudre("qa", "", "")
	return a
}

func TestUnFichierLisibleParDAutresEstRefuse(t *testing.T) {
	f := filepath.Join(t.TempDir(), "jeton")
	if err := os.WriteFile(f, []byte("rdk_secret_du_fichier"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := (&Lecteur{Adresses: qa()}).Valeur(context.Background(), Source{Fichier: f})
	if err == nil || !strings.Contains(err.Error(), "chmod 600") || strings.Contains(err.Error(), "rdk_secret") {
		t.Fatalf("attendu un refus qui ne cite pas le jeton : %v", err)
	}
	_ = os.Chmod(f, 0o600)
	if v, err := (&Lecteur{Adresses: qa()}).Valeur(context.Background(), Source{Fichier: f}); err != nil || v != "rdk_secret_du_fichier" {
		t.Fatalf("un jeton nu en 0600 se lit : %q %v", v, err)
	}
}

func TestUnJetonDUnAutreEmetteurNePartPas(t *testing.T) {
	f := filepath.Join(t.TempDir(), "jeton")
	if err := Ecrire(f, Rangement{Environnement: "production", Emetteur: "https://connexion.recodanse.fr",
		JetonDAcces: essai.JetonDAcces}); err != nil {
		t.Fatal(err)
	}
	_, err := (&Lecteur{Adresses: qa()}).Valeur(context.Background(), Source{Fichier: f})
	if err == nil || !strings.Contains(err.Error(), "ne change pas d'environnement") || strings.Contains(err.Error(), essai.JetonDAcces) {
		t.Fatalf("attendu un refus qui nomme les deux environnements sans le jeton : %v", err)
	}
}

func TestUnJetonExpireSeRenouvelleEtLeFichierEstReecrit(t *testing.T) {
	e := essai.NouvelEmetteur(t)
	a, err := environnement.Resoudre("qa", "", e.URL())
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "jeton")
	maintenant := time.Date(2026, 11, 2, 21, 0, 0, 0, time.UTC)
	if err := Ecrire(f, Rangement{Environnement: "qa", Emetteur: e.URL(), Client: "recodanse-outils",
		JetonDAcces: essai.JetonDAcces, ExpireLe: maintenant.Add(time.Minute), Renouvellement: essai.Renouvellement}); err != nil {
		t.Fatal(err)
	}
	l := &Lecteur{Adresses: a, Maintenant: func() time.Time { return maintenant }}
	v, err := l.Valeur(context.Background(), Source{Fichier: f})
	if err != nil || v != essai.JetonDAccesRenouvele {
		t.Fatalf("à une minute de l'expiration, le jeton se renouvelle : %q %v", v, err)
	}
	r, err := Lire(f)
	if err != nil || r.Renouvellement != essai.RenouvellementSuivant || r.JetonDAcces != essai.JetonDAccesRenouvele {
		t.Fatalf("le fichier garde le renouvellement SUIVANT (Hydra fait tourner l'ancien) : %+v %v", r, err)
	}
	if info, _ := os.Stat(f); info.Mode().Perm() != 0o600 {
		t.Fatalf("réécrit en %#o", info.Mode().Perm())
	}
}

func TestUnJetonExpireSansRenouvellementDitQuoiFaire(t *testing.T) {
	f := filepath.Join(t.TempDir(), "jeton")
	if err := Ecrire(f, Rangement{Environnement: "qa", Emetteur: "https://connexion-qa.recodanse.fr",
		JetonDAcces: essai.JetonDAcces, ExpireLe: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	_, err := (&Lecteur{Adresses: qa()}).Valeur(context.Background(), Source{Fichier: f})
	if err == nil || !strings.Contains(err.Error(), "a expiré") || !strings.Contains(err.Error(), "--hors-ligne") {
		t.Fatalf("%v", err)
	}
}

func TestUnJetonDonneEnValeurNeSeLitPasAvecDesBlancs(t *testing.T) {
	if _, err := (&Lecteur{Adresses: qa()}).Valeur(context.Background(), Source{Valeur: "rdk_a rdk_b", Origine: VariableJeton}); err == nil {
		t.Fatal("deux jetons dans une variable doivent être refusés")
	}
}
