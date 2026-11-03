package fournisseur

// application.go — la ressource `recodanse_application`.
//
// Ce qu'elle décrit, c'est l'application telle que sa propriétaire la VEUT :
// son nom, ses portées DEMANDÉES, ses adresses de retour — et deux intentions
// que l'écran porte par des boutons :
//
//   - `rotation` : une carte libre (comme les `keepers` du fournisseur `random`).
//     Quand elle change, le secret TOURNE : l'ancien cesse de valoir à l'instant,
//     le nouveau entre dans l'état, marqué sensible. Une rotation datée s'écrit
//     avec `time_rotating` ; une rotation à la main, en changeant une valeur ;
//   - `soumettre` : demander la revue. ReCoDanse relit chaque application ;
//     le fournisseur ne publie rien, il ne fait que demander.
//
// ⚠ UNE SOUMISSION NE SE RETIRE PAS : repasser `soumettre` à `false` ne la
//   reprend pas (aucune route ne le fait). Le fournisseur le laisse dire au plan.
// ⚠ UNE PORTÉE AJOUTÉE À UNE APPLICATION PUBLIÉE est refusée par l'API
//   (`portees_figees`) : elle repasse par la revue, jamais par un `apply`.

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/api"
)

// StatutEnDeveloppement — le seul statut d'où l'on soumet.
const StatutEnDeveloppement = "developpement"

type modeleApplication struct {
	ID                   types.String `tfsdk:"id"`
	ClientID             types.String `tfsdk:"client_id"`
	ClientSecret         types.String `tfsdk:"client_secret"`
	Nom                  types.String `tfsdk:"nom"`
	Description          types.String `tfsdk:"description"`
	SiteWeb              types.String `tfsdk:"site_web"`
	Categorie            types.String `tfsdk:"categorie"`
	Portees              types.Set    `tfsdk:"portees"`
	Redirections         types.Set    `tfsdk:"redirections"`
	Publique             types.Bool   `tfsdk:"publique"`
	Rotation             types.Map    `tfsdk:"rotation"`
	Soumettre            types.Bool   `tfsdk:"soumettre"`
	AccepteLesConditions types.String `tfsdk:"accepte_les_conditions"`
	Statut               types.String `tfsdk:"statut"`
	CreeLe               types.String `tfsdk:"cree_le"`
}

type ressourceApplication struct{ client *api.Client }

var (
	_ resource.ResourceWithConfigure      = (*ressourceApplication)(nil)
	_ resource.ResourceWithImportState    = (*ressourceApplication)(nil)
	_ resource.ResourceWithModifyPlan     = (*ressourceApplication)(nil)
	_ resource.ResourceWithValidateConfig = (*ressourceApplication)(nil)
)

func nouvelleApplication() resource.Resource { return &ressourceApplication{} }

func (r *ressourceApplication) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

// sansEspacesAuBord — l'API retire les espaces au bord du nom et de la
// description : les accepter ferait relire une valeur différente de celle écrite.
var sansEspacesAuBord = stringvalidator.RegexMatches(regexp.MustCompile(`^(\S(.*\S)?)?$`),
	"pas d'espace au début ni à la fin (ReCoDanse les retire)")

