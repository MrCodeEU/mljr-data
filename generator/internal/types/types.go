// Package types mirrors the site-data.v1 JSON Schema used by the mljr.eu
// homepage. Field shapes must stay in sync with
// ../../schemas/site-data.schema.json.
package types

// SiteData is the merged, versioned payload consumed by the homepage.
type SiteData struct {
	SchemaVersion  string       `json:"schema_version"`
	GeneratedAt    string       `json:"generated_at"`
	GitHubProjects []Project    `json:"github_projects"`
	LinkedInData   LinkedInData `json:"linkedin_data"`
	StravaData     StravaData   `json:"strava_data"`
	GitHubStats    *GitHubStats `json:"github_stats,omitempty"`
}

type Project struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	URL         string        `json:"url"`
	Stars       int           `json:"stars"`
	Language    string        `json:"language"`
	Topics      []string      `json:"topics"`
	Images      []string      `json:"images"`
	Featured    bool          `json:"featured"`
	Links       []ProjectLink `json:"links"`
}

type ProjectLink struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type GitHubStats struct {
	CommitsYear   int               `json:"commits_year"`
	LongestStreak int               `json:"longest_streak"`
	Contributions []ContributionDay `json:"contributions"`
	LanguageShare []LanguageShare   `json:"language_share"`
}

type ContributionDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type LanguageShare struct {
	Name string  `json:"name"`
	Pct  float64 `json:"pct"`
}

type LinkedInData struct {
	Profile    Profile  `json:"profile"`
	Name       string   `json:"name"`
	Headline   string   `json:"headline"`
	Location   string   `json:"location"`
	About      string   `json:"about"`
	Experience []Job    `json:"experience"`
	Education  []School `json:"education"`
	Skills     []string `json:"skills"`
}

type Profile struct {
	Name     string `json:"name"`
	PhotoURL string `json:"photo_url"`
	Headline string `json:"headline,omitempty"`
	Location string `json:"location,omitempty"`
}

type Job struct {
	Title    string `json:"title"`
	Company  string `json:"company"`
	Type     string `json:"type"`
	Period   string `json:"period"`
	Duration string `json:"duration"`
	Desc     string `json:"description"`
}

type School struct {
	School string `json:"school"`
	Degree string `json:"degree"`
	Period string `json:"period"`
}

// StravaData mirrors hpdata.StravaData. Distances are in meters, durations
// in seconds.
type StravaData struct {
	GeneratedAt       string              `json:"generated_at,omitempty"`
	YTDCalories       float64             `json:"ytd_calories,omitempty"`
	TotalStats        StravaStats         `json:"total_stats"`
	YearToDateStats   StravaStats         `json:"year_to_date_stats"`
	RecentActivities  []StravaActivity    `json:"recent_activities"`
	BestActivities    StravaBestRecords   `json:"best_activities"`
	PersonalRecords   []StravaRecord      `json:"personal_records"`
	Disciplines       []StravaDiscipline  `json:"disciplines"`
	MonthlyActivities []StravaMonthBucket `json:"monthly_activities,omitempty"`
	Year              string              `json:"year,omitempty"`
}

type StravaStats struct {
	Count         int     `json:"count"`
	Distance      float64 `json:"distance"`
	MovingTime    int     `json:"moving_time"`
	ElapsedTime   int     `json:"elapsed_time"`
	ElevationGain float64 `json:"elevation_gain"`
}

type StravaActivity struct {
	ID                 int64   `json:"id,omitempty"`
	Name               string  `json:"name"`
	Distance           float64 `json:"distance"`
	MovingTime         int     `json:"moving_time"`
	ElapsedTime        int     `json:"elapsed_time,omitempty"`
	TotalElevationGain float64 `json:"total_elevation_gain,omitempty"`
	Type               string  `json:"type"`
	StartDate          string  `json:"start_date,omitempty"`
	StartDateLocal     string  `json:"start_date_local,omitempty"`
	AveragePace        float64 `json:"average_pace,omitempty"`
	AverageSpeed       float64 `json:"average_speed,omitempty"`
	MaxSpeed           float64 `json:"max_speed,omitempty"`
	AverageHeartrate   float64 `json:"average_heartrate,omitempty"`
	MaxHeartrate       float64 `json:"max_heartrate,omitempty"`
	Calories           float64 `json:"calories,omitempty"`
}

type StravaBestRecords struct {
	LongestDistance StravaActivity `json:"longest_distance"`
	LongestTime     StravaActivity `json:"longest_time"`
	FastestPace     StravaActivity `json:"fastest_pace"`
	MostElevation   StravaActivity `json:"most_elevation"`
}

type StravaRecord struct {
	Type     string         `json:"type"`
	Time     int            `json:"time"`
	Distance float64        `json:"distance"`
	Date     string         `json:"date"`
	Activity StravaActivity `json:"activity"`
}

type StravaDiscipline struct {
	Type          string           `json:"type"`
	Label         string           `json:"label"`
	Count         int              `json:"count"`
	TotalTime     int              `json:"total_time"`
	TotalDistance float64          `json:"total_distance"`
	AvgHeartrate  float64          `json:"avg_heartrate"`
	Activities    []StravaActivity `json:"activities"`
}

type StravaMonthBucket struct {
	Month    string  `json:"month"`
	Count    int     `json:"count"`
	Distance float64 `json:"distance"`
	Time     int     `json:"time"`
}
