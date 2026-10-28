package core

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var macCMSEpisodePath = regexp.MustCompile(`^/play/([0-9]+)-([0-9]+)-([0-9]+)\.html$`)

func providerPageIdentity(address string, expectedSources ...string) (string, string) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", ""
	}
	source := providerSourceForURL(address)
	if source == "" && len(expectedSources) > 0 {
		source = expectedSources[0]
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	switch source {
	case sourceHuangguoAI:
		if len(parts) == 2 && parts[0] == "detail" {
			return source, "detail:" + parts[1]
		}
		if len(parts) >= 2 && parts[0] == "video" {
			_, number, valid := aiEpisodeReference(address, address, parts[1])
			if valid {
				return source, "video:" + parts[1] + ":" + strconv.Itoa(number)
			}
		}
	case sourceHuangguoVideo:
		if len(parts) == 2 && (parts[0] == "video" || parts[0] == "series") {
			return source, parts[0] + ":" + parts[1]
		}
	case sourceHanxiaoquan, sourceGuipian:
		if match := macCMSEpisodePath.FindStringSubmatch(parsed.Path); len(match) == 4 {
			return source, "play:" + strings.Join(match[1:], ":")
		}
		if len(parts) == 2 && (parts[0] == "hanxiaoquan" || parts[0] == "nv") && strings.HasSuffix(parts[1], ".html") {
			return source, "detail:" + strings.TrimSuffix(parts[1], ".html")
		}
	case sourceHsex:
		if match := reHsexVideoPath.FindStringSubmatch(parsed.Path); len(match) == 2 {
			return source, "detail:" + match[1]
		}
	case sourceHongguo:
		if parsed.Path == "/detail" && parsed.Query().Get("series_id") != "" {
			return source, "detail:" + parsed.Query().Get("series_id")
		}
		if len(parts) == 3 && parts[0] == "player" {
			return source, "player:" + parts[1] + ":" + parts[2]
		}
	}
	return source, ""
}

func validateProviderPageResponse(requested, actual string) error {
	expectedSource, expectedID := providerPageIdentity(requested)
	if expectedID == "" {
		return nil
	}
	actualSource, actualID := providerPageIdentity(actual, expectedSource)
	sameWorkEntry := false
	if strings.HasPrefix(expectedID, "detail:") {
		workID := strings.TrimPrefix(expectedID, "detail:")
		switch expectedSource {
		case sourceHuangguoAI:
			sameWorkEntry = strings.HasPrefix(actualID, "video:"+workID+":")
		case sourceHanxiaoquan, sourceGuipian:
			sameWorkEntry = strings.HasPrefix(actualID, "play:"+workID+":")
		case sourceHongguo:
			sameWorkEntry = strings.HasPrefix(actualID, "player:"+workID+":")
		}
	}
	if expectedSource != actualSource || expectedID != actualID && !sameWorkEntry {
		return errors.New("站源页面跳转到了其他作品或分集，已停止解析，请刷新详情")
	}
	return nil
}

func validateProviderEpisodeOwner(row map[string]any, expectedID string, fields ...string) error {
	for _, field := range fields {
		if identifier := mapString(row, field); identifier != "" && identifier != expectedID {
			return errors.New("站源返回了其他作品的分集，已停止加载，请刷新详情")
		}
	}
	return nil
}

func providerChapterNumber(chapter Chapter) (int, bool) {
	chapterSource, _, _ := strings.Cut(chapter.ID, ":")
	if canonicalProviderSource(firstNonEmpty(chapter.Source, chapterSource)) == sourceCloudFront {
		number := legacyChapterNumber(chapter, 0)
		return number, number > 0 && number <= 100000
	}
	var value any
	if len(chapter.CurrentEpisode) > 0 && string(chapter.CurrentEpisode) != "null" {
		decoder := json.NewDecoder(strings.NewReader(string(chapter.CurrentEpisode)))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return 0, false
		}
		var text string
		switch number := value.(type) {
		case json.Number:
			text = number.String()
		case string:
			text = strings.TrimSpace(number)
		default:
			return 0, false
		}
		number, err := strconv.Atoi(text)
		return number, err == nil && number > 0 && number <= 100000
	}
	number := episodeIndex(chapter.Title, 0)
	return number, number > 0 && number <= 100000
}

func macCMSEpisodeReference(source, address, sourceID string) (int, bool) {
	switch source {
	case sourceHanxiaoquan:
		address = hanxiaoquanPlayURL(address)
	case sourceGuipian:
		address = guipianPlayURL(address)
	default:
		return 0, false
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return 0, false
	}
	match := macCMSEpisodePath.FindStringSubmatch(parsed.Path)
	if len(match) != 4 || match[1] != sourceID {
		return 0, false
	}
	line, lineErr := strconv.Atoi(match[2])
	number, numberErr := strconv.Atoi(match[3])
	if source == sourceGuipian {
		return number + 1, lineErr == nil && line >= 0 && numberErr == nil && number >= 0 && number < 100000
	}
	return number + 1, lineErr == nil && line > 0 && numberErr == nil && number >= 0 && number < 100000
}

