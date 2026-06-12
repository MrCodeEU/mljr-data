// Package strava fetches public aggregate activity data from the Strava
// API. Ported from mljr-web's projects/homepage/scrapers/strava.go, using
// this module's own types (no maps or GPS traces are fetched or stored).
package strava

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"mljr-data/generator/internal/types"
)

const (
	apiBase  = "https://www.strava.com/api/v3"
	tokenURL = "https://www.strava.com/oauth/token" //nolint:gosec // OAuth endpoint URL, not a credential
)

type Config struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	MaxPages     int
}

type Client struct {
	cfg         Config
	http        *http.Client
	accessToken string
	tokenExpiry time.Time
}

func New(cfg Config) *Client {
	if cfg.MaxPages <= 0 {
		cfg.MaxPages = 5
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) Fetch(ctx context.Context, now time.Time) (types.StravaData, error) {
	if strings.TrimSpace(c.cfg.ClientID) == "" || strings.TrimSpace(c.cfg.ClientSecret) == "" || strings.TrimSpace(c.cfg.RefreshToken) == "" {
		return types.StravaData{}, fmt.Errorf("missing strava credentials")
	}
	if err := c.ensureAccessToken(ctx); err != nil {
		return types.StravaData{}, err
	}

	athleteID, err := c.fetchAthleteID(ctx)
	if err != nil {
		return types.StravaData{}, err
	}
	stats, err := c.fetchStats(ctx, athleteID)
	if err != nil {
		return types.StravaData{}, err
	}
	raw, err := c.fetchActivities(ctx)
	if err != nil {
		return types.StravaData{}, err
	}
	return buildStravaData(stats, raw, now), nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

type activityDTO struct {
	ID                 int64   `json:"id"`
	Name               string  `json:"name"`
	Distance           float64 `json:"distance"`
	MovingTime         int     `json:"moving_time"`
	ElapsedTime        int     `json:"elapsed_time"`
	TotalElevationGain float64 `json:"total_elevation_gain"`
	Type               string  `json:"type"`
	StartDate          string  `json:"start_date"`
	StartDateLocal     string  `json:"start_date_local"`
	AverageSpeed       float64 `json:"average_speed"`
	MaxSpeed           float64 `json:"max_speed"`
	AverageHeartrate   float64 `json:"average_heartrate"`
	MaxHeartrate       float64 `json:"max_heartrate"`
	Calories           float64 `json:"calories"`
	Kilojoules         float64 `json:"kilojoules"`
}

type statsDTO struct {
	AllRunTotals statsTotals `json:"all_run_totals"`
	YTDRunTotals statsTotals `json:"ytd_run_totals"`
}

type statsTotals struct {
	Count         int     `json:"count"`
	Distance      float64 `json:"distance"`
	MovingTime    float64 `json:"moving_time"`
	ElapsedTime   float64 `json:"elapsed_time"`
	ElevationGain float64 `json:"elevation_gain"`
}

func (c *Client) ensureAccessToken(ctx context.Context) error {
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry.Add(-time.Minute)) {
		return nil
	}
	form := url.Values{}
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", c.cfg.RefreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var token tokenResponse
	if err := c.doJSON(req, &token); err != nil {
		return fmt.Errorf("refresh strava token: %w", err)
	}
	if token.AccessToken == "" {
		return fmt.Errorf("refresh strava token: missing access token")
	}
	c.accessToken = token.AccessToken
	c.tokenExpiry = time.Unix(token.ExpiresAt, 0)
	if token.RefreshToken != "" {
		c.cfg.RefreshToken = token.RefreshToken
	}
	return nil
}

func (c *Client) fetchAthleteID(ctx context.Context) (int64, error) {
	var athlete struct {
		ID int64 `json:"id"`
	}
	req, err := c.authRequest(ctx, http.MethodGet, apiBase+"/athlete")
	if err != nil {
		return 0, err
	}
	if err := c.doJSON(req, &athlete); err != nil {
		return 0, fmt.Errorf("fetch strava athlete: %w", err)
	}
	if athlete.ID == 0 {
		return 0, fmt.Errorf("fetch strava athlete: missing athlete id")
	}
	return athlete.ID, nil
}

func (c *Client) fetchStats(ctx context.Context, athleteID int64) (statsDTO, error) {
	var stats statsDTO
	req, err := c.authRequest(ctx, http.MethodGet, fmt.Sprintf("%s/athletes/%d/stats", apiBase, athleteID))
	if err != nil {
		return stats, err
	}
	if err := c.doJSON(req, &stats); err != nil {
		return stats, fmt.Errorf("fetch strava stats: %w", err)
	}
	return stats, nil
}

func (c *Client) fetchActivities(ctx context.Context) ([]activityDTO, error) {
	var out []activityDTO
	after := time.Now().AddDate(-1, 0, 0)
	for page := 1; page <= c.cfg.MaxPages; page++ {
		endpoint := fmt.Sprintf("%s/athlete/activities?per_page=200&page=%d&after=%d", apiBase, page, after.Unix())
		req, err := c.authRequest(ctx, http.MethodGet, endpoint)
		if err != nil {
			return nil, err
		}
		var batch []activityDTO
		if err := c.doJSON(req, &batch); err != nil {
			return nil, fmt.Errorf("fetch strava activities page %d: %w", page, err)
		}
		if len(batch) == 0 {
			break
		}
		out = append(out, batch...)
		if len(batch) < 200 {
			break
		}
	}
	return out, nil
}

func (c *Client) authRequest(ctx context.Context, method, endpoint string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	return req, nil
}

func (c *Client) doJSON(req *http.Request, target any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func buildStravaData(stats statsDTO, raw []activityDTO, now time.Time) types.StravaData {
	activities := make([]types.StravaActivity, 0, len(raw))
	for _, a := range raw {
		activities = append(activities, convertActivity(a))
	}
	sort.Slice(activities, func(i, j int) bool {
		return activityTime(activities[i]).After(activityTime(activities[j]))
	})

	runs := filterActivities(activities, "running")
	recent := activities
	if len(recent) > 10 {
		recent = recent[:10]
	}
	return types.StravaData{
		GeneratedAt:       now.UTC().Format(time.RFC3339),
		Year:              now.Format("2006"),
		TotalStats:        convertStats(stats.AllRunTotals),
		YearToDateStats:   convertStats(stats.YTDRunTotals),
		RecentActivities:  recent,
		BestActivities:    bestActivities(runs),
		PersonalRecords:   personalRecords(runs),
		Disciplines:       disciplines(activities),
		MonthlyActivities: monthBuckets(activities),
	}
}

func convertStats(t statsTotals) types.StravaStats {
	return types.StravaStats{
		Count:         t.Count,
		Distance:      t.Distance,
		MovingTime:    int(t.MovingTime),
		ElapsedTime:   int(t.ElapsedTime),
		ElevationGain: t.ElevationGain,
	}
}

func convertActivity(a activityDTO) types.StravaActivity {
	calories := a.Calories
	if calories == 0 && a.Kilojoules > 0 {
		calories = a.Kilojoules * 0.239
	}
	avgPace := 0.0
	if a.AverageSpeed > 0 {
		avgPace = 1 / a.AverageSpeed
	}
	return types.StravaActivity{
		ID:                 a.ID,
		Name:               a.Name,
		Distance:           a.Distance,
		MovingTime:         a.MovingTime,
		ElapsedTime:        a.ElapsedTime,
		TotalElevationGain: a.TotalElevationGain,
		Type:               a.Type,
		StartDate:          firstNonEmpty(a.StartDateLocal, a.StartDate),
		StartDateLocal:     a.StartDateLocal,
		AveragePace:        avgPace,
		AverageSpeed:       a.AverageSpeed,
		MaxSpeed:           a.MaxSpeed,
		AverageHeartrate:   a.AverageHeartrate,
		MaxHeartrate:       a.MaxHeartrate,
		Calories:           calories,
	}
}

func disciplineType(kind string) string {
	switch strings.ToLower(kind) {
	case "run", "trailrun", "virtualrun":
		return "running"
	case "ride", "virtualride", "mountainbikeride", "gravelride", "ebikeride", "emountainbikeride":
		return "cycling"
	default:
		return "training"
	}
}

func filterActivities(activities []types.StravaActivity, discipline string) []types.StravaActivity {
	out := make([]types.StravaActivity, 0)
	for _, a := range activities {
		if disciplineType(a.Type) == discipline {
			out = append(out, a)
		}
	}
	return out
}

func bestActivities(activities []types.StravaActivity) types.StravaBestRecords {
	if len(activities) == 0 {
		return types.StravaBestRecords{}
	}
	best := types.StravaBestRecords{
		LongestDistance: activities[0],
		LongestTime:     activities[0],
		FastestPace:     activities[0],
		MostElevation:   activities[0],
	}
	for _, a := range activities {
		if a.Distance > best.LongestDistance.Distance {
			best.LongestDistance = a
		}
		if a.MovingTime > best.LongestTime.MovingTime {
			best.LongestTime = a
		}
		if a.AveragePace > 0 && (best.FastestPace.AveragePace == 0 || a.AveragePace < best.FastestPace.AveragePace) {
			best.FastestPace = a
		}
		if a.TotalElevationGain > best.MostElevation.TotalElevationGain {
			best.MostElevation = a
		}
	}
	return best
}

func personalRecords(activities []types.StravaActivity) []types.StravaRecord {
	targets := []struct {
		name string
		m    float64
	}{
		{"5k", 5000},
		{"10k", 10000},
		{"half_marathon", 21097.5},
		{"marathon", 42195},
	}
	records := make([]types.StravaRecord, 0, len(targets))
	for _, target := range targets {
		var best types.StravaActivity
		for _, a := range activities {
			tolerance := target.m * 0.02
			if a.Distance < target.m-tolerance || a.Distance > target.m+tolerance {
				continue
			}
			if best.MovingTime == 0 || a.MovingTime < best.MovingTime {
				best = a
			}
		}
		if best.MovingTime > 0 {
			records = append(records, types.StravaRecord{
				Type:     target.name,
				Time:     best.MovingTime,
				Distance: best.Distance,
				Date:     displayDate(best),
				Activity: best,
			})
		}
	}
	return records
}

func disciplines(activities []types.StravaActivity) []types.StravaDiscipline {
	type bucket struct {
		label    string
		items    []types.StravaActivity
		time     int
		distance float64
		hrTotal  float64
		hrCount  int
	}
	buckets := map[string]*bucket{
		"running":  {label: "Running"},
		"cycling":  {label: "Cycling"},
		"training": {label: "Training"},
	}
	for _, a := range activities {
		key := disciplineType(a.Type)
		b := buckets[key]
		b.items = append(b.items, a)
		b.time += a.MovingTime
		b.distance += a.Distance
		if a.AverageHeartrate > 0 {
			b.hrTotal += a.AverageHeartrate
			b.hrCount++
		}
	}
	order := []string{"running", "cycling", "training"}
	out := make([]types.StravaDiscipline, 0, len(order))
	for _, key := range order {
		b := buckets[key]
		if len(b.items) == 0 {
			continue
		}
		items := b.items
		if len(items) > 5 {
			items = items[:5]
		}
		avgHR := 0.0
		if b.hrCount > 0 {
			avgHR = b.hrTotal / float64(b.hrCount)
		}
		out = append(out, types.StravaDiscipline{
			Type:          key,
			Label:         b.label,
			Count:         len(b.items),
			TotalTime:     b.time,
			TotalDistance: b.distance,
			AvgHeartrate:  avgHR,
			Activities:    items,
		})
	}
	return out
}

func monthBuckets(activities []types.StravaActivity) []types.StravaMonthBucket {
	byMonth := map[string]*types.StravaMonthBucket{}
	for _, a := range activities {
		t := activityTime(a)
		if t.IsZero() {
			continue
		}
		key := t.Format("2006-01")
		b := byMonth[key]
		if b == nil {
			b = &types.StravaMonthBucket{Month: key}
			byMonth[key] = b
		}
		b.Count++
		b.Distance += a.Distance
		b.Time += a.MovingTime
	}
	keys := make([]string, 0, len(byMonth))
	for key := range byMonth {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]types.StravaMonthBucket, 0, len(keys))
	for _, key := range keys {
		out = append(out, *byMonth[key])
	}
	return out
}

func activityTime(a types.StravaActivity) time.Time {
	raw := firstNonEmpty(a.StartDate, a.StartDateLocal)
	t, _ := time.Parse(time.RFC3339, raw)
	return t
}

func displayDate(a types.StravaActivity) string {
	t := activityTime(a)
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
