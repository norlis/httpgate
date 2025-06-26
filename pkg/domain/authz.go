package domain

type PolicyInput struct {
	Roles  []string `json:"roles"`
	Action string   `json:"action"`
}