func macCMSPlaylistKey(address string) string {
	parsed, err := url.Parse(address)
	if err != nil {
		return ""
	}
	match := macCMSEpisodePath.FindStringSubmatch(parsed.Path)
	if len(match) != 4 {
		return ""
	}
	return match[1] + ":" + match[2]
}

func validateProviderChapter(dramaID string, chapter Chapter, index int) error {
	source, sourceID, valid := splitProviderDramaID(dramaID)
	chapterSource, remainder, hasSource := strings.Cut(chapter.ID, ":")
	prefix := sourceID + ":"
	if !valid || !hasSource || canonicalProviderSource(chapterSource) != source ||
		!strings.HasPrefix(remainder, prefix) || strings.TrimSpace(strings.TrimPrefix(remainder, prefix)) == "" ||
		chapter.Source != "" && canonicalProviderSource(chapter.Source) != source {
		return errors.New("分集不属于当前站源或作品，请重新打开详情；旧下载任务请重新添加")
	}
	number, valid := providerChapterNumber(chapter)
	if !valid || index < 0 || index > 100000 || index > 0 && number != index {
		return errors.New("分集编号无效或与选集不符，请刷新详情后重新选择")
	}
	key := strings.TrimPrefix(remainder, prefix)
	switch source {
	case sourceHuangguoAI:
		if chapter.PageURL != "" {
			_, routeNumber, matched := aiEpisodeReference(huangguoAIBaseURL+"/", chapter.PageURL, sourceID)
			if !matched || routeNumber != number {
				return errors.New("黄果 AI 播放页与当前作品或集号不符，请刷新详情后重新选择")
			}
		}
	case sourceHuangguoVideo:
		if !huangguoVideoChapterReference(sourceID, chapter) {
			return errors.New("黄果视频播放页与当前作品或分集不符，请刷新详情后重新选择")
		}
	case sourceHanxiaoquan, sourceGuipian:
		routeNumber, matched := macCMSEpisodeReference(source, key, sourceID)
		if !matched || routeNumber != number {
			return errors.New("播放页与当前作品或集号不符，请刷新详情后重新选择")
		}
		if chapter.PageURL != "" && chapter.PageURL != key {
			return errors.New("分集播放入口不一致，请刷新详情后重新选择")
		}
	case sourceHsex:
		pageSource, pageID := providerPageIdentity(chapter.PageURL)
		if key != sourceID || chapter.VideoURL != "hsex-play://"+sourceID || pageSource != sourceHsex || pageID != "detail:"+sourceID {
			return errors.New("好色 TV 播放页与当前视频不符，请刷新详情后重新选择")
		}
	case sourceDSD:
		parts := strings.Split(key, "-")
		if len(parts) != 2 || atoiDefault(parts[0], 0) < 1 || atoiDefault(parts[1], 0) != number {
			return errors.New("帝果播放线路或集号不符，请刷新详情后重新选择")
		}
	case sourceHuangdou:
		if atoiDefault(key, 0) != number {
			return errors.New("黄豆播放集号不符，请刷新详情后重新选择")
		}
	case sourceHongguo, sourceHuangju, sourceYeguo, sourceSorani:
		scheme := map[string]string{sourceHongguo: "hongguo-cenc://", sourceHuangju: "huangju-play://", sourceYeguo: "yeguo-play://", sourceSorani: "sorani-play://"}[source]
		addressKey, err := url.PathUnescape(strings.TrimPrefix(chapter.VideoURL, scheme))
		if !strings.HasPrefix(chapter.VideoURL, scheme) || err != nil || addressKey != key {
			return errors.New("分集标识与播放入口不一致，请刷新详情后重新选择")
		}
	}
	return nil
}

func normalizeProviderChapters(dramaID string, chapters []Chapter) ([]Chapter, error) {
	if len(chapters) > 10000 {
		return nil, errors.New("分集目录过大，请刷新详情")
	}
	result := make([]Chapter, 0, len(chapters))
	seenIDs := map[string]Chapter{}
	seenNumbers := map[int]string{}
	for _, chapter := range chapters {
		if err := validateProviderChapter(dramaID, chapter, 0); err != nil {
			return nil, err
		}
		number, _ := providerChapterNumber(chapter)
		if previous, exists := seenIDs[chapter.ID]; exists {
			previousNumber, _ := providerChapterNumber(previous)
			if previousNumber != number {
				return nil, errors.New("同一分集对应多个编号，请刷新详情")
			}
			continue
		}
		if previous, exists := seenNumbers[number]; exists && previous != chapter.ID {
			return nil, errors.New("同一集号对应不同分集，已停止加载，请刷新详情")
		}
		chapter.Source, _, _ = splitProviderDramaID(dramaID)
		chapter.CurrentEpisode = rawEpisode(number)
		seenIDs[chapter.ID], seenNumbers[number] = chapter, chapter.ID
		result = append(result, chapter)
	}
	sort.SliceStable(result, func(i, j int) bool {
		left, _ := providerChapterNumber(result[i])
		right, _ := providerChapterNumber(result[j])
		return left < right
	})
	return result, nil
}
