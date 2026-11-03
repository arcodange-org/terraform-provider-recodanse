// Package oauth — ce que les outils du développeur demandent à la porte
// d'autorisation de ReCoDanse (Ory Hydra) : la découverte, l'OAuth device code
// (RFC 8628), le renouvellement d'un jeton, la révocation (RFC 7009).
//
// Le client est PREMIÈRE PARTIE et PUBLIC : `recodanse-outils`, sans secret
// (`token_endpoint_auth_method: none`). Il ne demande que `openid developpeur`
// (et `offline_access` sur demande) : le jeton gère des APPLICATIONS DE
// DÉVELOPPEUR, jamais une vidéo, un compte ni une autorisation d'une personne
// (contrat dev-recodanse-iac, point 2).
//
// ⚠ CE QU'IL REFUSE :
//   - d'écrire un jeton dans une erreur : les réponses de l'émetteur se lisent
//     par leur `error` et leur `error_description`, jamais recopiées entières ;
//   - de suivre une découverte qui annonce un autre émetteur que celui demandé
//     (OpenID Connect Discovery § 4.3) : ce serait envoyer le code ailleurs ;
//   - de presser l'émetteur : `interval` est respecté, et `slow_down` l'allonge
//     de cinq secondes pour TOUTE la suite (RFC 8628 § 3.5).
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientDesOutils — le client OAuth des outils du développeur (créé côté API).
const ClientDesOutils = "recodanse-outils"

// PorteeDesOutils — ce qu'on demande, toujours ; `offline_access` s'y ajoute avec `--hors-ligne`.
const PorteeDesOutils = "openid developpeur"

// GrantDeviceCode — le type de grant de la RFC 8628.
const GrantDeviceCode = "urn:ietf:params:oauth:grant-type:device_code"

// Decouverte — ce que l'émetteur annonce (/.well-known/openid-configuration).
type Decouverte struct {
	Emetteur                    string   `json:"issuer"`
	DeviceAuthorizationEndpoint string   `json:"device_authorization_endpoint"`
	TokenEndpoint               string   `json:"token_endpoint"`
	RevocationEndpoint          string   `json:"revocation_endpoint"`
	UserinfoEndpoint            string   `json:"userinfo_endpoint"`
	GrantTypesSupported         []string `json:"grant_types_supported"`
}

// Client — un client de l'émetteur.
type Client struct {
	Emetteur string
	ClientID string // défaut : ClientDesOutils
	HTTP     *http.Client
	// Attendre — pour les essais (nil : une vraie attente, bornée par le contexte).
	Attendre func(ctx context.Context, d time.Duration) error
	// Maintenant — pour les essais (nil : time.Now).
	Maintenant func() time.Time
}

// ErreurOAuth — un refus de l'émetteur, tel que la RFC 6749 § 5.2 l'écrit.
type ErreurOAuth struct {
	Statut      int
	Code        string // error
	Description string // error_description
}

func (e *ErreurOAuth) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("l'émetteur a refusé (%d, %s) : %s", e.Statut, e.Code, e.Description)
	}
	return fmt.Sprintf("l'émetteur a refusé (%d, %s)", e.Statut, e.Code)
}

// CodeOAuth — le code `error` d'un refus de l'émetteur ("" sinon).
func CodeOAuth(err error) string {
	var e *ErreurOAuth
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Les fins d'une attente de device code, nommées pour que la CLI les dise.
var (
	ErrExpire = errors.New("le code a expiré avant d'être approuvé : relance la connexion")
	ErrRefuse = errors.New("la connexion a été refusée dans le navigateur")
)

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) clientID() string {
	if c.ClientID != "" {
		return c.ClientID
	}
	return ClientDesOutils
}

func (c *Client) maintenant() time.Time {
	if c.Maintenant != nil {
		return c.Maintenant()
	}
	return time.Now()
}

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

