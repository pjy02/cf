package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.cloudflare.com/client/v4"

type Client struct {
	Token      string
	BaseURL    string
	HTTPClient *http.Client
}

type Record struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	Success    bool            `json:"success"`
	Errors     []apiError      `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

func New(token string) *Client {
	return &Client{Token: token, BaseURL: defaultBaseURL, HTTPClient: &http.Client{Timeout: 20 * time.Second}}
}

func (c *Client) VerifyToken(ctx context.Context) error {
	var result struct {
		Status string `json:"status"`
	}
	if err := c.request(ctx, http.MethodGet, "/user/tokens/verify", nil, &result, nil); err != nil {
		return fmt.Errorf("验证 API Token: %w", err)
	}
	if result.Status != "active" {
		return fmt.Errorf("API Token 状态不是 active: %s", result.Status)
	}
	return nil
}

func (c *Client) ResolveZone(ctx context.Context, zoneName string) (string, error) {
	var zones []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	path := "/zones?name=" + url.QueryEscape(strings.TrimSuffix(zoneName, ".")) + "&status=active&per_page=50"
	if err := c.request(ctx, http.MethodGet, path, nil, &zones, nil); err != nil {
		return "", fmt.Errorf("查询 Cloudflare Zone: %w", err)
	}
	if len(zones) == 0 {
		return "", fmt.Errorf("Cloudflare 中找不到活动域名 %s，请检查 Token 权限", zoneName)
	}
	for _, zone := range zones {
		if strings.EqualFold(zone.Name, strings.TrimSuffix(zoneName, ".")) {
			return zone.ID, nil
		}
	}
	return "", fmt.Errorf("Cloudflare 返回的 Zone 与 %s 不匹配", zoneName)
}

func (c *Client) ListARecords(ctx context.Context, zoneID, hostname string) ([]Record, error) {
	var records []Record
	path := fmt.Sprintf("/zones/%s/dns_records?type=A&name=%s&per_page=100", url.PathEscape(zoneID), url.QueryEscape(hostname))
	if err := c.request(ctx, http.MethodGet, path, nil, &records, nil); err != nil {
		return nil, fmt.Errorf("查询 %s 的 A 记录: %w", hostname, err)
	}
	filtered := records[:0]
	for _, record := range records {
		if record.Type == "A" && strings.EqualFold(strings.TrimSuffix(record.Name, "."), strings.TrimSuffix(hostname, ".")) {
			filtered = append(filtered, record)
		}
	}
	return filtered, nil
}

func (c *Client) CreateARecord(ctx context.Context, zoneID, hostname, ip string, ttl int) (Record, error) {
	body := map[string]any{
		"type": "A", "name": hostname, "content": ip, "ttl": ttl,
		"proxied": false, "comment": "managed by cf (github.com/pjy02/cf)",
	}
	var record Record
	path := fmt.Sprintf("/zones/%s/dns_records", url.PathEscape(zoneID))
	if err := c.request(ctx, http.MethodPost, path, body, &record, nil); err != nil {
		return Record{}, fmt.Errorf("创建 %s -> %s: %w", hostname, ip, err)
	}
	return record, nil
}

func (c *Client) UpdateARecord(ctx context.Context, zoneID, recordID, hostname, ip string, ttl int) (Record, error) {
	body := map[string]any{
		"type": "A", "name": hostname, "content": ip, "ttl": ttl,
		"proxied": false, "comment": "managed by cf (github.com/pjy02/cf)",
	}
	var record Record
	path := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(zoneID), url.PathEscape(recordID))
	if err := c.request(ctx, http.MethodPut, path, body, &record, nil); err != nil {
		return Record{}, fmt.Errorf("更新 %s -> %s: %w", hostname, ip, err)
	}
	return record, nil
}

func (c *Client) DeleteRecord(ctx context.Context, zoneID, recordID string) error {
	path := fmt.Sprintf("/zones/%s/dns_records/%s", url.PathEscape(zoneID), url.PathEscape(recordID))
	if err := c.request(ctx, http.MethodDelete, path, nil, nil, nil); err != nil {
		return fmt.Errorf("删除 DNS 记录 %s: %w", recordID, err)
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path string, body any, result any, rawEnvelope *envelope) error {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, requestBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 4<<20)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	var env envelope
	if err := json.Unmarshal(responseBody, &env); err != nil {
		return fmt.Errorf("Cloudflare 返回非 JSON 内容（HTTP %d）", resp.StatusCode)
	}
	if rawEnvelope != nil {
		*rawEnvelope = env
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !env.Success {
		messages := make([]string, 0, len(env.Errors))
		for _, item := range env.Errors {
			messages = append(messages, fmt.Sprintf("%d: %s", item.Code, item.Message))
		}
		if len(messages) == 0 {
			messages = append(messages, fmt.Sprintf("HTTP %d", resp.StatusCode))
		}
		return fmt.Errorf("Cloudflare API 错误: %s", strings.Join(messages, "; "))
	}
	if result != nil && len(env.Result) > 0 && string(env.Result) != "null" {
		if err := json.Unmarshal(env.Result, result); err != nil {
			return fmt.Errorf("解析 Cloudflare 结果: %w", err)
		}
	}
	return nil
}
