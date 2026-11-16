// Copyright 2026 yhgrwav
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

package config_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// Step templates (v0.9 unit 3b). Compile stub: config.Step has Templates and
// TemplatesOn, but nulls.go still refuses the key and TemplatesOn is false, so
// every test of a step that is accepted, or refused in the words of the spec,
// is red until the code. The guard tests below it are green now and stay so.

const (
	// templatedCall is the lines of a call with templates on and a placeholder of
	// its own in its data.
	templatedCall = "      templates: true\n      data:\n        id: \"{{uuid}}\"\n"
	// bareTemplatedCall is a call with templates on and no placeholder in it.
	bareTemplatedCall = "      templates: true\n      data:\n        id: fixed\n"
	// stepHead is the first line of a Transfer step; its keys go 10 spaces deep.
	stepHead = "        - method: " + transfer + "\n"
	// stepTemplatesOn is the line that turns the templates of a step on.
	stepTemplatesOn = "          templates: true\n"
	// stepNeedsCall and stepNeedsKey are the refusal texts of 2a and 2d.
	stepNeedsCall = "a step's templates need templates: true on the call"
	stepNeedsKey  = "set templates: true on the step, or templates: false to send the braces as text"
)

// tplScenario is a scenario call: callLines first, then the steps as YAML.
func tplScenario(callLines, steps string) string {
	return scenarioCall(callLines + "      scenario: pay-and-check\n      steps:\n" + steps)
}

// stepData is Transfer with the given lines of data (each indented 12 spaces by
// the caller) and, when templates is not empty, the templates line first.
func stepData(templates, data string) string {
	out := stepHead
	if templates != "" {
		out += "          templates: " + templates + "\n"
	}

	return out + "          data:\n" + data
}

// secondCall is a plain second call whose scenario has one Close step; the step
// lines (10 spaces deep) go after its method.
func secondCall(stepLines string) string {
	return "    - method: " + getHistory + "\n      rps: 5\n      duration: 1s\n      scenario: s2\n      steps:\n" +
		"        - method: wallet.v1.WalletService/Close\n" + stepLines
}

func requireRefusal(t *testing.T, raw string, is error, want ...string) {
	t.Helper()

	_, err := config.Parse([]byte(raw))
	if err == nil {
		t.Fatalf("Parse() = nil, want a refusal with %q", want)
	}
	if is != nil && !errors.Is(err, is) {
		t.Errorf("error = %v, want it to be %v", err, is)
	}
	for _, text := range want {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("error = %q, want it to contain %q", err, text)
		}
	}
}

// A step takes `templates` as a call does; what is written is what the step
// carries, and a step that leaves it out is nil, so off.
// Ground: contract — spec 1, steps[].templates is config API.
func TestScenario_StepTemplatesIsAccepted(t *testing.T) {
	cfg, err := config.Parse([]byte(tplScenario(templatedCall,
		stepData("true", "            from: \"wallet-{{seq}}\"\n            to: \"{{uuid}}\"\n            amount: 100\n")+
			"        - method: "+getHistory+"\n")))
	if err != nil {
		t.Fatalf("Parse() = %v, want a templated step accepted", err)
	}

	call := cfg.Load.Calls[0]
	if !call.TemplatesOn() {
		t.Error("the call's TemplatesOn = false, want true as written")
	}
	if len(call.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(call.Steps))
	}
	if first := call.Steps[0]; first.Templates == nil || !*first.Templates || !first.TemplatesOn() {
		t.Errorf("Steps[0].Templates = %v, TemplatesOn = %v, want true and true", first.Templates, first.TemplatesOn())
	}
	if second := call.Steps[1]; second.Templates != nil || second.TemplatesOn() {
		t.Errorf("Steps[1].Templates = %v, TemplatesOn = %v, want nil and false: a step that leaves the key out is plain", second.Templates, second.TemplatesOn())
	}

	data, ok := call.Steps[0].Data.(map[string]any)
	if !ok || data["from"] != "wallet-{{seq}}" || data["to"] != "{{uuid}}" {
		t.Errorf("Steps[0].Data = %#v, want the data as written, braces and all", call.Steps[0].Data)
	}
}

