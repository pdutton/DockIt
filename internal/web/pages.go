package web

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/service"
)

// formErrors turns a validation error into per-field messages, and a
// conflict into the current record.  ok is false for any other error, which
// the caller returns for an error page.
func formErrors(err error) (fields map[string]string, current any, ok bool) {
	var ve *service.ValidationError
	var ce *service.ConflictError
	switch {
	case errors.As(err, &ve):
		fields = map[string]string{}
		for _, f := range ve.Fields {
			// "urls.web[0]" is reported against the urls field.
			name, _, _ := strings.Cut(f.Field, ".")
			name, _, _ = strings.Cut(name, "[")
			if _, dup := fields[name]; !dup {
				fields[name] = f.Message
			}
		}
		return fields, nil, true
	case errors.As(err, &ce):
		return nil, ce.Current, true
	case errors.Is(err, service.ErrExists):
		return map[string]string{"id": "already exists"}, nil, true
	}
	return nil, nil, false
}

func formInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.PostForm.Get(name))
	return n
}

// Projects.

type projectForm struct {
	ID          string
	Name        string
	State       string
	Description string
	URLs        map[string]string // URL type -> one URL per line
	Version     int
}

func projectFormFrom(p *model.Project) projectForm {
	f := projectForm{ID: p.ID, Name: p.Name, State: p.State, Description: p.Description,
		URLs: map[string]string{}, Version: p.Version}
	for k, v := range p.URLs {
		f.URLs[k] = strings.Join(v, "\n")
	}
	return f
}

func readProjectForm(r *http.Request) projectForm {
	f := projectForm{
		ID:          strings.TrimSpace(r.PostForm.Get("id")),
		Name:        r.PostForm.Get("name"),
		State:       r.PostForm.Get("state"),
		Description: normalizeNewlines(r.PostForm.Get("description")),
		URLs:        map[string]string{},
		Version:     formInt(r, "version"),
	}
	for _, t := range model.URLTypes.Values() {
		f.URLs[t.ID] = r.PostForm.Get("urls_" + t.ID)
	}
	return f
}

// urls parses the one-per-line URL fields.
func (f projectForm) urls() model.URLs {
	out := model.URLs{}
	for typ, text := range f.URLs {
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				out[typ] = append(out[typ], line)
			}
		}
	}
	return out
}

// Browsers submit textareas with CRLF line endings; store LF.
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

type projectFormData struct {
	New      bool
	Form     projectForm
	Errors   map[string]string
	Conflict *model.Project
}

func (w *Web) projectList(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	ps, err := w.svc.Projects(c.me.ID)
	if err != nil {
		return err
	}
	w.render(rw, r, c, http.StatusOK, "projects", "Projects", ps)
	return nil
}

func (w *Web) projectNew(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	if c.me.Role != model.RoleAdmin {
		return service.ErrForbidden
	}
	w.render(rw, r, c, http.StatusOK, "project_form", "New project",
		projectFormData{New: true, Form: projectForm{State: model.ProjectPlanned}})
	return nil
}

func (w *Web) projectCreate(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	f := readProjectForm(r)
	p, err := w.svc.CreateProject(c.me.ID, service.NewProject{
		ID: f.ID, Name: f.Name, State: f.State, Description: f.Description, URLs: f.urls(),
	})
	if err != nil {
		fields, _, ok := formErrors(err)
		if !ok {
			return err
		}
		w.render(rw, r, c, http.StatusUnprocessableEntity, "project_form", "New project",
			projectFormData{New: true, Form: f, Errors: fields})
		return nil
	}
	return redirect(rw, r, "/projects/"+p.ID)
}

func (w *Web) projectEdit(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	if c.me.Role != model.RoleAdmin {
		return service.ErrForbidden
	}
	p, err := w.svc.Project(c.me.ID, r.PathValue("pid"))
	if err != nil {
		return err
	}
	w.render(rw, r, c, http.StatusOK, "project_form", "Edit "+p.Name, projectFormData{Form: projectFormFrom(p)})
	return nil
}

