package fournisseur

// sources.go — les sources de données : `recodanse_catalogue`,
// `recodanse_application` (une, par identifiant) et `recodanse_applications`
// (toutes celles du compte).
//
// ⚠ CE QU'ELLES NE DISENT PAS, EXPRÈS : la version des conditions des
// développeurs EN VIGUEUR. La lire dans une source de données permettrait
// d'écrire `accepte_les_conditions = data.….conditions_en_vigueur` — accepter un
// texte sans l'avoir lu, ce que le fournisseur s'interdit. La version se
// lit sur dev.recodanse.fr et s'écrit de sa main ; le plan refuse une version
// périmée en nommant la bonne.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/api"
)

func configurerSource(req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) *api.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := clientDe(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Fournisseur mal configuré", fmt.Sprintf("client inattendu : %T", req.ProviderData))
		return nil
	}
	return c
}

func listeDeChaines(ctx context.Context, l []string, diags *diag.Diagnostics) types.List {
	if l == nil {
		l = []string{}
	}
	v, d := types.ListValueFrom(ctx, types.StringType, l)
	diags.Append(d...)
	return v
}

// ── recodanse_catalogue ──────────────────────────────────────────────────────

type modeleCatalogue struct {
	Especes               types.List `tfsdk:"especes"`
	EspecesDeclarables    types.List `tfsdk:"especes_declarables"`
	Portees               types.Map  `tfsdk:"portees"`
	EvenementsDesWebhooks types.List `tfsdk:"evenements_des_webhooks"`
	PorteesAnnoncees      types.List `tfsdk:"portees_annoncees"`
}

type sourceCatalogue struct{ client *api.Client }

func nouveauCatalogue() datasource.DataSource { return &sourceCatalogue{} }

func (s *sourceCatalogue) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_catalogue"
}

func (s *sourceCatalogue) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Le catalogue de ReCoDanse : les espèces d'applications, les portées que chacune peut demander, " +
			"les événements qu'un webhook peut écouter — et les portées que l'API ANNONCE, relues à chaque lecture.",
		Attributes: map[string]schema.Attribute{
			"especes": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				Description: "Toutes les espèces : agent, connexion, service."},
			"especes_declarables": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				Description: "Celles qu'on déclare (`recodanse_application.categorie`) : connexion, service. L'agent s'enregistre seul (RFC 7591)."},
			"portees": schema.MapAttribute{Computed: true, ElementType: types.ListType{ElemType: types.StringType},
				Description: "Les portées permises, par espèce (la copie du fournisseur, confrontée à l'API)."},
			"evenements_des_webhooks": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				Description: "Ce qu'un `recodanse_webhook` peut écouter."},
			"portees_annoncees": schema.ListAttribute{Computed: true, ElementType: types.StringType,
				Description: "Les portées que l'API annonce (`/.well-known/oauth-protected-resource`, `scopes_supported`). " +
					"Un écart avec `portees` produit un AVERTISSEMENT : le fournisseur est à mettre à jour."},
		},
	}
}

func (s *sourceCatalogue) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	s.client = configurerSource(req, resp)
}

// Ecarts — ce que la copie du fournisseur et l'annonce de l'API ne partagent pas.
func Ecarts(annoncees []string) (copieSeule, apiSeule []string) {
	copie := map[string]bool{}
	for _, l := range PorteesParCategorie {
		for _, p := range l {
			copie[p] = true
		}
	}
	for _, p := range PorteesDesAgents {
		copie[p] = true
	}
	vues := map[string]bool{}
	for _, p := range annoncees {
		vues[p] = true
		if !copie[p] {
			apiSeule = append(apiSeule, p)
		}
	}
	for p := range copie {
		if !vues[p] {
			copieSeule = append(copieSeule, p)
		}
	}
	sort.Strings(copieSeule)
	sort.Strings(apiSeule)
	return copieSeule, apiSeule
}

