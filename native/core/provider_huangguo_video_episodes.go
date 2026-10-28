package core

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

type huangguoVideoEpisodeIdentity struct {
	number int
	path   string
}

type huangguoVideoDirectory struct {
	episodes map[string]huangguoVideoEpisodeIdentity
	expires  time.Time
}

func (d *Downloader) rememberHuangguoVideoDirectory(dramaID string, chapters []Chapter) {
	source, sourceID, valid := splitProviderDramaID(dramaID)
	if !valid || source != sourceHuangguoVideo || strings.HasPrefix(sourceID, "video/") {
		return
	}
	directory := huangguoVideoDirectory{episodes: map[string]huangguoVideoEpisodeIdentity{}, expires: time.Now().Add(2 * time.Minute)}
	for _, chapter := range chapters {
		address, valid := huangguoVideoAddress(chapter.PageURL)
		if !valid {
			return
		}
		directory.episodes[chapter.ID] = huangguoVideoEpisodeIdentity{number: chapterEpisodeNumber(chapter, 0), path: address.Path}
	}
	d.providerMu.Lock()
	defer d.providerMu.Unlock()
	if d.videoEpisodeDirectories == nil {
		d.videoEpisodeDirectories = map[string]huangguoVideoDirectory{}
	}
	for id, existing := range d.videoEpisodeDirectories {
		if !time.Now().Before(existing.expires) {
			delete(d.videoEpisodeDirectories, id)
		}
	}
	if len(d.videoEpisodeDirectories) >= 16 {
		oldest := ""
		for id, existing := range d.videoEpisodeDirectories {
			if oldest == "" || existing.expires.Before(d.videoEpisodeDirectories[oldest].expires) {
				oldest = id
			}
		}
		delete(d.videoEpisodeDirectories, oldest)
	}
	d.videoEpisodeDirectories[dramaID] = directory
}

func huangguoVideoAddress(address string) (*url.URL, bool) {
	parsed, err := url.Parse(resolveProviderURL(huangguoVideoBaseURL+"/", address))
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		!strings.EqualFold(parsed.Hostname(), "huangguo.video") {
		return nil, false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || (parts[0] != "series" && parts[0] != "video") || parts[1] == "" {
		return nil, false
	}
	parsed.Fragment = ""
	return parsed, true
}

func huangguoVideoChapterReference(sourceID string, chapter Chapter) bool {
	if chapter.PageURL == "" {
		return true
	}
	parsed, valid := huangguoVideoAddress(chapter.PageURL)
	if !valid {
		return false
	}
	path := strings.Trim(parsed.Path, "/")
	chapterPrefix := providerChapterID(sourceHuangguoVideo, sourceID, "")
	if !strings.HasPrefix(sourceID, "series/") && !strings.HasPrefix(sourceID, "video/") {
		sourceID = "series/" + sourceID
	}
	if strings.HasPrefix(sourceID, "video/") {
		return path == sourceID && chapterEpisodeNumber(chapter, 0) == 1
	}
	return strings.HasPrefix(path, "video/") &&
		strings.TrimPrefix(chapter.ID, chapterPrefix) == path
}

func huangguoVideoRecommendation(node *html.Node) bool {
	for current := node; current != nil; current = current.Parent {
		if current.Data == "aside" || current.Data == "nav" || current.Data == "footer" {
			return true
		}
		for _, token := range strings.Fields(strings.ToLower(providerHTMLAttr(current, "class") + " " + providerHTMLAttr(current, "id"))) {
			if strings.Contains(token, "recommend") || strings.Contains(token, "related") || token == "sidebar" {
				return true
			}
		}
		if current.Data == "body" || current.Data == "html" || current.Data == "main" {
			break
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			if child.Data == "h2" || child.Data == "h3" || child.Data == "h4" {
				text := providerHTMLText(child)
				if strings.Contains(text, "推荐") || strings.Contains(text, "猜你喜欢") || strings.Contains(text, "相关视频") {
					return true
				}
			}
		}
	}
	return false
}

func huangguoVideoSingleMedia(rawHTML, pageURL string) string {
	document, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return ""
	}
	addresses := map[string]bool{}
	for _, node := range providerHTMLNodes(document, func(node *html.Node) bool {
		return providerHTMLAttr(node, "data-hls") != ""
	}) {
		if huangguoVideoRecommendation(node) {
			continue
		}
		if address := normalizeProviderMediaURL(pageURL, providerHTMLAttr(node, "data-hls")); address != "" {
			addresses[address] = true
		}
	}
	if len(addresses) == 1 {
		for address := range addresses {
			return address
		}
	}
	return ""
}

