// Package essai — les doublures des essais : un ÉMETTEUR (la porte
// d'autorisation, ce que fait Hydra) et une API ReCoDanse réduite à ce que les
// outils appellent. Rien de ce paquet n'entre dans un binaire construit : seuls
// des fichiers `_test.go` l'importent.
//
// La doublure d'émetteur joue un SCÉNARIO au point de jeton — une suite de
// réponses (« authorization_pending », « slow_down », « succes »,
// « expired_token », « access_denied ») — pour que les essais prouvent ce que
// la RFC 8628 exige du client : attendre, ralentir, s'arrêter.
package essai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Les jetons des doublures : longs et reconnaissables, pour que les essais
// puissent affirmer qu'aucun ne s'est affiché.
const (
	JetonDAcces           = "ory_at_JETON-D-ACCES-QUI-NE-S-AFFICHE-JAMAIS"
	JetonDAccesRenouvele  = "ory_at_JETON-RENOUVELE-QUI-NE-S-AFFICHE-JAMAIS"
	Renouvellement        = "ory_rt_RENOUVELLEMENT-QUI-NE-S-AFFICHE-JAMAIS"
	RenouvellementSuivant = "ory_rt_RENOUVELLEMENT-SUIVANT"
	CodeUtilisateur       = "BCDF-GHJK"
	Sujet                 = "sujet-pseudonyme-42"
)

// Emetteur — une porte d'autorisation en mémoire.
type Emetteur struct {
	Serveur *httptest.Server

	mu sync.Mutex
	// Scenario — les réponses successives du point de jeton au device code.
	Scenario []string
	// Intervalle — le `interval` annoncé (secondes).
	Intervalle int
	// AutreEmetteur — si non vide, la découverte annonce cet émetteur-là.
	AutreEmetteur string
	// SansRevocation — la découverte n'annonce pas de révocation.
	SansRevocation bool
	// RevocationRefusee — la révocation répond 400 invalid_client (un client qui n'est pas le bon).
	RevocationRefusee bool

	// Ce que l'émetteur a vu.
	PorteeDemandee string
	ClientsVus     []string
	AppelsAuJeton  int
	Revoques       []string // "<indice>:<jeton>"
}

// NouvelEmetteur — démarré, arrêté à la fin de l'essai.
func NouvelEmetteur(t *testing.T, scenario ...string) *Emetteur {
	t.Helper()
	e := &Emetteur{Scenario: scenario, Intervalle: 5}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", e.decouverte)
	mux.HandleFunc("POST /oauth2/device/auth", e.demande)
	mux.HandleFunc("POST /oauth2/token", e.jeton)
	mux.HandleFunc("POST /oauth2/revoke", e.revoquer)
	mux.HandleFunc("GET /userinfo", e.userinfo)
	e.Serveur = httptest.NewServer(mux)
	t.Cleanup(e.Serveur.Close)
	return e
}

// URL — l'adresse de l'émetteur.
func (e *Emetteur) URL() string { return e.Serveur.URL }

func ecrire(w http.ResponseWriter, statut int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statut)
	_ = json.NewEncoder(w).Encode(v)
}

func (e *Emetteur) decouverte(w http.ResponseWriter, _ *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	emetteur := e.Serveur.URL
	if e.AutreEmetteur != "" {
		emetteur = e.AutreEmetteur
	}
	d := map[string]any{
		"issuer":                        emetteur,
		"device_authorization_endpoint": e.Serveur.URL + "/oauth2/device/auth",
		"token_endpoint":                e.Serveur.URL + "/oauth2/token",
		"userinfo_endpoint":             e.Serveur.URL + "/userinfo",
		"grant_types_supported":         []string{"authorization_code", "refresh_token", "urn:ietf:params:oauth:grant-type:device_code"},
	}
	if !e.SansRevocation {
		d["revocation_endpoint"] = e.Serveur.URL + "/oauth2/revoke"
	}
	ecrire(w, http.StatusOK, d)
}

