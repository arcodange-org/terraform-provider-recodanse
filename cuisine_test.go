package main

// cuisine_test.go — CE DÉPÔT EST PUBLIC : aucun texte suivi ne renvoie à
// l'organisation interne du projet (décisions datées et qui les a prises,
// numéros de lots, issues ou spécifications internes, outils de travail,
// hôtes du réseau privé). Une règle s'y explique par sa RAISON technique,
// jamais par l'endroit d'où elle vient.
//
// La garde balaie `git ls-files` et rougit en nommant le fichier, la ligne et
// le motif. Deux témoins l'empêchent d'être verte pour une mauvaise raison :
//   - chaque motif doit MATCHER son exemple (un motif qui ne matche rien serait
//     vert pour toujours), et chaque exception doit être réellement retirée ;
//   - le balayage doit avoir lu des fichiers, README.md compris (une liste vide
//     serait verte sur tout).
//
// Exclus, déclarés : `go.sum` (des empreintes, aucune prose), ce fichier (il
// porte les motifs), et, pour l'hôte de la forge seulement, `.gitea/` (la CI de
// la forge peut avoir besoin de son adresse).

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

type motifDeCuisine struct {
	nom     string
	re      *regexp.Regexp
	exemple string // doit matcher : le témoin que le motif mord
	horsDe  string // préfixe de chemin où le motif ne s'applique pas ("" : partout)
}

var motifsDeCuisine = []motifDeCuisine{
	{"qui a décidé", regexp.MustCompile(`(?i)fondat(eur|rice)`), "demandé par le Fondateur", ""},
	{"numéro de lot", regexp.MustCompile(`\bL[0-9]{2,3}\b`), "construit par le lot L261", ""},
	{"dépôt ou issue interne", regexp.MustCompile(`(?i)kadans`), "voir kadans-api#405", ""},
	{"spécification interne", regexp.MustCompile(`(?i)\bfiche [0-9]+`), "(fiche 52 § H)", ""},
	{"règle de spec interne", regexp.MustCompile(`\b(RG|QO|ADR)-[0-9]+`), "RG-55, QO-2, ADR-0044", ""},
	{"outil de travail", regexp.MustCompile(`(?i)\b(haiku|passation|sessions?|recette)\b`), "la session de recette", ""},
	{"fichier d'agent", regexp.MustCompile(`(?i)\b(AGENTS|CLAUDE)\.md\b`), "(AGENTS.md § 2)", ""},
	{"rythme interne", regexp.MustCompile(`(?i)par nuit`), "une étude par nuit", ""},
	{"guide de publication interne", regexp.MustCompile(`scripts/publier`), "voir scripts/publier/README.md", ""},
	{"hôte du réseau privé", regexp.MustCompile(`(?i)[a-z0-9.-]*\.arcodange\.lab\b`), "gitea.arcodange.lab/x", ".gitea/"},
	{"adresse du réseau privé", regexp.MustCompile(`\b192\.168\.[0-9]+\.[0-9]+`), "ssh://git@192.168.1.202:2222/x", ".gitea/"},
}

// exceptionsDeCuisine — des chaînes FONCTIONNELLES qu'on ne peut pas renommer
// sans changer un comportement : elles sont retirées de la ligne avant le
// balayage. La route des comptes de démonstration de l'API locale s'appelle
// ainsi côté API.
var exceptionsDeCuisine = []string{"/v0/recette/session/"}

func sansExceptions(ligne string) string {
	for _, e := range exceptionsDeCuisine {
		ligne = strings.ReplaceAll(ligne, e, "")
	}
	return ligne
}

func TestLesMotifsDeLaCuisineMordent(t *testing.T) {
	for _, m := range motifsDeCuisine {
		if !m.re.MatchString(m.exemple) {
			t.Errorf("le motif « %s » ne matche pas son propre exemple %q : il serait vert pour toujours", m.nom, m.exemple)
		}
	}
	for _, e := range exceptionsDeCuisine {
		ligne := "rep := Post(api+\"" + e + "\"+persona)"
		for _, m := range motifsDeCuisine {
			if m.re.MatchString(sansExceptions(ligne)) {
				t.Errorf("l'exception %q n'est pas retirée : le motif « %s » la voit encore", e, m.nom)
			}
		}
	}
	// Ce qui DOIT rester permis : un mot courant proche d'un motif.
	for _, permis := range []string{"le dossier du binaire", "LICENSE", "L'API", "Lxx", "une seule fois"} {
		for _, m := range motifsDeCuisine {
			if m.re.MatchString(permis) {
				t.Errorf("le motif « %s » mord sur un texte permis %q", m.nom, permis)
			}
		}
	}
}

func TestAucuneCuisineInterneDansLeDepotPublic(t *testing.T) {
	sortie, err := exec.Command("git", "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files : %v — la garde a besoin d'une copie de travail git ; elle ne se saute pas", err)
	}
	var lus int
	var readme bool
	for _, chemin := range strings.Split(strings.TrimRight(string(sortie), "\x00"), "\x00") {
		if chemin == "" || chemin == "go.sum" || chemin == "cuisine_test.go" {
			continue
		}
		contenu, err := os.ReadFile(chemin)
		if err != nil {
			t.Fatalf("lire %s : %v", chemin, err)
		}
		if bytes.IndexByte(contenu, 0) >= 0 {
			continue // binaire
		}
		lus++
		readme = readme || chemin == "README.md"
		sc := bufio.NewScanner(bytes.NewReader(contenu))
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for n := 1; sc.Scan(); n++ {
			ligne := sansExceptions(sc.Text())
			for _, m := range motifsDeCuisine {
				if m.horsDe != "" && strings.HasPrefix(chemin, m.horsDe) {
					continue
				}
				if trouve := m.re.FindString(ligne); trouve != "" {
					t.Errorf("%s:%d : %s (%q) — un dépôt public dit la raison, pas d'où elle vient", chemin, n, m.nom, trouve)
				}
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("lire %s : %v", chemin, err)
		}
	}
	if lus < 20 || !readme {
		t.Fatalf("balayage aveugle : %d fichiers lus (README.md lu : %v) — attendu au moins 20, README.md compris", lus, readme)
	}
}
