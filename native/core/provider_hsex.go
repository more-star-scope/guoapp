package core

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	hsexSiteBaseURL = "https://hsex.men"
	hsexUserAgent   = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
	hsexPageSize    = 24
	hsexSearchSize  = 20
)

var hsexCategories = []nativeCategory{{ID: "new", Name: "最新"}}

var (
	reHsexVideoAnchor = regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']*video-(\d+)\.htm(?:\?[^"']*)?)["'][^>]*>.*?</a>`)
	reHsexVideoPath   = regexp.MustCompile(`^/video-(\d+)\.htm$`)
	reHsexDuration    = regexp.MustCompile(`\b\d{1,3}:\d{2}(?::\d{2})?\b`)
	reHsexPanelTitle  = regexp.MustCompile(`(?is)<[^>]+class=["'][^"']*\bpanel-title\b[^"']*["'][^>]*>(.*?)</[^>]+>`)
	reHsexTag         = regexp.MustCompile(`(?is)<[^>]+>`)
	reHsexSourceTag   = regexp.MustCompile(`(?is)<[^>]+\bid=["']video-source["'][^>]*>`)
	reHsexVideoTag    = regexp.MustCompile(`(?is)<video\b[^>]*>`)
	reHsexPageLink    = regexp.MustCompile(`(?is)href=["'][^"']*(?:list|search)-(\d+)\.htm`)
)

func validHsexCategory(category string) bool {
	return category == "" || category == "new"
}

func hsexNumericID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 16 {
		return false
	}
	number, err := strconv.ParseUint(value, 10, 64)
	return err == nil && number > 0
}

func hsexClean(value string) string {
	value = reHsexTag.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\u3000", " ")
	return strings.Join(strings.Fields(value), " ")
}

func hsexFixURL(pageURL, raw string) string {
	address := resolveProviderURL(pageURL, raw)
	parsed, err := url.Parse(address)
	if err != nil || !validNativeCoverURL(parsed) {
		return ""
	}
	return parsed.String()
}

func (d *Downloader) hsexGet(ctx context.Context, path string) (string, error) {
	site := d.providerBaseURL(sourceHsex)
	address := path
	if !strings.HasPrefix(address, "http://") && !strings.HasPrefix(address, "https://") {
		address = site + "/" + strings.TrimPrefix(address, "/")
	}
	pageContext := context.WithValue(ctx, providerTextUserAgentKey{}, hsexUserAgent)
	return d.fetchProviderText(pageContext, address, site+"/")
}

func (d *Downloader) fetchHsexCategories() []nativeCategory {
	return append([]nativeCategory(nil), hsexCategories...)
}

func parseHsexCards(body, pageURL string) []Drama {
	type parsedCard struct {
		drama Drama
		order int
	}
	cards := map[string]parsedCard{}
	order := 0
	for _, match := range reHsexVideoAnchor.FindAllStringSubmatch(body, -1) {
		id := match[2]
		if !hsexNumericID(id) {
			continue
		}
		block := match[0]
		entry, found := cards[id]
		if !found {
			entry.order = order
			order++
			entry.drama = Drama{
				ID: providerDramaID(sourceHsex, id), Source: sourceHsex, SourceID: id,
				ChannelName: "好色 TV", CategoryName: "成人视频",
			}
		}
		title := hsexClean(extractAttr(block, "title", "aria-label", "alt"))
		if title == "" {
			title = strings.TrimSpace(reHsexDuration.ReplaceAllString(hsexClean(block), ""))
			title = strings.TrimSpace(strings.TrimSuffix(title, "HD"))
		}
		if title != "" && len([]rune(title)) <= 512 {
			entry.drama.Title, entry.drama.Name = title, title
		}
		if cover := hsexFixURL(pageURL, extractAttr(block, "data-original", "data-src", "src")); cover != "" {
			entry.drama.Cover, entry.drama.CoverURL = cover, cover
		}
		if duration := reHsexDuration.FindString(hsexClean(block)); duration != "" {
			entry.drama.Remark = duration
		}
		cards[id] = entry
	}
	ordered := make([]parsedCard, 0, len(cards))
	for _, entry := range cards {
		if strings.TrimSpace(entry.drama.Title) != "" {
			ordered = append(ordered, entry)
		}
	}
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].order < ordered[j-1].order; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	items := make([]Drama, 0, len(ordered))
	for _, entry := range ordered {
		items = append(items, entry.drama)
	}
	return items
}

func hsexHasMore(body string, page, count, pageSize int) bool {
	for _, match := range reHsexPageLink.FindAllStringSubmatch(body, -1) {
		value, err := strconv.Atoi(match[1])
		if err == nil && value == page+1 {
			return true
		}
	}
	return count >= pageSize
}

func (d *Downloader) fetchHsexCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 {
		return nil, false, errors.New("好色 TV 目录页码无效")
	}
	if !validHsexCategory(category) {
		return nil, false, errors.New("好色 TV 内容分类无效")
	}
	path := fmt.Sprintf("/list-%d.htm", page)
	pageSize := hsexPageSize
	if query != "" {
		query = strings.TrimSpace(query)
		if query == "" || len([]rune(query)) > 128 {
			return nil, false, errors.New("好色 TV 搜索关键词无效")
		}
		path = "/search.htm"
		if page > 1 {
			path = fmt.Sprintf("/search-%d.htm", page)
		}
		path += "?" + url.Values{"search": {query}, "sort": {"new"}}.Encode()
		pageSize = hsexSearchSize
	}
	body, err := d.hsexGet(ctx, path)
	if err != nil {
		return nil, false, err
	}
	pageURL := d.providerBaseURL(sourceHsex) + strings.Split(path, "?")[0]
	items := parseHsexCards(body, pageURL)
	return items, hsexHasMore(body, page, len(items), pageSize), nil
}

