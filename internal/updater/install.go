package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/pjy02/cf/internal/lock"
)

const (
	installedPath   = "/usr/local/bin/cf"
	updateLockPath  = "/var/lib/cf-ip-sync/update.lock"
	maxDownloadSize = 128 << 20
	maxBinarySize   = 64 << 20
	maxExpandedSize = 256 << 20
)

type CleanupWarning struct {
	Path string
	Err  error
}

func (w *CleanupWarning) Error() string {
	return fmt.Sprintf("更新已成功，但删除备份 %s 失败: %v", w.Path, w.Err)
}

func (c *Client) Install(ctx context.Context, info Info, executable string) error {
	if runtime.GOOS != "linux" {
		return fmt.Errorf("自动更新仅支持 Linux")
	}
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("解析当前程序路径: %w", err)
	}
	if filepath.Clean(resolved) != installedPath {
		return fmt.Errorf("当前程序不在 %s，请使用一键安装命令更新", installedPath)
	}
	if !info.UpdateAvailable {
		return fmt.Errorf("当前没有可安装的新版本")
	}
	archive, checksums, err := FindAssets(info.Release, info.LatestVersion, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(updateLockPath), 0o700); err != nil {
		return err
	}
	updateLock, err := lock.Acquire(updateLockPath)
	if err != nil {
		return err
	}
	defer updateLock.Release()

	tempDir, err := os.MkdirTemp(filepath.Dir(resolved), ".cf-update-")
	if err != nil {
		return fmt.Errorf("创建更新临时目录: %w", err)
	}
	defer os.RemoveAll(tempDir)
	archivePath := filepath.Join(tempDir, archive.Name)
	checksumsPath := filepath.Join(tempDir, "checksums.txt")
	if err := c.download(ctx, archive.BrowserDownloadURL, archivePath); err != nil {
		return fmt.Errorf("下载更新包: %w", err)
	}
	if err := c.download(ctx, checksums.BrowserDownloadURL, checksumsPath); err != nil {
		return fmt.Errorf("下载校验文件: %w", err)
	}
	digest, err := verifyChecksum(archivePath, checksumsPath, archive.Name)
	if err != nil {
		return err
	}
	if archive.Digest != "" && !strings.EqualFold(archive.Digest, "sha256:"+digest) {
		return fmt.Errorf("更新包摘要与 GitHub Release 元数据不一致")
	}
	candidate := filepath.Join(tempDir, "cf.new")
	if err := extractBinary(archivePath, candidate); err != nil {
		return err
	}
	if err := validateBinary(ctx, candidate, info.LatestVersion); err != nil {
		return err
	}
	return replaceBinary(ctx, resolved, candidate, info.LatestVersion)
}

func (c *Client) download(ctx context.Context, rawURL, destination string) error {
	if err := validateDownloadURL(rawURL); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载地址返回 HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxDownloadSize {
		return fmt.Errorf("下载文件超过 %d 字节限制", maxDownloadSize)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(resp.Body, maxDownloadSize+1))
	syncErr := file.Sync()
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if written > maxDownloadSize {
		return fmt.Errorf("下载文件超过 %d 字节限制", maxDownloadSize)
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func validateDownloadURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return fmt.Errorf("拒绝非 GitHub HTTPS 下载地址")
	}
	if !strings.HasPrefix(path.Clean(parsed.Path), "/pjy02/cf/releases/download/") {
		return fmt.Errorf("拒绝仓库范围外的下载地址")
	}
	return nil
}

func validateReleaseAssetURL(rawURL, tag, name string) error {
	if err := validateDownloadURL(rawURL); err != nil {
		return err
	}
	parsed, _ := url.Parse(rawURL)
	expected := path.Join("/pjy02/cf/releases/download", tag, name)
	if path.Clean(parsed.Path) != expected {
		return fmt.Errorf("Release 资产下载地址与版本或文件名不匹配")
	}
	return nil
}

func verifyChecksum(archivePath, checksumsPath, assetName string) (string, error) {
	b, err := os.ReadFile(checksumsPath)
	if err != nil {
		return "", err
	}
	var expected string
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if len(expected) != sha256.Size*2 {
		return "", fmt.Errorf("checksums.txt 中缺少 %s 的有效 SHA256", assetName)
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return "", fmt.Errorf("checksums.txt 中的 SHA256 无效")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expected) {
		return "", fmt.Errorf("更新包 SHA256 校验失败")
	}
	return actual, nil
}

func extractBinary(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("打开更新压缩包: %w", err)
	}
	defer gz.Close()
	tarReader := tar.NewReader(io.LimitReader(gz, maxExpandedSize+1))
	found := false
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取更新压缩包: %w", err)
		}
		cleanName := path.Clean(header.Name)
		if path.IsAbs(header.Name) || cleanName == ".." || strings.HasPrefix(cleanName, "../") {
			return fmt.Errorf("更新压缩包包含不安全路径 %q", header.Name)
		}
		if cleanName != "cf" {
			continue
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxBinarySize {
			return fmt.Errorf("更新压缩包中的 cf 文件无效")
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		written, copyErr := io.CopyN(out, tarReader, header.Size)
		syncErr := out.Sync()
		closeErr := out.Close()
		if copyErr != nil || written != header.Size {
			return fmt.Errorf("提取 cf 文件失败: %w", copyErr)
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		found = true
	}
	if !found {
		return fmt.Errorf("更新压缩包中缺少 cf")
	}
	return nil
}

func validateBinary(ctx context.Context, binary, expectedVersion string) error {
	cmd := exec.CommandContext(ctx, binary, "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("运行新版本失败: %w: %s", err, strings.TrimSpace(string(output)))
	}
	text := string(output)
	if !strings.Contains(text, "github.com/pjy02/cf") || !strings.HasPrefix(text, "cf "+expectedVersion+" ") {
		return fmt.Errorf("新程序版本校验失败: %s", strings.TrimSpace(text))
	}
	return nil
}

func replaceBinary(ctx context.Context, executable, candidate, expectedVersion string) error {
	backup := executable + ".bak"
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("检测到上次更新备份 %s，请确认后再更新", backup)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := copyFile(executable, backup, 0o755); err != nil {
		return fmt.Errorf("备份当前程序: %w", err)
	}
	rollback := func(cause error) error {
		if restoreErr := os.Rename(backup, executable); restoreErr != nil {
			return fmt.Errorf("%v；自动回滚失败: %v（备份位于 %s）", cause, restoreErr, backup)
		}
		return fmt.Errorf("%v；已自动恢复旧版本", cause)
	}
	if err := os.Chmod(candidate, 0o755); err != nil {
		_ = os.Remove(backup)
		return err
	}
	if err := os.Rename(candidate, executable); err != nil {
		_ = os.Remove(backup)
		return fmt.Errorf("替换程序: %w", err)
	}
	if err := validateBinary(ctx, executable, expectedVersion); err != nil {
		return rollback(err)
	}
	if err := os.Remove(backup); err != nil {
		return &CleanupWarning{Path: backup, Err: err}
	}
	return nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(destination)
		}
	}()
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	success = true
	return nil
}
