// Package fournisseur — le fournisseur Terraform / OpenTofu de ReCoDanse : les
// applications OpenID et les webhooks d'un compte développeur, en code (rotation
// de secret, portées demandées, webhooks, revue).
//
// Il parle à `/v1/applications` avec un JETON DE DÉVELOPPEUR (`rdk_…`), créé dans
// l'app, « Mes applications », ou avec un JETON D'ACCÈS OAuth du client
// `recodanse-outils`, obtenu par `recodanse-connexion connexion` (OAuth device
// code). Le fournisseur ne fait rien que l'écran ne fasse : c'est le même geste,
// écrit dans un fichier qu'on relit et qu'on versionne.
//
// ⚠ CE QU'IL REFUSE :
//   - d'accepter les conditions des développeurs à la place de la personne :
//     `soumettre = true` exige `accepte_les_conditions` égal à la version EN
//     VIGUEUR, écrite de sa main ;
//   - de relire un secret : l'API ne le montre qu'une fois. Il entre dans l'état
//     (marqué sensible) à la création et à chaque rotation ; une application
//     importée n'en a pas tant qu'on ne la fait pas tourner ;
//   - de deviner une portée : le catalogue (`portees.go`) est confronté à celui de
//     l'API par une garde du dépôt (`tiers_terraform_test.go`), dans les deux sens.
package fournisseur

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/api"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/environnement"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/jeton"
)

// Nouveau — le fournisseur, pour `providerserver.Serve`.
func Nouveau(version string) func() provider.Provider {
	return func() provider.Provider {
		return &fournisseur{version: version, env: os.Getenv, parDefaut: jeton.FichierParDefaut()}
	}
}

type fournisseur struct {
	version string
	// env — os.Getenv en vrai ; les essais le remplacent (aucune variable du
	// poste ne doit fuir dans un essai, ni un fichier de ~/.config).
	env       func(string) string
	parDefaut string
}

type configuration struct {
	Adresse       types.String `tfsdk:"adresse"`
	API           types.String `tfsdk:"api"`
	Emetteur      types.String `tfsdk:"emetteur"`
	Environnement types.String `tfsdk:"environnement"`
	Jeton         types.String `tfsdk:"jeton"`
	JetonFichier  types.String `tfsdk:"jeton_fichier"`
}

func (f *fournisseur) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "recodanse"
	resp.Version = f.version
}

func (f *fournisseur) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Les applications tierces et les webhooks d'un compte développeur ReCoDanse, pilotés par un jeton " +
			"de développeur `rdk_…` ou par un jeton d'accès OAuth obtenu avec `recodanse-connexion connexion`.",
		Attributes: map[string]schema.Attribute{
			"environnement": schema.StringAttribute{
				Optional: true,
				Description: "production (défaut), qa, ou bac-a-sable (déclaré, adresses à confirmer : donne alors api ET emetteur). " +
					"Défaut : variable RECODANSE_ENVIRONNEMENT.",
				Validators: []validator.String{stringvalidator.OneOf(environnement.Noms()...)},
			},
			"api": schema.StringAttribute{
				Optional:    true,
				Description: "L'adresse de l'API, si elle n'est pas celle de l'environnement (défaut : variable RECODANSE_API).",
			},
			"emetteur": schema.StringAttribute{
				Optional: true,
				Description: "L'adresse de la porte d'autorisation (Hydra), si elle n'est pas celle de l'environnement " +
					"(défaut : variable RECODANSE_EMETTEUR). Elle sert à renouveler un jeton rangé par recodanse-connexion.",
			},
			"adresse": schema.StringAttribute{
				Optional:           true,
				DeprecationMessage: "remplacé par `api` (même sens) ; `adresse` sera retiré en 1.0.",
				Description:        "Ancien nom de `api`.",
			},
			"jeton": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "Un jeton de développeur `rdk_…`, ou un jeton d'accès OAuth du client recodanse-outils " +
					"(défaut : variable RECODANSE_JETON). Jamais en clair dans un fichier versionné.",
			},
			"jeton_fichier": schema.StringAttribute{
				Optional: true,
				Description: "Un fichier 0600 qui porte le jeton : celui écrit par `recodanse-connexion connexion`, ou un jeton nu " +
					"(défaut : variable RECODANSE_JETON_FICHIER, sinon ~/.config/recodanse/jeton s'il existe).",
			},
		},
	}
}

func valeur(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

func (f *fournisseur) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var conf configuration
	resp.Diagnostics.Append(req.Config.Get(ctx, &conf)...)
	if resp.Diagnostics.HasError() {
		return
	}
	env := f.env
	if env == nil {
		env = func(string) string { return "" }
	}
	if valeur(conf.API) != "" && valeur(conf.Adresse) != "" {
		resp.Diagnostics.AddAttributeError(path.Root("adresse"), "`api` et `adresse` sont tous deux donnés",
			"`adresse` est l'ancien nom de `api` : garde `api`.")
		return
	}
	adresses, err := environnement.Resoudre(
		premier(valeur(conf.Environnement), env("RECODANSE_ENVIRONNEMENT")),
		premier(valeur(conf.API), valeur(conf.Adresse), env("RECODANSE_API")),
		premier(valeur(conf.Emetteur), env("RECODANSE_EMETTEUR")))
	if err != nil {
		resp.Diagnostics.AddError("Environnement ReCoDanse", err.Error())
		return
	}
	src, err := jeton.Choisir(valeur(conf.Jeton), valeur(conf.JetonFichier), env, f.parDefaut)
	if err != nil {
		resp.Diagnostics.AddError("Jeton manquant", err.Error())
		return
	}
	// ⚠ La valeur du jeton n'entre dans AUCUN diagnostic : les erreurs du lecteur
	// nomment la source (variable, fichier), jamais ce qu'elle contient.
	v, err := (&jeton.Lecteur{Adresses: adresses}).Valeur(ctx, src)
	if err != nil {
		resp.Diagnostics.AddError("Jeton illisible", err.Error())
		return
	}
	c := &api.Client{Adresse: adresses.API, Jeton: v, Agent: "terraform-provider-recodanse/" + f.version,
		Environnement: adresses.Environnement}
	resp.ResourceData = c
	resp.DataSourceData = c
}

func premier(v ...string) string {
	for _, x := range v {
		if x != "" {
			return x
		}
	}
	return ""
}

func (f *fournisseur) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{nouvelleApplication, nouveauWebhook, nouvelEvenementDuBacASable}
}

func (f *fournisseur) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{nouveauCatalogue, nouvelleApplicationLue, nouvellesApplicationsLues,
		nouveauxComptesFictifs}
}

// clientDe — le client posé par Configure, ou une erreur lisible.
func clientDe(donnee any) (*api.Client, bool) {
	c, ok := donnee.(*api.Client)
	return c, ok && c != nil
}
