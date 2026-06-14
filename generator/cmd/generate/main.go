// Command generate fetches GitHub account stats, enriches the curated
// project list, optionally refreshes Strava data, and writes
// generated/site-data.json validated against schemas/site-data.schema.json.
//
// Required env: GITHUB_TOKEN.
// Optional env: GITHUB_USER (default MrCodeEU), STRAVA_CLIENT_ID,
// STRAVA_CLIENT_SECRET, STRAVA_REFRESH_TOKEN (Strava is skipped, keeping the
// previous data, if these are unset).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"mljr-data/generator/internal/githubapi"
	"mljr-data/generator/internal/strava"
	"mljr-data/generator/internal/types"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN is required")
	}
	user := os.Getenv("GITHUB_USER")
	if user == "" {
		user = "MrCodeEU"
	}

	ctx := context.Background()
	now := time.Now()

	log.Printf("repo root: %s", root)

	existing, err := loadSiteData(filepath.Join(root, "generated", "site-data.json"))
	if err != nil {
		return fmt.Errorf("load existing site-data.json: %w", err)
	}

	projectsFile, err := loadProjects(filepath.Join(root, "projects.json"))
	if err != nil {
		return fmt.Errorf("load projects.json: %w", err)
	}
	log.Printf("projects.json: %d curated project(s)", len(projectsFile.Curated))

	content, err := loadContent(filepath.Join(root, "content.json"))
	if err != nil {
		return fmt.Errorf("load content.json: %w", err)
	}
	log.Printf("content.json: loaded, %d thesis entries", len(content.Thesis))

	gh := githubapi.New(token)

	log.Printf("github: listing repos for %s", user)
	repos, err := gh.ListRepos(ctx, user)
	if err != nil {
		return fmt.Errorf("list repos: %w", err)
	}
	log.Printf("github: found %d non-archived, non-fork repo(s)", len(repos))

	projectsPath := filepath.Join(root, "projects.json")
	if added := syncCuratedProjects(&projectsFile, repos); added > 0 {
		log.Printf("projects.json: added %d new repo(s) with show=false", added)
		if err := writeProjects(projectsPath, projectsFile); err != nil {
			return fmt.Errorf("write projects.json: %w", err)
		}
		log.Printf("wrote %s", projectsPath)
	}

	shown := 0
	for _, c := range projectsFile.Curated {
		if c.Show {
			shown++
		}
	}
	log.Printf("projects.json: %d/%d curated project(s) marked show=true", shown, len(projectsFile.Curated))

	projects, err := buildProjects(projectsFile, repos)
	if err != nil {
		return fmt.Errorf("build projects: %w", err)
	}
	for _, p := range projects {
		log.Printf("  - %s: %d stars, %s", p.Name, p.Stars, p.Language)
	}

	log.Printf("github: fetching account stats for %s", user)
	stats, err := buildGitHubStats(ctx, gh, user, now)
	if err != nil {
		return fmt.Errorf("build github stats: %w", err)
	}
	log.Printf("github stats: %d commits/year, %d-day streak, %d contribution days, %d language(s)",
		stats.CommitsYear, stats.LongestStreak, len(stats.Contributions), len(stats.LanguageShare))

	stravaData := existing.StravaData
	if hasStravaCreds() {
		log.Println("strava: refreshing activity data")
		fresh, err := strava.New(strava.Config{
			ClientID:     os.Getenv("STRAVA_CLIENT_ID"),
			ClientSecret: os.Getenv("STRAVA_CLIENT_SECRET"),
			RefreshToken: os.Getenv("STRAVA_REFRESH_TOKEN"),
		}).Fetch(ctx, now)
		if err != nil {
			return fmt.Errorf("fetch strava data: %w", err)
		}
		stravaData = fresh
		log.Printf("strava: %d total activities, %d recent", stravaData.TotalStats.Count, len(stravaData.RecentActivities))
	} else {
		log.Println("strava credentials not set, keeping existing strava_data")
	}

	normalizeStravaData(&stravaData)

	out := types.SiteData{
		SchemaVersion:  "site-data.v1",
		GeneratedAt:    now.UTC().Format(time.RFC3339),
		GitHubProjects: projects,
		LinkedInData:   existing.LinkedInData,
		StravaData:     stravaData,
		GitHubStats:    stats,
		Content:        content.SiteContent,
		Thesis:         content.Thesis,
	}

	log.Println("validating against schemas/site-data.schema.json")
	if err := validate(root, out); err != nil {
		return fmt.Errorf("validate site-data.json: %w", err)
	}

	outPath := filepath.Join(root, "generated", "site-data.json")
	if err := writeSiteData(outPath, out); err != nil {
		return err
	}
	log.Printf("wrote %s", outPath)
	return nil
}

// normalizeStravaData ensures slice fields that the schema requires as
// arrays (additionalProperties: false, type: array) are never null, which
// can happen with hand-written sample data.
func normalizeStravaData(d *types.StravaData) {
	for i := range d.Disciplines {
		if d.Disciplines[i].Activities == nil {
			d.Disciplines[i].Activities = []types.StravaActivity{}
		}
	}
	if d.RecentActivities == nil {
		d.RecentActivities = []types.StravaActivity{}
	}
	if d.PersonalRecords == nil {
		d.PersonalRecords = []types.StravaRecord{}
	}
	if d.Disciplines == nil {
		d.Disciplines = []types.StravaDiscipline{}
	}
}