func (r *ressourceApplication) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Une application tierce de ReCoDanse (OpenID Connect ou compte de service).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "L'identifiant ReCoDanse (`appl_…`), distinct du `client_id`.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"client_id": schema.StringAttribute{
				Computed: true, Description: "L'identifiant de client OAuth2 (Hydra).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"client_secret": schema.StringAttribute{
				Computed: true, Sensitive: true,
				Description: "Le secret du client — connu à la création et après chaque rotation, jamais relu. Nul pour un client public.",
			},
			"nom": schema.StringAttribute{
				Required: true, Description: "Le nom que la personne lit sur la page « Autoriser » (2 à 60 caractères).",
				Validators: []validator.String{stringvalidator.LengthBetween(2, 60), sansEspacesAuBord},
			},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Validators: []validator.String{stringvalidator.LengthAtMost(500), sansEspacesAuBord},
			},
			"site_web": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Description: "https:// seulement.",
			},
			"categorie": schema.StringAttribute{
				Required: true, Description: "`connexion` (OpenID Connect, une personne autorise) ou `service` (publie pour le compte qui l'a déclaré). Changer recrée l'application.",
				Validators:    []validator.String{stringvalidator.OneOf("connexion", "service")},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"portees": schema.SetAttribute{
				Required: true, ElementType: types.StringType,
				Description: "Les portées DEMANDÉES. connexion : openid, profile, email, offline_access, videos.lire, videos.ranger · service : evenements.publier, creneaux.publier.",
				Validators:  []validator.Set{setvalidator.SizeAtLeast(1)},
			},
			"redirections": schema.SetAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Les adresses de retour (connexion seulement) : https://, http://127.0.0.1 ou un schéma d'app native ; dix au plus.",
				Validators:  []validator.Set{setvalidator.SizeAtMost(10)},
			},
			"publique": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description:   "Client sans secret (app native, page web) : PKCE obligatoire. Changer recrée l'application.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"rotation": schema.MapAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Des valeurs libres : quand l'une change, le secret TOURNE (l'ancien cesse de valoir à l'instant).",
			},
			"soumettre": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Demander la revue de ReCoDanse (developpement → en_revue). Une soumission ne se retire pas.",
			},
			"accepte_les_conditions": schema.StringAttribute{
				Optional:    true,
				Description: "La version des conditions des développeurs que tu acceptes (dev.recodanse.fr) — exigée pour soumettre, jamais remplie à ta place.",
			},
			"statut": schema.StringAttribute{
				Computed: true, Description: "developpement, en_revue, publiee, suspendue ou non_verifiee.",
			},
			"cree_le": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ressourceApplication) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := clientDe(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Fournisseur mal configuré", fmt.Sprintf("client inattendu : %T", req.ProviderData))
		return
	}
	r.client = c
}

func chaines(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	var out []string
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	sort.Strings(out)
	return out
}

// ValidateConfig — ce qui se refuse sans demander à l'API : une portée hors de sa
// catégorie, des adresses de retour pour un service, une rotation sans secret.
func (r *ressourceApplication) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m modeleApplication
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Categorie.IsUnknown() || m.Categorie.IsNull() {
		return
	}
	cat := m.Categorie.ValueString()
	for _, p := range chaines(ctx, m.Portees, &resp.Diagnostics) {
		if !porteePermise(cat, p) {
			resp.Diagnostics.AddAttributeError(path.Root("portees"), "Portée refusée",
				fmt.Sprintf("« %s » n'est pas permise à une application « %s ». Permises : %v.", p, cat, PorteesParCategorie[cat]))
		}
	}
	avecRedirections := !m.Redirections.IsNull() && !m.Redirections.IsUnknown() && len(m.Redirections.Elements()) > 0
	switch cat {
	case "service":
		if avecRedirections {
			resp.Diagnostics.AddAttributeError(path.Root("redirections"), "Adresses de retour inutiles",
				"un compte de service n'a pas d'adresse de retour : personne n'est redirigé.")
		}
		if m.Publique.ValueBool() {
			resp.Diagnostics.AddAttributeError(path.Root("publique"), "Service sans secret",
				"un compte de service s'authentifie par son secret : il ne peut pas être public.")
		}
	case "connexion":
		if m.Redirections.IsNull() || (!m.Redirections.IsUnknown() && !avecRedirections) {
			resp.Diagnostics.AddAttributeError(path.Root("redirections"), "Adresse de retour manquante",
				"une application « connexion » a au moins une adresse de retour.")
		}
	}
	if m.Publique.ValueBool() && !m.Rotation.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("rotation"), "Rotation sans objet",
			"un client public n'a pas de secret : rien à faire tourner.")
	}
	if m.Soumettre.ValueBool() && m.AccepteLesConditions.IsNull() {
		resp.Diagnostics.AddAttributeError(path.Root("accepte_les_conditions"), "Conditions des développeurs à accepter",
			"soumettre à la revue exige d'accepter la version en vigueur des conditions des développeurs (dev.recodanse.fr) : "+
				"écris-la dans accepte_les_conditions. Le fournisseur ne l'accepte jamais à ta place.")
	}
}

