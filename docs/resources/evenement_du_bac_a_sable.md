---
page_title: "recodanse_evenement_du_bac_a_sable (Resource) - recodanse"
subcategory: "Bac à sable"
description: |-
  BAC À SABLE SEULEMENT — déclencher autorisation.retiree ou compte.efface pour un compte fictif.
---

# recodanse_evenement_du_bac_a_sable (ressource)

**Bac à sable seulement** : refusée dès le plan si
l'environnement du fournisseur n'est pas `bac-a-sable`.

Déclenche, sur les webhooks d'une de **tes** applications, `autorisation.retiree`
ou `compte.efface` pour un compte fictif qui l'a autorisée. L'accord est
vraiment retiré ; l'envoi part signé comme en production ; `compte.efface`
n'efface pas le compte fictif (il sert à tous).

- **Créer** déclenche une fois.
- Changer **`triggers`** redéclenche (le compte doit d'abord autoriser de nouveau
  l'application, sinon l'API répond 409 `pas_autorisee`).
- **Détruire ne fait rien** : un envoi parti ne se reprend pas.

## Exemple

```terraform
data "recodanse_comptes_fictifs" "tous" {}

resource "recodanse_evenement_du_bac_a_sable" "effacement" {
  application = recodanse_application.site.id
  compte      = data.recodanse_comptes_fictifs.tous.comptes[0].cle
  evenement   = "compte.efface"
  triggers    = { essai = "1" }
}
```

## Schéma

### Requis

- `application` (String) — `appl_…`.
- `compte` (String) — la `cle` d'un compte fictif.
- `evenement` (String) — `autorisation.retiree` ou `compte.efface`.

### Optionnel

- `triggers` (Map of String) — changer une valeur redéclenche.

### Lecture seule

- `id` (String) — `<application>/<compte>/<evenement>/<instant>`, propre au fournisseur.
- `sujet` (String) — le pseudonyme du compte fictif pour cette application, celui que le webhook reçoit.
- `declenche_le` (String) — RFC 3339.

## Contrat (supposé)

`POST /v1/bac-a-sable/evenements` `{"application","compte","evenement"}` → 202
`{"evenement","sujet"}` ; 400 `evenement_non_declenchable` / `compte_fictif_inconnu` ;
404 (application d'un autre compte, ou bac à sable inactif) ; 409 `pas_autorisee`.
