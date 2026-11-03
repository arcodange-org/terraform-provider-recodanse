// Package api — le client des routes `/v1/applications` de ReCoDanse, avec un
// jeton de développeur.
//
// Il ne connaît que ce que le contrat publié (`openapi-tiers.yaml`) promet :
// aucun type de l'API n'est importé, le fournisseur vit dans son propre module
// et parle au service comme n'importe quel intégrateur.
//
// ⚠ CE QU'IL REFUSE :
//   - de relire un secret : l'API ne le rend qu'à la création et à la rotation ;
//   - de réessayer une écriture sur autre chose qu'un 429 : rejouer un POST qui a
//     peut-être abouti déclarerait l'application deux fois ;
//   - de mettre le jeton ailleurs que dans l'en-tête `Authorization`.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// AdresseParDefaut — l'API publique de ReCoDanse.
const AdresseParDefaut = "https://api.recodanse.fr"

// Client — un client authentifié par un jeton de développeur.
type Client struct {
	Adresse string
	Jeton   string
	HTTP    *http.Client
	// Agent — l'en-tête User-Agent (le fournisseur y met sa version).
	Agent string
	// Environnement — production, qa ou bac-a-sable : ce que le fournisseur a
	// résolu (les objets du bac à sable se refusent ailleurs, dès le plan).
	Environnement string
	// Attendre — pour les essais (nil : time.Sleep borné par le contexte).
	Attendre func(ctx context.Context, d time.Duration) error
}

// Erreur — une réponse refusée, telle que l'API l'écrit ({error, code}).
type Erreur struct {
	Statut  int
	Code    string
	Message string
}

func (e *Erreur) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("ReCoDanse a répondu %d (%s) : %s", e.Statut, e.Code, e.Message)
	}
	return fmt.Sprintf("ReCoDanse a répondu %d : %s", e.Statut, e.Message)
}

// EstIntrouvable — un 404 : la ressource n'existe plus (ou n'est pas à ce compte).
func EstIntrouvable(err error) bool {
	var e *Erreur
	return errors.As(err, &e) && e.Statut == http.StatusNotFound
}

