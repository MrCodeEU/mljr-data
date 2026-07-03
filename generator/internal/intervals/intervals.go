// Package intervals fetches activity data from the intervals.icu API,
// replacing the Strava integration after Strava closed API access to free
// tier accounts. Activities are synced to intervals.icu via the official
// Zepp/Amazfit integration; this package only reads them back out.
//
// intervals.icu has no equivalent of Strava's per-athlete "stats" endpoint
// (all-time / YTD totals), so TotalStats/YearToDateStats here are computed
// from the fetched activity window instead of a server-side aggregate.
package intervals

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"mljr-data/generator/internal/types"
)

const apiBase = "https://intervals.icu/api/v1"

type Config struct {
	APIKey string
	// AthleteID defaults to "0", which intervals.icu treats as "the
	// authenticated athlete" so a numeric athlete ID is not required.
	AthleteID string
	// HistoryYears bounds how far back activities are fetched (there is no
	// all-time totals endpoint to fall back on). Defaults to 5.
	HistoryYears int
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	if strings.TrimSpace(cfg.AthleteID) == "" {
		cfg.AthleteID = "0"
	}
	if cfg.HistoryYears <= 0 {
		cfg.HistoryYears = 5
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

// activityDTO covers the subset of intervals.icu activity fields used here.
// Field names are per the public API's activity list response; "id" is a
// string like "i12345678" rather than Strava's numeric ID.
type activityDTO struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Type               string  `json:"type"`
	StartDateLocal     string  `json:"start_date_local"`
	Distance           float64 `json:"distance"`
	MovingTime         int     `json:"moving_time"`
	ElapsedTime        int     `json:"elapsed_time"`
	TotalElevationGain float64 `json:"total_elevation_gain"`
	AverageSpeed       float64 `json:"average_speed"`
	MaxSpeed           float64 `json:"max_speed"`
	AverageHeartrate   float64 `json:"average_heartrate"`
	MaxHeartrate       float64 `json:"max_heartrate"`
	Calories           float64 `json:"calories"`
	IcuJoules          float64 `json:"icu_joules"`
}

func (c *Client) Fetch(ctx context.Context, now time.Time) (types.StravaData, error) {
	if strings.TrimSpace(c.cfg.APIKey) == "" {
		return types.StravaData{}, fmt.Errorf("missing intervals.icu API key")
	}

	oldest := now.AddDate(-c.cfg.HistoryYears, 0, 0)
	raw, err := c.fetchActivities(ctx, oldest, now)
	if err != nil {
		return types.StravaData{}, err
	}
	return buildStravaData(raw, now), nil
}

// wellnessDayDTO covers the wellness.json fields the Amazfit Balance 2 /
// Zepp actually populates (confirmed via a live probe of 31 days of data);
// the endpoint also returns many device-specific fields (weight, spO2,
// vo2max, stress, ...) that are consistently null for this device and are
// left out entirely.
type wellnessDayDTO struct {
	ID           string  `json:"id"` // "2026-07-03"
	CTL          float64 `json:"ctl"`
	ATL          float64 `json:"atl"`
	RestingHR    int     `json:"restingHR"`
	HRV          float64 `json:"hrv"`
	SleepSecs    int     `json:"sleepSecs"`
	SleepScore   float64 `json:"sleepScore"`
	SleepQuality int     `json:"sleepQuality"`
	Steps        int     `json:"steps"`
}

// FetchWellness returns daily training-load and health metrics (fitness/
// fatigue, resting heart rate, HRV, sleep, steps) for the given window.
func (c *Client) FetchWellness(ctx context.Context, oldest, newest time.Time) ([]types.WellnessDay, error) {
	fields := "id,ctl,atl,restingHR,hrv,sleepSecs,sleepScore,sleepQuality,steps"
	endpoint := fmt.Sprintf("%s/athlete/%s/wellness.json?%s", apiBase, url.PathEscape(c.cfg.AthleteID), url.Values{
		"oldest": {oldest.Format("2006-01-02")},
		"newest": {newest.Format("2006-01-02")},
		"fields": {fields},
	}.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("API_KEY", c.cfg.APIKey)

	var days []wellnessDayDTO
	if err := c.doJSON(req, &days); err != nil {
		return nil, fmt.Errorf("fetch intervals.icu wellness: %w", err)
	}

	out := make([]types.WellnessDay, 0, len(days))
	for _, d := range days {
		out = append(out, types.WellnessDay{
			Date:             d.ID,
			CTL:              d.CTL,
			ATL:              d.ATL,
			Form:             d.CTL - d.ATL,
			RestingHeartrate: d.RestingHR,
			HRV:              d.HRV,
			SleepTime:        d.SleepSecs,
			SleepScore:       d.SleepScore,
			SleepQuality:     d.SleepQuality,
			Steps:            d.Steps,
		})
	}
	return out, nil
}

func (c *Client) fetchActivities(ctx context.Context, oldest, newest time.Time) ([]activityDTO, error) {
	endpoint := fmt.Sprintf("%s/athlete/%s/activities?%s", apiBase, url.PathEscape(c.cfg.AthleteID), url.Values{
		"oldest": {oldest.Format("2006-01-02")},
		"newest": {newest.Format("2006-01-02")},
	}.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("API_KEY", c.cfg.APIKey)

	var out []activityDTO
	if err := c.doJSON(req, &out); err != nil {
		return nil, fmt.Errorf("fetch intervals.icu activities: %w", err)
	}
	return out, nil
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

func buildStravaData(raw []activityDTO, now time.Time) types.StravaData {
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
	yearStr := now.Format("2006")
	ytd := make([]types.StravaActivity, 0, len(activities))
	for _, a := range activities {
		if strings.HasPrefix(activityTime(a).Format("2006"), yearStr) {
			ytd = append(ytd, a)
		}
	}

	return types.StravaData{
		GeneratedAt:       now.UTC().Format(time.RFC3339),
		Year:              yearStr,
		TotalStats:        aggregateStats(activities),
		YearToDateStats:   aggregateStats(ytd),
		RecentActivities:  recent,
		BestActivities:    bestActivities(runs),
		PersonalRecords:   personalRecords(runs),
		Disciplines:       disciplines(activities),
		MonthlyActivities: monthBuckets(activities),
	}
}

func aggregateStats(activities []types.StravaActivity) types.StravaStats {
	var s types.StravaStats
	for _, a := range activities {
		s.Count++
		s.Distance += a.Distance
		s.MovingTime += a.MovingTime
		s.ElapsedTime += a.ElapsedTime
		s.ElevationGain += a.TotalElevationGain
	}
	return s
}

var idDigits = regexp.MustCompile(`\d+`)

// activityID converts intervals.icu's string activity IDs (e.g. "i12345678")
// to the int64 the shared StravaActivity type / site-data schema expects.
func activityID(raw string) int64 {
	digits := idDigits.FindString(raw)
	if digits == "" {
		return 0
	}
	id, _ := strconv.ParseInt(digits, 10, 64)
	return id
}

func convertActivity(a activityDTO) types.StravaActivity {
	calories := a.Calories
	if calories == 0 && a.IcuJoules > 0 {
		calories = a.IcuJoules / 1000
	}
	avgPace := 0.0
	if a.AverageSpeed > 0 {
		avgPace = 1000 / (a.AverageSpeed * 60)
	}
	startDate := normalizeStartDate(a.StartDateLocal)
	return types.StravaActivity{
		ID:                 activityID(a.ID),
		Name:               a.Name,
		Distance:           a.Distance,
		MovingTime:         a.MovingTime,
		ElapsedTime:        a.ElapsedTime,
		TotalElevationGain: a.TotalElevationGain,
		Type:               a.Type,
		StartDate:          startDate,
		StartDateLocal:     startDate,
		AveragePace:        avgPace,
		AverageSpeed:       a.AverageSpeed,
		MaxSpeed:           a.MaxSpeed,
		AverageHeartrate:   a.AverageHeartrate,
		MaxHeartrate:       a.MaxHeartrate,
		Calories:           calories,
	}
}

// normalizeStartDate converts intervals.icu's timezone-less
// "2006-01-02T15:04:05" local timestamp into RFC3339 so it round-trips
// through the schema's date-time format and activityTime's RFC3339 parse.
func normalizeStartDate(raw string) string {
	if raw == "" {
		return ""
	}
	t, err := time.Parse("2006-01-02T15:04:05", raw)
	if err != nil {
		return raw
	}
	return t.Format(time.RFC3339)
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
