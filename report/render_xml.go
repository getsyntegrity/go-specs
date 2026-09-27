package report

import (
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

// JUnit XML shape. Every text/attribute value is written through encoding/xml, which escapes
// it — a Message or Output containing "<", "&", or raw control bytes cannot corrupt the document
// or inject markup a consumer would parse as structure.
type junitTestSuites struct {
	XMLName  xml.Name         `xml:"testsuites"`
	Tests    int              `xml:"tests,attr"`
	Failures int              `xml:"failures,attr"`
	Errors   int              `xml:"errors,attr"`
	Skipped  int              `xml:"skipped,attr"`
	Time     string           `xml:"time,attr"`
	Suites   []junitTestSuite `xml:"testsuite"`
}

type junitTestSuite struct {
	XMLName    xml.Name        `xml:"testsuite"`
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr"`
	Failures   int             `xml:"failures,attr"`
	Errors     int             `xml:"errors,attr"`
	Skipped    int             `xml:"skipped,attr"`
	Time       string          `xml:"time,attr"`
	Properties []junitProperty `xml:"properties>property"`
	TestCases  []junitTestCase `xml:"testcase"`
}

type junitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitTestCase struct {
	XMLName   xml.Name `xml:"testcase"`
	Name      string   `xml:"name,attr"`
	ClassName string   `xml:"classname,attr"`
	Time      string   `xml:"time,attr"`
	// Hook marks a synthetic group hook case ("BeforeAll"/"AfterAll") as a structural attribute,
	// omitted entirely for a real spec (issue #207 H8) so a suite with no group hooks renders no
	// hook attribute anywhere. JUnit has no native vocabulary for this, so it is a plain custom
	// attribute rather than an invented status.
	Hook    string        `xml:"hook,attr,omitempty"`
	Failure *junitFailure `xml:"failure,omitempty"`
	Error   *junitFailure `xml:"error,omitempty"`
	Skipped *junitSkipped `xml:"skipped,omitempty"`
}

// junitFailure covers both <failure> and <error>: JUnit gives them the same shape (an optional
// message attribute plus free-text body), so one struct renders as either element name depending
// on which field of junitTestCase holds it.
type junitFailure struct {
	Message string `xml:"message,attr,omitempty"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// RenderXML writes r as JUnit-compatible XML: one <testsuites> containing one <testsuite> per
// Suite, each carrying the module's coverage as deterministic <properties> (see
// coverageProperties) and one <testcase> per Case, mapped per the table in issue #141 —
// Failed -> <failure>, Error -> <error>, Skipped/Filtered -> <skipped>. Pending has no JUnit
// equivalent, so it renders as <skipped message="pending"/> and folds into the same skipped
// attribute, exactly as Filtered does — see renderJUnitCase and issue #208.
func RenderXML(w io.Writer, r NormalizedReport) error {
	doc := junitTestSuites{
		Tests:    r.Execution.Total,
		Failures: r.Execution.Failed,
		Errors:   r.Execution.Error,
		Skipped:  r.Execution.Skipped + r.Execution.Filtered + r.Execution.Pending,
		Time:     formatSeconds(r.Duration),
	}
	props := coverageProperties(r.Coverage)
	for _, s := range r.Suites {
		js := junitTestSuite{
			Name:       s.Name,
			Tests:      s.Totals.Total,
			Failures:   s.Totals.Failed,
			Errors:     s.Totals.Error,
			Skipped:    s.Totals.Skipped + s.Totals.Filtered + s.Totals.Pending,
			Time:       formatSeconds(s.Duration),
			Properties: props,
		}
		for _, c := range s.Cases {
			js.TestCases = append(js.TestCases, renderJUnitCase(s.Name, c))
		}
		doc.Suites = append(doc.Suites, js)
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}

func renderJUnitCase(suiteName string, c Case) junitTestCase {
	tc := junitTestCase{Name: c.Name, ClassName: junitClassName(suiteName, c.Path, c.Hook != ""), Time: formatSeconds(c.Duration), Hook: c.Hook}
	switch c.Status {
	case StatusFailed:
		tc.Failure = &junitFailure{Message: c.Message, Body: c.Output}
	case StatusError:
		tc.Error = &junitFailure{Message: c.Message, Body: c.Output}
	case StatusSkipped:
		tc.Skipped = &junitSkipped{}
	case StatusFiltered:
		tc.Skipped = &junitSkipped{Message: "filtered"}
	case StatusPending:
		tc.Skipped = &junitSkipped{Message: "pending"}
	}
	return tc
}

// junitClassName derives a JUnit classname from a case's Path.
//
// For a real spec, Path ends in the spec's own name (SpecStartEvent.Path's doc in events.go), so
// the classname is the enclosing scopes only: c.Path[:len(c.Path)-1] (outermost first), joined
// with "/". Two specs with the same leaf name declared under different When/Describe branches
// therefore get different classnames, so their (classname, name) pair stays unique. Falls back to
// suiteName when Path carries no scopes at all (e.g. a Case built without Path, outside the normal
// collector path).
//
// For a synthetic group hook case (isHook true), Path is already the GROUP path — there is no
// trailing "own name" element to drop, because the case's Name is the bracketed hook marker
// ("[BeforeAll]"/"[AfterAll]"), not a scope. Dropping the last element there collapsed a real group
// level: "[BeforeAll]" of Checkout/when cart has items got classname "Checkout" instead of
// "Checkout/when cart has items", so two sibling groups' hook cases could end up sharing the same
// (classname, name) pair (issue #207, docs/SUITE_HOOKS_CONTRACT.md H8). A hook case's classname is
// therefore the full Path, joined with "/", falling back to suiteName when Path is empty.
func junitClassName(suiteName string, path []string, isHook bool) string {
	if isHook {
		if len(path) == 0 {
			return suiteName
		}
		return strings.Join(path, "/")
	}
	if len(path) <= 1 {
		return suiteName
	}
	return strings.Join(path[:len(path)-1], "/")
}

// coverageProperties renders Coverage as deterministic <property> entries (sorted by
// Coverage.Packages, which ParseCoverageProfile already returns in ImportPath order): one
// aggregate covered/total/percentage triple, then one percentage entry per package. Returns nil
// when no coverage was attached, so a report with no coverage requested emits no <properties>
// block at all rather than a misleading 0%.
func coverageProperties(cov Coverage) []junitProperty {
	if len(cov.Packages) == 0 {
		return nil
	}
	props := []junitProperty{
		{Name: "go-specs.coverage.aggregate.covered", Value: strconv.Itoa(cov.Total.Covered)},
		{Name: "go-specs.coverage.aggregate.total", Value: strconv.Itoa(cov.Total.Total)},
		{Name: "go-specs.coverage.aggregate.percentage", Value: formatPercentage(cov.Total.Percentage())},
	}
	for _, p := range cov.Packages {
		props = append(props, junitProperty{
			Name:  "go-specs.coverage.package." + p.ImportPath,
			Value: formatPercentage(p.Percentage()),
		})
	}
	return props
}
