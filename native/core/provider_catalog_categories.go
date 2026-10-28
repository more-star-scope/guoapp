package core

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var macCMSCatalogCategoryPath = regexp.MustCompile(`^/(hxq|list)/(\d+)\.html$`)

func (d *Downloader) fetchMacCMSCatalogCategories(ctx context.Context, source string) ([]nativeCategory, error) {
	paths, route := []string{"/hxq/1.html"}, "hxq"
	known := hanxiaoquanCategories
	if source == sourceGuipian {
		paths, route, known = []string{"/list/1.html", "/list/2.html", "/list/4.html", "/list/3.html"}, "list", guipianCategories
	}
	names := map[string]string{}
	for _, category := range known {
		names[category.ID] = category.Name
	}
	seen := map[string]bool{}
	categories := []nativeCategory{}
	for _, path := range paths {
		var body string
		var err error
		if source == sourceGuipian {
			body, err = d.guipianGet(ctx, path)
		} else {
			body, err = d.hanxiaoquanGet(ctx, path)
		}
		if err != nil {
			return nil, err
		}
		for _, anchor := range categoryAnchorPattern.FindAllStringSubmatch(body, -1) {
			href := extractAttr("<a "+anchor[1]+">", "href")
			address, err := url.Parse(resolveProviderURL(d.providerBaseURL(source)+"/", href))
			if err != nil || providerSourceForURL(address.String()) != source && providerSourceForURL(d.providerBaseURL(source)) != "" {
				continue
			}
			match := macCMSCatalogCategoryPath.FindStringSubmatch(address.Path)
			if len(match) != 3 || match[1] != route || seen[match[2]] || !validNativeCategory(source, match[2]) {
				continue
			}
			id := match[2]
			if source == sourceGuipian && (id == "1" || id == "2" || id == "4") {
				continue
			}
			name := names[id]
			if name == "" {
				name = cleanText(anchor[2])
				if name == "" || name == "查看更多" || name == "更多" || len([]rune(name)) > 48 || strings.Contains(name, "首页") {
					name = "分类 " + id
				}
			}
			seen[id] = true
			categories = append(categories, nativeCategory{ID: id, Name: name})
		}
	}
	if len(categories) == 0 || len(categories) > 256 {
		return nil, errors.New("站源分类菜单无法识别，已保留目录位置")
	}
	return categories, nil
}