func (w *Web) projectUpdate(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	f := readProjectForm(r)
	f.ID = r.PathValue("pid")
	urls := f.urls()
	p, err := w.svc.UpdateProject(c.me.ID, f.ID, f.Version, service.ProjectPatch{
		Name: &f.Name, State: &f.State, Description: &f.Description, URLs: &urls,
	})
	if err != nil {
		fields, current, ok := formErrors(err)
		if !ok {
			return err
		}
		status := http.StatusUnprocessableEntity
		data := projectFormData{Form: f, Errors: fields}
		if cur, isP := current.(*model.Project); isP {
			status = http.StatusConflict
			data.Conflict = cur
			data.Form.Version = cur.Version // resubmitting now overwrites, knowingly
		}
		w.render(rw, r, c, status, "project_form", "Edit "+f.ID, data)
		return nil
	}
	return redirect(rw, r, "/projects/"+p.ID)
}

// Project detail and its task list.

type projectViewData struct {
	Project *model.Project
	Tasks   []*model.Task
	Users   []*model.User
	Filter  service.TaskFilter
	Sort    string
}

// StateShown reports whether the filter includes tasks in state id.
func (d projectViewData) StateShown(id string) bool {
	return slices.Contains(d.Filter.States, id)
}

// openStates are the states a task list shows when first opened.
func openStates() []string {
	var ids []string
	for _, v := range model.TaskStates.Values() {
		if v.ID != model.TaskComplete && v.ID != model.TaskDeferred {
			ids = append(ids, v.ID)
		}
	}
	return ids
}

func (w *Web) projectView(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	p, err := w.svc.Project(c.me.ID, r.PathValue("pid"))
	if err != nil {
		return err
	}
	q := r.URL.Query()
	f := service.TaskFilter{States: slices.DeleteFunc(q["state"], func(s string) bool { return s == "" }), Owner: q.Get("owner")}
	f.Priority, _ = strconv.Atoi(q.Get("priority"))
	f.PriorityOrHigher = true
	// A submitted form always has a query, so no query means first opened.
	// A submitted form with no state ticked means every state.
	if r.URL.RawQuery == "" {
		f.States = openStates()
	}
	order := service.SortByID
	switch q.Get("sort") {
	case "priority":
		order = service.SortByPriority
	case "modified":
		order = service.SortByModified
	}
	tasks, err := w.svc.Tasks(c.me.ID, p.ID, f, order)
	if err != nil {
		return err
	}
	users, err := w.svc.Users(c.me.ID)
	if err != nil {
		return err
	}
	w.render(rw, r, c, http.StatusOK, "project", p.Name,
		projectViewData{Project: p, Tasks: tasks, Users: users, Filter: f, Sort: q.Get("sort")})
	return nil
}

// Tasks.

type taskForm struct {
	Title       string
	Description string
	Owner       string
	State       string
	Substate    string
	Priority    int
	Version     int
}

func taskFormFrom(t *model.Task) taskForm {
	return taskForm{t.Title, t.Description, t.Owner, t.State, t.Substate, t.Priority, t.Version}
}

func readTaskForm(r *http.Request) taskForm {
	return taskForm{
		Title:       r.PostForm.Get("title"),
		Description: normalizeNewlines(r.PostForm.Get("description")),
		Owner:       r.PostForm.Get("owner"),
		State:       r.PostForm.Get("state"),
		Substate:    r.PostForm.Get("substate"),
		Priority:    formInt(r, "priority"),
		Version:     formInt(r, "version"),
	}
}

// activeUsers lists users who may be chosen as owner, plus current, who may
// be kept even if deactivated.
func (w *Web) activeUsers(c *ctx, current string) ([]*model.User, error) {
	all, err := w.svc.Users(c.me.ID)
	if err != nil {
		return nil, err
	}
	var out []*model.User
	for _, u := range all {
		if u.Active || u.ID == current {
			out = append(out, u)
		}
	}
	return out, nil
}

type taskNewData struct {
	Project *model.Project
	Form    taskForm
	Errors  map[string]string
	Owners  []*model.User
}