// templates: false on a step is the braces as text, with no refusal: the
// explicit word is the user's answer to 2d. Left out means something else (2d),
// so the three call shapes below all take it.
// Ground: contract — spec 2d as amended: an explicit false is not left out.
func TestScenario_StepTemplatesFalseSendsTheBracesAsText(t *testing.T) {
	for name, callLines := range map[string]string{
		"call with templates on":    templatedCall,
		"call with templates false": "      templates: false\n      data:\n        id: fixed\n",
		"call without the key":      "      data:\n        id: fixed\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(tplScenario(callLines,
				stepData("false", "            note: \"{{nope}}\"\n            more: \"a {{b\"\n"))))
			if err != nil {
				t.Fatalf("Parse() = %v, want the braces accepted as text under templates: false", err)
			}

			step := cfg.Load.Calls[0].Steps[0]
			if step.Templates == nil || *step.Templates {
				t.Errorf("Steps[0].Templates = %v, want a pointer to false: the key was written", step.Templates)
			}
			if step.TemplatesOn() {
				t.Error("Steps[0].TemplatesOn = true, want false")
			}

			want := map[string]any{"note": "{{nope}}", "more": "a {{b"}
			if !reflect.DeepEqual(step.Data, want) {
				t.Errorf("Steps[0].Data = %#v, want %#v: the text as written", step.Data, want)
			}
		})
	}
}

// 2a: a step cannot switch on what its call has off. The refusal names the step
// by its path and says what to do, whichever step and whichever call it is in.
// Ground: contract — spec 2a: no step with templates under a call without them.
func TestScenario_StepTemplatesNeedTheCallsTemplates(t *testing.T) {
	templatedStep := stepData("true", "            note: \"{{seq}}\"\n")

	for _, tc := range []struct {
		name string
		cfg  string
		path string
	}{
		{"call without the key", tplScenario("      data:\n        id: fixed\n", templatedStep), "load.calls[0].steps[0]"},
		{"call with templates false", tplScenario("      templates: false\n      data:\n        id: fixed\n", templatedStep), "load.calls[0].steps[0]"},
		{"the second step", tplScenario("      data:\n        id: fixed\n", "        - method: "+getHistory+"\n"+
			"          templates: true\n          data:\n            note: \"{{seq}}\"\n"), "load.calls[0].steps[1]"},
		{"the second call", scenarioCall(templatedCall) + secondCall("          templates: true\n          data:\n            note: \"{{seq}}\"\n"),
			"load.calls[1].steps[0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireRefusal(t, tc.cfg, config.ErrScenario, tc.path, stepNeedsCall)
		})
	}
}

