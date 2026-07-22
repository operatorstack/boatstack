package boatstack

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const configFieldMarkerPrefix = "boatstack-config-field:"

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

func configFieldMarkers(content string) []string {
	var fields []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, configFieldMarkerPrefix) {
			fields = append(fields, strings.TrimPrefix(line, configFieldMarkerPrefix))
		}
	}
	sort.Strings(fields)
	return fields
}

func documentedConfigSurface(t *testing.T, path string) []string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read configuration documentation %s: %v", path, err)
	}
	return configFieldMarkers(string(content))
}

func TestConfigFieldMarkersAcceptWindowsLineEndings(t *testing.T) {
	content := "<!--\r\nboatstack-config-field:project.name\r\nboatstack-config-field:workflow\r\n-->\r\n"
	want := []string{"project.name", "workflow"}
	if got := configFieldMarkers(content); !reflect.DeepEqual(got, want) {
		t.Fatalf("CRLF configuration markers were not parsed: got %v, want %v", got, want)
	}
}

func TestPublicConfigurationSurfaceIsDocumented(t *testing.T) {
	want := configSurface(reflect.TypeOf(ProjectConfig{}), "")
	sort.Strings(want)

	for _, document := range []string{
		"references/config-schema.md",
		"../boatstack-distribution/CONFIGURATION.md",
	} {
		got := documentedConfigSurface(t, document)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("configuration documentation drift in %s\nimplementation: %v\ndocumented:     %v", document, want, got)
		}
	}
}
