package model

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestValidProjectID(t *testing.T) {
	for id, want := range map[string]bool{
		"WEB":         true,
		"A1":          true,
		"ABCDEFGHIJ":  true,
		"A":           false, // too short
		"ABCDEFGHIJK": false, // too long
		"web":         false, // lowercase
		"1AB":         false, // must start with a letter
		"WE-B":        false,
		"../X":        false,
		"":            false,
	} {
		if got := ValidProjectID(id); got != want {
			t.Errorf("ValidProjectID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestValidUserID(t *testing.T) {
	for id, want := range map[string]bool{
		"pdutton":                          true,
		"p.dutton-2_x":                     true,
		"ab":                               true,
		"a":                                false,
		"Pdutton":                          false,
		"2peter":                           false,
		".hidden":                          false,
		"../etc":                           false,
		"a" + strings.Repeat("b", 31):      true,
		"a" + strings.Repeat("b", 32):      false,
		"peter dutton":                     false,
		"peter/dutton":                     false,
		"peter\\dutton":                    false,
		"ab" + string(rune(0x00e9)) + "cd": false,
	} {
		if got := ValidUserID(id); got != want {
			t.Errorf("ValidUserID(%q) = %v, want %v", id, got, want)
		}
	}
}

func TestParseTaskID(t *testing.T) {
	pid, n, err := ParseTaskID("WEB-12")
	if err != nil || pid != "WEB" || n != 12 {
		t.Fatalf("ParseTaskID(WEB-12) = %q, %d, %v", pid, n, err)
	}
	if got := TaskID(pid, n); got != "WEB-12" {
		t.Errorf("TaskID round trip = %q", got)
	}
	for _, bad := range []string{"WEB", "WEB-", "WEB-0", "WEB-012", "WEB--1", "WEB-+1", "web-1", "WEB-1x", "-1", "../WEB-1"} {
		if _, _, err := ParseTaskID(bad); err == nil {
			t.Errorf("ParseTaskID(%q) succeeded, want error", bad)
		}
	}
}

func TestEnumDisplay(t *testing.T) {
	if d, ok := TaskStates.Display(TaskInProgress); !ok || d != "In Progress" {
		t.Errorf("Display(in_progress) = %q, %v", d, ok)
	}
	if d, ok := TaskStates.Display("someday"); ok || d != "someday" {
		t.Errorf("Display(unknown) = %q, %v; want raw id and false", d, ok)
	}
	if !Substates[TaskComplete].Valid(SubstateRejected) {
		t.Error("rejected is not a substate of complete")
	}
	if _, ok := Substates[TaskNew]; ok {
		t.Error("new has substates")
	}
}

func TestURLsMarshalOrder(t *testing.T) {
	p := struct {
		URLs URLs `yaml:"urls"`
	}{URLs{
		"zzz":  {"https://unknown.example"},
		URLWeb: {"https://example.com"},
		URLDoc: {},
		URLCode: {
			"https://github.com/example/site",
			"https://github.com/example/site-infra",
		},
	}}
	out, err := yaml.Marshal(&p)
	if err != nil {
		t.Fatal(err)
	}
	want := `urls:
    code:
        - https://github.com/example/site
        - https://github.com/example/site-infra
    web:
        - https://example.com
    zzz:
        - https://unknown.example
`
	if string(out) != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}

	var back struct {
		URLs URLs `yaml:"urls"`
	}
	if err := yaml.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if got := back.URLs[URLCode]; len(got) != 2 || got[0] != "https://github.com/example/site" {
		t.Errorf("round trip lost order: %v", got)
	}
}

func TestCheckText(t *testing.T) {
	if err := CheckText("title", "Fix It", 200); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "tab\there", "line\nbreak", strings.Repeat("x", 201), "bad\xffutf8"} {
		if err := CheckText("title", bad, 200); err == nil {
			t.Errorf("CheckText(%q) succeeded", bad)
		}
	}
	// The limit counts characters, not bytes.
	if err := CheckText("title", strings.Repeat("é", 200), 200); err != nil {
		t.Error(err)
	}
}

func TestCheckEmail(t *testing.T) {
	if err := CheckEmail("peter@example.com"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"", "peter", "Peter <peter@example.com>", "a@b.com, c@d.com", " peter@example.com"} {
		if err := CheckEmail(bad); err == nil {
			t.Errorf("CheckEmail(%q) succeeded", bad)
		}
	}
}
