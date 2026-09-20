package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/itchyny/gojq"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sigs.k8s.io/yaml"
)

// outputFilter is the set of server-side result-shaping knobs every read
// tool accepts under its "filter" argument. It exists so an agent can cut a
// large response down to what it actually needs on the server, rather than
// spending context-window tokens receiving a payload just to discard most
// of it — the job a `| grep -i … | tail -20 | cut -c1-260` pipeline does in a
// terminal. The steps run in a fixed order: jq, then grep (with ignoreCase and
// context), then grepV, then count, then head, then tail, then maxLineLength,
// then maxBytes.
type outputFilter struct {
	Jq            string `json:"jq,omitempty" jsonschema:"jq program (github.com/itchyny/gojq syntax) run over the result server-side, e.g. '.items[] | {name, phase: .status}' or '.items | length'. A JSON result is filtered as-is; a YAML manifest result is parsed to JSON, filtered, then re-emitted as YAML. Multiple outputs come back newline-delimited, like jq -c."`
	Grep          string `json:"grep,omitempty" jsonschema:"keep only result lines matching this RE2 regular expression, e.g. 'error|failed' (applied after jq)"`
	GrepV         string `json:"grepV,omitempty" jsonschema:"drop result lines matching this RE2 regular expression (applied after grep)"`
	IgnoreCase    bool   `json:"ignoreCase,omitempty" jsonschema:"make grep and grepV case-insensitive, like grep -i"`
	Context       int    `json:"context,omitempty" jsonschema:"with grep: also keep N lines before and after every match, like grep -C N; a -- line separates groups that aren't adjacent"`
	Count         bool   `json:"count,omitempty" jsonschema:"return only the number of lines left after grep/grepV, like grep -c, instead of the lines themselves"`
	Head          int    `json:"head,omitempty" jsonschema:"keep only the first N lines of the result"`
	Tail          int    `json:"tail,omitempty" jsonschema:"keep only the last N lines of the result (applied after head if both are set)"`
	MaxLineLength int    `json:"maxLineLength,omitempty" jsonschema:"cut every line to this many characters and append …, like cut -c1-N; tames very long log or JSON lines"`
	MaxBytes      int    `json:"maxBytes,omitempty" jsonschema:"hard cap on the returned size; the result is cut at a line boundary near this many bytes and a truncation marker is appended"`
}

func (f outputFilter) isZero() bool {
	return f == outputFilter{}
}

// apply runs the filter pipeline over a successful tool result. isYAML says
// whether body is a YAML document (get_manifest / get_crd_manifest) rather
// than JSON — it only changes how the jq step reads and re-emits. An empty
// filter returns body untouched.
func (f outputFilter) apply(body []byte, isYAML bool) ([]byte, error) {
	if f.isZero() {
		return body, nil
	}
	out := body
	var err error
	if f.Jq != "" {
		if out, err = applyJq(out, f.Jq, isYAML); err != nil {
			return nil, err
		}
	}
	if f.hasLineSteps() {
		if out, err = applyLineFilters(out, f); err != nil {
			return nil, err
		}
	}
	if f.MaxBytes > 0 {
		out = capBytes(out, f.MaxBytes)
	}
	return out, nil
}

