package boatstack

import (
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Planning Markdown crosses a shell hook as literal data, not as executable
// command text. The guard admits exactly two envelopes:
//
//   - a POSIX single-quoted heredoc; and
//   - a PowerShell single-quoted here-string inside a local UTF-8 output scope.
//
// The parser removes only a structurally complete body from effect scanning.
// Any truncation, delimiter collision, expansion-capable delimiter, malformed
// helper command, or trailing command stays closed before the shell runs.
// control-law: planning-document-body-is-literal-data

const powerShellPlanningEncodingLine = `$OutputEncoding = [System.Text.UTF8Encoding]::new($false)`

var posixPlanningHeader = regexp.MustCompile(`^(.*\S)[ \t]+<<'([A-Za-z_][A-Za-z0-9_]{0,63})'[ \t]*$`)
var powerShellPlanningClose = regexp.MustCompile(`^'@[ \t]+\|[ \t]+&[ \t]+(.+)$`)
var planningWriteMention = regexp.MustCompile(`(?i)\bboatstack-helper(?:\.exe)?['"]?[ \t]+planning-write(?:[ \t]|$)`)

type planningTransportInspection struct {
	Matched       bool
	Header        string
	Content       []byte
	Feature       string
	Repository    string
	Executable    string
	InvalidReason string
}

type planningWriteInvocation struct {
	Executable string
	Repository string
	Feature    string
}

func structuralLine(value string) string {
	return strings.TrimSuffix(value, "\r")
}

func nextLine(value string, start int) (line string, next int, hasNewline bool) {
	if start > len(value) {
		return "", len(value), false
	}
	if relative := strings.IndexByte(value[start:], '\n'); relative >= 0 {
		end := start + relative
		return value[start:end], end + 1, true
	}
	return value[start:], len(value), false
}

// literalCommandWords is a deliberately smaller grammar than either shell. It
// accepts ordinary quoted argv (including paths with spaces) but no expansion,
// compound syntax, glob, grouping, redirection, or pipeline syntax. The partial
// words returned on failure let the guard recognize a malformed planning-write
// attempt and deny it with the transport-specific recovery instead of executing
// a prefix with shell side effects.
func literalCommandWords(value string) (words []string, complete bool) {
	runes := []rune(strings.TrimSpace(value))
	var current strings.Builder
	var quote rune
	inWord := false
	flush := func() {
		if inWord {
			words = append(words, current.String())
			current.Reset()
			inWord = false
		}
	}
	for index := 0; index < len(runes); index++ {
		char := runes[index]
		if quote != 0 {
			if char == quote {
				quote = 0
				inWord = true
				continue
			}
			if char == 0 || char == '\n' || char == '\r' || (quote == '"' && (char == '$' || char == '`')) {
				flush()
				return words, false
			}
			current.WriteRune(char)
			inWord = true
			continue
		}
		if unicode.IsSpace(char) {
			flush()
			continue
		}
		switch char {
		case '\'', '"':
			quote = char
			inWord = true
		case '\\':
			inWord = true
			if index+1 < len(runes) {
				next := runes[index+1]
				if unicode.IsSpace(next) || next == '\\' || next == '\'' || next == '"' {
					current.WriteRune(next)
					index++
					continue
				}
			}
			// Preserve Windows separators and ordinary POSIX backslashes.
			current.WriteRune(char)
		case ';', '&', '|', '<', '>', '$', '`', '(', ')', '{', '}', '*', '?', '[', ']', '#', 0:
			flush()
			return words, false
		default:
			current.WriteRune(char)
			inWord = true
		}
	}
	if quote != 0 {
		flush()
		return words, false
	}
	flush()
	return words, true
}

func portableExecutableBase(value string) string {
	return strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(value, "\\", "/"))), ".exe")
}

func planningWriteAttempt(value string) bool {
	words, _ := literalCommandWords(value)
	return len(words) >= 2 && portableExecutableBase(words[0]) == "boatstack-helper" && words[1] == "planning-write"
}

func planningVerbInvocationAttempt(value string) bool {
	words, _ := literalCommandWords(value)
	return len(words) >= 2 && words[1] == "planning-write"
}

func powerShellPlanningAttempt(command string) bool {
	for position := 0; position <= len(command); {
		line, next, hasNewline := nextLine(command, position)
		if match := powerShellPlanningClose.FindStringSubmatch(structuralLine(line)); match != nil && planningWriteAttempt(strings.TrimSpace(match[1])) {
			return true
		}
		if !hasNewline {
			return false
		}
		position = next
	}
	return false
}