// 2b: what a call's data may not hold, a step's may not: the same errors, and
// the path goes down to the field.
// Ground: contract — spec 2b, one rule for the data of a call and of a step.
func TestScenario_StepTemplatesUnknownBracesAndKeyBraces(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
		is   error
		path string
		text string
	}{
		{"unknown placeholder", "            note: \"{{nope}}\"\n", config.ErrTemplatesUnknown, "load.calls[0].steps[0].data.note", "{{nope}}"},
		{"spaces inside", "            note: \"{{ seq }}\"\n", config.ErrTemplatesUnknown, "load.calls[0].steps[0].data.note", "{{ seq }}"},
		{"nested", "            child:\n              name: \"{{x}}\"\n", config.ErrTemplatesUnknown, "load.calls[0].steps[0].data.child.name", "{{x}}"},
		{"in a list", "            tags: [\"ok\", \"{{x}}\"]\n", config.ErrTemplatesUnknown, "load.calls[0].steps[0].data.tags[1]", "{{x}}"},
		{"a map key", "            attrs:\n              \"{{uuid}}\": v\n", config.ErrTemplatesKey, "load.calls[0].steps[0].data.attrs", "{{uuid}}"},
		{"a field key", "            \"{{seq}}\": 1\n", config.ErrTemplatesKey, "load.calls[0].steps[0].data", "{{seq}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A valid placeholder beside the bad one, so that 2e does not speak first.
			raw := tplScenario(templatedCall, stepData("true", "            ok: \"{{seq}}\"\n"+tc.data))

			requireRefusal(t, raw, tc.is, tc.path, tc.text)
		})
	}

	t.Run("the second step names its own index", func(t *testing.T) {
		raw := tplScenario(templatedCall, "        - method: "+getHistory+"\n"+
			"          templates: true\n          data:\n            ok: \"{{seq}}\"\n            note: \"{{nope}}\"\n")

		requireRefusal(t, raw, config.ErrTemplatesUnknown, "load.calls[0].steps[1].data.note")
	})

	t.Run("every bad field is named", func(t *testing.T) {
		raw := tplScenario(templatedCall, stepData("true", "            a: \"{{x}}\"\n            b: \"{{ y }}\"\n"))

		requireRefusal(t, raw, config.ErrTemplatesUnknown, "steps[0].data.a", "{{x}}", "steps[0].data.b", "{{ y }}")
	})
}

// 2c: the call's «templates: true without placeholders» looks at its own data
// and at the data of its templated steps; one placeholder anywhere is enough.
// Ground: contract — spec 2c: a call can be templated for the sake of its steps.
func TestScenario_CallWithoutOwnPlaceholdersIsValidWithATemplatedStep(t *testing.T) {
	for name, callLines := range map[string]string{
		"data without braces": bareTemplatedCall,
		"no data at all":      "      templates: true\n",
		"empty data":          "      templates: true\n      data: {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			raw := tplScenario(callLines, stepData("true", "            from: \"wallet-{{seq}}\"\n"))

			if _, err := config.Parse([]byte(raw)); err != nil {
				t.Errorf("Parse() = %v, want it valid: the placeholder is in the step", err)
			}
		})
	}
}

// 2c, the other side: no placeholder in the call and none in the steps that
// count is still the call's error. A step with templates false or without the
// key does not count, whatever braces its data has.
// Ground: contract — spec 2c: a switch with no effect is refused, as in a call.
func TestScenario_CallWithTemplatesAndNoPlaceholderAnywhereIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  string
	}{
		{"steps without templates", tplScenario(bareTemplatedCall, stepData("", "            note: plain\n"))},
		{"a templated step with no placeholder", tplScenario(bareTemplatedCall, stepData("true", "            note: plain\n"))},
		{"braces of a step with templates false do not count", tplScenario(bareTemplatedCall, stepData("false", "            note: \"{{seq}}\"\n"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireRefusal(t, tc.cfg, config.ErrTemplatesNoPlaceholders, "templates: true", "placeholder")
		})
	}
}

// 2d: a step with no templates key under a templated call may hold no braces:
// the user may think they work. The refusal says how to fix it both ways; only
// a key that is absent comes here (see the false test above).
// Ground: contract — spec 2d: a step's braces are never text by accident.
func TestScenario_BracesInAPlainStepOfATemplatedCallAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		data string
	}{
		{"a value", "            note: \"{{seq}}\"\n"},
		{"unknown braces", "            note: \"{{nope}}\"\n"},
		{"a lone open brace", "            note: \"a {{b\"\n"},
		{"nested", "            child:\n              name: \"{{uuid}}\"\n"},
		{"in a list", "            tags: [\"ok\", \"{{seq}}\"]\n"},
		{"a key", "            attrs:\n              \"{{uuid}}\": v\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireRefusal(t, tplScenario(templatedCall, stepData("", tc.data)), config.ErrScenario,
				"load.calls[0].steps[0]", stepNeedsKey)
		})
	}

	t.Run("the second step names its own index", func(t *testing.T) {
		raw := tplScenario(templatedCall, "        - method: "+getHistory+"\n        - method: "+transfer+"\n"+
			"          data:\n            note: \"{{seq}}\"\n")

		requireRefusal(t, raw, config.ErrScenario, "load.calls[0].steps[1]", stepNeedsKey)
	})

	t.Run("the second call", func(t *testing.T) {
		raw := scenarioCall(templatedCall) + secondCall("          data:\n            note: \"{{seq}}\"\n")

		requireRefusal(t, raw, config.ErrScenario, "load.calls[1].steps[0]", stepNeedsKey)
	})
}

