package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
)

// HubTag represents a tag entry returned by Docker Hub v2 API
type HubTag struct {
	Name        string     `json:"name"`
	LastUpdated *time.Time `json:"last_updated"`
	Digest      string     `json:"digest"`
}

type hubTagsResponse struct {
	Count   int      `json:"count"`
	Next    string   `json:"next"`
	Results []HubTag `json:"results"`
}

// Client interacts with Docker Hub
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Docker Hub client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		baseURL:    "https://hub.docker.com/v2/repositories",
	}
}

// NormalizeRepo splits an image reference into a Docker Hub repository path and tag
func NormalizeRepo(imageRef string) (string, string) {
	ref := strings.TrimSpace(imageRef)
	// Strip registry prefix if docker.io
	ref = strings.TrimPrefix(ref, "docker.io/")
	ref = strings.TrimPrefix(ref, "index.docker.io/")

	parts := strings.Split(ref, ":")
	repo := parts[0]
	tag := "latest"
	if len(parts) > 1 {
		tag = parts[1]
	}

	// Official images without slash are under library/
	if !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}

	return repo, tag
}

// FetchTags retrieves recent tags for a repository from Docker Hub
func (c *Client) FetchTags(ctx context.Context, repo string) ([]HubTag, error) {
	url := fmt.Sprintf("%s/%s/tags?page_size=100&ordering=last_updated", c.baseURL, repo)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Kraken-Admin-Tool/1.0")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query Docker Hub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository '%s' not found on Docker Hub", repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker Hub returned status %d", resp.StatusCode)
	}

	var res hubTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("failed to decode Docker Hub response: %w", err)
	}

	return res.Results, nil
}

// SemverTag represents a parsed semantic version with an optional distribution variant
type SemverTag struct {
	OriginalTag string
	Major       int
	Minor       int
	Patch       int
	Variant     string
	Digest      string
	LastUpdated *time.Time
}

var (
	// Matches versions like 7, 7.0, 7.0.12, 1.25.4-alpine, 20-alpine3.18, 3.19.1
	semverRegex = regexp.MustCompile(`^v?(\d+)(?:\.(\d+))?(?:\.(\d+))?(.*)$`)
)

// ParseSemverTag parses an image tag into numeric versions and variant suffix
func ParseSemverTag(tag string, digest string, updated *time.Time) (SemverTag, bool) {
	clean := strings.TrimSpace(tag)
	if clean == "" || clean == "latest" || strings.HasPrefix(clean, "sha256") {
		return SemverTag{}, false
	}

	// Filter out pre-releases, release candidates
	lower := strings.ToLower(clean)
	if strings.Contains(lower, "-rc") || strings.Contains(lower, "-beta") || strings.Contains(lower, "-alpha") || strings.Contains(lower, "-dev") {
		return SemverTag{}, false
	}

	matches := semverRegex.FindStringSubmatch(clean)
	if len(matches) < 2 {
		return SemverTag{}, false
	}

	major, _ := strconv.Atoi(matches[1])
	minor := 0
	if matches[2] != "" {
		minor, _ = strconv.Atoi(matches[2])
	}
	patch := 0
	if matches[3] != "" {
		patch, _ = strconv.Atoi(matches[3])
	}
	variant := ""
	if len(matches) > 4 {
		variant = matches[4]
	}

	return SemverTag{
		OriginalTag: clean,
		Major:       major,
		Minor:       minor,
		Patch:       patch,
		Variant:     variant,
		Digest:      digest,
		LastUpdated: updated,
	}, true
}

// CompareVersions returns > 0 if a > b, < 0 if a < b, 0 if equal
func CompareVersions(a, b SemverTag) int {
	if a.Major != b.Major {
		return a.Major - b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor - b.Minor
	}
	return a.Patch - b.Patch
}

// CheckUpdates inspects Docker Hub tags to find Retain vs Full upgrade targets
func (c *Client) CheckUpdates(
	ctx context.Context,
	imageRef string,
	currentDigest string,
	currentCreatedAt time.Time,
) (*models.ImageUpdateInfo, error) {
	repo, currentTagStr := NormalizeRepo(imageRef)

	info := &models.ImageUpdateInfo{
		Repository:       repo,
		CurrentTag:       currentTagStr,
		CurrentDigest:    currentDigest,
		CurrentCreatedAt: currentCreatedAt,
	}

	tags, err := c.FetchTags(ctx, repo)
	if err != nil {
		return nil, err
	}

	currentParsed, isSemver := ParseSemverTag(currentTagStr, currentDigest, &currentCreatedAt)

	// Check if current tag has a newer upstream digest on Docker Hub (e.g. security rebuild)
	for _, t := range tags {
		if t.Name == currentTagStr {
			if t.LastUpdated != nil {
				info.UpstreamCreatedAt = *t.LastUpdated
			}
			if currentDigest != "" && t.Digest != "" && !strings.EqualFold(t.Digest, currentDigest) {
				if t.LastUpdated != nil && t.LastUpdated.After(currentCreatedAt) {
					info.IsRebuildDigest = true
					info.RetainHasUpdate = true
					info.RetainTag = currentTagStr
					info.RetainDigest = t.Digest
				}
			}
			break
		}
	}

	if !isSemver {
		// Non-semver (e.g. "latest"); can only check digest rebuild
		return info, nil
	}

	var parsedTags []SemverTag
	for _, t := range tags {
		if p, ok := ParseSemverTag(t.Name, t.Digest, t.LastUpdated); ok {
			parsedTags = append(parsedTags, p)
		}
	}

	// Sort tags ascending
	sort.Slice(parsedTags, func(i, j int) bool {
		return CompareVersions(parsedTags[i], parsedTags[j]) < 0
	})

	var bestRetain *SemverTag
	var bestFull *SemverTag

	for _, cand := range parsedTags {
		// Variant must match (e.g. -alpine with -alpine)
		if !strings.EqualFold(cand.Variant, currentParsed.Variant) {
			continue
		}

		// Retain candidate: must match same major and minor line (patch update)
		isRetainLine := (cand.Major == currentParsed.Major && cand.Minor == currentParsed.Minor)

		if isRetainLine && CompareVersions(cand, currentParsed) > 0 {
			if bestRetain == nil || CompareVersions(cand, *bestRetain) > 0 {
				cCopy := cand
				bestRetain = &cCopy
			}
		}

		// Full upgrade candidate: highest overall
		if CompareVersions(cand, currentParsed) > 0 {
			if bestFull == nil || CompareVersions(cand, *bestFull) > 0 {
				fCopy := cand
				bestFull = &fCopy
			}
		}
	}

	if bestRetain != nil {
		info.RetainTag = bestRetain.OriginalTag
		info.RetainDigest = bestRetain.Digest
		info.RetainHasUpdate = true
	}

	if bestFull != nil {
		info.FullTag = bestFull.OriginalTag
		info.FullDigest = bestFull.Digest
		info.FullHasUpdate = true
	}

	return info, nil
}
