package domain

type PolicyInput struct {
	Payload map[string]any `json:"payload"`
	Action  string         `json:"action"`
}
