// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plugins

import (
	"strings"
	"testing"
)


// renderTemplateFuzz mirrors the logic in agent.go for isolated fuzz testing.
func renderTemplateFuzz(template string, values map[string]string) (string, error) {
	missing := make([]string, 0)
	rendered := templateVariablePattern.ReplaceAllStringFunc(template, func(match string) string {
		parts := templateVariablePattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		name := parts[1]
		value, ok := values[name]
		if !ok {
			missing = append(missing, name)
			return match
		}
		return value
	})

	if len(missing) > 0 {
		return "", &missingError{names: missing}
	}

	return rendered, nil
}

type missingError struct {
	names []string
}

func (e *missingError) Error() string {
	return "missing template values: " + strings.Join(e.names, ", ")
}

func FuzzRenderTemplate(f *testing.F) {
	// Seed with representative examples
	f.Add("", "")
	f.Add("Hello $INPUT", "INPUT=world")
	f.Add("Hello $INPUT, your score is $SCORE", "INPUT=Alice,SCORE=42")
	f.Add("Missing $MISSING_VAR", "")
	f.Add("Multiple $A $A $B", "A=alpha,B=beta")
	f.Add("$LOWERCASE", "lowercase=should not match")
	f.Add("$$$", "")
	f.Add("Mixed $VAR1 and $VAR_2", "VAR1=v1,VAR_2=v2")
	f.Add("$EMPTY_VALUE", "EMPTY_VALUE=")
	f.Add("Value with spaces $VAR", "VAR=hello world")

	f.Fuzz(func(t *testing.T, template string, valuesJSON string) {
		// Parse values from simple format: "k1=v1,k2=v2,..."
		values := parseFuzzValues(valuesJSON)

		// Test that rendering completes without panic
		result, err := renderTemplateFuzz(template, values)

		// Either succeeds with non-nil result or returns meaningful error
		if err == nil && result == "" && template != "" {
			// This is acceptable - template with no variables
		}

		// Verify no remaining variable patterns if all values provided
		missingCount := countTemplateVariables(template)
		if len(values) >= missingCount {
			if err == nil {
				// All variables should be substituted
				remaining := templateVariablePattern.FindAllString(result, -1)
				if len(remaining) > 0 {
					t.Errorf("template=%q, values=%v, result=%q has unsubstituted vars: %v",
						template, values, result, remaining)
				}
			}
		}
	})
}

// countTemplateVariables counts dollar-prefixed variable patterns in template.
// Returns count of $VARNAME patterns (case-sensitive uppercase check skipped for fuzzing).
func countTemplateVariables(template string) int {
	matches := templateVariablePattern.FindAllString(template, -1)
	return len(matches)
}

// parseFuzzValues converts a simple string format to map.
// Format: "key1=value1,key2=value2,..." where commas and equals are literal.
func parseFuzzValues(data string) map[string]string {
	if len(data) == 0 {
		return nil
	}

	result := make(map[string]string)
	parts := strings.Split(data, ",")
	for _, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(part, "=")
		if idx >= 0 {
			key := part[:idx]
			value := part[idx+1:]
			result[key] = value
		} else {
			result[part] = ""
		}
	}
	return result
}

func FuzzRenderTemplateEdgeCases(f *testing.F) {
	testCases := []struct {
		name       string
		template   string
		valuesJSON string
	}{
		{"empty template", "", "INPUT=value"},
		{"empty values", "hello $INPUT", ""},
		{"only variable", "$INPUT", "INPUT=result"},
		{"multiple same variable", "$A $A $A", "A=single"},
		{"variable at boundaries", "$A", "A=x"},
		{"variable followed by dollar", "$A$", "A=x"},
		{"dollar followed by variable", "$$INPUT", "INPUT=x"},
		{"underscore in name", "$VAR_NAME", "VAR_NAME=val"},
		{"numbers in name", "$VAR123", "VAR123=val"},
		{"mixed case not matched", "$input", "input=val"},
		{"trailing dollar", "test$", ""},
		{"leading dollar", "$test", ""},
		{"consecutive dollars", "$$$$", ""},
		{"narrow char gap", "$A$B", "A=x,B=y"},
		{"unicode in template", "Hello $INPUT 世界", "INPUT=世界"},
		{"control chars", "$\x00INPUT", "INPUT=val"},
		{"very long var name", "$" + strings.Repeat("A", 100), ""},
	}

	for _, tc := range testCases {
		f.Add(tc.template, tc.valuesJSON)
	}

	f.Fuzz(func(t *testing.T, template string, valuesJSON string) {
		values := parseFuzzValues(valuesJSON)

		// Must not panic
		_, _ = renderTemplateFuzz(template, values)
	})
}

func BenchmarkRenderTemplate(b *testing.B) {
	templates := []string{
		"Hello $INPUT, welcome to $PLACE",
		"$A $B $C $D $E",
		"Config: api=$API_KEY, url=$BASE_URL, model=$MODEL",
		strings.Repeat("$VAR ", 100),
	}
	valuesMaps := []map[string]string{
		{"INPUT": "World", "PLACE": "Server"},
		{"A": "1", "B": "2", "C": "3", "D": "4", "E": "5"},
		{"API_KEY": "secret", "BASE_URL": "https://api.example.com", "MODEL": "gpt-4"},
		{},
	}

	for i, tmpl := range templates {
		b.Run(string(rune('A'+i)), func(b *testing.B) {
			vals := valuesMaps[i]
			for j := 0; j < b.N; j++ {
				_, _ = renderTemplateFuzz(tmpl, vals)
			}
		})
	}
}