// 2e: a step with templates true must use a placeholder of its own, as a call
// must: the call's one does not make the step's switch do anything.
// Ground: contract — spec 2e: the same refusal as for a call, with the step's path.
func TestScenario_StepTemplatesTrueWithoutPlaceholdersIsRefused(t *testing.T) {
	for name, step := range map[string]string{
		"data without braces": stepHead + stepTemplatesOn + "          data:\n            note: plain\n            n: 5\n",
		"no data":             stepHead + stepTemplatesOn,
		"empty data":          stepHead + stepTemplatesOn + "          data: {}\n",
		"a lone brace":        stepHead + stepTemplatesOn + "          data:\n            note: \"{ seq }\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			// The call has a placeholder, so only the step can be at fault.
			requireRefusal(t, tplScenario(templatedCall, step), config.ErrTemplatesNoPlaceholders,
				"load.calls[0].steps[0]", "templates: true", "placeholder")
		})
	}

	t.Run("the second step names its own index", func(t *testing.T) {
		raw := tplScenario(templatedCall, stepData("true", "            from: \"{{seq}}\"\n")+
			"        - method: "+getHistory+"\n"+stepTemplatesOn+"          data:\n            note: plain\n")

		requireRefusal(t, raw, config.ErrTemplatesNoPlaceholders, "load.calls[0].steps[1]")
	})
}

// The key with no value is lost by the decoder as a key left out, and a step
// would send its braces as text (or refuse them) without saying why; a value
// that is no bool is no answer either. Both are ErrTemplatesValue with the line
// and the full path of the key.
// Ground: contract — spec 1, as dataset_reuse with no value in a call.
func TestScenario_StepTemplatesNotABoolIsAnError(t *testing.T) {
	for name, value := range map[string]string{
		"nothing":        "",
		"a tilde":        " ~",
		"null":           " null",
		"an anchor":      " &a ~",
		"a null tag":     " !!null ~",
		"a number":       " 1",
		"zero":           " 0",
		"a quoted true":  ` "true"`,
		"a quoted false": ` "false"`,
		"a word":         " maybe",
		"a list":         " [true]",
	} {
		t.Run(name, func(t *testing.T) {
			raw := tplScenario(templatedCall, stepHead+"          templates:"+value+"\n          data:\n            note: \"{{seq}}\"\n")

			requireRefusal(t, raw, config.ErrTemplatesValue, "line ", "load.calls[0].steps[0].templates")
		})
	}

	t.Run("an alias of a null", func(t *testing.T) {
		raw := tplScenario(templatedCall, stepHead+"          data: &a ~\n          templates: *a\n")

		requireRefusal(t, raw, config.ErrTemplatesValue, "line ", "load.calls[0].steps[0].templates")
	})
}

// A templated call with plain steps without braces is valid and was before
// this unit: the rules above must not reach it.
// Ground: contract — spec 2d is about braces only; guard, green before and after.
func TestScenario_TemplatedCallWithPlainStepsIsValid(t *testing.T) {
	raw := tplScenario(templatedCall, stepData("", "            from: a-1\n            amount: 100\n")+
		"        - method: "+getHistory+"\n")

	cfg, err := config.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse() = %v, want a templated call with plain steps valid", err)
	}
	for k, step := range cfg.Load.Calls[0].Steps {
		if step.TemplatesOn() {
			t.Errorf("Steps[%d].TemplatesOn = true, want false: no key was written", k)
		}
	}
}
