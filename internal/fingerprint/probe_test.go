// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package fingerprint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProbeRejectsMissingContextAndOutput(t *testing.T) {
	if _, err := Probe(nil, "output", Options{}); err == nil {
		t.Fatal("Probe(nil, ...) unexpectedly succeeded")
	}
	if _, err := Probe(context.Background(), "", Options{}); err == nil {
		t.Fatal("Probe(..., empty output) unexpectedly succeeded")
	}
}

func TestTargetSchemaTopLevelPropertiesMatchGoType(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "contracts", "target-fingerprint-v0.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	typeOfTarget := reflect.TypeOf(TargetFingerprint{})
	for i := 0; i < typeOfTarget.NumField(); i++ {
		name := typeOfTarget.Field(i).Tag.Get("json")
		if comma := len(name); comma > 0 {
			for index, character := range name {
				if character == ',' {
					name = name[:index]
					break
				}
			}
		}
		want[name] = true
	}
	got := map[string]bool{}
	for name := range schema.Properties {
		got[name] = true
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema properties = %v, Go JSON fields = %v", got, want)
	}
}
