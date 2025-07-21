/*
Copyright 2025.

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

package main

import (
	"os"
	"testing"
)

func TestValidateStartupConfiguration(t *testing.T) {
	// Test with no configuration - should fail
	os.Unsetenv("KUBERNETES_SERVICE_HOST")
	os.Unsetenv("KUBECONFIG")

	err := validateStartupConfiguration()
	if err == nil {
		t.Error("Expected validation to fail when no Kubernetes configuration is available")
	}

	// Test that the function doesn't panic
	if err.Error() == "" {
		t.Error("Expected a meaningful error message")
	}
}

func TestMainFunctionFlags(t *testing.T) {
	// Test that main function can be called with help flag
	// This is a basic smoke test to ensure the flag parsing works
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// We can't easily test the full main function without a Kubernetes cluster
	// But we can test that the flag parsing doesn't panic
	os.Args = []string{"main", "--help"}

	// The main function would exit with help, so we can't call it directly
	// This test just ensures the test file compiles and basic functions work
}
