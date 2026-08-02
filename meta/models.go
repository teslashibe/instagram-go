package meta

import "encoding/json"

type AccountSelection struct {
	PageID             string `json:"page_id,omitempty"`
	InstagramAccountID string `json:"instagram_account_id,omitempty"`
	AdAccountID        string `json:"ad_account_id,omitempty"`
}

type FacebookPage struct {
	ID                       string                    `json:"id"`
	Name                     string                    `json:"name"`
	Category                 string                    `json:"category,omitempty"`
	InstagramBusinessAccount *InstagramBusinessAccount `json:"instagram_business_account,omitempty"`
}

type InstagramBusinessAccount struct {
	ID       string `json:"id"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
}

type AdAccount struct {
	ID            string `json:"id"`
	AccountID     string `json:"account_id"`
	Name          string `json:"name"`
	Currency      string `json:"currency"`
	TimezoneName  string `json:"timezone_name,omitempty"`
	AccountStatus int    `json:"account_status,omitempty"`
	DisableReason int    `json:"disable_reason,omitempty"`
}

type Campaign struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Objective       string `json:"objective,omitempty"`
	Status          string `json:"status,omitempty"`
	EffectiveStatus string `json:"effective_status,omitempty"`
	BuyingType      string `json:"buying_type,omitempty"`
	StartTime       string `json:"start_time,omitempty"`
	StopTime        string `json:"stop_time,omitempty"`
	UpdatedTime     string `json:"updated_time,omitempty"`
}

type AdSet struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	CampaignID       string          `json:"campaign_id,omitempty"`
	Status           string          `json:"status,omitempty"`
	EffectiveStatus  string          `json:"effective_status,omitempty"`
	DailyBudget      string          `json:"daily_budget,omitempty"`
	LifetimeBudget   string          `json:"lifetime_budget,omitempty"`
	StartTime        string          `json:"start_time,omitempty"`
	EndTime          string          `json:"end_time,omitempty"`
	OptimizationGoal string          `json:"optimization_goal,omitempty"`
	BillingEvent     string          `json:"billing_event,omitempty"`
	Targeting        json.RawMessage `json:"targeting,omitempty"`
}

type Ad struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	AdSetID         string      `json:"adset_id,omitempty"`
	CampaignID      string      `json:"campaign_id,omitempty"`
	Status          string      `json:"status,omitempty"`
	EffectiveStatus string      `json:"effective_status,omitempty"`
	Creative        CreativeRef `json:"creative,omitempty"`
	UpdatedTime     string      `json:"updated_time,omitempty"`
}

type CreativeRef struct {
	ID string `json:"id"`
}

type AdCreative struct {
	ID               string          `json:"id"`
	Name             string          `json:"name,omitempty"`
	Title            string          `json:"title,omitempty"`
	Body             string          `json:"body,omitempty"`
	ObjectStoryID    string          `json:"object_story_id,omitempty"`
	InstagramActorID string          `json:"instagram_actor_id,omitempty"`
	ThumbnailURL     string          `json:"thumbnail_url,omitempty"`
	URLTags          string          `json:"url_tags,omitempty"`
	ObjectStorySpec  json.RawMessage `json:"object_story_spec,omitempty"`
}

type DeliveryStatus struct {
	ID               string `json:"id"`
	Name             string `json:"name,omitempty"`
	Status           string `json:"status,omitempty"`
	ConfiguredStatus string `json:"configured_status,omitempty"`
	EffectiveStatus  string `json:"effective_status,omitempty"`
}
