// recodanse-connexion — la CLI compagnon du fournisseur : se connecter à son
// compte développeur ReCoDanse par l'OAuth device code (RFC 8628), sans jamais
// copier un jeton à la main (contrat dev-recodanse-iac, point 3).
//
//	recodanse-connexion connexion   [--environnement qa] [--hors-ligne] [--sortie FICHIER] [--ci]
//	recodanse-connexion qui         [--environnement qa] [--fichier FICHIER]
//	recodanse-connexion deconnexion [--fichier FICHIER | --ci]
//
// Le geste humain est UN seul : ouvrir l'adresse affichée, se connecter avec SON
// compte ReCoDanse, approuver « les outils du développeur ». Le reste se lit.
//
// ⚠ LE JETON NE S'AFFICHE JAMAIS. Il va dans un fichier 0600 (défaut
// ~/.config/recodanse/jeton), ou, avec --ci, dans $GITHUB_ENV après un
// `::add-mask::` (GitHub Actions et Gitea Actions le lisent tous deux) — et
// nulle part ailleurs. Les messages vont sur la sortie d'erreur ; la sortie
// standard ne porte que les commandes de workflow de la CI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/api"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/environnement"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/jeton"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/oauth"
)

// version — posée à la construction (`-ldflags "-X main.version=0.2.0"`).
var version = "dev"

// outils — ce que les essais remplacent (le reste est le vrai code).
type outils struct {
	env        func(string) string
	http       *http.Client
	attendre   func(context.Context, time.Duration) error
	maintenant func() time.Time
	parDefaut  string // le fichier par défaut
}

func main() {
	ctx, arreter := signal.NotifyContext(context.Background(), os.Interrupt)
	defer arreter()
	o := outils{env: os.Getenv, parDefaut: jeton.FichierParDefaut()}
	os.Exit(executer(ctx, os.Args[1:], os.Stdout, os.Stderr, o))
}

const usage = `recodanse-connexion %s — se connecter à son compte développeur ReCoDanse (OAuth device code)

  connexion     ouvre une connexion : une adresse et un code à approuver dans le navigateur
                --environnement production|qa|bac-a-sable  (RECODANSE_ENVIRONNEMENT ; défaut production)
                --api URL, --emetteur URL                  (RECODANSE_API, RECODANSE_EMETTEUR)
                --sortie FICHIER   où ranger le jeton (0600) ; défaut RECODANSE_JETON_FICHIER, sinon ~/.config/recodanse/jeton
                --hors-ligne       demande offline_access : le jeton se renouvelle seul trente jours
                --ci               n'écrit aucun fichier : ::add-mask:: puis RECODANSE_JETON dans $GITHUB_ENV
  qui           dit qui est connecté, et le prouve par GET /v1/applications (sans afficher le jeton)
  deconnexion   révoque le jeton chez l'émetteur (RFC 7009) et efface le fichier
                --ci   sans fichier : révoque RECODANSE_JETON (et RECODANSE_JETON_DE_RENOUVELLEMENT s'il existe)
                       à la révocation que découvre RECODANSE_EMETTEUR ; 0 révoqué ou déjà inactif, 1 sinon
`

func executer(ctx context.Context, args []string, sortie, erreurs io.Writer, o outils) int {
	if o.env == nil {
		o.env = func(string) string { return "" }
	}
	if len(args) == 0 {
		fmt.Fprintf(erreurs, usage, version)
		return 2
	}
	var err error
	switch args[0] {
	case "connexion":
		err = connexion(ctx, args[1:], sortie, erreurs, o)
	case "qui":
		err = qui(ctx, args[1:], erreurs, o)
	case "deconnexion":
		err = deconnexion(ctx, args[1:], erreurs, o)
	case "version", "--version":
		fmt.Fprintln(erreurs, "recodanse-connexion", version)
		return 0
	case "aide", "-h", "--help", "help":
		fmt.Fprintf(erreurs, usage, version)
		return 0
	default:
		fmt.Fprintf(erreurs, "commande inconnue « %s »\n\n", args[0])
		fmt.Fprintf(erreurs, usage, version)
		return 2
	}
	var u errUsage
	if errors.As(err, &u) {
		fmt.Fprintln(erreurs, "recodanse-connexion :", err)
		return 2
	}
	if err != nil {
		fmt.Fprintln(erreurs, "recodanse-connexion :", err)
		return 1
	}
	return 0
}

