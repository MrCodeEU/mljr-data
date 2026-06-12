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

	existing, err := loadSiteData(filepath.Join(root, "generated", "site-data.json"))
	if err != nil {
		return fmt.Errorf("load existing site-data.json: %w", err)
	}

	projectsFile, err := loadProjects(filepath.Join(root, "projects.json"))
	if err != nil {
		return fmt.Errorf("load projects.json: %w", err)
	}

	gh := githubapi.New(token)

	projects, err := buildProjects(ctx, gh, projectsFile)
	if err != nil {
		return fmt.Errorf("build projects: %w", err)
	}

	stats, err := buildGitHubStats(ctx, gh, user, now)
	if err != nil {
		return fmt.Errorf("build github stats: %w", err)
	}

	stravaData := existing.StravaData
	if hasStravaCreds() {
		fresh, err := strava.New(strava.Config{
			ClientID:     os.Getenv("STRAVA_CLIENT_ID"),
			ClientSecret: os.Getenv("STRAVA_CLIENT_SECRET"),
			RefreshToken: os.Getenv("STRAVA_REFRESH_TOKEN"),
		}).Fetch(ctx, now)
		if err != nil {
			return fmt.Errorf("fetch strava data: %w", err)
		}
		stravaData = fresh
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
	}

	if err := validate(root, out); err != nil {
		return fmt.Errorf("validate site-data.json: %w", err)
	}

	return writeSiteData(filepath.Join(root, "generated", "site-data.json"), out)
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

func buildProjects(ctx context.Context, gh *githubapi.Client, pf types.ProjectsFile) ([]types.Project, error) {
	projects := make([]types.Project, 0, len(pf.Curated))
	for _, c := range pf.Curated {
		p := types.Project{
			Name:        c.Name,
			Description: c.Summary,
			URL:         c.Repo,
			Topics:      c.Topics,
			Images:      c.Images,
			Featured:    c.Featured,
			Links:       c.Links,
		}
		if p.Links == nil {
			p.Links = []types.ProjectLink{}
		}
		if c.Homepage != nil && *c.Homepage != "" {
			p.Links = append(p.Links, types.ProjectLink{Name: "Live", URL: *c.Homepage})
		}

		owner, name, err := githubapi.ParseRepo(c.Repo)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", c.ID, err)
		}
		info, err := gh.FetchRepo(ctx, owner, name)
		if err != nil {
			return nil, fmt.Errorf("project %s: %w", c.ID, err)
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