func hsexPageTitle(body string) string {
	if match := reHsexPanelTitle.FindStringSubmatch(body); len(match) > 1 {
		if title := hsexClean(match[1]); title != "" {
			return title
		}
	}
	title := extractPageTitle(body)
	for _, suffix := range []string{" - 好色™ Tv", " - 好色™ TV", " - 好色 Tv", " - 好色 TV"} {
		title = strings.TrimSuffix(title, suffix)
	}
	return strings.TrimSpace(title)
}

func hsexCover(body, pageURL string) string {
	for _, tag := range coverMetaTag.FindAllString(body, -1) {
		property := strings.ToLower(extractAttr(tag, "property", "name"))
		if property == "og:image" || property == "twitter:image" {
			if address := hsexFixURL(pageURL, extractAttr(tag, "content")); address != "" {
				return address
			}
		}
	}
	for _, tag := range reHsexVideoTag.FindAllString(body, -1) {
		if address := hsexFixURL(pageURL, extractAttr(tag, "poster")); address != "" {
			return address
		}
	}
	return ""
}

func hsexLabelValue(body, label string) string {
	pattern := regexp.MustCompile(`(?is)` + regexp.QuoteMeta(label) + `\s*[:：]\s*(.{0,320})`)
	match := pattern.FindStringSubmatch(body)
	if len(match) < 2 {
		return ""
	}
	segment := match[1]
	if end := strings.Index(segment, "</div>"); end >= 0 {
		segment = segment[:end]
	} else if end := strings.Index(segment, "<br"); end >= 0 {
		segment = segment[:end]
	}
	return truncate(hsexClean(segment), 128)
}

func (d *Downloader) hsexVideoPageURL(sourceID string) string {
	return d.providerBaseURL(sourceHsex) + "/video-" + sourceID + ".htm"
}

func (d *Downloader) fetchHsexDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !hsexNumericID(sourceID) {
		return Drama{}, nil, errors.New("好色 TV 视频 ID 无效")
	}
	pageURL := d.hsexVideoPageURL(sourceID)
	body, err := d.hsexGet(ctx, pageURL)
	if err != nil {
		return Drama{}, nil, err
	}
	title := hsexPageTitle(body)
	if title == "" {
		return Drama{}, nil, errors.New("好色 TV 详情缺少视频名称")
	}
	author := hsexLabelValue(body, "作者")
	duration := hsexLabelValue(body, "时长")
	views := hsexLabelValue(body, "观看")
	date := hsexLabelValue(body, "日期")
	tags := make([]string, 0, 3)
	for _, value := range []string{author, duration, date} {
		if value != "" {
			tags = append(tags, value)
		}
	}
	cover := hsexCover(body, pageURL)
	drama := Drama{
		ID: providerDramaID(sourceHsex, sourceID), Source: sourceHsex, SourceID: sourceID,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Cover: cover, CoverURL: cover, ChannelName: "好色 TV", CategoryName: "成人视频",
		Remark: truncate(duration, 64), Views: truncate(views, 32), OnlineDate: truncate(date, 32), Tags: tags,
		TotalEpisode: 1, EpisodeCount: 1,
	}
	chapter := Chapter{
		ID: providerChapterID(sourceHsex, sourceID, sourceID), Source: sourceHsex,
		Title: "完整视频", CurrentEpisode: rawEpisode(1), VideoURL: "hsex-play://" + sourceID,
		PageURL: pageURL, Referer: pageURL,
	}
	return drama, []Chapter{chapter}, nil
}

func hsexMediaAddress(body, pageURL string) string {
	for _, tag := range reHsexSourceTag.FindAllString(body, -1) {
		address := normalizeProviderMediaURL(pageURL, extractAttr(tag, "src", "data-src"))
		if address != "" {
			return address
		}
	}
	return ""
}

func (d *Downloader) resolveHsexMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceHsex, sourceID, "")
	if !valid || source != sourceHsex || !hsexNumericID(sourceID) || task.Chapter.ID != prefix+sourceID {
		return providerMedia{}, errors.New("好色 TV 播放分集信息无效，请刷新详情")
	}
	pageURL := d.hsexVideoPageURL(sourceID)
	responses := &playbackResponseURLs{}
	pageContext := context.WithValue(ctx, playbackResponseURLsKey{}, responses)
	body, err := d.hsexGet(pageContext, pageURL)
	if err != nil {
		return providerMedia{}, err
	}
	if actual, ok := responses.values.Load(pageURL); ok {
		pageURL = actual.(string)
	}
	address := hsexMediaAddress(body, pageURL)
	if address == "" {
		return providerMedia{}, errors.New("好色 TV 当前视频暂无可用播放地址")
	}
	parsed, err := url.Parse(address)
	if err != nil || !isProviderHTTPMediaURL(address) {
		return providerMedia{}, errors.New("好色 TV 播放地址无效")
	}
	media := providerMedia{URL: address, Referer: pageURL}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return d.prepareWebProviderMedia(ctx, media, "好色 TV")
	}
	return media, nil
}
