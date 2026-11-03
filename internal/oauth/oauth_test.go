package oauth

// Ce que la RFC 8628 exige du client, joué contre une doublure d'émetteur qui
// rejoue un scénario au point de jeton : attendre `interval`, ralentir de cinq
// secondes à chaque `slow_down` (pour TOUTE la suite), s'arrêter sur
// `expired_token` et `access_denied` en le disant.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/essai"
)

func clientDe(e *essai.Emetteur) (*Client, *[]time.Duration) {
	attentes := &[]time.Duration{}
	return &Client{Emetteur: e.URL(),
		Attendre:   func(_ context.Context, d time.Duration) error { *attentes = append(*attentes, d); return nil },
		Maintenant: func() time.Time { return time.Date(2026, 11, 2, 21, 0, 0, 0, time.UTC) },
	}, attentes
}

func connexion(t *testing.T, e *essai.Emetteur, portee string) (Jetons, []time.Duration, error) {
	t.Helper()
	c, attentes := clientDe(e)
	d, err := c.Decouvrir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	dem, err := c.DemanderUnCode(context.Background(), d, portee)
	if err != nil {
		t.Fatal(err)
	}
	if dem.UserCode != essai.CodeUtilisateur || !strings.Contains(dem.VerificationURIComplete, essai.CodeUtilisateur) {
		t.Fatalf("demande : %+v", dem)
	}
	j, err := c.AttendreLApprobation(context.Background(), d, dem)
	return j, *attentes, err
}

func TestLAttenteRespecteIntervalPuisRalentitSurSlowDown(t *testing.T) {
	e := essai.NouvelEmetteur(t, "authorization_pending", "slow_down", "authorization_pending", "succes")
	j, attentes, err := connexion(t, e, PorteeDesOutils)
	if err != nil {
		t.Fatal(err)
	}
	if j.AccessToken != essai.JetonDAcces || j.ExpireLe.IsZero() {
		t.Fatalf("jetons : %+v", j)
	}
	// 5 s, puis 5 s (pending), puis 10 s après le slow_down — et 10 s ENCORE ensuite.
	if want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second}; !reflect.DeepEqual(attentes, want) {
		t.Fatalf("attentes %v, attendu %v", attentes, want)
	}
	portee, clients, appels, _ := e.Vu()
	if portee != "openid developpeur" || appels != 4 {
		t.Fatalf("portée %q, appels %d", portee, appels)
	}
	for _, c := range clients {
		if c != ClientDesOutils {
			t.Fatalf("client inattendu %q (attendu %s partout)", c, ClientDesOutils)
		}
	}
}

func TestUnCodeExpireArreteLAttenteEnLeDisant(t *testing.T) {
	e := essai.NouvelEmetteur(t, "authorization_pending", "expired_token", "succes")
	_, _, err := connexion(t, e, PorteeDesOutils)
	if !errors.Is(err, ErrExpire) {
		t.Fatalf("attendu ErrExpire, reçu %v", err)
	}
	if _, _, appels, _ := e.Vu(); appels != 2 {
		t.Fatalf("après expired_token, plus aucun appel : %d", appels)
	}
}

func TestUnRefusArreteLAttenteEnLeDisant(t *testing.T) {
	e := essai.NouvelEmetteur(t, "access_denied")
	if _, _, err := connexion(t, e, PorteeDesOutils); !errors.Is(err, ErrRefuse) {
		t.Fatalf("attendu ErrRefuse, reçu %v", err)
	}
}

func TestUneErreurInconnueRemonteAvecSonCode(t *testing.T) {
	e := essai.NouvelEmetteur(t, "invalid_client")
	if _, _, err := connexion(t, e, PorteeDesOutils); CodeOAuth(err) != "invalid_client" {
		t.Fatalf("attendu invalid_client, reçu %v", err)
	}
}

func TestLaDecouverteRefuseUnAutreEmetteur(t *testing.T) {
	e := essai.NouvelEmetteur(t)
	e.AutreEmetteur = "https://ailleurs.example"
	c, _ := clientDe(e)
	if _, err := c.Decouvrir(context.Background()); err == nil || !strings.Contains(err.Error(), "autre émetteur") {
		t.Fatalf("attendu un refus, reçu %v", err)
	}
}

func TestRenouvelerEtRevoquer(t *testing.T) {
	e := essai.NouvelEmetteur(t)
	c, _ := clientDe(e)
	d, err := c.Decouvrir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	j, err := c.Renouveler(context.Background(), d, essai.Renouvellement)
	if err != nil || j.AccessToken != essai.JetonDAccesRenouvele || j.RefreshToken != essai.RenouvellementSuivant {
		t.Fatalf("%+v %v", j, err)
	}
	if _, err := c.Renouveler(context.Background(), d, "inconnu"); CodeOAuth(err) != "invalid_grant" {
		t.Fatalf("un renouvellement inconnu : %v", err)
	}
	if err := c.Revoquer(context.Background(), d, essai.JetonDAcces, "access_token"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, r := e.Vu(); len(r) != 1 || r[0] != "access_token:"+essai.JetonDAcces {
		t.Fatalf("révocations vues : %v", r)
	}
}
