package service

import (
	"github.com/pdutton/DockIt/internal/model"
)

// Comments have their own versions, so adding, editing or deleting a comment
// never conflicts with an edit of the task's own fields, and vice versa.  Any
// comment change still updates the task's modified time.

// AddComment adds a comment to task tid.  Members and admins.
func (s *Service) AddComment(actor, tid, text string) (*model.Comment, error) {
	if err := checkCommentText(text); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, err := s.authorize(actor, editors)
	if err != nil {
		return nil, err
	}
	t := s.x.Task(tid)
	if t == nil {
		return nil, ErrNotFound
	}
	now := s.now()
	c := model.Comment{
		ID:        nextCommentID(t),
		Version:   1,
		Commenter: u.ID,
		Created:   now,
		Modified:  now,
		Text:      text,
	}
	t.Comments = append(t.Comments, c)
	t.Modified = now
	if err := s.writeComments(t); err != nil {
		return nil, err
	}
	return &c, nil
}

// EditComment replaces the text of comment cid on task tid.  Only the
// commenter may edit a comment; admins get no override.
func (s *Service) EditComment(actor, tid string, cid, version int, text string) (*model.Comment, error) {
	if err := checkCommentText(text); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, i, err := s.ownComment(actor, tid, cid, version)
	if err != nil {
		return nil, err
	}
	c := &t.Comments[i]
	if c.Text == text {
		return c, nil
	}
	now := s.now()
	c.Text = text
	c.Version++
	c.Modified = now
	t.Modified = now
	if err := s.writeComments(t); err != nil {
		return nil, err
	}
	return c, nil
}

// DeleteComment removes comment cid from task tid.  Only the commenter may
// delete a comment.  Its ID is never reused.
func (s *Service) DeleteComment(actor, tid string, cid, version int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, i, err := s.ownComment(actor, tid, cid, version)
	if err != nil {
		return err
	}
	t.Comments = append(t.Comments[:i], t.Comments[i+1:]...)
	t.Modified = s.now()
	return s.writeComments(t)
}

// ownComment finds a comment the actor may change, at the given version.
func (s *Service) ownComment(actor, tid string, cid, version int) (*model.Task, int, error) {
	u, err := s.authorize(actor, editors)
	if err != nil {
		return nil, 0, err
	}
	t := s.x.Task(tid)
	if t == nil {
		return nil, 0, ErrNotFound
	}
	for i := range t.Comments {
		c := &t.Comments[i]
		if c.ID != cid {
			continue
		}
		if c.Commenter != u.ID {
			return nil, 0, ErrForbidden
		}
		if err := checkVersion(version, c.Version, *c); err != nil {
			return nil, 0, err
		}
		return t, i, nil
	}
	return nil, 0, ErrNotFound
}

// nextCommentID returns one more than the highest comment ID the task has
// ever had.  Deleted comments leave the file, so the high-water mark cannot
// be recovered from the comments alone; the task keeps it in LastCommentID.
func nextCommentID(t *model.Task) int {
	n := t.LastCommentID
	for _, c := range t.Comments {
		n = max(n, c.ID)
	}
	return n + 1
}

func (s *Service) writeComments(t *model.Task) error {
	for _, c := range t.Comments {
		t.LastCommentID = max(t.LastCommentID, c.ID)
	}
	if err := validate(t.Validate(), func(f string) bool { return fieldSet{"comments": true}.has(f) }); err != nil {
		return err
	}
	if err := s.store.WriteTask(t); err != nil {
		return err
	}
	s.x.PutTask(t)
	return nil
}

func checkCommentText(text string) error {
	if text == "" {
		return invalid("text", "required")
	}
	if len(text) > model.MaxMarkdownLen {
		return invalid("text", "longer than %d bytes", model.MaxMarkdownLen)
	}
	return nil
}
