package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/blang/semver"
	"github.com/direct-connect/go-dcpp/version"
)

// ReleaseInfo contains an information about a specific software release.
type ReleaseInfo struct {
	Version semver.Version
	Time    time.Time
	URL     string
	Desc    string
}

// CheckForUpdates fetches the latest release info and returns it, if it's different from the current version.
// The method does its own caching so it is safe to call it as frequently as necessary. Force flag can be used to bypass the cache.
func CheckForUpdates(ctx context.Context, force bool) (*ReleaseInfo, error) {
	cur, err := semver.Parse(version.Vers)
	if err != nil {
		return nil, fmt.Errorf("cannot parse hub's own version: %w", err)
	}
	latest, err := getLatestVersion(ctx, force)
	if err != nil {
		return nil, fmt.Errorf("cannot check the new version: %w", err)
	}
	if latest == nil || !latest.Version.GT(cur) {
		return nil, nil
	}
	return latest, nil
}

var latest struct {
	sync.RWMutex
	release *ReleaseInfo
	updated time.Time
}

func getLatestVersion(ctx context.Context, force bool) (*ReleaseInfo, error) {
	const updateInterval = 24 * time.Hour
	now := time.Now()

	if !force {
		latest.RLock()
		rel := latest.release
		upd := latest.updated
		latest.RUnlock()

		if now.Sub(upd) < updateInterval {
			return rel, nil
		}
	}

	latest.Lock()
	defer latest.Unlock()
	if !force && now.Sub(latest.updated) < updateInterval {
		return latest.release, nil
	}

	const (
		orgName  = "direct-connect"
		repoName = "go-dcpp"
		endpoint = "https://api.github.com/repos/" + orgName + "/" + repoName + "/releases/latest"
	)
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		// TODO(dennwc): parse error
		return nil, fmt.Errorf("status: %s", resp.Status)
	}
	var out struct {
		Tag         string    `json:"tag_name"` // v1.2.3
		URL         string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
		Body        string    `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("cannot decode release info: %w", err)
	}
	if out.Tag == "" {
		latest.updated = now
		latest.release = nil
		return nil, nil
	}
	vers, err := semver.Parse(strings.TrimPrefix(out.Tag, "v"))
	if err != nil {
		return nil, fmt.Errorf("cannot parse release version: %w", err)
	}
	latest.updated = now
	latest.release = &ReleaseInfo{
		Version: vers,
		Time:    out.PublishedAt,
		URL:     out.URL,
		Desc:    out.Body,
	}
	return latest.release, nil
}
