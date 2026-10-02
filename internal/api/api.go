// Package api is DockIt's REST interface: JSON over HTTP under /api/v1.  It
// translates HTTP to service calls and service errors to HTTP; every rule
// lives in the service.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/ratelimit"
	"github.com/pdutton/DockIt/internal/service"
)

// Prefix is where the API is mounted.
const Prefix = "/api/v1"

// maxBody bounds request bodies.  The largest legitimate body is a markdown
// field of 64 KiB, JSON-escaped.
const maxBody = 1 << 20

// Failed token attempts allowed per client address.  Tokens are 256-bit
// random values, so this only exists to keep scanners from hammering us.
const (
	failMax    = 20
	failWindow = time.Minute
)

// Options configures the API.
type Options struct {
	// DevUser, if set, treats every request as coming from this user, with
	// no authentication.  For local development only; the caller must
	// ensure the server is bound to localhost.
	DevUser string
	Logger  *slog.Logger
}

// API serves the REST interface.
type API struct {
	svc      *service.Service
	opts     Options
	log      *slog.Logger
	failures *ratelimit.Limiter
	mux      *http.ServeMux
}

// New returns the REST handler.
func New(svc *service.Service, opts Options) *API {
	a := &API{
		svc:      svc,
		opts:     opts,
		log:      opts.Logger,
		failures: ratelimit.New(failMax, failWindow),
		mux:      http.NewServeMux(),
	}
	if a.log == nil {
		a.log = slog.New(slog.DiscardHandler)
	}
	a.routes()
	return a
}

// handlerFunc is an authenticated handler.  It returns an error to be
// reported to the client, or nil once it has written a response.
type handlerFunc func(w http.ResponseWriter, r *http.Request, actor string) error

func (a *API) routes() {
	h := func(pattern string, f handlerFunc) {
		a.mux.Handle(pattern, a.authenticated(f))
	}
	h("GET "+Prefix+"/projects", a.listProjects)
	h("POST "+Prefix+"/projects", a.createProject)
	h("GET "+Prefix+"/projects/{pid}", a.getProject)
	h("PATCH "+Prefix+"/projects/{pid}", a.updateProject)

	h("GET "+Prefix+"/projects/{pid}/tasks", a.listTasks)
	h("POST "+Prefix+"/projects/{pid}/tasks", a.createTask)
	h("GET "+Prefix+"/tasks/{tid}", a.getTask)
	h("PATCH "+Prefix+"/tasks/{tid}", a.updateTask)
	h("POST "+Prefix+"/tasks/{tid}/transitions/{action}", a.transitionTask)

	h("GET "+Prefix+"/tasks/{tid}/comments", a.listComments)
	h("POST "+Prefix+"/tasks/{tid}/comments", a.addComment)
	h("GET "+Prefix+"/tasks/{tid}/comments/{cid}", a.getComment)
	h("PATCH "+Prefix+"/tasks/{tid}/comments/{cid}", a.editComment)
	h("DELETE "+Prefix+"/tasks/{tid}/comments/{cid}", a.deleteComment)

	h("GET "+Prefix+"/tasks/{tid}/links", a.listLinks)
	h("POST "+Prefix+"/tasks/{tid}/links", a.addLink)
	h("GET "+Prefix+"/tasks/{tid}/links/{type}/{other}", a.getLink)
	h("DELETE "+Prefix+"/tasks/{tid}/links/{type}/{other}", a.removeLink)

	h("GET "+Prefix+"/users", a.listUsers)
	h("POST "+Prefix+"/users", a.createUser)
	h("GET "+Prefix+"/users/{uid}", a.getUser)
	h("PATCH "+Prefix+"/users/{uid}", a.updateUser)
	h("POST "+Prefix+"/users/{uid}/password", a.resetPassword)

	h("GET "+Prefix+"/me", a.getMe)
	h("GET "+Prefix+"/me/tokens", a.listTokens)
	h("POST "+Prefix+"/me/tokens", a.createToken)
	h("DELETE "+Prefix+"/me/tokens/{id}", a.revokeToken)

	h("GET "+Prefix+"/enums", a.getEnums)

	// Anything else under the prefix is a JSON 404 (or 405), not HTML.
	a.mux.HandleFunc(Prefix+"/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, &apiError{http.StatusNotFound, "not_found", "no such endpoint", ""})
	})
}

func (a *API) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	a.mux.ServeHTTP(w, r)
}

// authenticated resolves the caller from a bearer token (or the dev user)
// and runs f, reporting any error it returns.
func (a *API) authenticated(f handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, err := a.authenticate(r)
		if err == nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
			err = f(w, r, actor)
		}
		if err != nil {
			a.fail(w, r, err)
		}
	})
}

func (a *API) authenticate(r *http.Request) (string, error) {
	if a.opts.DevUser != "" {
		return a.opts.DevUser, nil
	}
	client := clientAddr(r)
	if blocked, wait := a.failures.Blocked(client); blocked {
		return "", &apiError{http.StatusTooManyRequests, "rate_limited",
			fmt.Sprintf("too many failed attempts; try again in %d seconds", int(math.Ceil(wait.Seconds()))), ""}
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		return "", errUnauthorized
	}
	u, err := a.svc.TokenLogin(strings.TrimSpace(token))
	if err != nil {
		a.failures.Fail(client)
		return "", errUnauthorized
	}
	return u.ID, nil
}