func planningWriteHeader(value string) (planningWriteInvocation, bool) {
	words, complete := literalCommandWords(value)
	if !complete || len(words) < 2 || portableExecutableBase(words[0]) != "boatstack-helper" || words[1] != "planning-write" {
		return planningWriteInvocation{}, false
	}
	values := map[string]string{}
	for index := 2; index < len(words); index++ {
		flag := words[index]
		value := ""
		if split := strings.IndexByte(flag, '='); split >= 0 {
			value = flag[split+1:]
			flag = flag[:split]
		} else {
			if index+1 >= len(words) {
				return planningWriteInvocation{}, false
			}
			index++
			value = words[index]
		}
		if flag != "--repo" && flag != "--feature" && flag != "--artifact" {
			return planningWriteInvocation{}, false
		}
		if value == "" || values[flag] != "" {
			return planningWriteInvocation{}, false
		}
		values[flag] = value
	}
	if !featureSlugPattern.MatchString(values["--feature"]) || !planningArtifacts[values["--artifact"]] {
		return planningWriteInvocation{}, false
	}
	repository := values["--repo"]
	if repository == "" {
		repository = "."
	}
	return planningWriteInvocation{Executable: words[0], Repository: repository, Feature: values["--feature"]}, true
}

func planningTransportBinding(repo string, transport planningTransportInspection) string {
	root, err := filepath.Abs(repo)
	if err != nil {
		return "repository-mismatch"
	}
	if canonical, canonicalErr := filepath.EvalSymlinks(root); canonicalErr == nil {
		root = canonical
	}
	normalizedRepo := filepath.FromSlash(strings.ReplaceAll(transport.Repository, "\\", "/"))
	targetRepo := normalizedRepo
	if !filepath.IsAbs(targetRepo) {
		targetRepo = filepath.Join(root, targetRepo)
	}
	targetRepo, err = filepath.Abs(targetRepo)
	if err != nil {
		return "repository-mismatch"
	}
	if canonical, canonicalErr := filepath.EvalSymlinks(targetRepo); canonicalErr == nil {
		targetRepo = canonical
	}
	if filepath.Clean(targetRepo) != filepath.Clean(root) {
		return "repository-mismatch"
	}

	normalizedExecutable := filepath.FromSlash(strings.ReplaceAll(transport.Executable, "\\", "/"))
	executable := normalizedExecutable
	if !filepath.IsAbs(executable) {
		executable = filepath.Join(root, executable)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return "helper-path-mismatch"
	}
	if canonical, canonicalErr := filepath.EvalSymlinks(executable); canonicalErr == nil {
		executable = canonical
	}
	base := strings.ToLower(filepath.Base(executable))
	canonicalBase := strings.ToLower(helperName())
	if base != canonicalBase && !(runtime.GOOS == "windows" && base == "boatstack-helper") {
		return "helper-path-mismatch"
	}
	workspace, workspaceErr := ResolveWorkspaceContext(root)
	if workspaceErr != nil {
		return "workspace-binding-unverified"
	}
	expected := workspace.HelperPath()
	if runtime.GOOS == "windows" && base == "boatstack-helper" {
		expected = strings.TrimSuffix(expected, ".exe")
	}
	if canonical, canonicalErr := filepath.EvalSymlinks(expected); canonicalErr == nil {
		expected = canonical
	}
	if filepath.Clean(executable) != filepath.Clean(expected) {
		return "helper-path-mismatch"
	}
	return ""
}

func validPlanningBody(value string) string {
	if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return "invalid-content"
	}
	if strings.TrimSpace(value) == "" {
		return "empty-content"
	}
	return ""
}

func inspectPosixPlanningTransport(command string, first string, bodyStart int) planningTransportInspection {
	match := posixPlanningHeader.FindStringSubmatch(first)
	if match == nil {
		if planningWriteAttempt(first) {
			return planningTransportInspection{Matched: true, InvalidReason: "single-quoted-delimiter-required"}
		}
		return planningTransportInspection{}
	}
	header := strings.TrimSpace(match[1])
	invocation, validHeader := planningWriteHeader(header)
	if !validHeader {
		if planningWriteAttempt(header) || planningVerbInvocationAttempt(header) {
			return planningTransportInspection{Matched: true, Header: header, InvalidReason: "invalid-command-shape"}
		}
		return planningTransportInspection{}
	}
	delimiter := match[2]
	for position := bodyStart; position <= len(command); {
		line, next, hasNewline := nextLine(command, position)
		if structuralLine(line) == delimiter {
			if next != len(command) {
				return planningTransportInspection{Matched: true, Header: header, Feature: invocation.Feature, Repository: invocation.Repository, Executable: invocation.Executable, InvalidReason: "delimiter-collision-or-trailing-command"}
			}
			content := command[bodyStart:position]
			if reason := validPlanningBody(content); reason != "" {
				return planningTransportInspection{Matched: true, Header: header, Feature: invocation.Feature, Repository: invocation.Repository, Executable: invocation.Executable, InvalidReason: reason}
			}
			return planningTransportInspection{Matched: true, Header: header, Content: []byte(content), Feature: invocation.Feature, Repository: invocation.Repository, Executable: invocation.Executable}
		}
		if !hasNewline {
			break
		}
		position = next
	}
	return planningTransportInspection{Matched: true, Header: header, Feature: invocation.Feature, Repository: invocation.Repository, Executable: invocation.Executable, InvalidReason: "missing-terminator"}
}

