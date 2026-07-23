package boatstack

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const (
	configFieldMarkerPrefix     = "boatstack-config-field:"
	userConfigFieldMarkerPrefix = "boatstack-user-config-field:"
)

func configSurface(value reflect.Type, prefix string) []string {
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	var fields []string
	for index := 0; index < value.NumField(); index++ {
		field := value.Field(index)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		fields = append(fields, path)

		nested := field.Type
		if nested.Kind() == reflect.Pointer {
			nested = nested.Elem()
		}
		switch nested.Kind() {
		case reflect.Struct:
			fields = append(fields, configSurface(nested, path)...)
		case reflect.Map:
			item := nested.Elem()
			if item.Kind() == reflect.Struct {
				fields = append(fields, configSurface(item, path+".*")...)
			}
		}
	}
	return fields
}

func configFieldMarkers(content, prefix string) []string {
	var fields []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			fields = append(fields, strings.TrimPrefix(line, prefix))
		}
	}
	sort.Strings(fields)
	return fields
}

func documentedConfigSurface(t *testing.T, path, prefix string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read configuration documentation %s: %v", path, err)
	}
	return configFieldMarkers(string(content), prefix)
}

func publicConfigurationDocument(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../boatstack-distribution/CONFIGURATION.md",
		"../docs/configuration.md",
	}
	var found []string
	for _, candidate := range candidates {
		_, err := os.Stat(candidate)
		switch {
		case err == nil:
			found = append(found, candidate)
		case os.IsNotExist(err):
			continue
		default:
			t.Fatalf("inspect public configuration document %s: %v", candidate, err)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one public configuration document from %v, found %v", candidates, found)
	}
	return found[0]
}

func TestConfigFieldMarkersAcceptWindowsLineEndings(t *testing.T) {
	content := "<!--\r\nboatstack-config-field:project.name\r\nboatstack-config-field:workflow\r\n-->\r\n"
	want := []string{"project.name", "workflow"}
	if got := configFieldMarkers(content, configFieldMarkerPrefix); !reflect.DeepEqual(got, want) {
		t.Fatalf("CRLF configuration markers were not parsed: got %v, want %v", got, want)
	}
}

func TestSerializedConfigurationSurfaceIsDocumentedInternally(t *testing.T) {
	want := configSurface(reflect.TypeOf(ProjectConfig{}), "")
	sort.Strings(want)
	document := "references/config-schema.md"
	got := documentedConfigSurface(t, document, configFieldMarkerPrefix)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("configuration documentation drift in %s\nimplementation: %v\ndocumented:     %v", document, want, got)
	}
}

func TestPublicConfigurationGuideContainsOnlySupportedUserControls(t *testing.T) {
	want := []string{
		"adapters",
		"project.commands",
		"project.context",
		"project.default_branch",
		"project.high_risk_paths",
		"workflow.allow_pass_with_gaps",
		"workflow.boundary_analysis",
		"workflow.human_plan_approval",
		"workflow.ignored_deliveries",
		"workflow.independent_review_for_high_risk",
		"workflow.maintain_changelog",
		"workflow.pr_visual_evidence",
		"workspace.cleanup",
		"workspace.cleanup_after",
		"workspace.enabled",
		"workspace.mode",
	}
	document := publicConfigurationDocument(t)
	got := documentedConfigSurface(t, document, userConfigFieldMarkerPrefix)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("public user-control documentation drift in %s\nsupported:  %v\ndocumented: %v", document, want, got)
	}
	serialized := configSurface(reflect.TypeOf(ProjectConfig{}), "")
	for _, field := range got {
		if !contains(serialized, field) {
			t.Errorf("public guide exposes unknown configuration field %s", field)
		}
	}
}