func (e *Emetteur) demande(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.PorteeDemandee = r.PostForm.Get("scope")
	e.ClientsVus = append(e.ClientsVus, r.PostForm.Get("client_id"))
	ecrire(w, http.StatusOK, map[string]any{
		"device_code":               "code-d-appareil-1",
		"user_code":                 CodeUtilisateur,
		"verification_uri":          e.Serveur.URL + "/appareil",
		"verification_uri_complete": e.Serveur.URL + "/appareil?user_code=" + CodeUtilisateur,
		"expires_in":                600,
		"interval":                  e.Intervalle,
	})
}

func (e *Emetteur) jeton(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ClientsVus = append(e.ClientsVus, r.PostForm.Get("client_id"))
	switch r.PostForm.Get("grant_type") {
	case "urn:ietf:params:oauth:grant-type:device_code":
		if r.PostForm.Get("device_code") != "code-d-appareil-1" {
			ecrire(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
			return
		}
		e.AppelsAuJeton++
		if len(e.Scenario) == 0 {
			ecrire(w, http.StatusBadRequest, map[string]string{"error": "expired_token"})
			return
		}
		pas := e.Scenario[0]
		e.Scenario = e.Scenario[1:]
		if pas != "succes" {
			ecrire(w, http.StatusBadRequest, map[string]string{"error": pas, "error_description": "étape " + pas})
			return
		}
		rep := map[string]any{"access_token": JetonDAcces, "token_type": "bearer", "expires_in": 3600,
			"scope": e.PorteeDemandee, "id_token": "eyJ.e30.sig"}
		if strings.Contains(e.PorteeDemandee, "offline_access") {
			rep["refresh_token"] = Renouvellement
		}
		ecrire(w, http.StatusOK, rep)
	case "refresh_token":
		if r.PostForm.Get("refresh_token") != Renouvellement {
			ecrire(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant", "error_description": "renouvellement inconnu"})
			return
		}
		ecrire(w, http.StatusOK, map[string]any{"access_token": JetonDAccesRenouvele, "refresh_token": RenouvellementSuivant,
			"token_type": "bearer", "expires_in": 3600})
	default:
		ecrire(w, http.StatusBadRequest, map[string]string{"error": "unsupported_grant_type"})
	}
}

func (e *Emetteur) revoquer(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.RevocationRefusee {
		ecrire(w, http.StatusBadRequest, map[string]string{"error": "invalid_client", "error_description": "client inconnu"})
		return
	}
	e.Revoques = append(e.Revoques, r.PostForm.Get("token_type_hint")+":"+r.PostForm.Get("token"))
	w.WriteHeader(http.StatusOK)
}

func (e *Emetteur) userinfo(w http.ResponseWriter, r *http.Request) {
	a := r.Header.Get("Authorization")
	if a != "Bearer "+JetonDAcces && a != "Bearer "+JetonDAccesRenouvele {
		ecrire(w, http.StatusUnauthorized, map[string]string{"error": "invalid_token"})
		return
	}
	ecrire(w, http.StatusOK, map[string]string{"sub": Sujet})
}

// Vu — une copie de ce que l'émetteur a vu (sous verrou).
func (e *Emetteur) Vu() (portee string, clients []string, appels int, revoques []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.PorteeDemandee, append([]string(nil), e.ClientsVus...), e.AppelsAuJeton, append([]string(nil), e.Revoques...)
}

// API — une API ReCoDanse réduite : /v1/applications (au jeton) et la découverte
// de la ressource (publique).
type API struct {
	Serveur *httptest.Server
	// JetonsAdmis — les jetons qui ouvrent /v1 (les autres : 401 jeton_invalide).
	JetonsAdmis []string
	// Annoncees — ce que /.well-known/oauth-protected-resource annonce.
	Annoncees []string
	// Applications — ce que /v1/applications rend (JSON brut des objets).
	Applications []map[string]any

	// BacASable — monte les routes /v1/bac-a-sable/* (sinon : 404, comme l'API hors bac à sable).
	BacASable bool
	// ComptesFictifs — ce que GET /v1/bac-a-sable/comptes rend ; AutorisentApplication —
	// les clés des comptes qui ont autorisé (sinon : 409 pas_autorisee).
	ComptesFictifs        []map[string]string
	AutorisentApplication map[string]bool

	mu sync.Mutex
	// Declenches — les corps reçus par POST /v1/bac-a-sable/evenements.
	Declenches []map[string]string
	// AutorisationsVues — l'en-tête Authorization de chaque requête, par chemin.
	AutorisationsVues map[string][]string
}

// NouvelleAPI — démarrée, arrêtée à la fin de l'essai.
func NouvelleAPI(t *testing.T, jetons ...string) *API {
	t.Helper()
	a := &API{JetonsAdmis: jetons, AutorisationsVues: map[string][]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", func(w http.ResponseWriter, r *http.Request) {
		a.noter(r)
		ecrire(w, http.StatusOK, map[string]any{"resource": a.Serveur.URL, "scopes_supported": a.Annoncees})
	})
	mux.HandleFunc("GET /v1/applications", func(w http.ResponseWriter, r *http.Request) {
		if !a.admis(r) {
			ecrire(w, http.StatusUnauthorized, map[string]string{"error": "jeton absent ou invalide", "code": "jeton_invalide"})
			return
		}
		apps := a.Applications
		if apps == nil {
			apps = []map[string]any{}
		}
		ecrire(w, http.StatusOK, map[string]any{"applications": apps, "max": 10, "conditions_en_vigueur": "2026-10-20"})
	})
	mux.HandleFunc("GET /v1/applications/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !a.admis(r) {
			ecrire(w, http.StatusUnauthorized, map[string]string{"error": "jeton absent ou invalide", "code": "jeton_invalide"})
			return
		}
		for _, app := range a.Applications {
			if app["id"] == r.PathValue("id") {
				ecrire(w, http.StatusOK, app)
				return
			}
		}
		ecrire(w, http.StatusNotFound, map[string]string{"error": "application introuvable"})
	})
	mux.HandleFunc("GET /v1/bac-a-sable/comptes", func(w http.ResponseWriter, r *http.Request) {
		if !a.BacASable {
			http.NotFound(w, r)
			return
		}
		if !a.admis(r) {
			ecrire(w, http.StatusUnauthorized, map[string]string{"error": "jeton absent ou invalide", "code": "jeton_invalide"})
			return
		}
		ecrire(w, http.StatusOK, map[string]any{"comptes": a.ComptesFictifs})
	})
	mux.HandleFunc("POST /v1/bac-a-sable/evenements", func(w http.ResponseWriter, r *http.Request) {
		if !a.BacASable {
			http.NotFound(w, r)
			return
		}
		if !a.admis(r) {
			ecrire(w, http.StatusUnauthorized, map[string]string{"error": "jeton absent ou invalide", "code": "jeton_invalide"})
			return
		}
		var corps map[string]string
		_ = json.NewDecoder(r.Body).Decode(&corps)
		a.mu.Lock()
		a.Declenches = append(a.Declenches, corps)
		a.mu.Unlock()
		if !a.AutorisentApplication[corps["compte"]] {
			ecrire(w, http.StatusConflict, map[string]string{"error": "ce compte fictif n'a pas autorisé cette application", "code": "pas_autorisee"})
			return
		}
		ecrire(w, http.StatusAccepted, map[string]string{"evenement": corps["evenement"], "sujet": "sujet-de-" + corps["compte"]})
	})
	a.Serveur = httptest.NewServer(mux)
	t.Cleanup(a.Serveur.Close)
	return a
}

// LesDeclenches — une copie des corps reçus par POST /v1/bac-a-sable/evenements.
func (a *API) LesDeclenches() []map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]map[string]string(nil), a.Declenches...)
}

// URL — l'adresse de l'API.
func (a *API) URL() string { return a.Serveur.URL }

func (a *API) noter(r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.AutorisationsVues[r.URL.Path] = append(a.AutorisationsVues[r.URL.Path], r.Header.Get("Authorization"))
}

func (a *API) admis(r *http.Request) bool {
	a.noter(r)
	for _, j := range a.JetonsAdmis {
		if r.Header.Get("Authorization") == "Bearer "+j {
			return true
		}
	}
	return false
}

// Vues — l'en-tête Authorization vu pour ce chemin (copie).
func (a *API) Vues(chemin string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.AutorisationsVues[chemin]...)
}
