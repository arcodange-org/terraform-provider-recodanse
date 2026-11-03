package main

// La CLI jouée EN ENTIER (executer, ses drapeaux, ses fichiers) contre une
// doublure d'émetteur et une doublure d'API — seules les attentes sont
// remplacées, pour ne pas dormir cinq secondes par tour.
//
// ⚠ La propriété qui compte le plus est NÉGATIVE : le jeton ne s'affiche
// jamais. Chaque essai la vérifie sur la sortie standard ET sur la sortie
// d'erreur — et l'essai de la CI vérifie l'inverse (le masque DOIT porter le
// jeton), pour que l'assertion négative ne soit pas satisfaite par une sortie vide.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/essai"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/jeton"
)

type joue struct {
	code            int
	sortie, erreurs string
	attentes        []time.Duration
}

func jouer(t *testing.T, env map[string]string, parDefaut string, args ...string) joue {
	t.Helper()
	var so, se bytes.Buffer
	var attentes []time.Duration
	o := outils{
		env:        func(k string) string { return env[k] },
		attendre:   func(_ context.Context, d time.Duration) error { attentes = append(attentes, d); return nil },
		maintenant: func() time.Time { return time.Date(2026, 11, 2, 21, 0, 0, 0, time.UTC) },
		parDefaut:  parDefaut,
	}
	code := executer(context.Background(), args, &so, &se, o)
	return joue{code, so.String(), se.String(), attentes}
}

func sansJeton(t *testing.T, quoi, texte string) {
	t.Helper()
	for _, j := range []string{essai.JetonDAcces, essai.JetonDAccesRenouvele, essai.Renouvellement, essai.RenouvellementSuivant} {
		if strings.Contains(texte, j) {
			t.Fatalf("%s affiche un jeton :\n%s", quoi, texte)
		}
	}
}

func TestConnexionRangeLeJetonEn0600SansLAfficher(t *testing.T) {
	e := essai.NouvelEmetteur(t, "authorization_pending", "slow_down", "succes")
	f := filepath.Join(t.TempDir(), "recodanse", "jeton")
	r := jouer(t, nil, f, "connexion", "--environnement", "qa", "--emetteur", e.URL())
	if r.code != 0 {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	for _, attendu := range []string{e.URL() + "/appareil?user_code=" + essai.CodeUtilisateur, essai.CodeUtilisateur, "10 min", "Connecté (qa)", f} {
		if !strings.Contains(r.erreurs, attendu) {
			t.Errorf("la sortie d'erreur doit dire %q :\n%s", attendu, r.erreurs)
		}
	}
	if r.sortie != "" {
		t.Fatalf("hors CI, rien sur la sortie standard : %q", r.sortie)
	}
	sansJeton(t, "la sortie d'erreur", r.erreurs)
	if want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second}; !reflect.DeepEqual(r.attentes, want) {
		t.Fatalf("attentes %v, attendu %v", r.attentes, want)
	}
	info, err := os.Stat(f)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("fichier %v %v", info, err)
	}
	rg, err := jeton.Lire(f)
	if err != nil || rg.JetonDAcces != essai.JetonDAcces || rg.Environnement != "qa" || rg.Emetteur != e.URL() || rg.Renouvellement != "" {
		t.Fatalf("rangement %+v %v", rg, err)
	}
	if portee, _, _, _ := e.Vu(); portee != "openid developpeur" {
		t.Fatalf("sans --hors-ligne, pas d'offline_access : %q", portee)
	}
}

