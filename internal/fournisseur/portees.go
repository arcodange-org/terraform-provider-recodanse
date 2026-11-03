package fournisseur

import "sort"

// PorteesParCategorie — les portées qu'une application peut DEMANDER, par
// catégorie, pour les refuser dès le `plan` au lieu d'un 400 au milieu d'un
// `apply`.
//
// ⚠ C'EST UNE COPIE, ET ELLE EST GARDÉE : le catalogue fait autorité dans l'API
// (`internal/tiers/tiers.go`, `portesParCategorie`), que ce module ne peut pas
// importer (paquet `internal` d'un autre module). `tiers_terraform_test.go`, à la
// racine du dépôt, lit CETTE déclaration dans l'arbre syntaxique et la confronte
// à `tiers.PorteesPermises`, dans les deux sens. L'API reste l'arbitre : une
// portée retirée là-bas est refusée par elle, quoi que dise cette liste.
//
// L'agent n'y est pas : il ne se déclare pas, il s'enregistre seul (`/v1/enregistrement`) —
// ses portées vivent à part, dans PorteesDesAgents, pour le catalogue.
var PorteesParCategorie = map[string][]string{
	"connexion": {"openid", "profile", "email", "offline_access", "videos.lire", "videos.ranger",
		"videos.deposer", "videos.annoter", "programmes.poser", "evenements.proposer"},
	"service": {"evenements.publier", "creneaux.publier"},
}

func porteePermise(categorie, portee string) bool {
	for _, p := range PorteesParCategorie[categorie] {
		if p == portee {
			return true
		}
	}
	return false
}

// PorteesDesAgents — ce qu'un agent (client MCP enregistré seul, RFC 7591) peut
// recevoir. Il ne se déclare pas par ce fournisseur : la liste ne sert qu'à la
// source de données `recodanse_catalogue`. ⚠ Même règle que la carte ci-dessus :
// une COPIE des portées de la catégorie `agent` côté API, que la source de données confronte à ce que l'API
// ANNONCE (`scopes_supported`) à chaque lecture.
var PorteesDesAgents = []string{"openid", "offline_access", "videos.lire", "videos.ranger",
	"videos.annoter", "programmes.poser"}

// EspecesDeclarables — les espèces qu'on déclare (les clés de PorteesParCategorie, triées).
func EspecesDeclarables() []string {
	var e []string
	for k := range PorteesParCategorie {
		e = append(e, k)
	}
	sort.Strings(e)
	return e
}