var errUnauthorized = &apiError{http.StatusUnauthorized, "unauthorized", "missing or invalid API token", ""}

// clientAddr is the address rate limits are keyed on.  Behind a reverse
// proxy every client shares the proxy's address; the limit is generous
// enough that this only slows down a flood.
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Errors.

// apiError is an error with its HTTP status and the error body's fields.
type apiError struct {
	status  int
	code    string
	message string
	field   string
}

func (e *apiError) Error() string { return e.message }

func badRequest(field, format string, args ...any) *apiError {
	return &apiError{http.StatusBadRequest, "bad_request", fmt.Sprintf(format, args...), field}
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Field   string `json:"field,omitempty"`
	} `json:"error"`
	// Current is the record as it is now, sent with a 412 so the client can
	// merge and retry.
	Current any `json:"current,omitempty"`
}

func writeError(w http.ResponseWriter, e *apiError) {
	var b errorBody
	b.Error.Code, b.Error.Message, b.Error.Field = e.code, e.message, e.field
	if e.status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="dockit"`)
	}
	writeJSON(w, e.status, b)
}

// fail maps an error from a handler or the service to a response.
func (a *API) fail(w http.ResponseWriter, r *http.Request, err error) {
	var (
		ae  *apiError
		ve  *service.ValidationError
		ce  *service.ConflictError
		mbe *http.MaxBytesError
	)
	switch {
	case errors.As(err, &ae):
		writeError(w, ae)
	case errors.As(err, &ve):
		// Report the first field; the message lists them all.
		writeError(w, &apiError{http.StatusUnprocessableEntity, "invalid", ve.Error(), ve.Fields[0].Field})
	case errors.As(err, &ce):
		var b errorBody
		b.Error.Code = "conflict"
		b.Error.Message = "the record was changed by someone else; the current version is included"
		b.Current = ce.Current
		if v, ok := versionOf(ce.Current); ok {
			w.Header().Set("ETag", etag(v))
		}
		writeJSON(w, http.StatusPreconditionFailed, b)
	case errors.As(err, &mbe):
		writeError(w, &apiError{http.StatusRequestEntityTooLarge, "too_large", "request body too large", ""})
	case errors.Is(err, service.ErrNotFound):
		writeError(w, &apiError{http.StatusNotFound, "not_found", "not found", ""})
	case errors.Is(err, service.ErrForbidden):
		writeError(w, &apiError{http.StatusForbidden, "forbidden", "permission denied", ""})
	case errors.Is(err, service.ErrExists):
		writeError(w, &apiError{http.StatusConflict, "exists", "already exists", "id"})
	case errors.Is(err, service.ErrWrongState):
		writeError(w, &apiError{http.StatusConflict, "wrong_state", err.Error(), ""})
	case errors.Is(err, service.ErrVersionRequired):
		writeError(w, &apiError{http.StatusPreconditionRequired, "precondition_required",
			"send If-Match with the ETag of the version this change is based on", ""})
	default:
		a.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, &apiError{http.StatusInternalServerError, "internal", "internal error", ""})
	}
}

// JSON helpers.

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}

// writeRecord writes a versioned record with its ETag.
func writeRecord(w http.ResponseWriter, status int, v any) error {
	if ver, ok := versionOf(v); ok {
		w.Header().Set("ETag", etag(ver))
	}
	writeJSON(w, status, v)
	return nil
}

// readJSON decodes the request body into v, rejecting unknown fields and
// trailing data.
func readJSON(r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "application/json") &&
		!strings.HasPrefix(ct, "application/merge-patch+json") {
		return &apiError{http.StatusUnsupportedMediaType, "unsupported_media_type", "send application/json", ""}
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return err
		}
		if errors.Is(err, io.EOF) {
			return badRequest("", "request body required")
		}
		return badRequest(jsonField(err), "invalid JSON: %v", err)
	}
	if dec.More() {
		return badRequest("", "invalid JSON: trailing data after the object")
	}
	return nil
}

// jsonField extracts the offending field from a decode error, if any.
func jsonField(err error) string {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		return te.Field
	}
	if f, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		return strings.Trim(f, `"`)
	}
	return ""
}

// Versions and ETags.

func etag(v int) string { return `"` + strconv.Itoa(v) + `"` }

func versionOf(v any) (int, bool) {
	switch r := v.(type) {
	case *model.Project:
		return r.Version, true
	case *model.Task:
		return r.Version, true
	case *model.User:
		return r.Version, true
	case *model.Comment:
		return r.Version, true
	case model.Comment:
		return r.Version, true
	}
	return 0, false
}

// ifMatch returns the version named by the If-Match header, or 0 if there is
// none (which the service reports as 428).
func ifMatch(r *http.Request) (int, error) {
	h := strings.TrimSpace(r.Header.Get("If-Match"))
	if h == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(strings.Trim(h, `"`))
	if err != nil || v < 1 || h != etag(v) {
		return 0, badRequest("", `If-Match must be a single ETag such as "7"`)
	}
	return v, nil
}

// pathInt parses an integer path parameter.
func pathInt(r *http.Request, name string) (int, error) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil {
		return 0, service.ErrNotFound
	}
	return n, nil
}