func (w *Web) taskNew(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	if c.me.Role == model.RoleViewer {
		return service.ErrForbidden
	}
	p, err := w.svc.Project(c.me.ID, r.PathValue("pid"))
	if err != nil {
		return err
	}
	owners, err := w.activeUsers(c, "")
	if err != nil {
		return err
	}
	w.render(rw, r, c, http.StatusOK, "task_new", "New task in "+p.Name, taskNewData{
		Project: p, Owners: owners,
		Form: taskForm{Owner: c.me.ID, State: model.TaskNew, Priority: model.DefaultPriority},
	})
	return nil
}

func (w *Web) taskCreate(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	pid := r.PathValue("pid")
	f := readTaskForm(r)
	t, err := w.svc.CreateTask(c.me.ID, pid, service.NewTask{
		Title: f.Title, Description: f.Description, Owner: f.Owner,
		State: f.State, Substate: f.Substate, Priority: f.Priority,
	})
	if err != nil {
		fields, _, ok := formErrors(err)
		if !ok {
			return err
		}
		p, err := w.svc.Project(c.me.ID, pid)
		if err != nil {
			return err
		}
		owners, err := w.activeUsers(c, "")
		if err != nil {
			return err
		}
		w.render(rw, r, c, http.StatusUnprocessableEntity, "task_new", "New task in "+p.Name,
			taskNewData{Project: p, Form: f, Errors: fields, Owners: owners})
		return nil
	}
	return redirect(rw, r, "/tasks/"+t.ID)
}

type taskViewData struct {
	Task     *model.Task
	Project  *model.Project
	Owners   []*model.User
	Form     taskForm
	Errors   map[string]string
	Conflict *model.Task
	// Comment form state after a failed comment action.
	Comment        commentState
	EditingOpen    bool // open the edit form, after a failed edit
	CommentsByUser map[string]bool
}

type commentState struct {
	ID       int // 0 for a new comment
	Text     string
	Error    string
	Conflict *model.Comment
}

func (w *Web) taskPage(rw http.ResponseWriter, r *http.Request, c *ctx, status int, tid string, fill func(*taskViewData)) error {
	t, err := w.svc.Task(c.me.ID, tid)
	if err != nil {
		return err
	}
	pid, _, _ := model.ParseTaskID(t.ID)
	p, err := w.svc.Project(c.me.ID, pid)
	if err != nil {
		return err
	}
	data := taskViewData{Task: t, Project: p, Form: taskFormFrom(t)}
	if fill != nil {
		fill(&data)
	}
	if data.Owners, err = w.activeUsers(c, data.Task.Owner); err != nil {
		return err
	}
	w.render(rw, r, c, status, "task", t.ID+": "+t.Title, data)
	return nil
}

func (w *Web) taskView(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	return w.taskPage(rw, r, c, http.StatusOK, r.PathValue("tid"), nil)
}

func (w *Web) taskUpdate(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	tid := r.PathValue("tid")
	f := readTaskForm(r)
	_, err := w.svc.UpdateTask(c.me.ID, tid, f.Version, service.TaskPatch{
		Title: &f.Title, Description: &f.Description, Owner: &f.Owner,
		State: &f.State, Substate: &f.Substate, Priority: &f.Priority,
	})
	if err != nil {
		fields, current, ok := formErrors(err)
		if !ok {
			return err
		}
		status := http.StatusUnprocessableEntity
		if _, isT := current.(*model.Task); isT {
			status = http.StatusConflict
		}
		return w.taskPage(rw, r, c, status, tid, func(d *taskViewData) {
			d.Form, d.Errors, d.EditingOpen = f, fields, true
			if cur, isT := current.(*model.Task); isT {
				d.Conflict = cur
				d.Form.Version = cur.Version
			}
		})
	}
	return redirect(rw, r, "/tasks/"+tid)
}

// Comments.

