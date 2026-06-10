package gembatesting

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// JUnitTestSuites is the root element JUnit consumers (GitHub Actions,
// GitLab, Jenkins) look for. The fields are intentionally the common
// subset every renderer we care about understands — more elaborate
// extensions (system-out, properties, categories) are out of scope here.
type JUnitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Name     string           `xml:"name,attr,omitempty"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Skipped  int              `xml:"skipped,attr"`
	Suites   []JUnitTestSuite `xml:"testsuite"`
}

// JUnitTestSuite represents one conformance group.
type JUnitTestSuite struct {
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Skipped  int             `xml:"skipped,attr"`
	Cases    []JUnitTestCase `xml:"testcase"`
}

// JUnitTestCase represents one probe. Exactly one of Failure / Skipped
// is set on a non-passing case.
type JUnitTestCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Skipped   *JUnitSkipped `xml:"skipped,omitempty"`
}

// JUnitFailure carries the rolled-up probe messages as a <failure>
// element. Message is the one-line summary; the body is the full
// message block so operators can see every assertion that fired.
type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// JUnitSkipped carries the skip reason.
type JUnitSkipped struct {
	Message string `xml:"message,attr"`
}

// WriteJUnit serializes r as a JUnit XML <testsuites> document. The
// `gemba adaptor test --junit <path>` flag emits this exact format.
func WriteJUnit(w io.Writer, r *Report) error {
	suites := JUnitTestSuites{Name: fmt.Sprintf("gemba-conformance-%s", r.Plane)}
	for _, g := range r.Groups {
		suite := JUnitTestSuite{Name: g.Name}
		for _, p := range g.Probes {
			tc := JUnitTestCase{
				Name:      p.Name,
				ClassName: g.Name,
			}
			switch {
			case p.Skipped:
				suite.Skipped++
				suites.Skipped++
				tc.Skipped = &JUnitSkipped{Message: firstOrEmpty(p.Messages)}
			case !p.Passed:
				suite.Failures++
				suites.Failures++
				body := strings.Join(p.Messages, "\n")
				tc.Failure = &JUnitFailure{
					Message: firstOrEmpty(p.Messages),
					Body:    body,
				}
			}
			suite.Cases = append(suite.Cases, tc)
			suite.Tests++
			suites.Tests++
		}
		if g.NotApplicable && len(g.Probes) == 0 {
			// JUnit has no native "not applicable" concept; surface as a
			// single skipped case so CI still lists the group.
			suite.Cases = append(suite.Cases, JUnitTestCase{
				Name:      "group_not_applicable",
				ClassName: g.Name,
				Skipped:   &JUnitSkipped{Message: g.Note},
			})
			suite.Tests++
			suite.Skipped++
			suites.Tests++
			suites.Skipped++
		}
		suites.Suites = append(suites.Suites, suite)
	}
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(suites); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	return nil
}

func firstOrEmpty(msgs []string) string {
	if len(msgs) == 0 {
		return ""
	}
	return msgs[0]
}
