package governance

import (
	"time"

	"github.com/google/uuid"
)

// SCIM 2.0 wire types matching RFC 7643. Field names follow the SCIM
// convention (camelCase + nested objects) so we can serialize directly
// without an additional translation layer.
//
// We deliberately implement the practical subset that real IdPs (Okta,
// Microsoft Entra ID, Google Workspace) actually push. Full SCIM 2.0
// compliance is more surface area than the wedge needs.

const (
	scimUserSchema  = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimGroupSchema = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimErrorSchema = "urn:ietf:params:scim:api:messages:2.0:Error"
	scimListSchema  = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimPatchSchema = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
)

// SCIMUser is the wire shape for `/scim/v2/Users` responses.
type SCIMUser struct {
	Schemas      []string         `json:"schemas"`
	ID           string           `json:"id"`
	ExternalID   string           `json:"externalId,omitempty"`
	UserName     string           `json:"userName"`
	Name         *SCIMName        `json:"name,omitempty"`
	DisplayName  string           `json:"displayName,omitempty"`
	Active       bool             `json:"active"`
	Emails       []SCIMEmail      `json:"emails,omitempty"`
	Groups       []SCIMGroupRef   `json:"groups,omitempty"`
	Meta         SCIMMeta         `json:"meta"`
}

// SCIMName mirrors the `name` complex attribute.
type SCIMName struct {
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
	Formatted  string `json:"formatted,omitempty"`
}

// SCIMEmail is one entry in the `emails` multi-valued attribute.
type SCIMEmail struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
}

// SCIMGroupRef is the back-reference in a User's `groups` array.
type SCIMGroupRef struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

// SCIMGroup is the wire shape for `/scim/v2/Groups` responses.
type SCIMGroup struct {
	Schemas     []string          `json:"schemas"`
	ID          string            `json:"id"`
	ExternalID  string            `json:"externalId,omitempty"`
	DisplayName string            `json:"displayName"`
	Members     []SCIMMemberRef   `json:"members,omitempty"`
	Meta        SCIMMeta          `json:"meta"`
}

// SCIMMemberRef is one entry in a Group's `members` array.
type SCIMMemberRef struct {
	Value   string `json:"value"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
	Type    string `json:"type,omitempty"`
}

// SCIMMeta is the metadata block every SCIM resource carries.
type SCIMMeta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Location     string    `json:"location,omitempty"`
	Version      string    `json:"version,omitempty"`
}

// SCIMListResponse wraps GET /Users or /Groups responses.
type SCIMListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex,omitempty"`
	ItemsPerPage int      `json:"itemsPerPage,omitempty"`
	Resources    []any    `json:"Resources"`
}

// SCIMError is the standard error response shape.
type SCIMError struct {
	Schemas  []string `json:"schemas"`
	Detail   string   `json:"detail"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
}

// SCIMPatchOp is a PATCH operation envelope.
type SCIMPatchOp struct {
	Schemas    []string         `json:"schemas"`
	Operations []SCIMPatchEntry `json:"Operations"`
}

// SCIMPatchEntry is one operation inside a PATCH body. SCIM PATCH is
// lightly used by IdPs — typically `{op:"replace", path:"active", value:false}`
// for deactivation and `{op:"add"|"remove", path:"members", value:[...]}` for
// group membership changes.
type SCIMPatchEntry struct {
	Op    string `json:"op"`
	Path  string `json:"path,omitempty"`
	Value any    `json:"value,omitempty"`
}

// =====================================================================
// Internal models — what the store layer returns to handlers
// =====================================================================

type GovernanceUser struct {
	ID          uuid.UUID
	CustomerID  uuid.UUID
	ExternalID  string
	UserName    string
	Email       string
	DisplayName string
	GivenName   string
	FamilyName  string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

type GovernanceGroup struct {
	ID          uuid.UUID
	CustomerID  uuid.UUID
	ExternalID  string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

// toSCIM transforms internal models into the SCIM wire shape. Location is
// optional but most IdPs expect it.
func (u *GovernanceUser) toSCIM(baseURL string) SCIMUser {
	out := SCIMUser{
		Schemas:     []string{scimUserSchema},
		ID:          u.ID.String(),
		ExternalID:  u.ExternalID,
		UserName:    u.UserName,
		DisplayName: u.DisplayName,
		Active:      u.Active,
		Meta: SCIMMeta{
			ResourceType: "User",
			Created:      u.CreatedAt,
			LastModified: u.UpdatedAt,
			Location:     baseURL + "/Users/" + u.ID.String(),
		},
	}
	if u.GivenName != "" || u.FamilyName != "" {
		out.Name = &SCIMName{
			GivenName:  u.GivenName,
			FamilyName: u.FamilyName,
			Formatted:  u.DisplayName,
		}
	}
	if u.Email != "" {
		out.Emails = []SCIMEmail{{Value: u.Email, Type: "work", Primary: true}}
	}
	return out
}

func (g *GovernanceGroup) toSCIM(baseURL string, members []SCIMMemberRef) SCIMGroup {
	return SCIMGroup{
		Schemas:     []string{scimGroupSchema},
		ID:          g.ID.String(),
		ExternalID:  g.ExternalID,
		DisplayName: g.DisplayName,
		Members:     members,
		Meta: SCIMMeta{
			ResourceType: "Group",
			Created:      g.CreatedAt,
			LastModified: g.UpdatedAt,
			Location:     baseURL + "/Groups/" + g.ID.String(),
		},
	}
}
