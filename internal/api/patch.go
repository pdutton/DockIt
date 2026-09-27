package api

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"

	"github.com/pdutton/DockIt/internal/model"
)

// mergePatch is a JSON Merge Patch (RFC 7396) body: members present are set,
// members set to null are removed, members absent are left alone.
type mergePatch map[string]json.RawMessage

func readPatch(r *http.Request) (mergePatch, error) {
	var p mergePatch
	if err := readJSON(r, &p); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, badRequest("", "the patch must be a JSON object")
	}
	return p, nil
}

// allow rejects members other than the patchable ones, calling out fields
// that exist but cannot be changed.
func (p mergePatch) allow(readOnly []string, patchable ...string) error {
	for k := range p {
		switch {
		case slices.Contains(patchable, k):
		case slices.Contains(readOnly, k):
			return badRequest(k, "%s cannot be changed", k)
		default:
			return badRequest(k, "unknown field %q", k)
		}
	}
	return nil
}

func isNull(raw json.RawMessage) bool { return string(raw) == "null" }

// optional reads a member into a new *T.  null is only allowed for fields
// that may be empty, and then means the zero value.
func optional[T any](p mergePatch, field string, nullable bool) (*T, error) {
	raw, ok := p[field]
	if !ok {
		return nil, nil
	}
	var v T
	if isNull(raw) {
		if !nullable {
			return nil, badRequest(field, "%s cannot be null", field)
		}
		return &v, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, badRequest(field, "%s: %v", field, err)
	}
	return &v, nil
}

// urls applies the urls member to current.  Per RFC 7396 it merges: a URL
// type set to a list replaces that type's list, a type set to null is
// removed, and urls: null removes them all.
func (p mergePatch) urls(current model.URLs) (*model.URLs, error) {
	raw, ok := p["urls"]
	if !ok {
		return nil, nil
	}
	if isNull(raw) {
		return &model.URLs{}, nil
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, badRequest("urls", "urls must be an object of URL type to list of URLs")
	}
	out := maps.Clone(current)
	if out == nil {
		out = model.URLs{}
	}
	for typ, v := range members {
		if isNull(v) {
			delete(out, typ)
			continue
		}
		var list []string
		if err := json.Unmarshal(v, &list); err != nil {
			return nil, badRequest("urls."+typ, "urls.%s must be a list of URLs", typ)
		}
		out[typ] = list
	}
	return &out, nil
}
