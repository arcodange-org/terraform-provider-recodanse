package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Sabotages relevés (paquet compilable) :
//   - le rejeu étendu aux réponses ≥ 500 → « une écriture n'est jamais rejouée
//     sur un 500 » rougit ;
//   - le jeton posé en paramètre d'URL → « le jeton ne part que dans l'en-tête » ;
//   - `attenteDe` qui ignore `Retry-After` → « le 429 attend ce qu'on lui dit ».

func client(t *testing.T, h http.HandlerFunc) (*Client, *[]time.Duration) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	attentes := &[]time.Duration{}
	return &Client{Adresse: s.URL, Jeton: "rdk_essai", Agent: "essai/1",
		Attendre: func(_ context.Context, d time.Duration) error { *attentes = append(*attentes, d); return nil }}, attentes
}

func TestLe429AttendCeQuOnLuiDitPuisRejoue(t *testing.T) {
	n := 0
	c, attentes := client(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 3 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"nom":"Studio"`) {
			t.Errorf("le corps rejoué doit être le même : %s", b)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"appl_1","client_secret":"s3cret"}`))
	})
	a, err := c.Declarer(context.Background(), Declaration{Nom: "Studio", Categorie: "service", Portees: []string{"evenements.publier"}})
	if err != nil || a.ID != "appl_1" || a.ClientSecret != "s3cret" {
		t.Fatalf("%+v %v", a, err)
	}
	if n != 3 || len(*attentes) != 2 || (*attentes)[0] != 7*time.Second {
		t.Fatalf("essais %d, attentes %v", n, *attentes)
	}
}

func TestLe429FinitParRendreLaMain(t *testing.T) {
	n := 0
	c, _ := client(t, func(w http.ResponseWriter, _ *http.Request) {
		n++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"trop de requêtes","code":"quota_depasse"}`))
	})
	_, err := c.Applications(context.Background())
	if n != essaisSur429 || CodeDe(err) != "quota_depasse" {
		t.Fatalf("essais %d, err %v", n, err)
	}
}

func TestUneEcritureNEstJamaisRejoueeSurUn500(t *testing.T) {
	n := 0
	c, _ := client(t, func(w http.ResponseWriter, _ *http.Request) {
		n++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"erreur interne"}`))
	})
	_, err := c.Declarer(context.Background(), Declaration{Nom: "Studio"})
	if n != 1 || err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("essais %d, err %v", n, err)
	}
}

func TestLeJetonNePartQueDansLEnTete(t *testing.T) {
	c, _ := client(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rdk_essai" || strings.Contains(r.URL.String(), "rdk_") {
			t.Errorf("jeton mal placé : %q %q", r.Header.Get("Authorization"), r.URL)
		}
		if r.Header.Get("User-Agent") != "essai/1" {
			t.Errorf("User-Agent : %q", r.Header.Get("User-Agent"))
		}
		_, _ = w.Write([]byte(`{"applications":[],"conditions_en_vigueur":"2026-10"}`))
	})
	l, err := c.Applications(context.Background())
	if err != nil || l.ConditionsEnVigueur != "2026-10" {
		t.Fatalf("%+v %v", l, err)
	}
}

func TestUnRefusDitSonCodeEtUn404SeReconnait(t *testing.T) {
	c, _ := client(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/appl_absente") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"application introuvable"}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"une application publiée n'élargit pas ses portées sans revue","code":"portees_figees"}`))
	})
	if _, err := c.Application(context.Background(), "appl_absente"); !EstIntrouvable(err) {
		t.Fatalf("404 non reconnu : %v", err)
	}
	_, err := c.Corriger(context.Background(), "appl_1", Correction{Nom: "x"})
	if CodeDe(err) != "portees_figees" || EstIntrouvable(err) {
		t.Fatalf("%v", err)
	}
}

func TestRetryAfterEstBorne(t *testing.T) {
	for v, d := range map[string]time.Duration{"": time.Second, "abc": time.Second, "0": time.Second, "3": 3 * time.Second, "9999": attenteMax} {
		if got := attenteDe(v); got != d {
			t.Errorf("Retry-After %q : %v, attendu %v", v, got, d)
		}
	}
}