func inspectPowerShellPlanningTransport(command string) planningTransportInspection {
	position := 0
	line, next, ok := nextLine(command, position)
	if !ok || strings.TrimSpace(structuralLine(line)) != "& {" {
		return planningTransportInspection{}
	}
	position = next
	line, next, ok = nextLine(command, position)
	if !ok || strings.TrimSpace(structuralLine(line)) != powerShellPlanningEncodingLine {
		if powerShellPlanningAttempt(command) {
			return planningTransportInspection{Matched: true, InvalidReason: "powershell-utf8-scope-required"}
		}
		return planningTransportInspection{}
	}
	position = next
	line, next, ok = nextLine(command, position)
	if !ok || strings.TrimSpace(structuralLine(line)) != "@'" {
		return planningTransportInspection{Matched: true, InvalidReason: "single-quoted-here-string-required"}
	}
	bodyStart := next
	for position = bodyStart; position <= len(command); {
		line, next, hasNewline := nextLine(command, position)
		structural := structuralLine(line)
		if strings.HasPrefix(structural, "'@") {
			match := powerShellPlanningClose.FindStringSubmatch(structural)
			if match == nil {
				return planningTransportInspection{Matched: true, InvalidReason: "delimiter-collision-or-trailing-command"}
			}
			header := strings.TrimSpace(match[1])
			invocation, validHeader := planningWriteHeader(header)
			if !validHeader {
				return planningTransportInspection{Matched: true, Header: header, InvalidReason: "invalid-command-shape"}
			}
			if !hasNewline {
				return planningTransportInspection{Matched: true, Header: header, Feature: invocation.Feature, Repository: invocation.Repository, Executable: invocation.Executable, InvalidReason: "powershell-scope-not-closed"}
			}
			closing, afterClosing, _ := nextLine(command, next)
			if strings.TrimSpace(structuralLine(closing)) != "}" || afterClosing != len(command) {
				return planningTransportInspection{Matched: true, Header: header, Feature: invocation.Feature, Repository: invocation.Repository, Executable: invocation.Executable, InvalidReason: "delimiter-collision-or-trailing-command"}
			}
			content := command[bodyStart:position]
			if reason := validPlanningBody(content); reason != "" {
				return planningTransportInspection{Matched: true, Header: header, Feature: invocation.Feature, Repository: invocation.Repository, Executable: invocation.Executable, InvalidReason: reason}
			}
			return planningTransportInspection{Matched: true, Header: header, Content: []byte(content), Feature: invocation.Feature, Repository: invocation.Repository, Executable: invocation.Executable}
		}
		if !hasNewline {
			break
		}
		position = next
	}
	return planningTransportInspection{Matched: true, InvalidReason: "missing-terminator"}
}

func inspectPlanningWriteTransport(command string) planningTransportInspection {
	first, next, hasNewline := nextLine(command, 0)
	first = structuralLine(first)
	if strings.TrimSpace(first) == "& {" {
		if transport := inspectPowerShellPlanningTransport(command); transport.Matched {
			return transport
		}
	}
	if (strings.TrimSpace(first) == "@'" || strings.HasPrefix(strings.TrimSpace(first), "$OutputEncoding")) && powerShellPlanningAttempt(command) {
		return planningTransportInspection{Matched: true, InvalidReason: "powershell-utf8-scope-required"}
	}
	if hasNewline {
		if transport := inspectPosixPlanningTransport(command, first, next); transport.Matched {
			return transport
		}
	}
	if planningWriteAttempt(first) {
		return planningTransportInspection{Matched: true, Header: strings.TrimSpace(first), InvalidReason: "missing-literal-input"}
	}
	// A planning-write occurrence that did not match either complete envelope is
	// still owned by this boundary. This closes leading commands, compound shell
	// wrappers, and other alternate paths that could otherwise avoid binding and
	// body classification merely by moving the helper away from column zero.
	if planningWriteMention.MatchString(command) {
		return planningTransportInspection{Matched: true, InvalidReason: "complete-literal-envelope-required"}
	}
	return planningTransportInspection{}
}