func huangguoVideoEpisodeBlocks(rawHTML, pageURL string) []string {
	document, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return nil
	}
	page, valid := huangguoVideoAddress(pageURL)
	if !valid || !strings.HasPrefix(page.Path, "/series/") {
		return nil
	}
	seriesID := strings.TrimPrefix(strings.Trim(page.Path, "/"), "series/")
	var roots []*html.Node
	var headingRoots []*html.Node
	addRoot := func(target *[]*html.Node, node *html.Node) {
		if node != nil && node.Data != "body" && node.Data != "html" && node.Data != "main" && !huangguoVideoRecommendation(node) {
			*target = append(*target, node)
		}
	}
	for _, node := range providerHTMLNodes(document, func(node *html.Node) bool { return node.Type == html.ElementNode }) {
		owner := providerHTMLAttr(node, "data-series-id")
		if owner != "" && strings.TrimPrefix(owner, "series/") != seriesID {
			continue
		}
		for _, token := range strings.Fields(strings.ToLower(providerHTMLAttr(node, "class") + " " + providerHTMLAttr(node, "id"))) {
			switch token {
			case "series-episodes", "series-videos", "episode-list", "episode-grid", "episodes-list", "episodes", "chapter-list", "playlist":
				addRoot(&roots, node)
			}
		}
		if node.Data != "h2" && node.Data != "h3" && node.Data != "h4" {
			continue
		}
		heading := strings.TrimSpace(providerHTMLText(node))
		switch heading {
		case "剧集列表", "分集列表", "全部剧集", "选集", "剧集", "Episodes":
			if headingID := providerHTMLAttr(node, "id"); headingID != "" {
				for parent := node.Parent; parent != nil; parent = parent.Parent {
					if parent.Data == "body" || parent.Data == "html" || parent.Data == "main" {
						break
					}
					for _, label := range strings.Fields(providerHTMLAttr(parent, "aria-labelledby")) {
						if label == headingID {
							addRoot(&roots, parent)
						}
					}
				}
			}
			for parent := node.Parent; parent != nil; parent = parent.Parent {
				if parent.Data == "body" || parent.Data == "html" || parent.Data == "main" {
					break
				}
				if parent.Data == "section" {
					addRoot(&headingRoots, parent)
					break
				}
			}
			addRoot(&headingRoots, node.Parent)
			for sibling := node.NextSibling; sibling != nil; sibling = sibling.NextSibling {
				if sibling.Type == html.ElementNode {
					addRoot(&headingRoots, sibling)
					break
				}
			}
		}
	}
	selectRoots := func(candidates []*html.Node) []*html.Node {
		unique := map[*html.Node]bool{}
		for _, node := range candidates {
			for _, anchor := range providerHTMLNodes(node, func(node *html.Node) bool { return node.Data == "a" }) {
				address, valid := huangguoVideoAddress(resolveProviderURL(pageURL, providerHTMLAttr(anchor, "href")))
				if valid && strings.HasPrefix(address.Path, "/video/") && !huangguoVideoRecommendation(anchor) {
					unique[node] = true
					break
				}
			}
		}
		var selected []*html.Node
		processed := map[*html.Node]bool{}
		for _, node := range candidates {
			if !unique[node] || processed[node] {
				continue
			}
			processed[node] = true
			containsRoot := false
			for other := range unique {
				for parent := other.Parent; parent != nil; parent = parent.Parent {
					if parent == node {
						containsRoot = true
						break
					}
				}
			}
			if !containsRoot {
				selected = append(selected, node)
			}
		}
		return selected
	}
	roots = selectRoots(roots)
	if len(roots) == 0 {
		roots = selectRoots(headingRoots)
	}
	if len(roots) > 1 {
		for _, root := range roots {
			confirmed := false
			for node := root; node != nil; node = node.Parent {
				if strings.TrimPrefix(providerHTMLAttr(node, "data-series-id"), "series/") == seriesID {
					confirmed = true
					break
				}
			}
			if !confirmed {
				return nil
			}
		}
	}
	seen := map[*html.Node]bool{}
	var blocks []string
	for _, root := range roots {
		for _, anchor := range providerHTMLNodes(root, func(node *html.Node) bool { return node.Data == "a" }) {
			if seen[anchor] || huangguoVideoRecommendation(anchor) {
				continue
			}
			belongs := true
			for node := anchor; node != nil; node = node.Parent {
				if owner := providerHTMLAttr(node, "data-series-id"); owner != "" && strings.TrimPrefix(owner, "series/") != seriesID {
					belongs = false
					break
				}
			}
			address, valid := huangguoVideoAddress(resolveProviderURL(pageURL, providerHTMLAttr(anchor, "href")))
			if !belongs || !valid || !strings.HasPrefix(address.Path, "/video/") {
				continue
			}
			seen[anchor] = true
			var block bytes.Buffer
			if html.Render(&block, anchor) == nil {
				blocks = append(blocks, block.String())
			}
		}
	}
	return blocks
}

func (d *Downloader) validateHuangguoVideoMembership(ctx context.Context, task Task) error {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	if !valid || source != sourceHuangguoVideo || strings.HasPrefix(sourceID, "video/") {
		return nil
	}
	d.providerMu.Lock()
	directory, found := d.videoEpisodeDirectories[task.DramaID]
	d.providerMu.Unlock()
	if !found || !time.Now().Before(directory.expires) {
		_, chapters, err := d.GetHuangguoChapters(ctx, source, sourceID)
		if err != nil {
			return err
		}
		if len(chapters) == 0 {
			return errors.New("当前作品没有可确认归属的分集，请刷新详情")
		}
		d.providerMu.Lock()
		directory = d.videoEpisodeDirectories[task.DramaID]
		d.providerMu.Unlock()
	}
	if identity, found := directory.episodes[task.Chapter.ID]; found && identity.number == chapterEpisodeNumber(task.Chapter, 0) {
		if task.Chapter.PageURL == "" {
			return nil
		}
		address, valid := huangguoVideoAddress(task.Chapter.PageURL)
		if valid && address.Path == identity.path {
			return nil
		}
	}
	return errors.New("该分集已不在当前作品的选集目录中，请重新打开详情；旧下载任务请重新添加")
}
