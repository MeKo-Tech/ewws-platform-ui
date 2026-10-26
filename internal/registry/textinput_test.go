package registry

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseEnvLines(t *testing.T) {
	t.Parallel()

	got, err := ParseEnvLines(`
# comment
  PORT = 8080
EMPTY=
URL=https://x.example/?a=b
DB_PASSWORD=secret:app-db/DB_PASSWORD
`)
	if err != nil {
		t.Fatalf("ParseEnvLines: %v", err)
	}

	want := []EnvVar{
		{Name: "PORT", Value: "8080"},
		{Name: "EMPTY"},
		{Name: "URL", Value: "https://x.example/?a=b"},
		{Name: "DB_PASSWORD", Secret: "app-db", Key: "DB_PASSWORD"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseEnvLines = %+v, want %+v", got, want)
	}

	back, err := ParseEnvLines(FormatEnvLines(got))
	if err != nil || !reflect.DeepEqual(back, want) {
		t.Errorf("round trip = %+v, %v", back, err)
	}

	if got, err := ParseEnvLines("\n  \n"); err != nil || got != nil {
		t.Errorf("empty input = %+v, %v", got, err)
	}
}

func TestParseEnvLinesErrors(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"NOVALUE":            "line 1",
		"=x":                 "is not KEY=value",
		"A=1\nA=2":           "line 2: A is set twice",
		"A=secret:app-db":    "secret:<secret-name>/<key>",
		"A=secret:/k":        "secret:<secret-name>/<key>",
		"A=secret:app-db/":   "secret:<secret-name>/<key>",
		"# x\n\nB=1\nbroken": "line 4",
	} {
		_, err := ParseEnvLines(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseEnvLines(%q) error = %v, want it to contain %q", in, err, want)
		}
	}
}

func TestParseNameList(t *testing.T) {
	t.Parallel()

	got := ParseNameList(" smtp-relay, ldap-bind\nsmtp-relay  app-db,,\t")
	want := []string{"smtp-relay", "ldap-bind", "app-db"}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseNameList = %v, want %v", got, want)
	}

	if got := ParseNameList(" , "); got != nil {
		t.Errorf("ParseNameList(blank) = %v, want nil", got)
	}
}
