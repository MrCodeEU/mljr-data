package types

// ProjectsFile is the projects.json contract. The generator keeps
// curated[] in sync with the GitHub account's non-archived, non-fork repos,
// adding new repos with show=false. Set show=true and fill in summary,
// topics, images, links, featured, homepage by hand to publish a project.
type ProjectsFile struct {
	SchemaVersion string           `json:"schema_version"`
	Curated       []CuratedProject `json:"curated"`
}

type CuratedProject struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Show     bool          `json:"show"`
	Summary  string        `json:"summary"`
	Repo     string        `json:"repo"`
	Homepage *string       `json:"homepage"`
	Featured bool          `json:"featured"`
	Topics   []string      `json:"topics"`
	Images   []string      `json:"images"`
	Links    []ProjectLink `json:"links,omitempty"`
}
