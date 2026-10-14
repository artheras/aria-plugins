package commerce

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/artheras/aria-plugins/packages/sdk"
)

func TestPublishedContractCoversEveryAPIRouteAndVersion(t *testing.T) {
	data, err := os.ReadFile("../../contracts/commerce.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var api struct {
		Info  struct{ Version string }
		Paths map[string]map[string]json.RawMessage
	}
	if err = json.Unmarshal(data, &api); err != nil || api.Info.Version != sdk.Version {
		t.Fatal("API contract version mismatch", err)
	}
	source, _ := os.ReadFile("server.go")
	routes := regexp.MustCompile(`HandleFunc\("(GET|POST) (/api/[^" ]+)"`).FindAllStringSubmatch(string(source), -1)
	count := 0
	for _, route := range routes {
		if _, ok := api.Paths[route[2]][strings.ToLower(route[1])]; !ok {
			t.Fatal("undocumented route", route[1], route[2])
		}
		count++
	}
	total := 0
	for _, path := range api.Paths {
		total += len(path)
	}
	if count != total {
		t.Fatal("contract lists an unimplemented route", count, total)
	}
}
