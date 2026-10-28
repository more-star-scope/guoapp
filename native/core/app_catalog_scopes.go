package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const nativeCatalogScopeVersion = 1

type catalogSourceLockedKey struct{}

type nativeCatalogScope struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Head    bool   `json:"head,omitempty"`
	Warning string `json:"warning,omitempty"`
}

func nativeUsesCatalogScopes(source string) bool {
	switch source {
	case sourceHanxiaoquan, sourceGuipian, sourceDSD, sourceHuangguoAI, sourceCloudFront:
		return true
	}
	return false
}

func (engine *nativeEngine) catalogScopeStateLocked(source string, state nativeCatalogState) nativeCatalogState {
	if !nativeUsesCatalogScopes(source) {
		return state
	}
	if state.ScopeVersion != nativeCatalogScopeVersion || len(state.Scopes) == 0 {
		state.HasMore, state.UpdatedAt = true, time.Time{}
		return state
	}
	state.HasMore = state.Warning != "" || state.ScopeRefresh
	for _, scope := range state.Scopes {
		child, found := engine.catalogStates[nativeCatalogKey(source, scope.ID)]
		if scope.Head || scope.Warning != "" || !found || child.HasMore || child.Warning != "" {
			state.HasMore = true
		}
		if !found || child.UpdatedAt.IsZero() {
			state.UpdatedAt = time.Time{}
		} else if !state.UpdatedAt.IsZero() && child.UpdatedAt.Before(state.UpdatedAt) {
			state.UpdatedAt = child.UpdatedAt
		}
	}
	return state
}

func (engine *nativeEngine) nativeCatalog(ctx context.Context, input nativeInput) (nativeCatalogResult, error) {
	source := canonicalProviderSource(input.Source)
	if strings.TrimSpace(input.Query) == "" && strings.TrimSpace(input.Category) == "" && nativeUsesCatalogScopes(source) {
		return engine.nativeScopedCatalog(ctx, input, source)
	}
	return engine.nativeCatalogPage(ctx, input)
}

func (engine *nativeEngine) catalogScopesCanAdvance(source string) bool {
	if !nativeUsesCatalogScopes(source) {
		return false
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	state := engine.catalogStates[source]
	if state.ScopeRefresh || state.ScopeVersion != nativeCatalogScopeVersion {
		return false
	}
	for _, scope := range state.Scopes {
		child, found := engine.catalogStates[nativeCatalogKey(source, scope.ID)]
		if scope.Warning == "" && (scope.Head || !found || child.HasMore) {
			return true
		}
	}
	return false
}

func (engine *nativeEngine) saveCatalogScopes(source string, scopes []nativeCatalogScope, page int) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	state := engine.catalogStates[source]
	state.ScopeVersion, state.Scopes = nativeCatalogScopeVersion, append([]nativeCatalogScope(nil), scopes...)
	state.ScopeRefresh = false
	state.Page, state.UpdatedAt, state.Warning = max(1, page), time.Now(), ""
	for _, scope := range scopes {
		if scope.Warning != "" {
			state.Warning = joinNativeWarnings(state.Warning, scope.Name+"："+scope.Warning)
		}
	}
	engine.catalogStates[source] = engine.catalogScopeStateLocked(source, state)
	return engine.writeCatalogDiskLocked()
}