// applyJq compiles and runs program over body. For a YAML body it converts
// to JSON first and re-emits each jq output as a YAML document (joined by
// "---"); for a JSON body it emits each output as one compact JSON line,
// matching `jq -c`.
func applyJq(body []byte, program string, isYAML bool) ([]byte, error) {
	q, err := gojq.Parse(program)
	if err != nil {
		return nil, fmt.Errorf("invalid jq program: %w", err)
	}
	code, err := gojq.Compile(q)
	if err != nil {
		return nil, fmt.Errorf("invalid jq program: %w", err)
	}

	src := body
	if isYAML {
		if src, err = yaml.YAMLToJSON(body); err != nil {
			return nil, fmt.Errorf("parsing the YAML result before jq: %w", err)
		}
	}
	var input any
	if err := json.Unmarshal(src, &input); err != nil {
		return nil, fmt.Errorf("the result is not JSON, so jq cannot be applied to it: %w", err)
	}

	var outs [][]byte
	iter := code.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if e, ok := v.(error); ok {
			var halt *gojq.HaltError
			if errors.As(e, &halt) {
				break
			}
			return nil, fmt.Errorf("jq: %w", e)
		}
		enc, err := encodeJqOutput(v, isYAML)
		if err != nil {
			return nil, err
		}
		outs = append(outs, enc)
	}
	sep := []byte("\n")
	if isYAML {
		sep = []byte("---\n")
	}
	return bytes.Join(outs, sep), nil
}

func encodeJqOutput(v any, asYAML bool) ([]byte, error) {
	if asYAML {
		out, err := yaml.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("re-encoding a jq output as YAML: %w", err)
		}
		return bytes.TrimRight(out, "\n"), nil
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("re-encoding a jq output as JSON: %w", err)
	}
	return out, nil
}

// hasLineSteps reports whether any of the line-oriented steps is requested.
func (f outputFilter) hasLineSteps() bool {
	return f.Grep != "" || f.GrepV != "" || f.Count || f.Head > 0 || f.Tail > 0 || f.MaxLineLength > 0
}

// applyLineFilters runs the grep / grepV / count / head / tail /
// maxLineLength steps over body, treating it as newline-separated text. A
// single trailing newline is preserved so line counts stay intuitive.
func applyLineFilters(body []byte, f outputFilter) ([]byte, error) {
	text := string(body)
	trailingNL := strings.HasSuffix(text, "\n")
	if trailingNL {
		text = text[:len(text)-1]
	}
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}

	lines, err := grepSteps(lines, f)
	if err != nil {
		return nil, err
	}
	if f.Count {
		return []byte(strconv.Itoa(len(lines)) + "\n"), nil
	}
	if f.Head > 0 && f.Head < len(lines) {
		lines = lines[:f.Head]
	}
	if f.Tail > 0 && f.Tail < len(lines) {
		lines = lines[len(lines)-f.Tail:]
	}
	if f.MaxLineLength > 0 {
		lines = truncateLines(lines, f.MaxLineLength)
	}

	out := strings.Join(lines, "\n")
	if trailingNL && out != "" {
		out += "\n"
	}
	return []byte(out), nil
}

// grepSteps applies grep (with its context window) and then grepV.
func grepSteps(lines []string, f outputFilter) ([]string, error) {
	if f.Grep != "" {
		re, err := compileLineRegex(f.Grep, f.IgnoreCase, "grep")
		if err != nil {
			return nil, err
		}
		lines = grepLines(lines, re, f.Context)
	}
	if f.GrepV != "" {
		re, err := compileLineRegex(f.GrepV, f.IgnoreCase, "grepV")
		if err != nil {
			return nil, err
		}
		lines = dropLines(lines, re)
	}
	return lines, nil
}

func compileLineRegex(pattern string, ignoreCase bool, name string) (*regexp.Regexp, error) {
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid %s regex: %w", name, err)
	}
	return re, nil
}

// grepLines keeps the lines matching re plus, when context > 0, that many
// lines on each side — with a "--" line between groups that aren't adjacent,
// exactly like grep -C.
func grepLines(lines []string, re *regexp.Regexp, context int) []string {
	keep := make([]bool, len(lines))
	for i, l := range lines {
		if !re.MatchString(l) {
			continue
		}
		for j := max(0, i-context); j <= min(len(lines)-1, i+context); j++ {
			keep[j] = true
		}
	}
	var out []string
	last := -1
	for i, k := range keep {
		if !k {
			continue
		}
		if context > 0 && last >= 0 && i > last+1 {
			out = append(out, "--")
		}
		out = append(out, lines[i])
		last = i
	}
	return out
}