func (s *sourceCatalogue) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if s.client == nil {
		resp.Diagnostics.AddError("Fournisseur non configuré", "le catalogue a besoin de l'adresse de l'API")
		return
	}
	r, err := s.client.MetadonneesDeLaRessource(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Lire le catalogue de l'API", err.Error())
		return
	}
	copieSeule, apiSeule := Ecarts(r.PorteesAnnoncees)
	if len(copieSeule) > 0 {
		resp.Diagnostics.AddWarning("Le fournisseur connaît des portées que l'API n'annonce plus",
			strings.Join(copieSeule, ", ")+" : l'API les refusera ; mets à jour le fournisseur.")
	}
	if len(apiSeule) > 0 {
		resp.Diagnostics.AddWarning("L'API annonce des portées que le fournisseur ne connaît pas",
			strings.Join(apiSeule, ", ")+" : le plan les refuserait encore ; mets à jour le fournisseur.")
	}
	m := modeleCatalogue{
		Especes:               listeDeChaines(ctx, append([]string{"agent"}, EspecesDeclarables()...), &resp.Diagnostics),
		EspecesDeclarables:    listeDeChaines(ctx, EspecesDeclarables(), &resp.Diagnostics),
		EvenementsDesWebhooks: listeDeChaines(ctx, EvenementsDesWebhooks, &resp.Diagnostics),
		PorteesAnnoncees:      listeDeChaines(ctx, r.PorteesAnnoncees, &resp.Diagnostics),
	}
	parEspece := map[string]attr.Value{"agent": listeDeChaines(ctx, PorteesDesAgents, &resp.Diagnostics)}
	for e, l := range PorteesParCategorie {
		parEspece[e] = listeDeChaines(ctx, l, &resp.Diagnostics)
	}
	carte, d := types.MapValue(types.ListType{ElemType: types.StringType}, parEspece)
	resp.Diagnostics.Append(d...)
	m.Portees = carte
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// ── recodanse_application et recodanse_applications ─────────────────────────

type modeleApplicationLue struct {
	ID                  types.String `tfsdk:"id"`
	ClientID            types.String `tfsdk:"client_id"`
	Nom                 types.String `tfsdk:"nom"`
	Description         types.String `tfsdk:"description"`
	SiteWeb             types.String `tfsdk:"site_web"`
	Categorie           types.String `tfsdk:"categorie"`
	Statut              types.String `tfsdk:"statut"`
	Portees             types.List   `tfsdk:"portees"`
	Redirections        types.List   `tfsdk:"redirections"`
	Publique            types.Bool   `tfsdk:"publique"`
	CreeLe              types.String `tfsdk:"cree_le"`
	ConditionsAcceptees types.String `tfsdk:"conditions_acceptees"`
	ConditionsAJour     types.Bool   `tfsdk:"conditions_a_jour"`
}

// attributsLus — les attributs d'une application lue (`id` requis ou calculé).
func attributsLus(idRequis bool) map[string]schema.Attribute {
	chaine := func(d string) schema.Attribute { return schema.StringAttribute{Computed: true, Description: d} }
	liste := func(d string) schema.Attribute {
		return schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: d}
	}
	id := schema.StringAttribute{Computed: true, Description: "L'identifiant ReCoDanse (`appl_…`)."}
	if idRequis {
		id = schema.StringAttribute{Required: true, Description: "L'identifiant ReCoDanse (`appl_…`) de l'application à lire."}
	}
	return map[string]schema.Attribute{
		"id":           id,
		"client_id":    chaine("L'identifiant de client OAuth2 (Hydra)."),
		"nom":          chaine("Le nom lu sur la page « Autoriser »."),
		"description":  chaine(""),
		"site_web":     chaine(""),
		"categorie":    chaine("connexion, service ou agent."),
		"statut":       chaine("developpement, en_revue, publiee, suspendue ou non_verifiee."),
		"portees":      liste("Les portées demandées, triées."),
		"redirections": liste("Les adresses de retour, triées."),
		"publique":     schema.BoolAttribute{Computed: true, Description: "Client sans secret (PKCE)."},
		"cree_le":      chaine("RFC 3339."),
		"conditions_acceptees": chaine("La version des conditions des développeurs que la propriétaire a acceptée " +
			"(nulle si aucune). Le secret n'est JAMAIS lisible : l'API ne le montre qu'une fois."),
		"conditions_a_jour": schema.BoolAttribute{Computed: true,
			Description: "Vrai si la version acceptée est celle en vigueur."},
	}
}

