package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mljr-data/generator/internal/types"
)

// TestGeneratedDataMatchesSchema catches schema drift: a field added to the
// types or content.json must also be allowed by the schema, or the daily
// workflow fails at validation.
func TestGeneratedDataMatchesSchema(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	b, err := os.ReadFile(filepath.Join(root, "generated", "site-data.json"))
	if err != nil {
		t.Fatal(err)
	}
	var data types.SiteData
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	if err := validate(root, data); err != nil {
		t.Fatal(err)
	}
}