func hasStravaCreds() bool {
	return os.Getenv("STRAVA_CLIENT_ID") != "" && os.Getenv("STRAVA_CLIENT_SECRET") != "" && os.Getenv("STRAVA_REFRESH_TOKEN") != ""
}

// repoRoot returns the mljr-data repo root: the parent of the generator
// module's working directory.
func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// generator/cmd/generate -> repo root is three levels up when run via
	// `go run ./cmd/generate` from generator/, or via module path otherwise.
	for _, candidate := range []string{wd, filepath.Join(wd, ".."), filepath.Join(wd, "..", "..")} {
		if _, err := os.Stat(filepath.Join(candidate, "schemas", "site-data.schema.json")); err == nil {
			return filepath.Abs(candidate)
		}
	}
	return "", fmt.Errorf("could not locate repo root (schemas/site-data.schema.json) from %s", wd)
}

func loadSiteData(path string) (types.SiteData, error) {
	var data types.SiteData
	b, err := os.ReadFile(path)
	if err != nil {
		return data, err
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return data, err
	}
	return data, nil
}

// contentFile is the contract for content.json: hand-authored hero/contact
// copy (matching types.SiteContent's "hero"/"contact" fields) plus a
// top-level "thesis" array, passed straight through to site-data.json.
type contentFile struct {
	types.SiteContent
	Thesis []types.Thesis `json:"thesis"`
}

func loadContent(path string) (contentFile, error) {
	var cf contentFile
	b, err := os.ReadFile(path)
	if err != nil {
		return cf, err
	}
	if err := json.Unmarshal(b, &cf); err != nil {
		return cf, err
	}
	return cf, nil
}

func loadProjects(path string) (types.ProjectsFile, error) {
	var pf types.ProjectsFile
	b, err := os.ReadFile(path)
	if err != nil {
		return pf, err
	}
	if err := json.Unmarshal(b, &pf); err != nil {
		return pf, err
	}
	return pf, nil
}

func writeProjects(path string, pf types.ProjectsFile) error {
	b, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

// syncCuratedProjects merges the account's live, non-archived, non-fork
// repos into pf.Curated: existing entries (matched by repo URL) are left
// untouched so hand-edited fields (show, summary, topics, images, links,
// featured, homepage) are preserved; newly-discovered repos are appended
// with show=false. The result is sorted by ID for a stable diff.
func syncCuratedProjects(pf *types.ProjectsFile, repos []githubapi.RepoSummary) (added int) {
	known := make(map[string]bool, len(pf.Curated))
	for _, c := range pf.Curated {
		known[c.Repo] = true
	}
	for _, r := range repos {
		if known[r.URL] {
			continue
		}
		pf.Curated = append(pf.Curated, types.CuratedProject{
			ID:     r.Name,
			Name:   r.Name,
			Show:   false,
			Repo:   r.URL,
			Topics: []string{},
			Images: []string{},
		})
		added++
	}
	sort.Slice(pf.Curated, func(i, j int) bool { return pf.Curated[i].ID < pf.Curated[j].ID })
	return added
}

// buildProjects returns the published project cards: only curated entries
// with show=true, enriched with live stars/language/topics/description from
// repos (fetched once via ListRepos).
func buildProjects(pf types.ProjectsFile, repos []githubapi.RepoSummary) ([]types.Project, error) {
	byURL := make(map[string]githubapi.RepoSummary, len(repos))
	for _, r := range repos {
		byURL[r.URL] = r
	}

	projects := make([]types.Project, 0, len(pf.Curated))
	for _, c := range pf.Curated {
		if !c.Show {
			continue
		}
		p := types.Project{
			Name:        c.Name,
			Description: c.Summary,
			URL:         c.Repo,
			Topics:      c.Topics,
			Images:      c.Images,
			Featured:    c.Featured,
			Order:       c.Order,
			Links:       c.Links,
		}
		if p.Links == nil {
			p.Links = []types.ProjectLink{}
		}
		if c.Homepage != nil && *c.Homepage != "" {
			p.Links = append(p.Links, types.ProjectLink{Name: "Live", URL: *c.Homepage})
		}

		info, ok := byURL[c.Repo]
		if !ok {
			return nil, fmt.Errorf("project %s: repo %s not found in account repo list (archived/private/renamed?)", c.ID, c.Repo)
		}
		p.Stars = info.Stars
		p.Language = info.Language
		if p.Description == "" {
			p.Description = info.Description
		}
		if len(p.Topics) == 0 {
			p.Topics = info.Topics
		}
		if p.Topics == nil {
			p.Topics = []string{}
		}
		if p.Images == nil {
			p.Images = []string{}
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func buildGitHubStats(ctx context.Context, gh *githubapi.Client, user string, now time.Time) (*types.GitHubStats, error) {
	commitsYear, contributions, longestStreak, err := gh.ContributionStats(ctx, user, now)
	if err != nil {
		return nil, err
	}
	languageShare, err := gh.FetchLanguageShare(ctx, user)
	if err != nil {
		return nil, err
	}
	return &types.GitHubStats{
		CommitsYear:   commitsYear,
		LongestStreak: longestStreak,
		Contributions: contributions,
		LanguageShare: languageShare,
	}, nil
}

func validate(root string, data types.SiteData) error {
	schemaPath := filepath.Join(root, "schemas", "site-data.schema.json")
	compiler := jsonschema.NewCompiler()
	sch, err := compiler.Compile(schemaPath)
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}

	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		return err
	}
	return sch.Validate(doc)
}

func writeSiteData(path string, data types.SiteData) error {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}
