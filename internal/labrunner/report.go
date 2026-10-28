package labrunner

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
)

// Report separates scenario results from cleanup and restoration outcomes.
type Report struct {
	History            []Result    `json:"history"`
	RunID              string      `json:"run_id"`
	PlanHash           string      `json:"plan_hash"`
	Complete           bool        `json:"complete"`
	Success            bool        `json:"success"`
	Summary            string      `json:"summary"`
	Results            []Result    `json:"results"`
	RemainingResources []Resource  `json:"remaining_resources"`
	Targets            []Target    `json:"targets"`
	Artifacts          []Artifact  `json:"artifacts"`
	Tests              []Selection `json:"tests"`
}

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Cases    []junitCase `xml:"testcase"`
}
type junitCase struct {
	Name    string        `xml:"name,attr"`
	Class   string        `xml:"classname,attr"`
	Time    float64       `xml:"time,attr"`
	Failure *junitFailure `xml:"failure,omitempty"`
	Skipped *string       `xml:"skipped,omitempty"`
}
type junitFailure struct {
	Type    string `xml:"type,attr"`
	Message string `xml:",chardata"`
}

// WriteReport writes both machine formats, including explicit incomplete cases.
func WriteReport(dir string, r Report) error {
	if err := replaceJSON(filepath.Join(dir, "report.json"), r); err != nil {
		return err
	}
	suite := junitSuite{Name: r.RunID, Tests: len(r.Results)}
	for _, x := range r.Results {
		c := junitCase{Name: x.ID, Class: x.Phase}
		if !x.Started.IsZero() && !x.Finished.IsZero() {
			c.Time = x.Finished.Sub(x.Started).Seconds()
		}
		switch x.Status {
		case "passed":
		case "skipped":
			reason := "not applicable"
			c.Skipped = &reason
		default:
			suite.Failures++
			kind := x.Category
			if kind == "" {
				kind = "incomplete"
			}
			c.Failure = &junitFailure{Type: kind, Message: x.Status + ": " + x.Message}
		}
		suite.Cases = append(suite.Cases, c)
	}
	// A recovered teardown is a successful recovery action, but its earlier
	// failure still makes this run unsuccessful. Keep that visible in JUnit.
	for i, previous := range r.History {
		if previous.Status != "failed" {
			continue
		}
		stillFailed := false
		for _, current := range r.Results {
			stillFailed = stillFailed || current.ID == previous.ID && current.Status == "failed"
		}
		if stillFailed {
			continue
		}
		suite.Failures++
		suite.Cases = append(suite.Cases, junitCase{
			Name: fmt.Sprintf("%s.previous-failure-%d", previous.ID, i+1), Class: previous.Phase,
			Failure: &junitFailure{Type: previous.Category, Message: previous.Message},
		})
	}
	suite.Tests = len(suite.Cases)
	b, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".junit-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err = f.Write(append([]byte(xml.Header), b...)); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(dir, "report.xml")); err != nil {
		return err
	}
	return syncDirectory(dir)
}

func reportFor(p Plan, s State) Report {
	r := Report{RunID: p.RunID, PlanHash: s.PlanHash, Complete: s.Complete, Success: s.Complete && !s.Aborted, Results: s.Results, History: s.History, Targets: p.Targets, Artifacts: p.Artifacts, Tests: p.Tests}
	for _, x := range s.Results {
		if x.Status != "passed" && x.Status != "skipped" {
			r.Success = false
		}
	}
	for _, x := range s.History {
		if x.Status == "failed" {
			r.Success = false
		}
	}
	if !r.Success || p.PreserveFailures && s.Aborted {
		r.RemainingResources = p.Resources
	}
	r.Summary = fmt.Sprintf("run %s: complete=%t success=%t; %d declared resources may remain; targets are reusable and never deleted", p.RunID, r.Complete, r.Success, len(r.RemainingResources))
	return r
}
