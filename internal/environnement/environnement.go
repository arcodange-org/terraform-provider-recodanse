// Package environnement — où parler : l'API de ReCoDanse et son émetteur (la
// porte d'autorisation, Ory Hydra), par environnement.
//
// Trois noms, une seule table, lue par le fournisseur ET par la CLI
// `recodanse-connexion` : un jeton obtenu en « qa » ne doit jamais partir vers
// la production, et l'inverse — les deux outils doivent donc dire la même chose
// des mêmes noms.
//
//	production   https://api.recodanse.fr      https://connexion.recodanse.fr
//	qa           https://api-qa.recodanse.fr   https://connexion-qa.recodanse.fr
//	bac-a-sable  DÉCLARÉ, adresses à confirmer
//
// ⚠ LE BAC À SABLE N'A PAS ENCORE D'ADRESSE : les adresses prévues sont
// `bac-a-sable.recodanse.fr` et `connexion.bac-a-sable.recodanse.fr`, mais rien
// n'y répond aujourd'hui. Plutôt que de deviner une adresse qui enverrait un
// jeton vers un hôte inconnu, le nom refuse — clairement — tant qu'on ne donne
// pas `api` ET `emetteur` soi-même.
package environnement

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Les noms reconnus.
const (
	Production = "production"
	QA         = "qa"
	BacASable  = "bac-a-sable"
)

// ParDefaut — l'environnement quand rien n'est dit (comme avant : la production).
const ParDefaut = Production

// Adresses — où est l'API, où est l'émetteur.
type Adresses struct {
	Environnement string
	API           string
	Emetteur      string
}

// connues — les adresses de chaque environnement. Une chaîne vide : à confirmer.
var connues = map[string]Adresses{
	Production: {Environnement: Production, API: "https://api.recodanse.fr", Emetteur: "https://connexion.recodanse.fr"},
	QA:         {Environnement: QA, API: "https://api-qa.recodanse.fr", Emetteur: "https://connexion-qa.recodanse.fr"},
	BacASable:  {Environnement: BacASable},
}

// Noms — les environnements reconnus, triés (pour les messages et les validateurs).
func Noms() []string {
	var n []string
	for k := range connues {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// Resoudre — les adresses de `nom` ("" : la production), `api` et `emetteur`
// surchargeant celles de la table quand ils ne sont pas vides.
func Resoudre(nom, api, emetteur string) (Adresses, error) {
	nom = strings.TrimSpace(nom)
	if nom == "" {
		nom = ParDefaut
	}
	a, ok := connues[nom]
	if !ok {
		return Adresses{}, fmt.Errorf("environnement inconnu « %s » : attendu %s", nom, strings.Join(Noms(), ", "))
	}
	if api = strings.TrimRight(strings.TrimSpace(api), "/"); api != "" {
		a.API = api
	}
	if emetteur = strings.TrimRight(strings.TrimSpace(emetteur), "/"); emetteur != "" {
		a.Emetteur = emetteur
	}
	if a.API == "" || a.Emetteur == "" {
		return Adresses{}, fmt.Errorf("l'environnement « %s » est déclaré, mais ses adresses ne sont pas encore confirmées "+
			"(adresses prévues : https://bac-a-sable.recodanse.fr et https://connexion.bac-a-sable.recodanse.fr) : "+
			"donne `api` ET `emetteur` toi-même (RECODANSE_API, RECODANSE_EMETTEUR), ou choisis %s ou %s", nom, Production, QA)
	}
	for quoi, v := range map[string]string{"api": a.API, "emetteur": a.Emetteur} {
		if err := adresseAcceptable(v); err != nil {
			return Adresses{}, fmt.Errorf("%s « %s » : %w", quoi, v, err)
		}
	}
	return a, nil
}

// adresseAcceptable — https:// partout ; http:// seulement vers la machine même
// (essais locaux). Un jeton ne voyage jamais en clair sur un réseau.
func adresseAcceptable(v string) error {
	u, err := url.Parse(v)
	if err != nil || u.Host == "" {
		return fmt.Errorf("adresse illisible")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		h := u.Hostname()
		if h == "127.0.0.1" || h == "localhost" || h == "::1" {
			return nil
		}
		return fmt.Errorf("http:// n'est admis que vers 127.0.0.1 ou localhost : un jeton ne voyage pas en clair")
	}
	return fmt.Errorf("https:// attendu")
}
