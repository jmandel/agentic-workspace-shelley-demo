package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
)

type buildInfo struct {
	Name       string `json:"name"`
	Module     string `json:"module,omitempty"`
	Version    string `json:"version,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Commit     string `json:"commit,omitempty"`
	CommitTime string `json:"commitTime,omitempty"`
	Modified   bool   `json:"modified,omitempty"`
}

type runtimeVersionInfo struct {
	Namespace    string     `json:"namespace"`
	Workspace    string     `json:"workspace"`
	Mode         string     `json:"mode,omitempty"`
	APIBase      string     `json:"apiBase,omitempty"`
	Version      *buildInfo `json:"version,omitempty"`
	VersionError string     `json:"versionError,omitempty"`
}

func managerBuildInfo() buildInfo {
	info := buildInfo{Name: "shelleymanager"}

	buildMeta, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}

	if buildMeta.Main.Path != "" {
		info.Module = buildMeta.Main.Path
	}
	if buildMeta.Main.Version != "" {
		info.Version = buildMeta.Main.Version
	}
	for _, setting := range buildMeta.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.Commit = setting.Value
		case "vcs.time":
			info.CommitTime = setting.Value
		case "vcs.modified":
			info.Modified = setting.Value == "true"
		}
	}

	return info
}

func fetchShelleyVersion(ctx context.Context, apiBase *url.URL) (*buildInfo, error) {
	if apiBase == nil {
		return nil, fmt.Errorf("missing api base")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase.String()+"/version", nil)
	if err != nil {
		return nil, err
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("version returned %d", res.StatusCode)
	}

	var payload struct {
		Version    string `json:"version,omitempty"`
		Tag        string `json:"tag,omitempty"`
		Commit     string `json:"commit,omitempty"`
		CommitTime string `json:"commit_time,omitempty"`
		Modified   bool   `json:"modified,omitempty"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}

	info := &buildInfo{
		Name:       "shelley",
		Version:    payload.Version,
		Tag:        payload.Tag,
		Commit:     payload.Commit,
		CommitTime: payload.CommitTime,
		Modified:   payload.Modified,
	}
	return info, nil
}