type errUsage struct{ error }

// drapeauxDAdresses — --environnement, --api, --emetteur, communs aux trois commandes.
type drapeauxDAdresses struct{ environnement, api, emetteur string }

func (d *drapeauxDAdresses) poser(fs *flag.FlagSet) {
	fs.StringVar(&d.environnement, "environnement", "", "production, qa ou bac-a-sable")
	fs.StringVar(&d.api, "api", "", "l'adresse de l'API (surcharge)")
	fs.StringVar(&d.emetteur, "emetteur", "", "l'adresse de l'émetteur (surcharge)")
}

// resoudre — drapeau, sinon variable, sinon (pour qui et deconnexion) le rangement lu.
func (d *drapeauxDAdresses) resoudre(env func(string) string, r *jeton.Rangement) (environnement.Adresses, error) {
	nom, a, e := premier(d.environnement, env("RECODANSE_ENVIRONNEMENT")), premier(d.api, env("RECODANSE_API")),
		premier(d.emetteur, env("RECODANSE_EMETTEUR"))
	if nom == "" && a == "" && e == "" && r != nil {
		nom, a, e = r.Environnement, r.API, r.Emetteur
	}
	return environnement.Resoudre(nom, a, e)
}

func premier(v ...string) string {
	for _, x := range v {
		if x != "" {
			return x
		}
	}
	return ""
}

func analyser(fs *flag.FlagSet, args []string, erreurs io.Writer) error {
	fs.SetOutput(erreurs)
	if err := fs.Parse(args); err != nil {
		return errUsage{err}
	}
	if fs.NArg() > 0 {
		return errUsage{fmt.Errorf("argument inattendu « %s »", fs.Arg(0))}
	}
	return nil
}

func (o outils) clientOAuth(emetteur string) *oauth.Client {
	return &oauth.Client{Emetteur: emetteur, HTTP: o.http, Attendre: o.attendre, Maintenant: o.maintenant}
}

func (o outils) fichierParDefaut() string {
	return premier(o.env(jeton.VariableFichier), o.parDefaut)
}

// ── connexion ────────────────────────────────────────────────────────────────

