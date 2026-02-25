package versioning

import (
	"sort"
	"strings"
)

const (
	APIMajorVersion   = "v1"
	CurrentAPIVersion = "2026-02-25"
	PinHeader         = "X-RailPush-API-Version"
	PinQueryParam     = "api_version"
)

type VersionRecord struct {
	ID             string   `json:"id"`
	Aliases        []string `json:"aliases,omitempty"`
	Deprecated     bool     `json:"deprecated"`
	Sunset         string   `json:"sunset,omitempty"`
	DeprecationURL string   `json:"deprecation_url,omitempty"`
	Notes          string   `json:"notes,omitempty"`
}

type ChangelogEntry struct {
	Date       string   `json:"date"`
	Version    string   `json:"version"`
	Highlights []string `json:"highlights"`
}

type VersionResponse struct {
	MajorVersion string          `json:"major_version"`
	Current      string          `json:"current"`
	PinHeader    string          `json:"pin_header"`
	PinQuery     string          `json:"pin_query"`
	Supported    []VersionRecord `json:"supported"`
	ChangelogURL string          `json:"changelog_url"`
}

type ResolvedVersion struct {
	VersionRecord
	Requested string `json:"requested"`
	Source    string `json:"source"`
}

var supportedVersions = []VersionRecord{
	{
		ID:      CurrentAPIVersion,
		Aliases: []string{"v1", "latest"},
		Notes:   "Current stable API schema for /api/v1",
	},
	{
		ID:             "2025-12-01",
		Deprecated:     true,
		Sunset:         "Wed, 01 Jul 2026 00:00:00 GMT",
		DeprecationURL: "https://railpush.com/docs#api-versioning",
		Notes:          "Legacy compatibility pin. Migrate to 2026-02-25.",
	},
}

var changelog = []ChangelogEntry{
	{
		Date:    "2026-02-25",
		Version: CurrentAPIVersion,
		Highlights: []string{
			"Introduced API version pinning via X-RailPush-API-Version and api_version query parameter",
			"Added deprecation/sunset signaling for deprecated pinned versions",
			"Added /api/v1/version and /api/v1/version/changelog metadata endpoints",
		},
	},
	{
		Date:    "2025-12-01",
		Version: "2025-12-01",
		Highlights: []string{
			"Initial dated compatibility version for /api/v1 integrations",
		},
	},
}

func VersionInfo() VersionResponse {
	cloned := make([]VersionRecord, 0, len(supportedVersions))
	for _, v := range supportedVersions {
		cloned = append(cloned, v)
	}
	return VersionResponse{
		MajorVersion: APIMajorVersion,
		Current:      CurrentAPIVersion,
		PinHeader:    PinHeader,
		PinQuery:     PinQueryParam,
		Supported:    cloned,
		ChangelogURL: "/api/v1/version/changelog",
	}
}

func Changelog() []ChangelogEntry {
	out := make([]ChangelogEntry, 0, len(changelog))
	for _, entry := range changelog {
		copied := entry
		copied.Highlights = append([]string{}, entry.Highlights...)
		out = append(out, copied)
	}
	return out
}

func ResolveVersionPin(raw string) (ResolvedVersion, bool) {
	raw = strings.TrimSpace(raw)
	normalized := strings.ToLower(raw)
	if normalized == "" {
		return ResolvedVersion{VersionRecord: supportedVersions[0], Requested: "", Source: "default"}, true
	}

	for _, v := range supportedVersions {
		if strings.EqualFold(v.ID, normalized) {
			return ResolvedVersion{VersionRecord: v, Requested: raw}, true
		}
		for _, alias := range v.Aliases {
			if strings.EqualFold(alias, normalized) {
				return ResolvedVersion{VersionRecord: v, Requested: raw}, true
			}
		}
	}
	return ResolvedVersion{}, false
}

func SupportedPins() []string {
	pins := []string{}
	seen := map[string]struct{}{}
	for _, v := range supportedVersions {
		id := strings.TrimSpace(v.ID)
		if id != "" {
			lower := strings.ToLower(id)
			if _, ok := seen[lower]; !ok {
				seen[lower] = struct{}{}
				pins = append(pins, id)
			}
		}
		for _, alias := range v.Aliases {
			alias = strings.TrimSpace(alias)
			if alias == "" {
				continue
			}
			lower := strings.ToLower(alias)
			if _, ok := seen[lower]; ok {
				continue
			}
			seen[lower] = struct{}{}
			pins = append(pins, alias)
		}
	}
	sort.Strings(pins)
	return pins
}