func (w *Web) commentFailed(rw http.ResponseWriter, r *http.Request, c *ctx, tid string, cs commentState, err error) error {
	fields, current, ok := formErrors(err)
	if !ok {
		return err
	}
	cs.Error = fields["text"]
	status := http.StatusUnprocessableEntity
	if cur, isC := current.(model.Comment); isC {
		status = http.StatusConflict
		cs.Conflict = &cur
		cs.Error = "Someone changed this comment while you were editing it. Its current text is shown; submit again to replace it."
	}
	return w.taskPage(rw, r, c, status, tid, func(d *taskViewData) { d.Comment = cs })
}

func (w *Web) commentAdd(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	tid := r.PathValue("tid")
	text := normalizeNewlines(r.PostForm.Get("text"))
	cm, err := w.svc.AddComment(c.me.ID, tid, text)
	if err != nil {
		return w.commentFailed(rw, r, c, tid, commentState{Text: text}, err)
	}
	return redirect(rw, r, "/tasks/"+tid+"#comment-"+strconv.Itoa(cm.ID))
}

func (w *Web) commentEdit(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	tid := r.PathValue("tid")
	cid, err := strconv.Atoi(r.PathValue("cid"))
	if err != nil {
		return service.ErrNotFound
	}
	text := normalizeNewlines(r.PostForm.Get("text"))
	_, err = w.svc.EditComment(c.me.ID, tid, cid, formInt(r, "version"), text)
	if err != nil {
		return w.commentFailed(rw, r, c, tid, commentState{ID: cid, Text: text}, err)
	}
	return redirect(rw, r, "/tasks/"+tid+"#comment-"+strconv.Itoa(cid))
}

func (w *Web) commentDelete(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	tid := r.PathValue("tid")
	cid, err := strconv.Atoi(r.PathValue("cid"))
	if err != nil {
		return service.ErrNotFound
	}
	if err := w.svc.DeleteComment(c.me.ID, tid, cid, formInt(r, "version")); err != nil {
		return w.commentFailed(rw, r, c, tid, commentState{ID: cid}, err)
	}
	return redirect(rw, r, "/tasks/"+tid+"#comments")
}

// Users.

type userForm struct {
	ID      string
	Name    string
	Email   string
	Role    string
	Active  bool
	Version int
}

type userFormData struct {
	New      bool
	Form     userForm
	Errors   map[string]string
	Conflict *model.User
}

type passwordData struct {
	User     *model.User
	Password string
	Reset    bool
}

func (w *Web) userList(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	us, err := w.svc.Users(c.me.ID)
	if err != nil {
		return err
	}
	w.render(rw, r, c, http.StatusOK, "users", "Users", us)
	return nil
}

func (w *Web) userNew(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	if c.me.Role != model.RoleAdmin {
		return service.ErrForbidden
	}
	w.render(rw, r, c, http.StatusOK, "user_form", "New user",
		userFormData{New: true, Form: userForm{Role: model.RoleMember, Active: true}})
	return nil
}

func (w *Web) userCreate(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	f := userForm{
		ID:     strings.TrimSpace(r.PostForm.Get("id")),
		Name:   r.PostForm.Get("name"),
		Email:  strings.TrimSpace(r.PostForm.Get("email")),
		Role:   r.PostForm.Get("role"),
		Active: true,
	}
	u, password, err := w.svc.CreateUser(c.me.ID, service.NewUser{ID: f.ID, Name: f.Name, Email: f.Email, Role: f.Role})
	if err != nil {
		fields, _, ok := formErrors(err)
		if !ok {
			return err
		}
		w.render(rw, r, c, http.StatusUnprocessableEntity, "user_form", "New user",
			userFormData{New: true, Form: f, Errors: fields})
		return nil
	}
	// Shown directly rather than after a redirect: the password is never
	// stored, so this response is the only place it exists.
	w.render(rw, r, c, http.StatusCreated, "password", "User created", passwordData{User: u, Password: password})
	return nil
}

func (w *Web) userEdit(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	if c.me.Role != model.RoleAdmin {
		return service.ErrForbidden
	}
	u, err := w.svc.User(c.me.ID, r.PathValue("uid"))
	if err != nil {
		return err
	}
	w.render(rw, r, c, http.StatusOK, "user_form", "Edit "+u.ID, userFormData{
		Form: userForm{u.ID, u.Name, u.Email, u.Role, u.Active, u.Version},
	})
	return nil
}

