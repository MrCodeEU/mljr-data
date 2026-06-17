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
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Show          bool             `json:"show"`
	Summary       string           `json:"summary"`
	Description   string           `json:"description,omitempty"`
	DescriptionDE string           `json:"description_de,omitempty"`
	Repo          string           `json:"repo"`
	Homepage      *string          `json:"homepage"`
	Featured      bool             `json:"featured"`
	Order         int              `json:"order,omitempty"`
	Topics        []string         `json:"topics"`
	Images        []string         `json:"images"`
	Links         []ProjectLink    `json:"links,omitempty"`
	LongDesc      string           `json:"long_desc,omitempty"`
	LongDescDE    string           `json:"long_desc_de,omitempty"`
	Diagram       string           `json:"diagram,omitempty"`
	Snippets      []ProjectSnippet `json:"snippets,omitempty"`
}

// ProjectSnippet is a real code excerpt shown on a project's detail page,
// hand-authored in projects.json alongside the rest of its curated content.
type ProjectSnippet struct {
	Caption  string `json:"caption"`
	Filename string `json:"filename"`
	Language string `json:"language"`
	Code     string `json:"code"`
}