func (engine *nativeEngine) nativeScopedCatalog(ctx context.Context, input nativeInput, source string) (nativeCatalogResult, error) {
	unlock, err := engine.lockSourceCatalog(ctx, source+"|scopes")
	if err != nil {
		return nativeCatalogResult{}, err
	}
	defer unlock()
	if retried, err := engine.retryCatalogSave(); retried && err != nil {
		return engine.nativeCached(source), nil
	}
	page := max(1, input.Page)
	if page == 1 && !input.Force {
		if cached := engine.nativeCached(source); cached.Fresh {
			return cached, nil
		}
	}
	engine.mu.Lock()
	state := engine.catalogStates[source]
	scopes := append([]nativeCatalogScope(nil), state.Scopes...)
	engine.mu.Unlock()
	if page == 1 || state.ScopeRefresh || state.ScopeVersion != nativeCatalogScopeVersion || len(scopes) == 0 {
		categories, err := engine.nativeCategories(ctx, source, input.Force)
		if err != nil {
			engine.mu.Lock()
			state.ScopeRefresh, state.Warning = true, publicError(err).Error()
			engine.catalogStates[source] = state
			engine.writeCatalogDiskLocked()
			engine.mu.Unlock()
			return engine.nativeCached(source), err
		}
		scopes = nil
		seen := map[string]bool{}
		for _, category := range categories {
			if category.ID == "" || seen[category.ID] {
				continue
			}
			seen[category.ID] = true
			scopes = append(scopes, nativeCatalogScope{ID: category.ID, Name: category.Name, Head: true})
		}
		if len(scopes) == 0 {
			return nativeCatalogResult{}, errors.New("站源未提供可遍历的分类，已保留目录位置")
		}
		if state.ScopeVersion != nativeCatalogScopeVersion {
			engine.mu.Lock()
			for _, scope := range scopes {
				key := nativeCatalogKey(source, scope.ID)
				engine.catalogStates[key] = nativeCatalogState{HasMore: true}
			}
			engine.mu.Unlock()
			page = 1
		}
		if err := engine.saveCatalogScopes(source, scopes, max(page, state.Page)); err != nil {
			return engine.nativeCached(source), nil
		}
	}
	for index := range scopes {
		if err := ctx.Err(); err != nil {
			return engine.nativeCached(source), err
		}
		scope := &scopes[index]
		if background, _ := ctx.Value(backgroundCatalogKey{}).(bool); background && page > 1 && scope.Warning != "" {
			continue
		}
		result, fetched, fetchErr := engine.nativeCatalogScopePage(ctx, source, *scope, index, len(scopes))
		if !fetched && fetchErr == nil {
			continue
		}
		if fetchErr == nil && result.Warning == "" {
			scope.Head, scope.Warning = false, ""
		} else {
			scope.Warning = result.Warning
			if fetchErr != nil {
				scope.Warning = publicError(fetchErr).Error()
			}
		}
		if err := engine.saveCatalogScopes(source, scopes, max(page, state.Page)); err != nil {
			return engine.nativeCached(source), nil
		}
		if result.saveError != nil {
			return engine.nativeCached(source), nil
		}
		var backoff *requestBackoff
		if errors.As(fetchErr, &backoff) {
			return engine.nativeCached(source), fetchErr
		}
	}
	return engine.nativeCached(source), ctx.Err()
}

func (engine *nativeEngine) nativeCatalogScopePage(ctx context.Context, source string, scope nativeCatalogScope, index, total int) (nativeCatalogResult, bool, error) {
	unlock, err := engine.lockSourceCatalog(ctx, source)
	if err != nil {
		return nativeCatalogResult{}, false, err
	}
	defer unlock()
	engine.mu.Lock()
	child, found := engine.catalogStates[nativeCatalogKey(source, scope.ID)]
	engine.mu.Unlock()
	if !scope.Head && found && !child.HasMore && child.Warning == "" && scope.Warning == "" {
		return nativeCatalogResult{}, false, nil
	}
	next := max(1, child.Page+1)
	if scope.Head {
		next = 1
	}
	if background, _ := ctx.Value(backgroundCatalogKey{}).(bool); background {
		engine.changeSourceRecord(source, func(record *nativeSourceRecord) {
			record.Stage = fmt.Sprintf("加载分类 %d/%d · %s · 第 %d 页", index+1, total, scope.Name, next)
		})
	}
	ctx = context.WithValue(ctx, catalogSourceLockedKey{}, source)
	result, err := engine.nativeCatalogPage(ctx, nativeInput{Source: source, Category: scope.ID, Page: next, Force: true})
	return result, true, err
}
