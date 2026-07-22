package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultAPIURL = "https://api.github.com/repos/pjy02/cf/releases/latest"
	maxAPIBytes   = 2 << 20
)

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type Release struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	Assets      []Asset   `json:"assets"`
}

type Info struct {
	CurrentVersion  string
	LatestVersion   string
	Development     bool
	UpdateAvailable bool
	Ahead           bool
	Release         Release
}

type Client struct {
	HTTPClient *http.Client
	APIURL     string
	UserAgent  string
}

func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 2 * time.Minute},
		APIURL:     DefaultAPIURL,
		UserAgent:  "cf-update/1.0 (+https://github.com/pjy02/cf)",
	}
}

func (c *Client) Check(ctx context.Context, current string) (Info, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL, nil)
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return Info{}, fmt.Errorf("连接 GitHub 检查更新: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		if reset, parseErr := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); parseErr == nil && reset > 0 {
			return Info{}, fmt.Errorf("GitHub API 请求受限，请在 %s 后重试", time.Unix(reset, 0).Local().Format("2006-01-02 15:04:05"))
		}
		return Info{}, fmt.Errorf("GitHub API 请求受限（HTTP %d），请稍后重试", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("GitHub 最新版本接口返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIBytes+1))
	if err != nil {
		return Info{}, err
	}
	if len(body) > maxAPIBytes {
		return Info{}, fmt.Errorf("GitHub Release 响应超过大小限制")
	}
	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return Info{}, fmt.Errorf("解析 GitHub Release: %w", err)
	}
	if release.Draft || release.Prerelease {
		return Info{}, fmt.Errorf("GitHub 最新版本不是正式 Release")
	}
	latest, err := ParseVersion(release.TagName)
	if err != nil {
		return Info{}, fmt.Errorf("Release 标签无效: %w", err)
	}
	info := Info{CurrentVersion: current, LatestVersion: latest.String(), Release: release}
	if strings.EqualFold(strings.TrimSpace(current), "dev") {
		info.Development = true
		info.UpdateAvailable = true
		return info, nil
	}
	currentVersion, err := ParseVersion(current)
	if err != nil {
		return Info{}, fmt.Errorf("当前程序版本无效: %w", err)
	}
	switch Compare(currentVersion, latest) {
	case -1:
		info.UpdateAvailable = true
	case 1:
		info.Ahead = true
	}
	return info, nil
}

func ExpectedAssetName(version, goos, goarch string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("自动更新仅支持 Linux")
	}
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("自动更新不支持架构 %s", goarch)
	}
	parsed, err := ParseVersion(version)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("cf_%s_linux_%s.tar.gz", parsed.String(), goarch), nil
}

func FindAssets(release Release, version, goos, goarch string) (Asset, Asset, error) {
	expected, err := ExpectedAssetName(version, goos, goarch)
	if err != nil {
		return Asset{}, Asset{}, err
	}
	var archive, checksums Asset
	for _, asset := range release.Assets {
		switch asset.Name {
		case expected:
			archive = asset
		case "checksums.txt":
			checksums = asset
		}
	}
	if archive.Name == "" {
		return Asset{}, Asset{}, fmt.Errorf("Release 中缺少 %s", expected)
	}
	if checksums.Name == "" {
		return Asset{}, Asset{}, fmt.Errorf("Release 中缺少 checksums.txt")
	}
	if err := validateReleaseAssetURL(archive.BrowserDownloadURL, release.TagName, archive.Name); err != nil {
		return Asset{}, Asset{}, err
	}
	if err := validateReleaseAssetURL(checksums.BrowserDownloadURL, release.TagName, checksums.Name); err != nil {
		return Asset{}, Asset{}, err
	}
	return archive, checksums, nil
}

func CleanReleaseNotes(value string, limit int) string {
	if limit <= 0 {
		limit = 2000
	}
	var b strings.Builder
	count := 0
	for _, r := range value {
		if count >= limit {
			b.WriteString("\n…（更新说明已截断）")
			break
		}
		if r == '\n' || r == '\t' || (r >= 32 && (r < 127 || r >= 160)) {
			b.WriteRune(r)
			count++
		}
	}
	cleaned := strings.TrimSpace(b.String())
	if cleaned == "" {
		return "（无更新说明）"
	}
	return cleaned
}