func (w *Web) userUpdate(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	f := userForm{
		ID:      r.PathValue("uid"),
		Name:    r.PostForm.Get("name"),
		Email:   strings.TrimSpace(r.PostForm.Get("email")),
		Role:    r.PostForm.Get("role"),
		Active:  r.PostForm.Get("active") == "on",
		Version: formInt(r, "version"),
	}
	_, err := w.svc.UpdateUser(c.me.ID, f.ID, f.Version, service.UserPatch{
		Name: &f.Name, Email: &f.Email, Role: &f.Role, Active: &f.Active,
	})
	if err != nil {
		fields, current, ok := formErrors(err)
		if !ok {
			return err
		}
		status := http.StatusUnprocessableEntity
		data := userFormData{Form: f, Errors: fields}
		if cur, isU := current.(*model.User); isU {
			status = http.StatusConflict
			data.Conflict = cur
			data.Form.Version = cur.Version
		}
		w.render(rw, r, c, status, "user_form", "Edit "+f.ID, data)
		return nil
	}
	return redirect(rw, r, "/users")
}

func (w *Web) userResetPassword(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	uid := r.PathValue("uid")
	password, err := w.svc.ResetPassword(c.me.ID, uid)
	if err != nil {
		return err
	}
	u, err := w.svc.User(c.me.ID, uid)
	if err != nil {
		return err
	}
	w.render(rw, r, c, http.StatusOK, "password", "Password reset", passwordData{User: u, Password: password, Reset: true})
	return nil
}

// My account: password and API tokens.

type accountData struct {
	MustChange    bool
	Changed       bool
	PasswordError string
	Tokens        []service.TokenInfo
	TokenName     string
	TokenError    string
	NewToken      string // shown once, right after creation
}

func (w *Web) accountPage(rw http.ResponseWriter, r *http.Request, c *ctx, status int, data accountData) error {
	ts, err := w.svc.Tokens(c.me.ID)
	if err != nil {
		return err
	}
	data.Tokens = ts
	data.MustChange = c.session.mustChange
	w.render(rw, r, c, status, "account", "My account", data)
	return nil
}

func (w *Web) account(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	return w.accountPage(rw, r, c, http.StatusOK, accountData{Changed: r.URL.Query().Has("changed")})
}

func (w *Web) accountPassword(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	current, password := r.PostForm.Get("current"), r.PostForm.Get("password")
	var err error
	if password != r.PostForm.Get("confirm") {
		err = &service.ValidationError{Fields: []*model.FieldError{{Field: "password", Message: "the two new passwords differ"}}}
	} else {
		err = w.svc.ChangePassword(c.me.ID, current, password)
	}
	if err != nil {
		msg := "Current password is wrong."
		if fields, _, ok := formErrors(err); ok {
			msg = "New password " + fields["password"] + "."
		} else if !errors.Is(err, service.ErrBadCredentials) {
			return err
		}
		return w.accountPage(rw, r, c, http.StatusUnprocessableEntity, accountData{PasswordError: msg})
	}
	w.sessions.passwordChanged(c.me.ID)
	return redirect(rw, r, "/account?changed")
}

func (w *Web) tokenCreate(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	name := strings.TrimSpace(r.PostForm.Get("name"))
	_, secret, err := w.svc.CreateToken(c.me.ID, name)
	if err != nil {
		fields, _, ok := formErrors(err)
		if !ok {
			return err
		}
		return w.accountPage(rw, r, c, http.StatusUnprocessableEntity,
			accountData{TokenName: name, TokenError: "Name " + fields["name"] + "."})
	}
	// Rendered directly, not redirected: the token exists nowhere else.
	return w.accountPage(rw, r, c, http.StatusCreated, accountData{NewToken: secret})
}

func (w *Web) tokenRevoke(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	if err := w.svc.RevokeToken(c.me.ID, r.PathValue("id")); err != nil {
		return err
	}
	return redirect(rw, r, "/account")
}
