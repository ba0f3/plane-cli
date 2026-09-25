package api

type Object map[string]any

type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type AuthContext struct {
	PrincipalType string     `json:"principal_type"`
	ScopeLevel    string     `json:"scope_level"`
	IsService     bool       `json:"is_service"`
	Workspace     *Workspace `json:"workspace"`
	Scopes        []string   `json:"scopes"`
}