// CodeDe — le code d'une erreur de l'API ("" sinon).
func CodeDe(err error) string {
	var e *Erreur
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// essaisSur429 — combien de fois on rejoue une requête refusée par le quota.
// Un `terraform apply` de vingt ressources dépasse vite dix écritures par minute.
const essaisSur429 = 4

// attenteMax — au-delà, on rend la main : mieux vaut un échec qui dit pourquoi
// qu'un apply qui semble figé.
const attenteMax = 90 * time.Second

func (c *Client) attendre(ctx context.Context, d time.Duration) error {
	if c.Attendre != nil {
		return c.Attendre(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) faire(ctx context.Context, methode, chemin string, corps any, sortie any) error {
	return c.faireAvec(ctx, methode, chemin, corps, sortie, true)
}

// faireAvec — `avecJeton` faux pour une route publique : un jeton ne part jamais
// là où il ne sert à rien.
func (c *Client) faireAvec(ctx context.Context, methode, chemin string, corps any, sortie any, avecJeton bool) error {
	var brut []byte
	if corps != nil {
		var err error
		if brut, err = json.Marshal(corps); err != nil {
			return err
		}
	}
	base := strings.TrimRight(c.Adresse, "/")
	if base == "" {
		base = AdresseParDefaut
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	for essai := 1; ; essai++ {
		var lecteur io.Reader
		if brut != nil {
			lecteur = bytes.NewReader(brut)
		}
		req, err := http.NewRequestWithContext(ctx, methode, base+chemin, lecteur)
		if err != nil {
			return err
		}
		if avecJeton {
			req.Header.Set("Authorization", "Bearer "+c.Jeton)
		}
		req.Header.Set("Accept", "application/json")
		if brut != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c.Agent != "" {
			req.Header.Set("User-Agent", c.Agent)
		}
		rep, err := hc.Do(req)
		if err != nil {
			return fmt.Errorf("ReCoDanse injoignable (%s %s) : %w", methode, chemin, err)
		}
		contenu, _ := io.ReadAll(io.LimitReader(rep.Body, 1<<20))
		_ = rep.Body.Close()
		if rep.StatusCode == http.StatusTooManyRequests && essai < essaisSur429 {
			d := attenteDe(rep.Header.Get("Retry-After"))
			if err := c.attendre(ctx, d); err != nil {
				return err
			}
			continue
		}
		if rep.StatusCode >= 300 {
			e := &Erreur{Statut: rep.StatusCode}
			var corpsErr struct {
				Error string `json:"error"`
				Code  string `json:"code"`
			}
			if json.Unmarshal(contenu, &corpsErr) == nil {
				e.Code, e.Message = corpsErr.Code, corpsErr.Error
			}
			if e.Message == "" {
				e.Message = strings.TrimSpace(string(contenu))
			}
			return e
		}
		if sortie != nil && len(contenu) > 0 {
			if err := json.Unmarshal(contenu, sortie); err != nil {
				return fmt.Errorf("réponse illisible de ReCoDanse (%s %s) : %w", methode, chemin, err)
			}
		}
		return nil
	}
}

// attenteDe — `Retry-After` en secondes, borné ; une seconde s'il manque.
func attenteDe(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return time.Second
	}
	d := time.Duration(n) * time.Second
	if d > attenteMax {
		return attenteMax
	}
	return d
}

// ── Les applications ─────────────────────────────────────────────────────────

// Application — une application, telle que l'API la rend.
type Application struct {
	ID           string     `json:"id"`
	ClientID     string     `json:"client_id"`
	Nom          string     `json:"nom"`
	Description  string     `json:"description"`
	SiteWeb      string     `json:"site_web"`
	Categorie    string     `json:"categorie"`
	Statut       string     `json:"statut"`
	Portees      []string   `json:"portees"`
	Redirections []string   `json:"redirections"`
	Publique     bool       `json:"publique"`
	CreeLe       string     `json:"cree_le"`
	Conditions   Conditions `json:"conditions"`
	// ClientSecret — rendu à la création seulement.
	ClientSecret string `json:"client_secret"`
}

// Conditions — ce que la propriétaire a accepté des conditions des développeurs.
type Conditions struct {
	VersionAcceptee *string `json:"version_acceptee"`
	AJour           bool    `json:"a_jour"`
}

// Declaration — ce qu'on déclare.
type Declaration struct {
	Nom          string   `json:"nom"`
	Description  string   `json:"description,omitempty"`
	SiteWeb      string   `json:"site_web,omitempty"`
	Categorie    string   `json:"categorie"`
	Portees      []string `json:"portees"`
	Redirections []string `json:"redirections,omitempty"`
	Publique     bool     `json:"publique,omitempty"`
}

// Correction — ce qu'on corrige (tous les champs sont envoyés : l'état voulu).
type Correction struct {
	Nom          string   `json:"nom"`
	Description  string   `json:"description"`
	SiteWeb      string   `json:"site_web"`
	Portees      []string `json:"portees"`
	Redirections []string `json:"redirections"`
}

// Liste — la liste des applications, et la version des conditions en vigueur.
type Liste struct {
	Applications        []Application `json:"applications"`
	Max                 int           `json:"max"`
	ConditionsEnVigueur string        `json:"conditions_en_vigueur"`
}

func chemin(id string) string { return "/v1/applications/" + url.PathEscape(id) }

// Applications — GET /v1/applications.
func (c *Client) Applications(ctx context.Context) (Liste, error) {
	var l Liste
	err := c.faire(ctx, http.MethodGet, "/v1/applications", nil, &l)
	return l, err
}

// Application — GET /v1/applications/{id}.
func (c *Client) Application(ctx context.Context, id string) (Application, error) {
	var a Application
	err := c.faire(ctx, http.MethodGet, chemin(id), nil, &a)
	return a, err
}

// Declarer — POST /v1/applications : l'application et son secret (une fois).
func (c *Client) Declarer(ctx context.Context, d Declaration) (Application, error) {
	var a Application
	err := c.faire(ctx, http.MethodPost, "/v1/applications", d, &a)
	return a, err
}

// Corriger — PATCH /v1/applications/{id}.
func (c *Client) Corriger(ctx context.Context, id string, corr Correction) (Application, error) {
	var a Application
	err := c.faire(ctx, http.MethodPatch, chemin(id), corr, &a)
	return a, err
}

// Retirer — DELETE /v1/applications/{id}.
func (c *Client) Retirer(ctx context.Context, id string) error {
	return c.faire(ctx, http.MethodDelete, chemin(id), nil, nil)
}

// ChangerLeSecret — POST /v1/applications/{id}/secret : le nouveau secret.
func (c *Client) ChangerLeSecret(ctx context.Context, id string) (string, error) {
	var r struct {
		ClientSecret string `json:"client_secret"`
	}
	err := c.faire(ctx, http.MethodPost, chemin(id)+"/secret", nil, &r)
	return r.ClientSecret, err
}

// Soumettre — POST /v1/applications/{id}/revue, en acceptant `version` des
// conditions des développeurs.
func (c *Client) Soumettre(ctx context.Context, id, version string) (Application, error) {
	var a Application
	err := c.faire(ctx, http.MethodPost, chemin(id)+"/revue", map[string]string{"accepte_les_conditions": version}, &a)
	return a, err
}

// ── La découverte de la ressource ────────────────────────────────────────────

// Ressource — GET /.well-known/oauth-protected-resource (RFC 9728), sans jeton.
type Ressource struct {
	Ressource             string   `json:"resource"`
	ServeursDAutorisation []string `json:"authorization_servers"`
	PorteesAnnoncees      []string `json:"scopes_supported"`
}

// MetadonneesDeLaRessource — ce que l'API annonce d'elle-même : son émetteur et
// TOUTES les portées de son catalogue (route publique : aucun jeton n'y part).
func (c *Client) MetadonneesDeLaRessource(ctx context.Context) (Ressource, error) {
	var r Ressource
	err := c.faireAvec(ctx, http.MethodGet, "/.well-known/oauth-protected-resource", nil, &r, false)
	return r, err
}

// ── Les webhooks ─────────────────────────────────────────────────────────────

// Webhook — un webhook d'une application.
type Webhook struct {
	ID          string   `json:"id"`
	Application string   `json:"application"`
	URL         string   `json:"url"`
	Evenements  []string `json:"evenements"`
	Actif       bool     `json:"actif"`
	// Secret — rendu à la pose seulement.
	Secret string `json:"secret"`
}

// Webhooks — GET /v1/applications/{id}/webhooks.
func (c *Client) Webhooks(ctx context.Context, application string) ([]Webhook, error) {
	var r struct {
		Webhooks []Webhook `json:"webhooks"`
	}
	err := c.faire(ctx, http.MethodGet, chemin(application)+"/webhooks", nil, &r)
	return r.Webhooks, err
}

// PoserUnWebhook — POST /v1/applications/{id}/webhooks : le webhook et son secret (une fois).
func (c *Client) PoserUnWebhook(ctx context.Context, application, adresse string, evenements []string) (Webhook, error) {
	var k Webhook
	err := c.faire(ctx, http.MethodPost, chemin(application)+"/webhooks",
		map[string]any{"url": adresse, "evenements": evenements}, &k)
	return k, err
}

// RetirerUnWebhook — DELETE /v1/webhooks/{id}.
func (c *Client) RetirerUnWebhook(ctx context.Context, id string) error {
	return c.faire(ctx, http.MethodDelete, "/v1/webhooks/"+url.PathEscape(id), nil, nil)
}

// ── Le bac à sable ───────────────────────────────────────────────────────────
//
// ⚠ CONTRAT SUPPOSÉ : ces deux routes /v1 n'existent pas encore —
// l'API les ouvrira derrière la garde Developpeur, et SEULEMENT quand le bac à
// sable est actif (ailleurs : 404). Elles reprennent les routes de l'app (par
// cookie) `/v0/tiers/bac-a-sable/danseuses` et `/v0/tiers/bac-a-sable/evenements`,
// avec `compte` au lieu de `danseuse`.

// CompteFictif — un compte fictif du bac à sable (un persona de démonstration).
type CompteFictif struct {
	Cle    string `json:"cle"`
	Prenom string `json:"prenom"`
	Titre  string `json:"titre"`
}

// ComptesFictifs — GET /v1/bac-a-sable/comptes.
func (c *Client) ComptesFictifs(ctx context.Context) ([]CompteFictif, error) {
	var r struct {
		Comptes []CompteFictif `json:"comptes"`
	}
	err := c.faire(ctx, http.MethodGet, "/v1/bac-a-sable/comptes", nil, &r)
	return r.Comptes, err
}

// EvenementDeclenche — ce que rend le déclenchement : l'événement et le sujet
// (le pseudonyme du compte fictif POUR cette application) dont l'accord est retiré.
type EvenementDeclenche struct {
	Evenement string `json:"evenement"`
	Sujet     string `json:"sujet"`
}

// DeclencherUnEvenement — POST /v1/bac-a-sable/evenements {application, compte, evenement}.
// L'accord du compte fictif est retiré chez l'émetteur, puis l'envoi part, signé
// comme en production. Jamais rejoué sur une autre erreur qu'un 429.
func (c *Client) DeclencherUnEvenement(ctx context.Context, application, compte, evenement string) (EvenementDeclenche, error) {
	var r EvenementDeclenche
	err := c.faire(ctx, http.MethodPost, "/v1/bac-a-sable/evenements",
		map[string]string{"application": application, "compte": compte, "evenement": evenement}, &r)
	return r, err
}
