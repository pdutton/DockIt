package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// Every form carries a submission key, fresh each time a page is rendered.
// A POST that repeats an earlier one exactly, key and all, is not acted on
// again: it waits for the first to finish and gets the same response.  A
// double click would otherwise create a task twice, or report a conflict
// with the user's own first click.  Because the whole form is compared, a
// form changed and sent again counts as a new submission.

// submissionKeep is how long a finished submission's response is kept to
// answer repeats.
const submissionKeep = time.Minute

type submission struct {
	done     chan struct{} // closed when the first request finishes
	resp     *recorder     // nil if the first request did not finish
	finished time.Time
}

type submissions struct {
	mu  sync.Mutex
	m   map[string]*submission
	now func() time.Time
}

func newSubmissions() *submissions {
	return &submissions{m: make(map[string]*submission), now: time.Now}
}

// submissionID identifies a form submission within a session.  Callers
// have parsed the form.
func submissionID(ss *session, r *http.Request) string {
	sum := sha256.Sum256([]byte(r.URL.Path + "?" + r.PostForm.Encode()))
	return ss.id + " " + hex.EncodeToString(sum[:])
}

// claim returns the submission for id, and whether the caller is the first
// to send it and so must run it.
func (s *submissions) claim(id string) (*submission, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	if sub := s.m[id]; sub != nil {
		return sub, false
	}
	sub := &submission{done: make(chan struct{})}
	s.m[id] = sub
	return sub, true
}

// run runs the first request of a submission and records its response.  If
// the request panics, the submission is forgotten, so a repeat runs afresh.
func (s *submissions) run(id string, sub *submission, f func(http.ResponseWriter)) *recorder {
	var resp *recorder
	defer func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if resp == nil {
			delete(s.m, id)
		} else {
			sub.resp, sub.finished = resp, s.now()
		}
		close(sub.done)
	}()
	rec := &recorder{header: http.Header{}}
	f(rec)
	resp = rec
	return resp
}

// expire drops finished submissions older than submissionKeep.  Caller
// holds s.mu.
func (s *submissions) expire() {
	for id, sub := range s.m {
		if sub.resp != nil && s.now().Sub(sub.finished) > submissionKeep {
			delete(s.m, id)
		}
	}
}

// once answers a form submission with f, unless the same submission has
// been sent before, in which case it waits for that one's response and
// sends it again.
func (w *Web) once(rw http.ResponseWriter, r *http.Request, id string, f func(http.ResponseWriter)) {
	for {
		sub, first := w.submissions.claim(id)
		if first {
			w.submissions.run(id, sub, f).replay(rw)
			return
		}
		select {
		case <-sub.done:
		case <-r.Context().Done():
			return
		}
		if sub.resp != nil {
			sub.resp.replay(rw)
			return
		}
		// The first request did not finish; claim the submission again.
	}
}

// recorder buffers a response so that it can be sent more than once.
type recorder struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (rec *recorder) Header() http.Header { return rec.header }

func (rec *recorder) WriteHeader(status int) {
	if rec.status == 0 {
		rec.status = status
	}
}

func (rec *recorder) Write(b []byte) (int, error) {
	rec.WriteHeader(http.StatusOK)
	return rec.body.Write(b)
}

// replay sends the recorded response.
func (rec *recorder) replay(rw http.ResponseWriter) {
	h := rw.Header()
	for k, v := range rec.header.Clone() {
		h[k] = v
	}
	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	rw.WriteHeader(status)
	rw.Write(rec.body.Bytes())
}
