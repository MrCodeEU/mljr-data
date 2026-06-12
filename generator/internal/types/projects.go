package types

// ProjectsFile is the manually-curated projects.json contract. Each curated
// entry is enriched with live stars/language from the GitHub REST API.
type ProjectsFile struct {
	SchemaVersion string           `json:"schema_version"`
	Curated       []CuratedProject `json:"curated"`
}

type CuratedProject struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Summary  string        `json:"summary"`
	Repo     string        `json:"repo"`
	Homepage *string       `json:"homepage"`
	Featured bool          `json:"featured"`
	Topics   []string      `json:"topics"`
	Images   []string      `json:"images"`
	Links    []ProjectLink `json:"links,omitempty"`
}
