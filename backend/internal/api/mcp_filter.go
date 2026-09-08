package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"sigs.k8s.io/yaml"
)

// outputFilter is the set of server-side result-shaping knobs every read
// tool accepts under its "filter" argument. It exists so an agent can cut a
// large response down to what it actually needs on the server, rather than
// spending context-window tokens receiving a payload just to discard most
// of it. The steps run in a fixed order: jq, then grep, then grepV, then
// head, then tail, then maxBytes.
type outputFilter struct {
	Jq       string `json:"jq,omitempty" jsonschema:"jq program (github.com/itchyny/gojq syntax) run over the result server-side, e.g. '.items[] | {name, phase: .status}' or '.items | length'. A JSON result is filtered as-is; a YAML manifest result is parsed to JSON, filtered, then re-emitted as YAML. Multiple outputs come back newline-delimited, like jq -c."`
	Grep     string `json:"grep,omitempty" jsonschema:"keep only result lines matching this RE2 regular expression (applied after jq)"`
	GrepV    string `json:"grepV,omitempty" jsonschema:"drop result lines matching this RE2 regular expression (applied after grep)"`
	Head     int    `json:"head,omitempty" jsonschema:"keep only the first N lines of the result"`
	Tail     int    `json:"tail,omitempty" jsonschema:"keep only the last N lines of the result (applied after head if both are set)"`
	MaxBytes int    `json:"maxBytes,omitempty" jsonschema:"hard cap on the returned size; the result is cut at a line boundary near this many bytes and a truncation marker is appended"`
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
	if f.Grep != "" || f.GrepV != "" || f.Head > 0 || f.Tail > 0 {
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

// applyLineFilters runs the grep / grepV / head / tail steps over body,
// treating it as newline-separated text. A single trailing newline is
// preserved so line counts stay intuitive.
func applyLineFilters(body []byte, f outputFilter) ([]byte, error) {
	text := string(body)
	trailingNL := strings.HasSuffix(text, "\n")
	if trailingNL {
		text = text[:len(text)-1]
	}
	lines := strings.Split(text, "\n")

	if f.Grep != "" {
		re, err := regexp.Compile(f.Grep)
		if err != nil {
			return nil, fmt.Errorf("invalid grep regex: %w", err)
		}
		lines = filterLines(lines, re, true)
	}
	if f.GrepV != "" {
		re, err := regexp.Compile(f.GrepV)
		if err != nil {
			return nil, fmt.Errorf("invalid grepV regex: %w", err)
		}
		lines = filterLines(lines, re, false)
	}
	if f.Head > 0 && f.Head < len(lines) {
		lines = lines[:f.Head]
	}
	if f.Tail > 0 && f.Tail < len(lines) {
		lines = lines[len(lines)-f.Tail:]
	}

	out := strings.Join(lines, "\n")
	if trailingNL && out != "" {
		out += "\n"
	}
	return []byte(out), nil
}

func filterLines(lines []string, re *regexp.Regexp, keepMatches bool) []string {
	out := lines[:0]
	for _, l := range lines {
		if re.MatchString(l) == keepMatches {
			out = append(out, l)
		}
	}
	return out
}

// capBytes truncates body at the last newline at or before max bytes and
// appends a marker naming how much was dropped.
func capBytes(body []byte, max int) []byte {
	if len(body) <= max {
		return body
	}
	cut := body[:max]
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
	out, err := f.apply(body, isYAML)
	if err != nil {
		return nil, nil, err
	}
	return toolResult(status, out)
}
