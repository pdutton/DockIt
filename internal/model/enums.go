package model

// EnumValue is one entry in a built-in enumeration: a stable id stored in the
// dataset and a display string that may change between releases.
type EnumValue struct {
	ID      string
	Display string
}

// Enum is a built-in enumeration.  Values are kept in their built-in order,
// which is also the order used when writing keyed data such as project URLs.
type Enum struct {
	name   string
	values []EnumValue
}

func newEnum(name string, values ...EnumValue) *Enum {
	return &Enum{name: name, values: values}
}

// Name is a human-readable name for the enumeration, used in error messages.
func (e *Enum) Name() string { return e.name }

// Values returns the enumeration's values in built-in order.
func (e *Enum) Values() []EnumValue {
	return append([]EnumValue(nil), e.values...)
}

// Valid reports whether id is a known value.
func (e *Enum) Valid(id string) bool {
	return e.Index(id) >= 0
}

// Index returns the position of id in built-in order, or -1 if it is unknown.
func (e *Enum) Index(id string) int {
	for i, v := range e.values {
		if v.ID == id {
			return i
		}
	}
	return -1
}

// Display returns the display string for id.  An unknown id is returned as-is
// with ok set to false, so callers can show it raw with a warning marker.
func (e *Enum) Display(id string) (display string, ok bool) {
	for _, v := range e.values {
		if v.ID == id {
			return v.Display, true
		}
	}
	return id, false
}

// Project states.
const (
	ProjectPlanned  = "planned"
	ProjectActive   = "active"
	ProjectInactive = "inactive"
	ProjectDormant  = "dormant"
	ProjectComplete = "complete"
)

// Task states.
const (
	TaskNew        = "new"
	TaskInProgress = "in_progress"
	TaskDeferred   = "deferred"
	TaskPaused     = "paused"
	TaskComplete   = "complete"
)

// Substates of TaskComplete.
const (
	SubstateDone     = "done"
	SubstateRejected = "rejected"
)

// Task types.
const (
	TypeBugfix        = "bugfix"
	TypeEnhancement   = "enhancement"
	TypeFeature       = "feature"
	TypeTask          = "task"
	TypeDocumentation = "documentation"
	TypeResearch      = "research"
)

// URL types.
const (
	URLCode = "code"
	URLDoc  = "doc"
	URLWeb  = "web"

	URLPR = "pr" // tasks only
)

// Roles.
const (
	RoleViewer = "viewer"
	RoleMember = "member"
	RoleAdmin  = "admin"
)

var (
	ProjectStates = newEnum("project state",
		EnumValue{ProjectPlanned, "Planned"},
		EnumValue{ProjectActive, "Active"},
		EnumValue{ProjectInactive, "Inactive"},
		EnumValue{ProjectDormant, "Dormant"},
		EnumValue{ProjectComplete, "Complete"},
	)

	TaskStates = newEnum("task state",
		EnumValue{TaskNew, "New"},
		EnumValue{TaskInProgress, "In Progress"},
		EnumValue{TaskDeferred, "Deferred"},
		EnumValue{TaskPaused, "Paused"},
		EnumValue{TaskComplete, "Complete"},
	)

	TaskTypes = newEnum("task type",
		EnumValue{TypeBugfix, "Bug Fix"},
		EnumValue{TypeEnhancement, "Enhancement"},
		EnumValue{TypeFeature, "Feature"},
		EnumValue{TypeTask, "Task"},
		EnumValue{TypeDocumentation, "Documentation"},
		EnumValue{TypeResearch, "Research"},
	)

	// Substates maps a task state to its substates.  A state that is absent
	// has no substates.
	Substates = map[string]*Enum{
		TaskComplete: newEnum("complete substate",
			EnumValue{SubstateDone, "Done"},
			EnumValue{SubstateRejected, "Rejected"},
		),
	}

	URLTypes = newEnum("URL type",
		EnumValue{URLCode, "Code"},
		EnumValue{URLDoc, "Documentation"},
		EnumValue{URLWeb, "Website"},
	)

	// TaskURLTypes are the URL types a task may have.  URLTypes are for
	// projects.
	TaskURLTypes = newEnum("task URL type",
		EnumValue{URLPR, "Pull Requests"},
	)

	Roles = newEnum("role",
		EnumValue{RoleViewer, "Viewer"},
		EnumValue{RoleMember, "Member"},
		EnumValue{RoleAdmin, "Admin"},
	)
)