// Decouvrir — GET <émetteur>/.well-known/openid-configuration.
func (c *Client) Decouvrir(ctx context.Context) (Decouverte, error) {
	base := strings.TrimRight(c.Emetteur, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/.well-known/openid-configuration", nil)
	if err != nil {
		return Decouverte{}, err
	}
	req.Header.Set("Accept", "application/json")
	rep, err := c.http().Do(req)
	if err != nil {
		return Decouverte{}, fmt.Errorf("émetteur injoignable (%s) : %w", base, err)
	}
	defer rep.Body.Close()
	corps, _ := io.ReadAll(io.LimitReader(rep.Body, 1<<20))
	if rep.StatusCode != http.StatusOK {
		return Decouverte{}, fmt.Errorf("la découverte de %s a répondu %d", base, rep.StatusCode)
	}
	var d Decouverte
	if err := json.Unmarshal(corps, &d); err != nil {
		return Decouverte{}, fmt.Errorf("découverte illisible (%s) : %w", base, err)
	}
	if strings.TrimRight(d.Emetteur, "/") != base {
		return Decouverte{}, fmt.Errorf("la découverte de %s annonce un autre émetteur (%q) : refusé", base, d.Emetteur)
	}
	return d, nil
}

// formulaire — POST application/x-www-form-urlencoded ; la réponse JSON dans `sortie`.
func (c *Client) formulaire(ctx context.Context, adresse string, valeurs url.Values, sortie any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, adresse, strings.NewReader(valeurs.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	rep, err := c.http().Do(req)
	if err != nil {
		// ⚠ l'URL seule : jamais le corps (il porte un code ou un jeton).
		return fmt.Errorf("émetteur injoignable (%s) : %w", adresse, errSansURL(err))
	}
	defer rep.Body.Close()
	corps, _ := io.ReadAll(io.LimitReader(rep.Body, 1<<20))
	if rep.StatusCode >= 300 {
		var e struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(corps, &e)
		if e.Error == "" {
			e.Error = "reponse_inattendue"
		}
		return &ErreurOAuth{Statut: rep.StatusCode, Code: e.Error, Description: e.Description}
	}
	if sortie != nil && len(corps) > 0 {
		if err := json.Unmarshal(corps, sortie); err != nil {
			return fmt.Errorf("réponse illisible de l'émetteur (%s)", adresse)
		}
	}
	return nil
}

// errSansURL — l'erreur de transport, sans l'URL que net/http y recopie (elle ne
// porte pas de secret ici, mais la règle est simple : on nomme l'adresse soi-même).
func errSansURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// Demande — la réponse de l'autorisation d'appareil (RFC 8628 § 3.2).
type Demande struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// Jetons — ce que le point de jeton rend.
type Jetons struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	// ExpireLe — calculé à la réception (ExpiresIn n'a de sens qu'à cet instant).
	ExpireLe time.Time `json:"-"`
}

// DemanderUnCode — POST device_authorization_endpoint.
func (c *Client) DemanderUnCode(ctx context.Context, d Decouverte, portee string) (Demande, error) {
	if d.DeviceAuthorizationEndpoint == "" {
		return Demande{}, fmt.Errorf("l'émetteur %s n'annonce pas d'OAuth device code (device_authorization_endpoint absent)", c.Emetteur)
	}
	var dem Demande
	err := c.formulaire(ctx, d.DeviceAuthorizationEndpoint, url.Values{"client_id": {c.clientID()}, "scope": {portee}}, &dem)
	if err != nil {
		return Demande{}, err
	}
	if dem.DeviceCode == "" || dem.UserCode == "" || dem.VerificationURI == "" {
		return Demande{}, fmt.Errorf("réponse incomplète de l'émetteur (device_code, user_code ou verification_uri manquant)")
	}
	if dem.Interval <= 0 {
		dem.Interval = 5 // RFC 8628 § 3.2 : défaut de cinq secondes
	}
	return dem, nil
}

// AttendreLApprobation — interroge le point de jeton jusqu'à l'approbation, au
// rythme que l'émetteur impose (RFC 8628 § 3.4 et 3.5).
func (c *Client) AttendreLApprobation(ctx context.Context, d Decouverte, dem Demande) (Jetons, error) {
	intervalle := time.Duration(dem.Interval) * time.Second
	var limite time.Time
	if dem.ExpiresIn > 0 {
		limite = c.maintenant().Add(time.Duration(dem.ExpiresIn) * time.Second)
	}
	for {
		if err := c.attendre(ctx, intervalle); err != nil {
			return Jetons{}, err
		}
		var j Jetons
		err := c.formulaire(ctx, d.TokenEndpoint, url.Values{
			"grant_type": {GrantDeviceCode}, "device_code": {dem.DeviceCode}, "client_id": {c.clientID()},
		}, &j)
		switch CodeOAuth(err) {
		case "":
			if err != nil {
				return Jetons{}, err
			}
			if j.AccessToken == "" {
				return Jetons{}, fmt.Errorf("l'émetteur a approuvé sans rendre de jeton d'accès")
			}
			j.ExpireLe = c.expiration(j.ExpiresIn)
			return j, nil
		case "authorization_pending":
		case "slow_down":
			intervalle += 5 * time.Second
		case "expired_token":
			return Jetons{}, ErrExpire
		case "access_denied":
			return Jetons{}, ErrRefuse
		default:
			return Jetons{}, err
		}
		if !limite.IsZero() && !c.maintenant().Before(limite) {
			return Jetons{}, ErrExpire
		}
	}
}

func (c *Client) expiration(secondes int) time.Time {
	if secondes <= 0 {
		return time.Time{}
	}
	return c.maintenant().Add(time.Duration(secondes) * time.Second).UTC().Truncate(time.Second)
}

// Renouveler — grant refresh_token. Hydra fait TOURNER le jeton de
// renouvellement : celui rendu remplace l'ancien, qui ne vaut plus.
func (c *Client) Renouveler(ctx context.Context, d Decouverte, renouvellement string) (Jetons, error) {
	var j Jetons
	err := c.formulaire(ctx, d.TokenEndpoint, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {renouvellement}, "client_id": {c.clientID()},
	}, &j)
	if err != nil {
		return Jetons{}, err
	}
	if j.AccessToken == "" {
		return Jetons{}, fmt.Errorf("l'émetteur a renouvelé sans rendre de jeton d'accès")
	}
	j.ExpireLe = c.expiration(j.ExpiresIn)
	return j, nil
}

