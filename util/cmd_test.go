/*
(c) Copyright 2017 Hewlett Packard Enterprise Development LP

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"os"
	"strings"
	"testing"
)

func TestLoadDefaultTimeout(t *testing.T) {
	tests := []struct {
		name  string
		value string
		set   bool
		want  int
	}{
		{name: "unset", want: defaultTimeoutSeconds},
		{name: "valid", value: "120", set: true, want: 120},
		{name: "zero", value: "0", set: true, want: defaultTimeoutSeconds},
		{name: "negative", value: "-1", set: true, want: defaultTimeoutSeconds},
		{name: "not an integer", value: "invalid", set: true, want: defaultTimeoutSeconds},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(cmdTimeoutEnvVar, test.value)
			if !test.set {
				if err := os.Unsetenv(cmdTimeoutEnvVar); err != nil {
					t.Fatalf("unset %s: %v", cmdTimeoutEnvVar, err)
				}
			}

			if got := loadDefaultTimeout(); got != test.want {
				t.Fatalf("loadDefaultTimeout() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestEchoExecCommandOutput(t *testing.T) {
	out, rc, err := ExecCommandOutput("echo", []string{"Hello"})
	if err != nil {
		t.Error(
			"Unexpected error", err,
		)
	}
	if rc > 0 {
		t.Error(
			"Unexpected rc", rc,
		)
	}
	if strings.HasSuffix(out, "Hello") {
		t.Error(
			"For", "return code of false",
			"expected", "1",
			"got", rc,
		)
	}

}

func TestFalseExecCommandOutput(t *testing.T) {
	out, rc, err := ExecCommandOutput("false", []string{"foo"})
	if err == nil {
		t.Error(
			"Expected error to not be nil", err,
		)
	}
	if rc != 1 {
		t.Error(
			"For", "return code of false",
			"expected", "1",
			"got", rc,
		)
	}
	if out != "" {
		t.Error(
			"For", "out of false",
			"expected", "",
			"got", out,
		)
	}
}

func TestFailExecCommandOutput(t *testing.T) {
	out, _, err := ExecCommandOutput("cp", []string{"x"})
	if err == nil {
		t.Error(
			"Expected error to be nil", err,
		)
	}
	if out == "" {
		t.Error(
			"For", "out of 'cp x'",
			"expected", "some text",
			"got", out,
		)
	}

	_, rc, err := ExecCommandOutput("nosuchcommand", []string{"x"})
	if err == nil {
		t.Error(
			"Expected error to not be nil", err,
		)
	}
	if rc != 999 {
		t.Error(
			"For", "rc",
			"expected", 999,
			"got", rc,
		)
	}
}
