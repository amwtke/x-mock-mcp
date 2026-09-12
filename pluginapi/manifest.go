package pluginapi

import "encoding/json"

type Role string

const (
	Left  Role = "left"
	Right Role = "right"
)

type Reference struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Role    Role   `json:"role"`
}

type Contract struct {
	ID           string   `json:"id"`
	Version      int      `json:"version"`
	Capabilities []string `json:"capabilities"`
}

type Descriptor struct {
	Ref            Reference            `json:"ref"`
	APIMajor       int                  `json:"plugin_api_version"`
	Contracts      []Contract           `json:"contracts"`
	ConfigSchema   json.RawMessage      `json:"config_schema"`
	ScenarioSchema json.RawMessage      `json:"scenario_schema,omitempty"`
	Preparation    *PreparationContract `json:"preparation,omitempty"`
	Features       []string             `json:"features,omitempty"`
}

type Manifest struct {
	Descriptor Descriptor        `json:"descriptor"`
	Platform   string            `json:"platform"`
	Entrypoint string            `json:"entrypoint"`
	Files      map[string]string `json:"files"`
}
