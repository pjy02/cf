package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") == "" || r.Header.Get("User-Agent") == "" {
			t.Errorf("missing GitHub headers: %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, releaseJSON("v1.2.0"))
	}))
	defer server.Close()
	client := NewClient()
	client.APIURL = server.URL
	client.HTTPClient = server.Client()
	info, err := client.Check(context.Background(), "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !info.UpdateAvailable || info.LatestVersion != "1.2.0" || info.Development || info.Ahead {
		t.Fatalf("unexpected update info: %#v", info)
	}
	info, err = client.Check(context.Background(), "1.2.0")
	if err != nil || info.UpdateAvailable {
		t.Fatalf("latest version should not update: %#v %v", info, err)
	}
	info, err = client.Check(context.Background(), "dev")
	if err != nil || !info.Development || !info.UpdateAvailable {
		t.Fatalf("dev version handling failed: %#v %v", info, err)
	}
}

func TestCheckReportsRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Reset", "4102444800")
		http.Error(w, "limited", http.StatusForbidden)
	}))
	defer server.Close()
	client := NewClient()
	client.APIURL = server.URL
	client.HTTPClient = server.Client()
	_, err := client.Check(context.Background(), "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "请求受限") {
		t.Fatalf("unexpected rate limit error: %v", err)
	}
}

func TestFindAssetsUsesExactVersionAndArchitecture(t *testing.T) {
	release := Release{TagName: "v1.2.0", Assets: []Asset{
		{Name: "cf_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://github.com/pjy02/cf/releases/download/v1.2.0/cf_1.2.0_linux_amd64.tar.gz"},
		{Name: "cf_1.2.0_linux_arm64.tar.gz", BrowserDownloadURL: "https://github.com/pjy02/cf/releases/download/v1.2.0/cf_1.2.0_linux_arm64.tar.gz"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://github.com/pjy02/cf/releases/download/v1.2.0/checksums.txt"},
	}}
	archive, checksums, err := FindAssets(release, "1.2.0", "linux", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if archive.Name != "cf_1.2.0_linux_arm64.tar.gz" || checksums.Name != "checksums.txt" {
		t.Fatalf("wrong assets: %#v %#v", archive, checksums)
	}
	if _, _, err := FindAssets(release, "1.2.0", "linux", "386"); err == nil {
		t.Fatal("unsupported architecture was accepted")
	}
}

func TestFindAssetsRejectsMismatchedDownloadURL(t *testing.T) {
	release := Release{TagName: "v1.2.0", Assets: []Asset{
		{Name: "cf_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://github.com/other/repo/releases/download/v1.2.0/cf_1.2.0_linux_amd64.tar.gz"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://github.com/pjy02/cf/releases/download/v1.2.0/checksums.txt"},
	}}
	if _, _, err := FindAssets(release, "1.2.0", "linux", "amd64"); err == nil {
		t.Fatal("mismatched repository URL was accepted")
	}
}

func TestCleanReleaseNotesRemovesTerminalControls(t *testing.T) {
	cleaned := CleanReleaseNotes("正常\x1b[31m红色\x7f\u0085内容", 100)
	if strings.ContainsAny(cleaned, "\x1b\x7f\u0085") || !strings.Contains(cleaned, "正常") {
		t.Fatalf("unsafe release notes: %q", cleaned)
	}
}

func releaseJSON(tag string) string {
	return fmt.Sprintf(`{
  "tag_name": %q,
  "name": %q,
  "body": "更新内容",
  "html_url": "https://github.com/pjy02/cf/releases/tag/%s",
  "published_at": "2026-07-22T08:00:00Z",
  "draft": false,
  "prerelease": false,
  "assets": []
}`, tag, tag, tag)
}
