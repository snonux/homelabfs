// Package behavior exercises bounded, observable Unix filesystem semantics.
// It is independent of the filesystem implementation under test.
package behavior

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"time"
)

// Result is one independently reported semantic check.
type Result struct {
	Name    string  `json:"name"`
	Family  string  `json:"family"`
	Status  string  `json:"status"`
	Detail  string  `json:"detail,omitempty"`
	Seconds float64 `json:"seconds"`
}

// Report describes exactly one target/profile; skips never count as passes.
type Report struct {
	// Fixture records exact owning harness inputs; standalone targets omit it.
	Fixture      map[string]string `json:"fixture,omitempty"`
	Target       string            `json:"target"`
	Profile      string            `json:"profile"`
	Identity     string            `json:"expected_identity,omitempty"`
	Capabilities map[string]bool   `json:"declared_capabilities,omitempty"`
	Platform     string            `json:"platform"`
	RunDirectory string            `json:"run_directory"`
	Started      time.Time         `json:"started"`
	Scope        string            `json:"scope"`
	Results      []Result          `json:"results"`
}

type junitSuite struct {
	XMLName    xml.Name        `xml:"testsuite"`
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr"`
	Failures   int             `xml:"failures,attr"`
	Skipped    int             `xml:"skipped,attr"`
	Properties []junitProperty `xml:"properties>property"`
	Cases      []junitCase     `xml:"testcase"`
}
type junitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}
type junitCase struct {
	Name    string  `xml:"name,attr"`
	Class   string  `xml:"classname,attr"`
	Time    float64 `xml:"time,attr"`
	Failure *string `xml:"failure,omitempty"`
	Skipped *string `xml:"skipped,omitempty"`
}

// Failed reports any failed check, including infrastructure/cleanup errors.
func (r Report) Failed() bool {
	for _, c := range r.Results {
		if c.Status == "fail" {
			return true
		}
	}
	return false
}

// JSON writes the machine-readable report.
func (r Report) JSON(w io.Writer) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	return e.Encode(r)
}

// JUnit writes a JUnit XML suite with explicit failures and skipped reasons.
func (r Report) JUnit(w io.Writer) error {
	fixture, err := json.Marshal(r.Fixture)
	if err != nil {
		return err
	}
	capabilities, err := json.Marshal(r.Capabilities)
	if err != nil {
		return err
	}
	s := junitSuite{Name: "homelabfs.behavior." + r.Profile, Tests: len(r.Results), Properties: []junitProperty{{"fixture", string(fixture)}, {"target", r.Target}, {"profile", r.Profile}, {"platform", r.Platform}, {"scope", r.Scope}, {"run_directory", r.RunDirectory}, {"expected_identity", r.Identity}, {"declared_capabilities", string(capabilities)}}}
	for _, c := range r.Results {
		item := junitCase{Name: c.Name, Class: c.Family, Time: c.Seconds}
		switch c.Status {
		case "pass":
		case "skip":
			s.Skipped++
			detail := c.Detail
			item.Skipped = &detail
		case "fail":
			s.Failures++
			detail := c.Detail
			item.Failure = &detail
		default:
			return fmt.Errorf("unknown case status %q", c.Status)
		}
		s.Cases = append(s.Cases, item)
	}
	e := xml.NewEncoder(w)
	e.Indent("", "  ")
	return e.Encode(s)
}