// ModifyPlan — ce que le plan doit dire de ce qui CHANGERA côté serveur : un
// secret qui tourne, un statut qui passe en revue — et, dès le plan, une version
// des conditions qui n'est plus celle en vigueur.
func (r *ressourceApplication) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destruction
	}
	var plan modeleApplication
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var etat *modeleApplication
	if !req.State.Raw.IsNull() {
		etat = &modeleApplication{}
		resp.Diagnostics.Append(req.State.Get(ctx, etat)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	vaSoumettre := plan.Soumettre.ValueBool() && (etat == nil || etat.Statut.ValueString() == StatutEnDeveloppement)
	if vaSoumettre && r.client != nil && !plan.AccepteLesConditions.IsUnknown() && !plan.AccepteLesConditions.IsNull() {
		l, err := r.client.Applications(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Lire la version des conditions en vigueur", err.Error())
			return
		}
		if l.ConditionsEnVigueur != "" && l.ConditionsEnVigueur != plan.AccepteLesConditions.ValueString() {
			resp.Diagnostics.AddAttributeError(path.Root("accepte_les_conditions"), "Conditions des développeurs périmées",
				fmt.Sprintf("la version en vigueur est « %s » : relis-la sur dev.recodanse.fr, puis écris accepte_les_conditions = %q.",
					l.ConditionsEnVigueur, l.ConditionsEnVigueur))
			return
		}
	}
	if etat == nil {
		if plan.Publique.ValueBool() {
			resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("client_secret"), types.StringNull())...)
		}
		return
	}
	// Le secret : inchangé, sauf rotation demandée.
	secret := etat.ClientSecret
	if !plan.Publique.ValueBool() && !plan.Rotation.Equal(etat.Rotation) {
		secret = types.StringUnknown()
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("client_secret"), secret)...)
	// Le statut : inchangé, sauf soumission.
	statut := etat.Statut
	if vaSoumettre {
		statut = types.StringUnknown()
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("statut"), statut)...)
	if !plan.Soumettre.ValueBool() && etat.Soumettre.ValueBool() && etat.Statut.ValueString() != StatutEnDeveloppement {
		resp.Diagnostics.AddAttributeWarning(path.Root("soumettre"), "Une soumission ne se retire pas",
			"l'application est « "+etat.Statut.ValueString()+" » : repasser soumettre à false ne la retire pas de la revue.")
	}
}

// remplir — l'état d'après ce que l'API a rendu. `avant` dit si les adresses de
// retour étaient NULLES (un service n'en écrit pas ; l'API rend une liste vide).
func remplir(ctx context.Context, m *modeleApplication, a api.Application, diags *diag.Diagnostics) {
	m.ID = types.StringValue(a.ID)
	m.ClientID = types.StringValue(a.ClientID)
	m.Nom = types.StringValue(a.Nom)
	m.Description = types.StringValue(a.Description)
	m.SiteWeb = types.StringValue(a.SiteWeb)
	m.Categorie = types.StringValue(a.Categorie)
	m.Publique = types.BoolValue(a.Publique)
	m.Statut = types.StringValue(a.Statut)
	m.CreeLe = types.StringValue(a.CreeLe)
	p, d := types.SetValueFrom(ctx, types.StringType, a.Portees)
	diags.Append(d...)
	m.Portees = p
	if len(a.Redirections) == 0 && (m.Redirections.IsNull() || m.Redirections.IsUnknown()) {
		m.Redirections = types.SetNull(types.StringType)
	} else {
		rs, d := types.SetValueFrom(ctx, types.StringType, a.Redirections)
		diags.Append(d...)
		m.Redirections = rs
	}
}

