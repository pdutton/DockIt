package api

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/service"
)

// Projects.

func (a *API) listProjects(w http.ResponseWriter, r *http.Request, actor string) error {
	ps, err := a.svc.Projects(actor)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ps)
	return nil
}

func (a *API) getProject(w http.ResponseWriter, r *http.Request, actor string) error {
	p, err := a.svc.Project(actor, r.PathValue("pid"))
	if err != nil {
		return err
	}
	return writeRecord(w, http.StatusOK, p)
}

func (a *API) createProject(w http.ResponseWriter, r *http.Request, actor string) error {
	var in struct {
		ID          string     `json:"id"`
		Name        string     `json:"name"`
		State       string     `json:"state"`
		Description string     `json:"description"`
		URLs        model.URLs `json:"urls"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	p, err := a.svc.CreateProject(actor, service.NewProject{
		ID: in.ID, Name: in.Name, State: in.State, Description: in.Description, URLs: in.URLs,
	})
	if err != nil {
		return err
	}
	w.Header().Set("Location", Prefix+"/projects/"+p.ID)
	return writeRecord(w, http.StatusCreated, p)
}

var readOnlyRecord = []string{"id", "version", "created", "modified"}

func (a *API) updateProject(w http.ResponseWriter, r *http.Request, actor string) error {
	version, err := ifMatch(r)
	if err != nil {
		return err
	}
	p, err := readPatch(r)
	if err != nil {
		return err
	}
	if err := p.allow(readOnlyRecord, "name", "state", "description", "urls"); err != nil {
		return err
	}
	var patch service.ProjectPatch
	if patch.Name, err = optional[string](p, "name", false); err != nil {
		return err
	}
	if patch.State, err = optional[string](p, "state", false); err != nil {
		return err
	}
	if patch.Description, err = optional[string](p, "description", true); err != nil {
		return err
	}
	if _, ok := p["urls"]; ok {
		// URLs merge into the current value, which the service checks
		// against the version.
		cur, err := a.svc.Project(actor, r.PathValue("pid"))
		if err != nil {
			return err
		}
		if version != 0 && cur.Version != version {
			return &service.ConflictError{Current: cur}
		}
		if patch.URLs, err = p.urls(cur.URLs); err != nil {
			return err
		}
	}
	proj, err := a.svc.UpdateProject(actor, r.PathValue("pid"), version, patch)
	if err != nil {
		return err
	}
	return writeRecord(w, http.StatusOK, proj)
}

// Tasks.

func (a *API) listTasks(w http.ResponseWriter, r *http.Request, actor string) error {
	q := r.URL.Query()
	f := service.TaskFilter{States: slices.DeleteFunc(q["state"], func(s string) bool { return s == "" }), Owner: q.Get("owner")}
	if s := q.Get("priority"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < model.MinPriority || n > model.MaxPriority {
			return badRequest("priority", "priority must be %d to %d", model.MinPriority, model.MaxPriority)
		}
		f.Priority = n
	}
	order := service.SortByID
	switch q.Get("sort") {
	case "", "id":
	case "priority":
		order = service.SortByPriority
	case "modified":
		order = service.SortByModified
	default:
		return badRequest("sort", "sort must be id, priority or modified")
	}
	ts, err := a.svc.Tasks(actor, r.PathValue("pid"), f, order)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, ts)
	return nil
}

func (a *API) getTask(w http.ResponseWriter, r *http.Request, actor string) error {
	t, err := a.svc.Task(actor, r.PathValue("tid"))
	if err != nil {
		return err
	}
	return writeRecord(w, http.StatusOK, t)
}

func (a *API) createTask(w http.ResponseWriter, r *http.Request, actor string) error {
	var in struct {
		Title       string     `json:"title"`
		Type        string     `json:"type"`
		Description string     `json:"description"`
		Owner       string     `json:"owner"`
		State       string     `json:"state"`
		Substate    string     `json:"substate"`
		Priority    int        `json:"priority"`
		FoundIn     string     `json:"found_in"`
		ResolvedIn  string     `json:"resolved_in"`
		URLs        model.URLs `json:"urls"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	t, err := a.svc.CreateTask(actor, r.PathValue("pid"), service.NewTask(in))
	if err != nil {
		return err
	}
	w.Header().Set("Location", Prefix+"/tasks/"+t.ID)
	return writeRecord(w, http.StatusCreated, t)
}

func (a *API) updateTask(w http.ResponseWriter, r *http.Request, actor string) error {
	version, err := ifMatch(r)
	if err != nil {
		return err
	}
	p, err := readPatch(r)
	if err != nil {
		return err
	}
	readOnly := append([]string{"creator", "comments", "last_comment_id"}, readOnlyRecord...)
	if err := p.allow(readOnly, "title", "type", "description", "owner", "state", "substate", "priority", "found_in", "resolved_in", "urls"); err != nil {
		return err
	}
	var patch service.TaskPatch
	for _, f := range []struct {
		name     string
		dst      **string
		nullable bool
	}{
		{"title", &patch.Title, false},
		{"type", &patch.Type, false},
		{"description", &patch.Description, true},
		{"owner", &patch.Owner, false},
		{"state", &patch.State, false},
		{"substate", &patch.Substate, true},
		{"found_in", &patch.FoundIn, true},
		{"resolved_in", &patch.ResolvedIn, true},
	} {
		if *f.dst, err = optional[string](p, f.name, f.nullable); err != nil {
			return err
		}
	}
	if patch.Priority, err = optional[int](p, "priority", false); err != nil {
		return err
	}
	if _, ok := p["urls"]; ok {
		// As for projects, URLs merge into the current value.
		cur, err := a.svc.Task(actor, r.PathValue("tid"))
		if err != nil {
			return err
		}
		if version != 0 && cur.Version != version {
			return &service.ConflictError{Current: cur}
		}
		if patch.URLs, err = p.urls(cur.URLs); err != nil {
			return err
		}
	}
	t, err := a.svc.UpdateTask(actor, r.PathValue("tid"), version, patch)
	if err != nil {
		return err
	}
	return writeRecord(w, http.StatusOK, t)
}

// Comments.

type commentBody struct {
	Text string `json:"text"`
}

func (a *API) listComments(w http.ResponseWriter, r *http.Request, actor string) error {
	cs, err := a.svc.Comments(actor, r.PathValue("tid"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, cs)
	return nil
}

func (a *API) getComment(w http.ResponseWriter, r *http.Request, actor string) error {
	cid, err := pathInt(r, "cid")
	if err != nil {
		return err
	}
	c, err := a.svc.Comment(actor, r.PathValue("tid"), cid)
	if err != nil {
		return err
	}
	return writeRecord(w, http.StatusOK, c)
}

func (a *API) addComment(w http.ResponseWriter, r *http.Request, actor string) error {
	var in commentBody
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, err := a.svc.AddComment(actor, r.PathValue("tid"), in.Text)
	if err != nil {
		return err
	}
	w.Header().Set("Location", Prefix+"/tasks/"+r.PathValue("tid")+"/comments/"+strconv.Itoa(c.ID))
	return writeRecord(w, http.StatusCreated, c)
}

func (a *API) editComment(w http.ResponseWriter, r *http.Request, actor string) error {
	cid, err := pathInt(r, "cid")
	if err != nil {
		return err
	}
	version, err := ifMatch(r)
	if err != nil {
		return err
	}
	var in commentBody
	if err := readJSON(r, &in); err != nil {
		return err
	}
	c, err := a.svc.EditComment(actor, r.PathValue("tid"), cid, version, in.Text)
	if err != nil {
		return err
	}
	return writeRecord(w, http.StatusOK, c)
}

func (a *API) deleteComment(w http.ResponseWriter, r *http.Request, actor string) error {
	cid, err := pathInt(r, "cid")
	if err != nil {
		return err
	}
	version, err := ifMatch(r)
	if err != nil {
		return err
	}
	if err := a.svc.DeleteComment(actor, r.PathValue("tid"), cid, version); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Users.

func (a *API) listUsers(w http.ResponseWriter, r *http.Request, actor string) error {
	us, err := a.svc.Users(actor)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, us)
	return nil
}

func (a *API) getUser(w http.ResponseWriter, r *http.Request, actor string) error {
	u, err := a.svc.User(actor, r.PathValue("uid"))
	if err != nil {
		return err
	}
	return writeRecord(w, http.StatusOK, u)
}

// passwordBody carries a one-time password, shown once.
type passwordBody struct {
	User     *model.User `json:"user,omitempty"`
	Password string      `json:"password"`
}

func (a *API) createUser(w http.ResponseWriter, r *http.Request, actor string) error {
	var in struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	u, password, err := a.svc.CreateUser(actor, service.NewUser(in))
	if err != nil {
		return err
	}
	w.Header().Set("Location", Prefix+"/users/"+u.ID)
	w.Header().Set("ETag", etag(u.Version))
	writeJSON(w, http.StatusCreated, passwordBody{User: u, Password: password})
	return nil
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request, actor string) error {
	version, err := ifMatch(r)
	if err != nil {
		return err
	}
	p, err := readPatch(r)
	if err != nil {
		return err
	}
	if err := p.allow(readOnlyRecord, "name", "email", "role", "active"); err != nil {
		return err
	}
	var patch service.UserPatch
	if patch.Name, err = optional[string](p, "name", false); err != nil {
		return err
	}
	if patch.Email, err = optional[string](p, "email", false); err != nil {
		return err
	}
	if patch.Role, err = optional[string](p, "role", false); err != nil {
		return err
	}
	if patch.Active, err = optional[bool](p, "active", false); err != nil {
		return err
	}
	u, err := a.svc.UpdateUser(actor, r.PathValue("uid"), version, patch)
	if err != nil {
		return err
	}
	return writeRecord(w, http.StatusOK, u)
}

func (a *API) resetPassword(w http.ResponseWriter, r *http.Request, actor string) error {
	password, err := a.svc.ResetPassword(actor, r.PathValue("uid"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, passwordBody{Password: password})
	return nil
}

// The current user.

func (a *API) getMe(w http.ResponseWriter, r *http.Request, actor string) error {
	u, err := a.svc.User(actor, actor)
	if err != nil {
		return err
	}
	return writeRecord(w, http.StatusOK, u)
}

func (a *API) listTokens(w http.ResponseWriter, r *http.Request, actor string) error {
	ts, err := a.svc.Tokens(actor)
	if err != nil {
		return err
	}
	if ts == nil {
		ts = []service.TokenInfo{}
	}
	writeJSON(w, http.StatusOK, ts)
	return nil
}

func (a *API) createToken(w http.ResponseWriter, r *http.Request, actor string) error {
	var in struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &in); err != nil {
		return err
	}
	info, secret, err := a.svc.CreateToken(actor, in.Name)
	if err != nil {
		return err
	}
	w.Header().Set("Location", Prefix+"/me/tokens/"+info.ID)
	writeJSON(w, http.StatusCreated, struct {
		ID      string    `json:"id"`
		Name    string    `json:"name"`
		Created time.Time `json:"created"`
		Token   string    `json:"token"`
	}{info.ID, info.Name, info.Created, secret})
	return nil
}

func (a *API) revokeToken(w http.ResponseWriter, r *http.Request, actor string) error {
	if err := a.svc.RevokeToken(actor, r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Enumerations.

type enumValue struct {
	ID      string `json:"id"`
	Display string `json:"display"`
}

func enumValues(e *model.Enum) []enumValue {
	var out []enumValue
	for _, v := range e.Values() {
		out = append(out, enumValue{v.ID, v.Display})
	}
	return out
}

func (a *API) getEnums(w http.ResponseWriter, r *http.Request, actor string) error {
	if _, err := a.svc.User(actor, actor); err != nil {
		return err
	}
	subs := map[string][]enumValue{}
	for state, e := range model.Substates {
		subs[state] = enumValues(e)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"project_states": enumValues(model.ProjectStates),
		"task_states":    enumValues(model.TaskStates),
		"substates":      subs,
		"task_types":     enumValues(model.TaskTypes),
		"url_types":      enumValues(model.URLTypes),
		"task_url_types": enumValues(model.TaskURLTypes),
		"roles":          enumValues(model.Roles),
	})
	return nil
}
