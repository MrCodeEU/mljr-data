// Package githubapi fetches account-level GitHub stats (contributions,
// commit count, longest streak, language share) and per-repo enrichment
// (stars, language, topics, description) for curated projects.
//
// Deliberately does NOT scan README files or repo metadata for "portfolio"
// markers - the project list is manually curated in projects.json.
package githubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"mljr-data/generator/internal/types"
)

const (
	restBase    = "https://api.github.com"
	graphqlBase = "https://api.github.com/graphql"
)

type Client struct {
	token string
	http  *http.Client
}

func New(token string) *Client {
	return &Client{token: token, http: &http.Client{Timeout: 30 * time.Second}}
}

// RepoInfo holds the live fields fetched from the GitHub REST API for a
// single repository.
type RepoInfo struct {
	Description string
	Stars       int
	Language    string
	Topics      []string
}

// FetchRepo fetches stars/language/topics/description for owner/repo.
func (c *Client) FetchRepo(ctx context.Context, owner, repo string) (RepoInfo, error) {
	var dto struct {
		Description string   `json:"description"`
		Stars       int      `json:"stargazers_count"`
		Language    string   `json:"language"`
		Topics      []string `json:"topics"`
	}
	if err := c.getJSON(ctx, fmt.Sprintf("%s/repos/%s/%s", restBase, owner, repo), &dto); err != nil {
		return RepoInfo{}, err
	}
	return RepoInfo{Description: dto.Description, Stars: dto.Stars, Language: dto.Language, Topics: dto.Topics}, nil
}

// FetchLanguageShare aggregates language byte counts across the user's
// non-fork public repositories and returns the share by percentage,
// sorted descending.
func (c *Client) FetchLanguageShare(ctx context.Context, user string) ([]types.LanguageShare, error) {
	var repos []struct {
		Name         string `json:"name"`
		Fork         bool   `json:"fork"`
		LanguagesURL string `json:"languages_url"`
	}
	for page := 1; page <= 5; page++ {
		var batch []struct {
			Name         string `json:"name"`
			Fork         bool   `json:"fork"`
			LanguagesURL string `json:"languages_url"`
		}
		url := fmt.Sprintf("%s/users/%s/repos?per_page=100&page=%d&type=owner", restBase, user, page)
		if err := c.getJSON(ctx, url, &batch); err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		repos = append(repos, batch...)
		if len(batch) < 100 {
			break
		}
	}

	totals := map[string]int64{}
	var grandTotal int64
	for _, r := range repos {
		if r.Fork {
			continue
		}
		var langs map[string]int64
		if err := c.getJSON(ctx, r.LanguagesURL, &langs); err != nil {
			return nil, fmt.Errorf("fetch languages for %s: %w", r.Name, err)
		}
		for lang, bytes := range langs {
			totals[lang] += bytes
			grandTotal += bytes
		}
	}
	if grandTotal == 0 {
		return nil, nil
	}

	out := make([]types.LanguageShare, 0, len(totals))
	for lang, bytes := range totals {
		out = append(out, types.LanguageShare{
			Name: lang,
			Pct:  float64(bytes) / float64(grandTotal) * 100,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pct > out[j].Pct })
	return out, nil
}

// ContributionStats fetches the last 365 days of contribution activity via
// the GraphQL API: total commit contributions, the daily calendar, and the
// longest current streak of consecutive active days.
func (c *Client) ContributionStats(ctx context.Context, user string, now time.Time) (commitsYear int, contributions []types.ContributionDay, longestStreak int, err error) {
	from := now.AddDate(-1, 0, 1)
	const query = `
query($login: String!, $from: DateTime!, $to: DateTime!) {
  user(login: $login) {
    contributionsCollection(from: $from, to: $to) {
      totalCommitContributions
      contributionCalendar {
        weeks {
          contributionDays {
            date
            contributionCount
          }
        }
      }
    }
  }
}`
	body, err := json.Marshal(map[string]any{
		"query": query,
		"variables": map[string]any{
			"login": user,
			"from":  from.UTC().Format(time.RFC3339),
			"to":    now.UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return 0, nil, 0, err
	}

	var resp struct {
		Data struct {
			User struct {
				ContributionsCollection struct {
					TotalCommitContributions int `json:"totalCommitContributions"`
					ContributionCalendar     struct {
						Weeks []struct {
							ContributionDays []struct {
								Date  string `json:"date"`
								Count int    `json:"contributionCount"`
							} `json:"contributionDays"`
						} `json:"weeks"`
					} `json:"contributionCalendar"`
				} `json:"contributionsCollection"`
			} `json:"user"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, graphqlBase, strings.NewReader(string(body)))
	if err != nil {
		return 0, nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	if err := c.do(req, &resp); err != nil {
		return 0, nil, 0, err
	}
	if len(resp.Errors) > 0 {
		return 0, nil, 0, fmt.Errorf("github graphql: %s", resp.Errors[0].Message)
	}

	cc := resp.Data.User.ContributionsCollection
	days := make([]types.ContributionDay, 0, 366)
	for _, week := range cc.ContributionCalendar.Weeks {
		for _, d := range week.ContributionDays {
			days = append(days, types.ContributionDay{Date: d.Date, Count: d.Count})
		}
	}

	streak, current := 0, 0
	for _, d := range days {
		if d.Count > 0 {
			current++
			if current > streak {
				streak = current
			}
		} else {
			current = 0
		}
	}

	return cc.TotalCommitContributions, days, streak, nil
}

func (c *Client) getJSON(ctx context.Context, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	return c.do(req, target)
}

func (c *Client) do(req *http.Request, target any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("github api %s: status %d: %s", req.URL, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

// ParseRepo splits a github.com repo URL into owner and name.
func ParseRepo(repoURL string) (owner, name string, err error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(repoURL), "/")
	const prefix = "https://github.com/"
	if !strings.HasPrefix(trimmed, prefix) {
		return "", "", fmt.Errorf("not a github.com repo url: %s", repoURL)
	}
	parts := strings.Split(strings.TrimPrefix(trimmed, prefix), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid github.com repo url: %s", repoURL)
	}
	return parts[0], parts[1], nil
}