func applicationLue(ctx context.Context, a api.Application, diags *diag.Diagnostics) modeleApplicationLue {
	tri := func(l []string) []string { l = append([]string(nil), l...); sort.Strings(l); return l }
	m := modeleApplicationLue{
		ID: types.StringValue(a.ID), ClientID: types.StringValue(a.ClientID), Nom: types.StringValue(a.Nom),
		Description: types.StringValue(a.Description), SiteWeb: types.StringValue(a.SiteWeb),
		Categorie: types.StringValue(a.Categorie), Statut: types.StringValue(a.Statut),
		Portees: listeDeChaines(ctx, tri(a.Portees), diags), Redirections: listeDeChaines(ctx, tri(a.Redirections), diags),
		Publique: types.BoolValue(a.Publique), CreeLe: types.StringValue(a.CreeLe),
		ConditionsAcceptees: types.StringNull(), ConditionsAJour: types.BoolValue(a.Conditions.AJour),
	}
	if a.Conditions.VersionAcceptee != nil {
		m.ConditionsAcceptees = types.StringValue(*a.Conditions.VersionAcceptee)
	}
	return m
}

type sourceApplication struct{ client *api.Client }

func nouvelleApplicationLue() datasource.DataSource { return &sourceApplication{} }

func (s *sourceApplication) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (s *sourceApplication) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Une de tes applications, lue par son identifiant — sans son secret (il ne se relit jamais).",
		Attributes:  attributsLus(true),
	}
}

func (s *sourceApplication) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	s.client = configurerSource(req, resp)
}

func (s *sourceApplication) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m modeleApplicationLue
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if s.client == nil {
		resp.Diagnostics.AddError("Fournisseur non configuré", "lire une application exige un jeton")
		return
	}
	a, err := s.client.Application(ctx, m.ID.ValueString())
	if api.EstIntrouvable(err) {
		resp.Diagnostics.AddError("Application introuvable",
			fmt.Sprintf("« %s » n'existe pas, ou n'est pas à ce compte (l'API rend le même 404).", m.ID.ValueString()))
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Lire l'application", err.Error())
		return
	}
	lue := applicationLue(ctx, a, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, lue)...)
}

type modeleApplicationsLues struct {
	Applications types.List  `tfsdk:"applications"`
	Max          types.Int64 `tfsdk:"max"`
}

type sourceApplications struct{ client *api.Client }

func nouvellesApplicationsLues() datasource.DataSource { return &sourceApplications{} }

func (s *sourceApplications) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_applications"
}

func (s *sourceApplications) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Toutes tes applications, la plus récente d'abord — sans secret.",
		Attributes: map[string]schema.Attribute{
			"applications": schema.ListNestedAttribute{Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: attributsLus(false)}},
			"max": schema.Int64Attribute{Computed: true, Description: "Combien d'applications un compte peut déclarer."},
		},
	}
}

func (s *sourceApplications) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	s.client = configurerSource(req, resp)
}

// typeApplicationLue — le type objet d'une application lue (pour la liste).
func typeApplicationLue() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"id": types.StringType, "client_id": types.StringType, "nom": types.StringType, "description": types.StringType,
		"site_web": types.StringType, "categorie": types.StringType, "statut": types.StringType,
		"portees": types.ListType{ElemType: types.StringType}, "redirections": types.ListType{ElemType: types.StringType},
		"publique": types.BoolType, "cree_le": types.StringType, "conditions_acceptees": types.StringType,
		"conditions_a_jour": types.BoolType,
	}}
}

func (s *sourceApplications) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if s.client == nil {
		resp.Diagnostics.AddError("Fournisseur non configuré", "lire les applications exige un jeton")
		return
	}
	l, err := s.client.Applications(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Lire les applications", err.Error())
		return
	}
	lues := make([]modeleApplicationLue, 0, len(l.Applications))
	for _, a := range l.Applications {
		lues = append(lues, applicationLue(ctx, a, &resp.Diagnostics))
	}
	liste, d := types.ListValueFrom(ctx, typeApplicationLue(), lues)
	resp.Diagnostics.Append(d...)
	resp.Diagnostics.Append(resp.State.Set(ctx, modeleApplicationsLues{Applications: liste, Max: types.Int64Value(int64(l.Max))})...)
}
