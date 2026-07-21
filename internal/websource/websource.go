package websource

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pjy02/cf/internal/config"
	"github.com/pjy02/cf/internal/model"
	"golang.org/x/net/html"
)

const maxHTMLSize = 2 << 20

var numberRE = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

type Client struct {
	HTTPClient *http.Client
	URL        string
	UserAgent  string
}

func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
		URL:        config.SourceURL,
		UserAgent:  "cfsync/1.0 (+https://github.com/pjy02/cf)",
	}
}

func (c *Client) Fetch(ctx context.Context) (model.SourceData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return model.SourceData{}, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return model.SourceData{}, fmt.Errorf("读取优选 IP 网页失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return model.SourceData{}, fmt.Errorf("优选 IP 网页返回 HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.Contains(strings.ToLower(ct), "text/html") {
		return model.SourceData{}, fmt.Errorf("优选 IP 网页返回了非 HTML 内容: %s", ct)
	}
	limited := io.LimitReader(resp.Body, maxHTMLSize+1)
	b, err := io.ReadAll(limited)
	if err != nil {
		return model.SourceData{}, err
	}
	if len(b) > maxHTMLSize {
		return model.SourceData{}, fmt.Errorf("优选 IP 网页超过 %d 字节限制", maxHTMLSize)
	}
	data, err := Parse(strings.NewReader(string(b)))
	if err != nil {
		return model.SourceData{}, err
	}
	data.FetchedAt = time.Now()
	return data, nil
}

func Parse(r io.Reader) (model.SourceData, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return model.SourceData{}, fmt.Errorf("解析优选 IP HTML: %w", err)
	}
	title := strings.TrimSpace(textContent(findTag(doc, "title")))
	if !strings.Contains(title, "优选") {
		return model.SourceData{}, fmt.Errorf("网页标题异常，可能返回了验证页: %q", title)
	}
	data := model.SourceData{Carriers: make(map[string]model.Snapshot)}
	foundPanels := 0
	for _, carrier := range []string{model.CarrierCT, model.CarrierCM, model.CarrierCU} {
		panel := findByID(doc, carrier)
		if panel == nil {
			data.Warnings = append(data.Warnings, fmt.Sprintf("未找到 %s(%s) 面板", model.CarrierNames[carrier], carrier))
			continue
		}
		foundPanels++
		snapshot := model.Snapshot{Carrier: carrier, Found: true}
		if syncInfo := findClass(panel, "sync-info"); syncInfo != nil {
			snapshot.SourceTime = strings.TrimSpace(textContent(findTag(syncInfo, "span")))
		}
		cards := findAllClass(panel, "card")
		malformed := 0
		for _, card := range cards {
			node, parseErr := parseCard(card, carrier, snapshot.SourceTime)
			if parseErr != nil {
				malformed++
				data.Warnings = append(data.Warnings, fmt.Sprintf("%s跳过异常卡片: %v", model.CarrierNames[carrier], parseErr))
				continue
			}
			snapshot.Nodes = append(snapshot.Nodes, node)
		}
		if len(cards) > 0 && len(snapshot.Nodes) == 0 {
			return model.SourceData{}, fmt.Errorf("%s面板有 %d 张卡片但全部解析失败，拒绝更新 DNS", model.CarrierNames[carrier], malformed)
		}
		data.Carriers[carrier] = snapshot
	}
	if foundPanels == 0 {
		return model.SourceData{}, fmt.Errorf("网页中未找到 ct/cm/cu 运营商面板，拒绝更新 DNS")
	}
	return data, nil
}

func parseCard(card *html.Node, carrier, sourceTime string) (model.Node, error) {
	ip := strings.TrimSpace(textContent(findClass(card, "ip-addr")))
	if ip == "" {
		return model.Node{}, fmt.Errorf("缺少 IP")
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil || !addr.Is4() {
		return model.Node{}, fmt.Errorf("无效 IPv4 地址 %q", ip)
	}
	ip = addr.String()
	region := strings.TrimSpace(textContent(findClass(card, "region-tag")))
	var speed, latency float64
	var haveSpeed bool
	for _, box := range findAllClass(card, "stat-box") {
		label := strings.TrimSpace(textContent(findClass(box, "stat-label")))
		value := strings.TrimSpace(textContent(findClass(box, "stat-value")))
		number := numberRE.FindString(value)
		if number == "" {
			continue
		}
		parsed, err := strconv.ParseFloat(number, 64)
		if err != nil {
			continue
		}
		switch {
		case strings.Contains(label, "下载速度"):
			speed, haveSpeed = parsed, true
		case strings.Contains(label, "网络延迟"):
			latency = parsed
		}
	}
	if !haveSpeed || speed <= 0 {
		return model.Node{}, fmt.Errorf("IP %s 缺少有效下载速度", ip)
	}
	return model.Node{IP: ip, Speed: speed, Latency: latency, Region: region, Carrier: carrier, SourceTime: sourceTime}, nil
}

func hasClass(n *html.Node, class string) bool {
	if n == nil {
		return false
	}
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			for _, value := range strings.Fields(attr.Val) {
				if value == class {
					return true
				}
			}
		}
	}
	return false
}

func findByID(n *html.Node, id string) *html.Node {
	return findNode(n, func(candidate *html.Node) bool {
		for _, attr := range candidate.Attr {
			if attr.Key == "id" && attr.Val == id {
				return true
			}
		}
		return false
	})
}

func findTag(n *html.Node, tag string) *html.Node {
	return findNode(n, func(candidate *html.Node) bool {
		return candidate.Type == html.ElementNode && candidate.Data == tag
	})
}

func findClass(n *html.Node, class string) *html.Node {
	return findNode(n, func(candidate *html.Node) bool { return hasClass(candidate, class) })
}

func findNode(n *html.Node, match func(*html.Node) bool) *html.Node {
	if n == nil {
		return nil
	}
	if match(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func findAllClass(n *html.Node, class string) []*html.Node {
	var result []*html.Node
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if hasClass(current, class) {
			result = append(result, current)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return result
}

func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(textContent(child))
	}
	return b.String()
}
