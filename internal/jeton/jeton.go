// Package jeton — d'où vient le jeton que présentent le fournisseur et la CLI,
// et le fichier où `recodanse-connexion connexion` le range.
//
// Deux espèces de jetons ouvrent `/v1/applications` (contrat dev-recodanse-iac,
// point 2) :
//   - le JETON DE DÉVELOPPEUR `rdk_…`, créé dans l'app (« Mes applications ») ;
//   - un JETON D'ACCÈS OAuth du client `recodanse-outils`, portée `developpeur`,
//     obtenu par l'OAuth device code (`recodanse-connexion connexion`).
//
// L'ordre, du plus explicite au plus implicite — et deux sources au MÊME rang
// sont une erreur, jamais un choix silencieux :
//
//  1. l'attribut `jeton`, ou l'attribut `jeton_fichier` ;
//  2. la variable RECODANSE_JETON, ou la variable RECODANSE_JETON_FICHIER ;
//  3. le fichier par défaut, ~/.config/recodanse/jeton, s'il existe.
//
// ⚠ CE QU'IL REFUSE :
//   - un fichier lisible par d'autres que soi (comme ssh refuse une clé en 0644) ;
//   - un fichier obtenu sur un AUTRE émetteur que celui de l'environnement choisi :
//     un jeton de la QA ne part pas vers la production ;
//   - d'écrire la valeur d'un jeton dans une erreur.
package jeton

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/environnement"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/oauth"
)

// Les variables d'environnement lues.
const (
	VariableJeton   = "RECODANSE_JETON"
	VariableFichier = "RECODANSE_JETON_FICHIER"
)

// marge — un jeton qui expire dans moins de ça est déjà renouvelé : un `apply`
// de quelques minutes ne doit pas mourir en chemin.
const marge = 2 * time.Minute

// FichierParDefaut — ~/.config/recodanse/jeton ("" si le dossier personnel est inconnu).
func FichierParDefaut() string {
	d, err := os.UserHomeDir()
	if err != nil || d == "" {
		return ""
	}
	return filepath.Join(d, ".config", "recodanse", "jeton")
}

// Rangement — le contenu du fichier écrit par `recodanse-connexion connexion`.
type Rangement struct {
	Environnement  string    `json:"environnement"`
	API            string    `json:"api"`
	Emetteur       string    `json:"emetteur"`
	Client         string    `json:"client"`
	Portee         string    `json:"portee,omitempty"`
	JetonDAcces    string    `json:"jeton_d_acces"`
	ExpireLe       time.Time `json:"expire_le,omitempty"`
	Renouvellement string    `json:"jeton_de_renouvellement,omitempty"`
}

// Source — d'où le jeton vient : une valeur, ou un fichier.
type Source struct {
	Valeur  string
	Fichier string
	// Origine — pour les messages : « l'attribut jeton », « RECODANSE_JETON_FICHIER »…
	Origine string
}

// ErrAucunJeton — rien n'a été donné, et le fichier par défaut n'existe pas.
var ErrAucunJeton = errors.New("aucun jeton : connecte-toi avec `recodanse-connexion connexion` " +
	"(OAuth device code), ou crée un jeton de développeur dans l'app ReCoDanse (« Mes applications » → " +
	"« Jetons de développeur ») et passe-le par RECODANSE_JETON — jamais en clair dans un fichier versionné")

// Choisir — la source du jeton, selon l'ordre du paquet. `env` lit une variable
// (os.Getenv en vrai) ; `parDefaut` est le fichier par défaut ("" : aucun).
func Choisir(attrJeton, attrFichier string, env func(string) string, parDefaut string) (Source, error) {
	if attrJeton != "" && attrFichier != "" {
		return Source{}, errors.New("`jeton` et `jeton_fichier` sont tous deux donnés : garde l'un ou l'autre")
	}
	if attrJeton != "" {
		return Source{Valeur: attrJeton, Origine: "l'attribut jeton"}, nil
	}
	if attrFichier != "" {
		return Source{Fichier: attrFichier, Origine: "l'attribut jeton_fichier"}, nil
	}
	ej, ef := env(VariableJeton), env(VariableFichier)
	if ej != "" && ef != "" {
		return Source{}, fmt.Errorf("%s et %s sont toutes deux posées : garde l'une ou l'autre", VariableJeton, VariableFichier)
	}
	if ej != "" {
		return Source{Valeur: ej, Origine: VariableJeton}, nil
	}
	if ef != "" {
		return Source{Fichier: ef, Origine: VariableFichier}, nil
	}
	if parDefaut != "" {
		if _, err := os.Stat(parDefaut); err == nil {
			return Source{Fichier: parDefaut, Origine: "le fichier par défaut"}, nil
		}
	}
	return Source{}, ErrAucunJeton
}

// Lecteur — ce qu'il faut pour lire un fichier de jeton et, au besoin, le renouveler.
type Lecteur struct {
	Adresses   environnement.Adresses
	OAuth      *oauth.Client // nil : un client vers Adresses.Emetteur
	Maintenant func() time.Time
}