func secretOuNul(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func (r *ressourceApplication) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modeleApplication
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.Declarer(ctx, api.Declaration{
		Nom: plan.Nom.ValueString(), Description: plan.Description.ValueString(), SiteWeb: plan.SiteWeb.ValueString(),
		Categorie: plan.Categorie.ValueString(), Portees: chaines(ctx, plan.Portees, &resp.Diagnostics),
		Redirections: chaines(ctx, plan.Redirections, &resp.Diagnostics), Publique: plan.Publique.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Déclarer l'application", err.Error())
		return
	}
	remplir(ctx, &plan, a, &resp.Diagnostics)
	plan.ClientSecret = secretOuNul(a.ClientSecret)
	if plan.Soumettre.ValueBool() {
		// ⚠ L'application EXISTE déjà : un échec ici ne doit pas la faire marquer
		// « à recréer » (un nouveau client_id casserait ce qui s'en sert). On le
		// dit, l'état garde « developpement », et le prochain apply retentera.
		if s, err := r.client.Soumettre(ctx, a.ID, plan.AccepteLesConditions.ValueString()); err != nil {
			resp.Diagnostics.AddWarning("Application déclarée, soumission refusée",
				err.Error()+" — le prochain `apply` retentera la soumission.")
		} else {
			plan.Statut = types.StringValue(s.Statut)
		}
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ressourceApplication) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var etat modeleApplication
	resp.Diagnostics.Append(req.State.Get(ctx, &etat)...)
	if resp.Diagnostics.HasError() {
		return
	}
	a, err := r.client.Application(ctx, etat.ID.ValueString())
	if api.EstIntrouvable(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Lire l'application", err.Error())
		return
	}
	remplir(ctx, &etat, a, &resp.Diagnostics)
	// Après un import, l'état ne sait rien des intentions : on les déduit du serveur.
	if etat.Soumettre.IsNull() {
		etat.Soumettre = types.BoolValue(a.Statut != StatutEnDeveloppement)
	}
	if etat.AccepteLesConditions.IsNull() && a.Conditions.VersionAcceptee != nil {
		etat.AccepteLesConditions = types.StringValue(*a.Conditions.VersionAcceptee)
	}
	if etat.Rotation.IsUnknown() {
		etat.Rotation = types.MapNull(types.StringType)
	}
	if etat.ClientSecret.IsUnknown() {
		etat.ClientSecret = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, etat)...)
}

func egales(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *ressourceApplication) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, etat modeleApplication
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &etat)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := etat.ID.ValueString()
	portees, redirections := chaines(ctx, plan.Portees, &resp.Diagnostics), chaines(ctx, plan.Redirections, &resp.Diagnostics)
	aCorriger := !plan.Nom.Equal(etat.Nom) || !plan.Description.Equal(etat.Description) || !plan.SiteWeb.Equal(etat.SiteWeb) ||
		!egales(portees, chaines(ctx, etat.Portees, &resp.Diagnostics)) ||
		!egales(redirections, chaines(ctx, etat.Redirections, &resp.Diagnostics))
	var a api.Application
	var err error
	if aCorriger {
		a, err = r.client.Corriger(ctx, id, api.Correction{
			Nom: plan.Nom.ValueString(), Description: plan.Description.ValueString(), SiteWeb: plan.SiteWeb.ValueString(),
			Portees: portees, Redirections: redirections,
		})
		if err != nil {
			detail := err.Error()
			if api.CodeDe(err) == "portees_figees" {
				detail += " — une application publiée qui demande une portée de plus repasse par la revue : retire-la, ou écris à ReCoDanse."
			}
			if api.CodeDe(err) == "revue_requise" {
				detail += " — une application publiée ne change ni de nom, ni de site, ni d'adresse de retour sans repasser par la revue : écris à ReCoDanse, ou garde l'ancienne valeur."
			}
			resp.Diagnostics.AddError("Corriger l'application", detail)
			return
		}
	} else if a, err = r.client.Application(ctx, id); err != nil {
		resp.Diagnostics.AddError("Lire l'application", err.Error())
		return
	}
	plan.ClientSecret = etat.ClientSecret
	if !plan.Publique.ValueBool() && !plan.Rotation.Equal(etat.Rotation) {
		s, err := r.client.ChangerLeSecret(ctx, id)
		if err != nil {
			resp.Diagnostics.AddError("Faire tourner le secret", err.Error())
			return
		}
		plan.ClientSecret = types.StringValue(s)
	}
	remplir(ctx, &plan, a, &resp.Diagnostics)
	if plan.Soumettre.ValueBool() && a.Statut == StatutEnDeveloppement {
		s, err := r.client.Soumettre(ctx, id, plan.AccepteLesConditions.ValueString())
		if err != nil {
			// L'état garde ce qui a déjà eu lieu (corrections, nouveau secret) : le
			// perdre serait perdre un secret qui ne se relit pas.
			plan.Statut = types.StringValue(a.Statut)
			resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
			resp.Diagnostics.AddError("Soumettre à la revue", err.Error())
			return
		}
		plan.Statut = types.StringValue(s.Statut)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ressourceApplication) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var etat modeleApplication
	resp.Diagnostics.Append(req.State.Get(ctx, &etat)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Retirer(ctx, etat.ID.ValueString()); err != nil && !api.EstIntrouvable(err) {
		resp.Diagnostics.AddError("Retirer l'application", err.Error())
	}
}

func (r *ressourceApplication) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("soumettre"), types.BoolNull())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("rotation"), types.MapNull(types.StringType))...)
}
