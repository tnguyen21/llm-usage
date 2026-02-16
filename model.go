package main

import "encoding/json"

type UsageResponse struct {
	FiveHour      *UsageBucket    `json:"five_hour"`
	SevenDay      *UsageBucket    `json:"seven_day"`
	SevenDayOpus  *UsageBucket    `json:"seven_day_opus"`
	SevenDayOAuth json.RawMessage `json:"seven_day_oauth_apps"`
	IguanaNecktie json.RawMessage `json:"iguana_necktie"`
}

type UsageBucket struct {
	Utilization float64 `json:"utilization"` // 0.0–100.0
	ResetsAt    *string `json:"resets_at"`   // ISO 8601 or null
}

type jsonlEntry struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Message   *struct {
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type KeychainCredentials struct {
	ClaudeAiOauth *OAuthEntry `json:"claudeAiOauth"`
}

type OAuthEntry struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	ExpiresAt        int64  `json:"expiresAt"`
	SubscriptionType string `json:"subscriptionType"`
}
