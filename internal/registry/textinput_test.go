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

func TestParseSecretLines(t *testing.T) {
	t.Parallel()

	got, err := ParseSecretLines(`
# secrets
app-db: DB_PASSWORD=db-password, DB_USER = db-user
app-api:API_TOKEN=api-token,
`)
	if err != nil {
		t.Fatalf("ParseSecretLines: %v", err)
	}

	want := []SecretInput{
		{Name: "app-db", Data: []SecretKey{
			{EnvVar: "DB_PASSWORD", VaultSuffix: "db-password"},
			{EnvVar: "DB_USER", VaultSuffix: "db-user"},
		}},
		{Name: "app-api", Data: []SecretKey{{EnvVar: "API_TOKEN", VaultSuffix: "api-token"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSecretLines = %+v, want %+v", got, want)
	}

	back, err := ParseSecretLines(FormatSecretLines(got))
	if err != nil || !reflect.DeepEqual(back, want) {
		t.Errorf("round trip = %+v, %v", back, err)
	}
}

func TestParseSecretLinesErrors(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"app-db":                   "line 1",
		": A=b":                    "line 1",
		"app-db:":                  "needs at least one",
		"app-db: A":                "is not ENV_VAR=vault-suffix",
		"app-db: A=":               "is not ENV_VAR=vault-suffix",
		"app-db: A=a, A=b":         "A is listed twice",
		"app-db: A=a\napp-db: B=b": "line 2: secret app-db is listed twice",
	} {
		_, err := ParseSecretLines(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseSecretLines(%q) error = %v, want it to contain %q", in, err, want)
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