// Revoquer — RFC 7009. `indice` : "access_token" ou "refresh_token". Un jeton
// déjà inconnu de l'émetteur est un succès (RFC 7009 § 2.2).
func (c *Client) Revoquer(ctx context.Context, d Decouverte, jeton, indice string) error {
	if d.RevocationEndpoint == "" {
		return fmt.Errorf("l'émetteur %s n'annonce pas de révocation (revocation_endpoint absent)", c.Emetteur)
	}
	return c.formulaire(ctx, d.RevocationEndpoint, url.Values{
		"token": {jeton}, "token_type_hint": {indice}, "client_id": {c.clientID()},
	}, nil)
}

// QuiEst — GET userinfo avec le jeton d'accès : le sujet (pseudonyme) et ce que
// l'accord laisse voir. Rien si l'émetteur n'annonce pas de userinfo.
func (c *Client) QuiEst(ctx context.Context, d Decouverte, jeton string) (map[string]any, error) {
	if d.UserinfoEndpoint == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.UserinfoEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+jeton)
	req.Header.Set("Accept", "application/json")
	rep, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo injoignable : %w", errSansURL(err))
	}
	defer rep.Body.Close()
	if rep.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo a répondu %d", rep.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(io.LimitReader(rep.Body, 1<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("userinfo illisible")
	}
	return m, nil
}
