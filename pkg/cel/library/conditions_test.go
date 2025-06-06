package library

import (
	"regexp"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestConditionsLib(t *testing.T) {
	trueVal := types.Bool(true)
	falseVal := types.Bool(false)

	cases := []struct {
		name                string
		expr                string
		expectValue         ref.Val
		expectedCompileErrs []string
		expectedRuntimeErr  string
	}{
		{
			name:        "null status",
			expr:        `hasConditions(null)`,
			expectValue: falseVal,
		},
		{
			name:        "missing conditions",
			expr:        `hasConditions({"some": "value"})`,
			expectValue: falseVal,
		},
		{
			name:        "null conditions",
			expr:        `hasConditions({"conditions": null})`,
			expectValue: falseVal,
		},
		{
			name:        "empty conditions",
			expr:        `hasConditions({"conditions": []})`,
			expectValue: falseVal,
		},
		{
			name:        "invalid type",
			expr:        `hasConditions({"conditions": {}})`,
			expectValue: falseVal,
		},
		{
			name:        "invalid object",
			expr:        `hasConditions('{"conditions": []}')`,
			expectValue: falseVal,
		},
		{
			name:        "valid conditions",
			expr:        `hasConditions({"conditions": [{"type": "Ready", "status": "True"}]})`,
			expectValue: trueVal,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testConditions(t, c.expr, c.expectValue, c.expectedRuntimeErr, c.expectedCompileErrs)
		})
	}
}

func testConditions(t *testing.T, expr string, expectValue ref.Val, expectRuntimeErrPattern string, expectCompileErrs []string) {
	env, err := cel.NewEnv(
		ConditionsLib(),
	)
	if err != nil {
		t.Fatalf("%v", err)
	}

	compiled, issues := env.Compile(expr)

	if len(expectCompileErrs) > 0 {
		missingCompileErrs := []string{}
		matchedCompileErrs := sets.New[int]()
		for _, expectedCompileErr := range expectCompileErrs {
			compiledPattern, err := regexp.Compile(expectedCompileErr)
			if err != nil {
				t.Fatalf("failed to compile expected err regex: %v", err)
			}

			didMatch := false

			for i, compileError := range issues.Errors() {
				if compiledPattern.Match([]byte(compileError.Message)) {
					didMatch = true
					matchedCompileErrs.Insert(i)
				}
			}

			if !didMatch {
				missingCompileErrs = append(missingCompileErrs, expectedCompileErr)
			} else if len(matchedCompileErrs) != len(issues.Errors()) {
				unmatchedErrs := []cel.Error{}
				for i, issue := range issues.Errors() {
					if !matchedCompileErrs.Has(i) {
						unmatchedErrs = append(unmatchedErrs, *issue)
					}
				}
				require.Empty(t, unmatchedErrs, "unexpected compilation errors")
			}
		}

		require.Empty(t, missingCompileErrs, "expected compilation errors")
		return
	} else if len(issues.Errors()) > 0 {
		t.Fatalf("%v", issues.Errors())
	}

	prog, err := env.Program(compiled)
	if err != nil {
		t.Fatalf("%v", err)
	}
	res, _, err := prog.Eval(map[string]interface{}{})
	if len(expectRuntimeErrPattern) > 0 {
		if err == nil {
			t.Fatalf("no runtime error thrown. Expected: %v", expectRuntimeErrPattern)
		} else if expectRuntimeErrPattern != err.Error() {
			t.Fatalf("unexpected err: %v", err)
		}
	} else if err != nil {
		t.Fatalf("%v", err)
	} else if expectValue != nil {
		converted := res.Equal(expectValue).Value().(bool)
		require.True(t, converted, "expectation not equal to output")
	} else {
		t.Fatal("expected result must not be nil")
	}
}