func connexion(ctx context.Context, args []string, sortie, erreurs io.Writer, o outils) error {
	fs := flag.NewFlagSet("connexion", flag.ContinueOnError)
	var adr drapeauxDAdresses
	adr.poser(fs)
	fichier := fs.String("sortie", "", "où ranger le jeton (0600)")
	horsLigne := fs.Bool("hors-ligne", false, "demander offline_access (renouvellement trente jours)")
	ci := fs.Bool("ci", false, "écrire RECODANSE_JETON dans $GITHUB_ENV, masqué, sans fichier")
	if err := analyser(fs, args, erreurs); err != nil {
		return err
	}
	if *ci && *fichier != "" {
		return errUsage{errors.New("--ci n'écrit aucun fichier : --sortie n'a pas de sens avec lui")}
	}
	if *ci && *horsLigne {
		return errUsage{errors.New("--ci ne demande pas offline_access : un jeton de CI vit une heure et ne se renouvelle pas")}
	}
	cheminCI := ""
	if *ci {
		if cheminCI = o.env("GITHUB_ENV"); cheminCI == "" {
			return errors.New("--ci : $GITHUB_ENV est absent — ce n'est pas un job GitHub Actions ou Gitea Actions")
		}
	}
	if *fichier == "" {
		*fichier = o.fichierParDefaut()
	}
	if !*ci && *fichier == "" {
		return errUsage{errors.New("aucun fichier où ranger le jeton : donne --sortie")}
	}
	a, err := adr.resoudre(o.env, nil)
	if err != nil {
		return err
	}
	c := o.clientOAuth(a.Emetteur)
	d, err := c.Decouvrir(ctx)
	if err != nil {
		return err
	}
	portee := oauth.PorteeDesOutils
	if *horsLigne {
		portee += " offline_access"
	}
	dem, err := c.DemanderUnCode(ctx, d, portee)
	if err != nil {
		return fmt.Errorf("demander un code : %w", err)
	}
	fmt.Fprintf(erreurs, "Pour connecter ces outils à ton compte ReCoDanse (%s) :\n", a.Environnement)
	if dem.VerificationURIComplete != "" {
		fmt.Fprintf(erreurs, "  ouvre        %s\n", dem.VerificationURIComplete)
		fmt.Fprintf(erreurs, "  ou va sur    %s\n", dem.VerificationURI)
	} else {
		fmt.Fprintf(erreurs, "  va sur       %s\n", dem.VerificationURI)
	}
	fmt.Fprintf(erreurs, "  et vérifie le code : %s\n", dem.UserCode)
	if dem.ExpiresIn > 0 {
		fmt.Fprintf(erreurs, "  il vaut %s.\n", duree(time.Duration(dem.ExpiresIn)*time.Second))
	}
	fmt.Fprintln(erreurs, "En attente de ton approbation…")
	j, err := c.AttendreLApprobation(ctx, d, dem)
	if err != nil {
		return err
	}
	if strings.ContainsAny(j.AccessToken, "\r\n") || strings.ContainsAny(j.RefreshToken, "\r\n") {
		return errors.New("l'émetteur a rendu un jeton sur plusieurs lignes : refusé")
	}
	if *ci {
		// D'abord le masque, PUIS l'écriture : le journal ne doit jamais l'avoir vu en clair.
		fmt.Fprintf(sortie, "::add-mask::%s\n", j.AccessToken)
		f, err := os.OpenFile(cheminCI, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
		if err != nil {
			return fmt.Errorf("ouvrir $GITHUB_ENV : %w", err)
		}
		_, err = fmt.Fprintf(f, "RECODANSE_JETON=%s\nRECODANSE_ENVIRONNEMENT=%s\nRECODANSE_API=%s\nRECODANSE_EMETTEUR=%s\n",
			j.AccessToken, a.Environnement, a.API, a.Emetteur)
		if errF := f.Close(); err == nil {
			err = errF
		}
		if err != nil {
			return fmt.Errorf("écrire $GITHUB_ENV : %w", err)
		}
		fmt.Fprintf(erreurs, "Connecté (%s). RECODANSE_JETON est posé pour les étapes suivantes du job, masqué%s.\n",
			a.Environnement, jusquA(j.ExpireLe))
		return nil
	}
	r := jeton.Rangement{Environnement: a.Environnement, API: a.API, Emetteur: a.Emetteur, Client: oauth.ClientDesOutils,
		Portee: portee, JetonDAcces: j.AccessToken, ExpireLe: j.ExpireLe, Renouvellement: j.RefreshToken}
	if err := jeton.Ecrire(*fichier, r); err != nil {
		return fmt.Errorf("ranger le jeton dans %s : %w", *fichier, err)
	}
	fmt.Fprintf(erreurs, "Connecté (%s). Jeton rangé dans %s (0600)%s.\n", a.Environnement, *fichier, jusquA(j.ExpireLe))
	if j.RefreshToken != "" {
		fmt.Fprintln(erreurs, "Il se renouvelle seul (offline_access) ; `recodanse-connexion deconnexion` le révoque.")
	}
	return nil
}

func jusquA(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return ", valable jusqu'à " + t.Local().Format("2006-01-02 15:04")
}

func duree(d time.Duration) string {
	// Hydra annonce 599 s pour dix minutes : on arrondit à la minute, c'est un ordre de grandeur.
	if d >= time.Minute {
		return fmt.Sprintf("%d min", int((d+30*time.Second)/time.Minute))
	}
	return d.String()
}

// ── qui ──────────────────────────────────────────────────────────────────────

func qui(ctx context.Context, args []string, erreurs io.Writer, o outils) error {
	fs := flag.NewFlagSet("qui", flag.ContinueOnError)
	var adr drapeauxDAdresses
	adr.poser(fs)
	fichier := fs.String("fichier", "", "le fichier de jeton (défaut : RECODANSE_JETON_FICHIER, sinon ~/.config/recodanse/jeton)")
	if err := analyser(fs, args, erreurs); err != nil {
		return err
	}
	src, err := jeton.Choisir("", *fichier, o.env, o.parDefaut)
	if err != nil {
		return err
	}
	var r *jeton.Rangement
	if src.Fichier != "" {
		if lu, err := jeton.Lire(src.Fichier); err == nil {
			r = &lu
		}
	}
	a, err := adr.resoudre(o.env, r)
	if err != nil {
		return err
	}
	lecteur := &jeton.Lecteur{Adresses: a, OAuth: o.clientOAuth(a.Emetteur), Maintenant: o.maintenant}
	valeur, err := lecteur.Valeur(ctx, src)
	if err != nil {
		return err
	}
	espece := "un jeton d'accès OAuth"
	if strings.HasPrefix(valeur, "rdk_") {
		espece = "un jeton de développeur (rdk_…)"
	}
	fmt.Fprintf(erreurs, "Environnement : %s (API %s, émetteur %s)\n", a.Environnement, a.API, a.Emetteur)
	fmt.Fprintf(erreurs, "Jeton : %s, lu depuis %s\n", espece, origine(src))
	if r != nil {
		if lu, err := jeton.Lire(src.Fichier); err == nil && !lu.ExpireLe.IsZero() { // relu : il a pu être renouvelé
			fmt.Fprintf(erreurs, "Expire le : %s%s\n", lu.ExpireLe.Local().Format("2006-01-02 15:04"),
				map[bool]string{true: " (se renouvelle seul)", false: ""}[lu.Renouvellement != ""])
		}
	}
	if !strings.HasPrefix(valeur, "rdk_") {
		c := o.clientOAuth(a.Emetteur)
		if d, err := c.Decouvrir(ctx); err == nil {
			if info, err := c.QuiEst(ctx, d, valeur); err == nil && info != nil {
				fmt.Fprintf(erreurs, "Compte : %s\n", decrire(info))
			}
		}
	}
	cl := &api.Client{Adresse: a.API, Jeton: valeur, HTTP: o.http, Agent: "recodanse-connexion/" + version}
	l, err := cl.Applications(ctx)
	if err != nil {
		return fmt.Errorf("l'API refuse ce jeton : %w", err)
	}
	fmt.Fprintf(erreurs, "Connecté : %d application(s) sur %d au plus.\n", len(l.Applications), l.Max)
	return nil
}

func origine(s jeton.Source) string {
	if s.Fichier != "" {
		return s.Fichier
	}
	return s.Origine
}

// decrire — le sujet (pseudonyme) et ce que l'accord laisse voir, dans un ordre stable.
func decrire(info map[string]any) string {
	var parts []string
	for _, k := range []string{"name", "email", "sub"} {
		if v, ok := info[k].(string); ok && v != "" {
			parts = append(parts, k+" "+v)
		}
	}
	if len(parts) == 0 {
		var ks []string
		for k := range info {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		return "(userinfo sans sujet : " + strings.Join(ks, ", ") + ")"
	}
	return strings.Join(parts, " · ")
}

// ── deconnexion ──────────────────────────────────────────────────────────────

func deconnexion(ctx context.Context, args []string, erreurs io.Writer, o outils) error {
	fs := flag.NewFlagSet("deconnexion", flag.ContinueOnError)
	var adr drapeauxDAdresses
	adr.poser(fs)
	fichier := fs.String("fichier", "", "le fichier de jeton (défaut : RECODANSE_JETON_FICHIER, sinon ~/.config/recodanse/jeton)")
	ci := fs.Bool("ci", false, "révoquer RECODANSE_JETON (posé par `connexion --ci`), sans fichier")
	if err := analyser(fs, args, erreurs); err != nil {
		return err
	}
	if *ci {
		if *fichier != "" {
			return errUsage{errors.New("--ci ne lit aucun fichier : --fichier n'a pas de sens avec lui")}
		}
		return deconnexionCI(ctx, adr, erreurs, o)
	}
	if *fichier == "" {
		*fichier = o.fichierParDefaut()
	}
	if _, err := os.Stat(*fichier); errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(erreurs, "Aucune connexion rangée dans %s : rien à révoquer.\n", *fichier)
		return nil
	}
	r, err := jeton.Lire(*fichier)
	if err != nil {
		return fmt.Errorf("%w — un jeton de développeur rdk_… se révoque dans l'app (« Mes applications »)", err)
	}
	a, err := adr.resoudre(o.env, &r)
	if err != nil {
		return err
	}
	c := o.clientOAuth(a.Emetteur)
	d, err := c.Decouvrir(ctx)
	if err != nil {
		return fmt.Errorf("révoquer : %w — le fichier est GARDÉ (rien n'est révoqué) ; réessaie, ou retire « Les outils du développeur » dans « Applications autorisées »", err)
	}
	// Le renouvellement d'abord : chez Hydra, le révoquer emporte toute la chaîne.
	if r.Renouvellement != "" {
		if err := c.Revoquer(ctx, d, r.Renouvellement, "refresh_token"); err != nil {
			return fmt.Errorf("révoquer le renouvellement : %w — le fichier est GARDÉ", err)
		}
	}
	if err := c.Revoquer(ctx, d, r.JetonDAcces, "access_token"); err != nil {
		return fmt.Errorf("révoquer le jeton d'accès : %w — le fichier est GARDÉ", err)
	}
	if err := os.Remove(*fichier); err != nil {
		return fmt.Errorf("jeton révoqué, mais %s n'a pas pu être effacé : %w", *fichier, err)
	}
	fmt.Fprintf(erreurs, "Déconnecté (%s) : jeton révoqué chez l'émetteur, %s effacé.\n", a.Environnement, *fichier)
	return nil
}

// VariableRenouvellement — un jeton de renouvellement posé par la CI elle-même.
// `connexion --ci` n'en écrit JAMAIS (il refuse --hors-ligne) ; s'il existe, il
// vient d'ailleurs, et on le révoque aussi — il emporte toute la chaîne chez Hydra.
const VariableRenouvellement = "RECODANSE_JETON_DE_RENOUVELLEMENT"

// deconnexionCI — `deconnexion --ci` : `connexion --ci` ne range aucun fichier,
// donc la déconnexion lit le jeton là où elle l'a posé, dans l'environnement du
// job (RECODANSE_JETON, et RECODANSE_EMETTEUR pour la découverte). Sans elle, le
// jeton vivait son heure après la fin du job.
//
// Code 0 : l'émetteur a répondu 200 à chaque révocation — RFC 7009 § 2.2 : un
// jeton déjà inactif ou inconnu rend 200 aussi. Code 1 sinon (rien à révoquer,
// jeton rdk_, émetteur injoignable ou qui refuse) : un job qui croit avoir
// révoqué ne doit pas se tromper.
func deconnexionCI(ctx context.Context, adr drapeauxDAdresses, erreurs io.Writer, o outils) error {
	acces, renouvellement := strings.TrimSpace(o.env(jeton.VariableJeton)), strings.TrimSpace(o.env(VariableRenouvellement))
	if acces == "" && renouvellement == "" {
		return fmt.Errorf("--ci : %s est absent — rien à révoquer (la connexion a-t-elle eu lieu dans ce job ?)", jeton.VariableJeton)
	}
	if strings.HasPrefix(acces, "rdk_") {
		return errors.New("--ci : RECODANSE_JETON est un jeton de développeur rdk_… — il ne se révoque pas chez l'émetteur, " +
			"mais dans l'app (« Mes applications » → « Jetons de développeur »)")
	}
	a, err := adr.resoudre(o.env, nil)
	if err != nil {
		return err
	}
	c := o.clientOAuth(a.Emetteur)
	d, err := c.Decouvrir(ctx)
	if err != nil {
		return fmt.Errorf("révoquer : %w — rien n'est révoqué", err)
	}
	// Le renouvellement d'abord : chez Hydra, le révoquer emporte toute la chaîne.
	if renouvellement != "" {
		if err := c.Revoquer(ctx, d, renouvellement, "refresh_token"); err != nil {
			return fmt.Errorf("révoquer le renouvellement : %w", err)
		}
	}
	if acces != "" {
		if err := c.Revoquer(ctx, d, acces, "access_token"); err != nil {
			return fmt.Errorf("révoquer le jeton d'accès : %w", err)
		}
	}
	fmt.Fprintf(erreurs, "Déconnecté (%s) : jeton révoqué chez l'émetteur %s (ou déjà inactif).\n", a.Environnement, a.Emetteur)
	return nil
}
