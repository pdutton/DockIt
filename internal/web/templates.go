package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/pdutton/DockIt/internal/model"
)

// parseTemplates builds one template set per page: the shared layout plus
// the page, so each page can define its own "content" block.
func parseTemplates(assets fs.FS) (map[string]*template.Template, error) {
	base, err := template.New("").Funcs(funcs).ParseFS(assets, "templates/layout.html")
	if err != nil {
		return nil, err
	}
	names, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	pages := make(map[string]*template.Template)
	for _, name := range names {
		page := strings.TrimSuffix(path.Base(name), ".html")
		if page == "layout" {
			continue
		}
		t, err := template.Must(base.Clone()).ParseFS(assets, name)
		if err != nil {
			return nil, err
		}
		pages[page] = t
	}
	return pages, nil
}

var funcs = template.FuncMap{
	"markdown": renderMarkdown,
	"time":     timeTag,

	"projectState": func(id string) template.HTML { return enumTag(model.ProjectStates, id) },
	"taskState":    func(id string) template.HTML { return enumTag(model.TaskStates, id) },
	"urlType":      func(id string) template.HTML { return enumTag(model.URLTypes, id) },
	"role":         func(id string) template.HTML { return enumTag(model.Roles, id) },
	"substate": func(state, id string) template.HTML {
		if e, ok := model.Substates[state]; ok {
			return enumTag(e, id)
		}
		return enumTag(emptyEnum, id)
	},

	"projectStates": model.ProjectStates.Values,
	"taskStates":    model.TaskStates.Values,
	"urlTypes":      model.URLTypes.Values,
	"roles":         model.Roles.Values,
	"allSubstates":  allSubstates,
	"priorities":    func() []int { return []int{1, 2, 3, 4, 5} },
	"isAdmin":       func(u *model.User) bool { return u != nil && u.Role == model.RoleAdmin },
	"canEdit": func(u *model.User) bool {
		return u != nil && (u.Role == model.RoleAdmin || u.Role == model.RoleMember)
	},
}

var emptyEnum = &model.Enum{}

// enumTag shows an enumerated value by its display string.  An unknown id,
// which should never occur, is shown raw with a warning marker.
func enumTag(e *model.Enum, id string) template.HTML {
	if id == "" {
		return ""
	}
	if d, ok := e.Display(id); ok {
		return template.HTML(template.HTMLEscapeString(d))
	}
	return template.HTML(fmt.Sprintf(`<span class="unknown" title="Unknown value; this DockIt does not recognize it">%s ⚠</span>`,
		template.HTMLEscapeString(id)))
}

// timeTag renders a timestamp in UTC; app.js converts it to local time.
func timeTag(t time.Time) template.HTML {
	if t.IsZero() {
		return ""
	}
	t = t.UTC()
	return template.HTML(fmt.Sprintf(`<time datetime="%s">%s</time>`,
		t.Format(time.RFC3339), t.Format("2006-01-02 15:04 UTC")))
}

// substateOption is a substate choice, labeled with its state.
type substateOption struct {
	State   string
	ID      string
	Display string
}

func allSubstates() []substateOption {
	var out []substateOption
	for _, s := range model.TaskStates.Values() {
		if e, ok := model.Substates[s.ID]; ok {
			for _, v := range e.Values() {
				out = append(out, substateOption{s.ID, v.ID, s.Display + ": " + v.Display})
			}
		}
	}
	return out
}