func dropLines(lines []string, re *regexp.Regexp) []string {
	var out []string
	for _, l := range lines {
		if !re.MatchString(l) {
			out = append(out, l)
		}
	}
	return out
}

// truncateLines cuts every line longer than n characters (runes, not bytes)
// and marks the cut with a trailing "…".
func truncateLines(lines []string, n int) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if utf8.RuneCountInString(l) > n {
			l = string([]rune(l)[:n]) + "…"
		}
		out[i] = l
	}
	return out
}

// capBytes truncates body at the last newline at or before maxBytes and
// appends a marker naming how much was dropped.
func capBytes(body []byte, maxBytes int) []byte {
	if len(body) <= maxBytes {
		return body
	}
	cut := body[:maxBytes]
	if i := bytes.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	omitted := len(body) - len(cut)
	marker := fmt.Sprintf("\n…[truncated: %d bytes omitted — narrow with jq / grep / head]", omitted)
	return append(bytes.Clone(cut), marker...)
}

// finishRead applies f to a successful REST body and packs the MCP result.
// A non-2xx status is passed straight through, unfiltered — an error body is
// never reshaped.
func finishRead(status int, body []byte, f outputFilter, isYAML bool) (*mcp.CallToolResult, any, error) {
	if status < 200 || status >= 300 {
		return toolResult(status, body)
	}
	if isYAML {
		body = unwrapYAMLEnvelope(body)
	} else {
		body = humanizeAges(body, time.Now())
	}
	out, err := f.apply(body, isYAML)
	if err != nil {
		return nil, nil, err
	}
	return toolResult(status, out)
}

// unwrapYAMLEnvelope returns the document inside the {"yaml": "<document>"}
// envelope the manifest REST routes reply with, so a manifest tool hands the
// agent (and its jq / grep / head / tail filters) the YAML itself rather than
// one JSON string holding it. A body that isn't that envelope comes back
// unchanged.
func unwrapYAMLEnvelope(body []byte) []byte {
	var env struct {
		YAML *string `json:"yaml"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.YAML == nil {
		return body
	}
	return []byte(*env.YAML)
}

// ageFieldRE matches the "age":"<RFC3339>" members the REST views emit — the
// creation timestamp the web UI turns into "3d"/"2h" itself.
var ageFieldRE = regexp.MustCompile(`"age":\s*"(\d{4}-\d{2}-\d{2}T[0-9:.]+Z)"`)

// humanizeAges rewrites each such member for an agent: "age" becomes the
// compact relative age ("3d", "2h", "40s") it is named for, and the original
// timestamp moves to a sibling "created" (the absolute time is what matters
// when correlating with a job window or an event). Only a body that is one
// valid JSON document is touched, so plain-text results (logs) that happen to
// contain the same characters are left alone.
func humanizeAges(body []byte, now time.Time) []byte {
	if !json.Valid(body) {
		return body
	}
	return ageFieldRE.ReplaceAllFunc(body, func(m []byte) []byte {
		ts := ageFieldRE.FindSubmatch(m)[1]
		t, err := time.Parse(time.RFC3339, string(ts))
		if err != nil {
			return m
		}
		return []byte(fmt.Sprintf(`"age":%q,"created":%q`, compactAge(now.Sub(t)), ts))
	})
}

// compactAge formats d like kubectl's AGE column (and the UI's age()): the
// largest whole unit of s/m/h/d/y.
func compactAge(d time.Duration) string {
	secs := int64(d / time.Second)
	if secs < 0 {
		secs = 0
	}
	switch {
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm", secs/60)
	case secs < 86400:
		return fmt.Sprintf("%dh", secs/3600)
	case secs/86400 < 365:
		return fmt.Sprintf("%dd", secs/86400)
	default:
		return fmt.Sprintf("%dy", secs/86400/365)
	}
}