func (l *Lecteur) maintenant() time.Time {
	if l.Maintenant != nil {
		return l.Maintenant()
	}
	return time.Now()
}

// Valeur — le jeton à présenter à l'API.
func (l *Lecteur) Valeur(ctx context.Context, s Source) (string, error) {
	if s.Fichier == "" {
		v := strings.TrimSpace(s.Valeur)
		if v == "" || strings.ContainsAny(v, " \t\r\n") {
			return "", fmt.Errorf("le jeton donné par %s est vide ou contient des blancs", s.Origine)
		}
		return v, nil
	}
	brut, err := LireLeFichier(s.Fichier)
	if err != nil {
		return "", err
	}
	texte := strings.TrimSpace(string(brut))
	if !strings.HasPrefix(texte, "{") {
		if texte == "" || strings.ContainsAny(texte, " \t\r\n") {
			return "", fmt.Errorf("%s : un seul jeton par fichier, sans blanc", s.Fichier)
		}
		return texte, nil // un jeton nu (rdk_…, ou un jeton d'accès posé par une CI)
	}
	var r Rangement
	if err := json.Unmarshal([]byte(texte), &r); err != nil || r.JetonDAcces == "" {
		return "", fmt.Errorf("%s : rangement illisible (ni un jeton nu, ni le JSON de recodanse-connexion)", s.Fichier)
	}
	if r.Emetteur != "" && strings.TrimRight(r.Emetteur, "/") != l.Adresses.Emetteur {
		return "", fmt.Errorf("%s a été obtenu sur %s (%s), mais le fournisseur parle à %s (%s) : "+
			"un jeton ne change pas d'environnement — relance `recodanse-connexion connexion --environnement %s`",
			s.Fichier, r.Emetteur, nomOuInconnu(r.Environnement), l.Adresses.Emetteur, l.Adresses.Environnement, l.Adresses.Environnement)
	}
	if r.ExpireLe.IsZero() || l.maintenant().Add(marge).Before(r.ExpireLe) {
		return r.JetonDAcces, nil
	}
	if r.Renouvellement == "" {
		return "", fmt.Errorf("le jeton de %s a expiré le %s : relance `recodanse-connexion connexion` "+
			"(avec --hors-ligne, il se renouvelle seul pendant trente jours)", s.Fichier, r.ExpireLe.Format(time.RFC3339))
	}
	return l.renouveler(ctx, s.Fichier, r)
}

func nomOuInconnu(n string) string {
	if n == "" {
		return "environnement non dit"
	}
	return n
}

// renouveler — refresh_token, puis le fichier réécrit (Hydra fait tourner le
// jeton de renouvellement : garder l'ancien, ce serait se déconnecter).
func (l *Lecteur) renouveler(ctx context.Context, fichier string, r Rangement) (string, error) {
	c := l.OAuth
	if c == nil {
		c = &oauth.Client{Emetteur: l.Adresses.Emetteur, ClientID: r.Client}
	}
	d, err := c.Decouvrir(ctx)
	if err != nil {
		return "", fmt.Errorf("renouveler le jeton de %s : %w", fichier, err)
	}
	j, err := c.Renouveler(ctx, d, r.Renouvellement)
	if err != nil {
		return "", fmt.Errorf("renouveler le jeton de %s : %w — relance `recodanse-connexion connexion`", fichier, err)
	}
	r.JetonDAcces, r.ExpireLe = j.AccessToken, j.ExpireLe
	if j.RefreshToken != "" {
		r.Renouvellement = j.RefreshToken
	}
	if err := Ecrire(fichier, r); err != nil {
		return "", fmt.Errorf("jeton renouvelé, mais %s n'a pas pu être réécrit : %w", fichier, err)
	}
	return r.JetonDAcces, nil
}

// LireLeFichier — le contenu, si le fichier n'est lisible que par soi.
func LireLeFichier(fichier string) ([]byte, error) {
	info, err := os.Stat(fichier)
	if err != nil {
		return nil, fmt.Errorf("fichier de jeton illisible : %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s est lisible par d'autres (%#o) : `chmod 600 %s`, puis recommence", fichier, info.Mode().Perm(), fichier)
	}
	return os.ReadFile(fichier)
}

// Ecrire — le rangement en JSON, en 0600, par renommage (jamais un fichier à moitié écrit).
func Ecrire(fichier string, r Rangement) error {
	if err := os.MkdirAll(filepath.Dir(fichier), 0o700); err != nil {
		return err
	}
	brut, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(fichier), ".jeton-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // sans effet après le renommage
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(brut, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), fichier)
}

// Lire — le rangement d'un fichier écrit par la CLI (pour `deconnexion` et `qui`).
func Lire(fichier string) (Rangement, error) {
	brut, err := LireLeFichier(fichier)
	if err != nil {
		return Rangement{}, err
	}
	var r Rangement
	if err := json.Unmarshal(brut, &r); err != nil || r.JetonDAcces == "" {
		return Rangement{}, fmt.Errorf("%s n'est pas un rangement de recodanse-connexion", fichier)
	}
	return r, nil
}