func TestConnexionHorsLigneDemandeOfflineAccessEtGardeLeRenouvellement(t *testing.T) {
	e := essai.NouvelEmetteur(t, "succes")
	f := filepath.Join(t.TempDir(), "jeton")
	r := jouer(t, nil, "", "connexion", "--emetteur", e.URL(), "--hors-ligne", "--sortie", f)
	if r.code != 0 {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	if portee, _, _, _ := e.Vu(); portee != "openid developpeur offline_access" {
		t.Fatalf("portée %q", portee)
	}
	if rg, _ := jeton.Lire(f); rg.Renouvellement != essai.Renouvellement || rg.Environnement != "production" {
		t.Fatalf("rangement %+v", rg)
	}
	sansJeton(t, "la sortie d'erreur", r.erreurs)
}

func TestUnCodeExpireNeRangeRien(t *testing.T) {
	e := essai.NouvelEmetteur(t, "authorization_pending", "expired_token")
	f := filepath.Join(t.TempDir(), "jeton")
	r := jouer(t, nil, f, "connexion", "--emetteur", e.URL())
	if r.code != 1 || !strings.Contains(r.erreurs, "expiré") {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("un code expiré ne doit rien ranger")
	}
}

func TestUnRefusNeRangeRien(t *testing.T) {
	e := essai.NouvelEmetteur(t, "access_denied")
	f := filepath.Join(t.TempDir(), "jeton")
	if r := jouer(t, nil, f, "connexion", "--emetteur", e.URL()); r.code != 1 || !strings.Contains(r.erreurs, "refusée") {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("un refus ne doit rien ranger")
	}
}

func TestEnCILeJetonEstMasquePuisEcritDansGithubEnvEtNullePartAilleurs(t *testing.T) {
	e := essai.NouvelEmetteur(t, "succes")
	dossier := t.TempDir()
	ghenv := filepath.Join(dossier, "github_env")
	defaut := filepath.Join(dossier, "defaut", "jeton")
	r := jouer(t, map[string]string{"GITHUB_ENV": ghenv}, defaut, "connexion", "--ci", "--environnement", "qa", "--emetteur", e.URL())
	if r.code != 0 {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	// Le témoin positif : le masque PORTE le jeton (sinon « rien d'affiché » serait trivial).
	if r.sortie != "::add-mask::"+essai.JetonDAcces+"\n" {
		t.Fatalf("la sortie standard doit être le seul masque : %q", r.sortie)
	}
	sansJeton(t, "la sortie d'erreur", r.erreurs)
	brut, err := os.ReadFile(ghenv)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{"RECODANSE_JETON=" + essai.JetonDAcces, "RECODANSE_ENVIRONNEMENT=qa", "RECODANSE_EMETTEUR=" + e.URL(),
		"RECODANSE_API=https://api-qa.recodanse.fr"} {
		if !strings.Contains(string(brut), l+"\n") {
			t.Errorf("$GITHUB_ENV doit porter %q :\n%s", l, brut)
		}
	}
	if _, err := os.Stat(defaut); !os.IsNotExist(err) {
		t.Fatal("en CI, aucun fichier de jeton")
	}
}

func TestEnCISansGithubEnvOnRefuse(t *testing.T) {
	e := essai.NouvelEmetteur(t, "succes")
	if r := jouer(t, nil, "", "connexion", "--ci", "--emetteur", e.URL()); r.code != 1 || !strings.Contains(r.erreurs, "GITHUB_ENV") {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	if _, _, appels, _ := e.Vu(); appels != 0 {
		t.Fatal("le refus doit venir AVANT toute demande à l'émetteur")
	}
}

func TestEnCIOfflineAccessEstRefuse(t *testing.T) {
	if r := jouer(t, map[string]string{"GITHUB_ENV": "/x"}, "", "connexion", "--ci", "--hors-ligne"); r.code != 2 {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
}

func TestQuiDitQuiSansAfficherLeJeton(t *testing.T) {
	e := essai.NouvelEmetteur(t)
	a := essai.NouvelleAPI(t, essai.JetonDAcces)
	a.Applications = []map[string]any{{"id": "appl_1", "nom": "Studio"}}
	f := filepath.Join(t.TempDir(), "jeton")
	if err := jeton.Ecrire(f, jeton.Rangement{Environnement: "qa", API: a.URL(), Emetteur: e.URL(), Client: "recodanse-outils",
		JetonDAcces: essai.JetonDAcces, ExpireLe: time.Date(2026, 11, 2, 22, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	r := jouer(t, nil, f, "qui")
	if r.code != 0 {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	for _, attendu := range []string{"qa", "jeton d'accès OAuth", "sub " + essai.Sujet, "1 application(s) sur 10"} {
		if !strings.Contains(r.erreurs, attendu) {
			t.Errorf("qui doit dire %q :\n%s", attendu, r.erreurs)
		}
	}
	sansJeton(t, "qui", r.erreurs+r.sortie)
	if v := a.Vues("/v1/applications"); len(v) != 1 || v[0] != "Bearer "+essai.JetonDAcces {
		t.Fatalf("l'API doit avoir reçu le jeton, une fois : %v", v)
	}
}

func TestQuiAvecUnJetonRefuseLeDit(t *testing.T) {
	a := essai.NouvelleAPI(t) // aucun jeton admis
	r := jouer(t, map[string]string{jeton.VariableJeton: "rdk_revoque", "RECODANSE_API": a.URL()}, "", "qui")
	if r.code != 1 || !strings.Contains(r.erreurs, "jeton_invalide") || strings.Contains(r.erreurs, "rdk_revoque") {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
}

func TestDeconnexionRevoqueLeRenouvellementPuisLAccesEtEfface(t *testing.T) {
	e := essai.NouvelEmetteur(t)
	f := filepath.Join(t.TempDir(), "jeton")
	if err := jeton.Ecrire(f, jeton.Rangement{Environnement: "qa", Emetteur: e.URL(), Client: "recodanse-outils",
		JetonDAcces: essai.JetonDAcces, Renouvellement: essai.Renouvellement}); err != nil {
		t.Fatal(err)
	}
	r := jouer(t, nil, f, "deconnexion")
	if r.code != 0 {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	sansJeton(t, "deconnexion", r.erreurs+r.sortie)
	_, clients, _, revoques := e.Vu()
	if want := []string{"refresh_token:" + essai.Renouvellement, "access_token:" + essai.JetonDAcces}; !reflect.DeepEqual(revoques, want) {
		t.Fatalf("révocations %v, attendu %v", revoques, want)
	}
	_ = clients // la révocation n'est pas comptée parmi les clients vus ; elle porte client_id (RFC 7009 § 2.1)
	if _, err := os.Stat(f); !os.IsNotExist(err) {
		t.Fatal("le fichier doit être effacé")
	}
}

func TestDeconnexionSansRevocationGardeLeFichier(t *testing.T) {
	e := essai.NouvelEmetteur(t)
	e.SansRevocation = true
	f := filepath.Join(t.TempDir(), "jeton")
	if err := jeton.Ecrire(f, jeton.Rangement{Environnement: "qa", Emetteur: e.URL(), JetonDAcces: essai.JetonDAcces}); err != nil {
		t.Fatal(err)
	}
	if r := jouer(t, nil, f, "deconnexion"); r.code != 1 || !strings.Contains(r.erreurs, "GARDÉ") {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	if _, err := os.Stat(f); err != nil {
		t.Fatal("rien n'est révoqué : le fichier reste, pour réessayer")
	}
}

func TestDeconnexionSansFichierNeFaitRien(t *testing.T) {
	if r := jouer(t, nil, filepath.Join(t.TempDir(), "absent"), "deconnexion"); r.code != 0 || !strings.Contains(r.erreurs, "rien à révoquer") {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
}

func TestUneCommandeInconnueEstUneFauteDUsage(t *testing.T) {
	if r := jouer(t, nil, "", "connecter"); r.code != 2 {
		t.Fatalf("code %d", r.code)
	}
}

// ── deconnexion --ci ─────────────────────────────────────────────────────────
// `connexion --ci` ne range aucun fichier : la déconnexion d'un job lit le jeton
// dans l'environnement que `connexion --ci` a posé dans $GITHUB_ENV.

func envDeCI(e *essai.Emetteur, extra map[string]string) map[string]string {
	m := map[string]string{"RECODANSE_JETON": essai.JetonDAcces, "RECODANSE_ENVIRONNEMENT": "qa", "RECODANSE_EMETTEUR": e.URL()}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestDeconnexionCIRevoqueLeJetonDeLEnvironnement(t *testing.T) {
	e := essai.NouvelEmetteur(t)
	defaut := filepath.Join(t.TempDir(), "jeton")
	r := jouer(t, envDeCI(e, nil), defaut, "deconnexion", "--ci")
	if r.code != 0 || !strings.Contains(r.erreurs, "révoqué") {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	sansJeton(t, "deconnexion --ci", r.erreurs+r.sortie)
	if _, _, _, rv := e.Vu(); !reflect.DeepEqual(rv, []string{"access_token:" + essai.JetonDAcces}) {
		t.Fatalf("révocations %v", rv)
	}
	if _, err := os.Stat(defaut); !os.IsNotExist(err) {
		t.Fatal("en CI, aucun fichier n'est écrit")
	}
}

func TestDeconnexionCIRevoqueAussiUnRenouvellementPresent(t *testing.T) {
	e := essai.NouvelEmetteur(t)
	r := jouer(t, envDeCI(e, map[string]string{VariableRenouvellement: essai.Renouvellement}), "", "deconnexion", "--ci")
	if r.code != 0 {
		t.Fatalf("code %d\n%s", r.code, r.erreurs)
	}
	sansJeton(t, "deconnexion --ci", r.erreurs+r.sortie)
	want := []string{"refresh_token:" + essai.Renouvellement, "access_token:" + essai.JetonDAcces}
	if _, _, _, rv := e.Vu(); !reflect.DeepEqual(rv, want) {
		t.Fatalf("révocations %v, attendu %v", rv, want)
	}
}

func TestDeconnexionCIEchoueQuandRienNEstRevoque(t *testing.T) {
	e := essai.NouvelEmetteur(t)
	cas := map[string]struct {
		env      map[string]string
		preparer func()
		mot      string
	}{
		"sans jeton":         {env: map[string]string{"RECODANSE_EMETTEUR": e.URL(), "RECODANSE_ENVIRONNEMENT": "qa"}, mot: "rien à révoquer"},
		"jeton rdk_":         {env: envDeCI(e, map[string]string{"RECODANSE_JETON": "rdk_ne_s_affiche_pas"}), mot: "dans l'app"},
		"révocation refusée": {env: envDeCI(e, nil), preparer: func() { e.RevocationRefusee = true }, mot: "invalid_client"},
		"sans révocation":    {env: envDeCI(e, nil), preparer: func() { e.RevocationRefusee, e.SansRevocation = false, true }, mot: "revocation_endpoint"},
	}
	for _, nom := range []string{"sans jeton", "jeton rdk_", "révocation refusée", "sans révocation"} {
		c := cas[nom]
		t.Run(nom, func(t *testing.T) {
			if c.preparer != nil {
				c.preparer()
			}
			r := jouer(t, c.env, "", "deconnexion", "--ci")
			if r.code != 1 || !strings.Contains(r.erreurs, c.mot) || strings.Contains(r.erreurs, "rdk_ne_s_affiche_pas") {
				t.Fatalf("code %d\n%s", r.code, r.erreurs)
			}
			sansJeton(t, nom, r.erreurs+r.sortie)
		})
	}
}

func TestDeconnexionCIEtFichierSontUneFauteDUsage(t *testing.T) {
	if r := jouer(t, nil, "", "deconnexion", "--ci", "--fichier", "/x"); r.code != 2 {
		t.Fatalf("code %d", r.code)
	}
}
