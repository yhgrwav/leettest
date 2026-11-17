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
	"reflect"
	"testing"

	"github.com/yhgrwav/leettest/pkg/config"
)

// Extract (v0.9 unit 4a, the config side; the checks against the descriptors
// are in internal/cli). Compile stubs: config.Call and config.Step have
// Extract, config.TemplatePart has Extract, none read or checked yet, so every
// test of an extract that is accepted, or refused in the words of the spec, is
// red until the code. The guard test at the end is green now and stays so.
//
// The texts of the refusals are part of the spec: each names the path of what
// is wrong (the extract of a call is load.calls[i].extract, of a step
// load.calls[i].steps[k].extract, a name under it .<name>) and says which rule.

const (
	// extractW and extractWAndAmount are the extract lines of a call, 6 spaces
	// deep, and stepExtractW the same of a step, 10 spaces deep.
	extractW          = "      extract:\n        w: wallet_id\n"
	extractWAndAmount = "      extract:\n        w: wallet_id\n        amount: balance.amount\n"
	stepExtractW      = "          extract:\n            w: wallet_id\n"
	// readsW is a templated Transfer step that reads w; plainStep reads nothing.
	readsW    = "          templates: true\n          data:\n            from: \"{{extract.w}}\"\n"
	plainStep = "        - method: " + getHistory + "\n"

	wantNamePattern = "[A-Za-z_][A-Za-z0-9_]*"
	wantEarlier     = "earlier step"
)

// readStep is a Transfer step with templates on and the given lines of data
// (12 spaces deep), after the lines of extra (10 spaces deep) it may have.
func readStep(extra, data string) string {
	return stepHead + extra + "          templates: true\n          data:\n" + data
}

// A call and its steps may extract, a step reads what an earlier one took, and
// the values are not among the placeholders the report lists: those stay uuid
// and seq.
// Ground: contract — spec 1, 2 and 3: extract is config API.
func TestExtract_IsAccepted(t *testing.T) {
	raw := tplScenario(templatedCall+extractWAndAmount,
		readStep("          extract:\n            tx: id\n",
			"            from: \"{{extract.w}}\"\n            note: \"pay-{{extract.w}}-{{seq}}\"\n            amount: \"{{extract.amount}}\"\n")+
			plainStep+"          templates: true\n          data:\n            id: \"tx-{{extract.tx}}\"\n")

	cfg, err := config.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse() = %v, want extract accepted", err)
	}

	call := cfg.Load.Calls[0]
	if want := map[string]string{"w": "wallet_id", "amount": "balance.amount"}; !reflect.DeepEqual(call.Extract, want) {
		t.Errorf("call.Extract = %v, want %v", call.Extract, want)
	}
	if want := map[string]string{"tx": "id"}; !reflect.DeepEqual(call.Steps[0].Extract, want) {
		t.Errorf("Steps[0].Extract = %v, want %v", call.Steps[0].Extract, want)
	}
	if got := call.Steps[1].Extract; got != nil {
		t.Errorf("Steps[1].Extract = %v, want nil: the key was left out", got)
	}

	if got, want := call.Steps[1].TemplatePlaceholders(), []string(nil); !reflect.DeepEqual(got, want) {
		t.Errorf("a step that reads only extract lists placeholders %v, want none: they stay uuid and seq", got)
	}
	if got, want := call.Steps[0].TemplatePlaceholders(), []string{"seq"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Steps[0].TemplatePlaceholders() = %v, want %v", got, want)
	}
	if got, want := call.AllTemplatePlaceholders(), []string{"seq", "uuid"}; !reflect.DeepEqual(got, want) {
		t.Errorf("AllTemplatePlaceholders() = %v, want %v: no extract name among them", got, want)
	}
}

// A call templated only for its steps' sake has no placeholder of its own and
// lists none: the extract reads count as braces for the check that templates
// change something, and are not listed.
// Ground: contract — spec 3, the closed set of placeholders.
func TestExtract_ACallTemplatedOnlyForExtractListsNoPlaceholders(t *testing.T) {
	raw := tplScenario(bareTemplatedCall+extractW, readStep("", "            from: \"{{extract.w}}\"\n"))

	cfg, err := config.Parse([]byte(raw))
	if err != nil {
		t.Fatalf("Parse() = %v, want templates: true accepted for the sake of an extract read", err)
	}
	if got := cfg.Load.Calls[0].AllTemplatePlaceholders(); got != nil {
		t.Errorf("AllTemplatePlaceholders() = %v, want none", got)
	}
}

// Ground: contract — spec 1 and 6: every shape of extract the spec names is
// refused at start with ErrScenario, naming the path of what is wrong and the
// rule; the call's data and the data of a step are checked, in their order.
func TestExtract_IsRefused(t *testing.T) {
	const getHistoryStep = plainStep

	for _, tc := range []struct {
		name string
		raw  string
		want []string
	}{
		{
			"a name that starts with a digit",
			tplScenario(templatedCall+"      extract:\n        1a: wallet_id\n", getHistoryStep),
			[]string{"load.calls[0].extract.1a", wantNamePattern},
		},
		{
			"a name with a dot",
			tplScenario(templatedCall+"      extract:\n        a.b: wallet_id\n", getHistoryStep),
			[]string{"load.calls[0].extract.a.b", wantNamePattern},
		},
		{
			"a name with a dash in a step",
			tplScenario(templatedCall, stepHead+"          extract:\n            a-b: wallet_id\n"),
			[]string{"load.calls[0].steps[0].extract.a-b", wantNamePattern},
		},
		{
			"a name with a space",
			tplScenario(templatedCall+"      extract:\n        \"a b\": wallet_id\n", getHistoryStep),
			[]string{"load.calls[0].extract.a b", wantNamePattern},
		},
		{
			"a name taken by the call and by a step",
			tplScenario(templatedCall+extractW, readStep(stepExtractW, "            from: \"{{extract.w}}\"\n")),
			[]string{"load.calls[0].steps[0].extract.w", "extracted twice"},
		},
		{
			"a name taken by two steps",
			tplScenario(templatedCall+extractW, readStep("", "            from: \"{{extract.w}}\"\n")+
				stepHead+stepExtractW+plainStep),
			[]string{"load.calls[0].steps[1].extract.w", "extracted twice"},
		},
		{
			"read by the step that extracts it",
			tplScenario(templatedCall, readStep(stepExtractW, "            from: \"{{extract.w}}\"\n")+plainStep),
			[]string{"load.calls[0].steps[0]", "{{extract.w}}", wantEarlier},
		},
		{
			"read before it is extracted",
			tplScenario(templatedCall, readStep("", "            from: \"{{extract.w}}\"\n")+stepHead+stepExtractW),
			[]string{"load.calls[0].steps[0]", "{{extract.w}}", wantEarlier},
		},
		{
			"read by the call itself",
			tplScenario("      templates: true\n      data:\n        id: \"{{extract.w}}\"\n", stepHead+stepExtractW+
				readStep("", "            from: \"{{extract.w}}\"\n")),
			[]string{"load.calls[0].data", "{{extract.w}}", wantEarlier},
		},
		{
			"read and never extracted",
			tplScenario(templatedCall+extractW, readStep("", "            from: \"{{extract.w}}\"\n            to: \"{{extract.y}}\"\n")),
			[]string{"load.calls[0].steps[0]", "{{extract.y}}", wantEarlier},
		},
		{
			"extracted and never read",
			tplScenario(templatedCall+extractW, plainStep),
			[]string{"load.calls[0].extract.w", "never read"},
		},
		{
			"extracted by a step and never read",
			tplScenario(templatedCall, stepHead+stepExtractW+plainStep),
			[]string{"load.calls[0].steps[0].extract.w", "never read"},
		},
		{
			"read as text by a step without templates",
			tplScenario(extractW, stepData("", "            from: \"{{extract.w}}\"\n")),
			[]string{"load.calls[0].extract.w", "never read"},
		},
		{
			"extract on a call without steps",
			scenarioCall(extractW),
			[]string{"load.calls[0].extract", "extract needs steps"},
		},
		{
			"call extract with no value",
			tplScenario("      extract:\n"+templatedCall, getHistoryStep),
			[]string{"load.calls[0].extract", "has no value"},
		},
		{
			"call extract null",
			tplScenario(templatedCall+"      extract: ~\n", getHistoryStep),
			[]string{"load.calls[0].extract", "has no value"},
		},
		{
			"step extract with no value",
			tplScenario(templatedCall, stepHead+"          extract:\n          templates: true\n"),
			[]string{"load.calls[0].steps[0].extract", "has no value"},
		},
		{
			"call extract empty",
			tplScenario(templatedCall+"      extract: {}\n", getHistoryStep),
			[]string{"load.calls[0].extract", "must not be empty"},
		},
		{
			"step extract empty",
			tplScenario(templatedCall, stepHead+"          extract: {}\n"),
			[]string{"load.calls[0].steps[0].extract", "must not be empty"},
		},
		{
			"call extract a list",
			tplScenario(templatedCall+"      extract: [a]\n", getHistoryStep),
			[]string{"load.calls[0].extract", "must map names to paths"},
		},
		{
			"step extract a list",
			tplScenario(templatedCall, stepHead+"          extract: [a]\n"),
			[]string{"load.calls[0].steps[0].extract", "must map names to paths"},
		},
		{
			"call extract a scalar",
			tplScenario(templatedCall+"      extract: abc\n", getHistoryStep),
			[]string{"load.calls[0].extract", "must map names to paths"},
		},
		{
			"a path that is a number",
			tplScenario(templatedCall+"      extract: {w: 1}\n", getHistoryStep),
			[]string{"load.calls[0].extract.w", "is not a path"},
		},
		{
			"a path that is null",
			tplScenario(templatedCall, stepHead+"          extract: {w: ~}\n"),
			[]string{"load.calls[0].steps[0].extract.w", "is not a path"},
		},
		{
			"a path that is empty",
			tplScenario(templatedCall+"      extract: {w: \"\"}\n", getHistoryStep),
			[]string{"load.calls[0].extract.w", "is not a path"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireRefusal(t, tc.raw, config.ErrScenario, tc.want...)
		})
	}
}

// Ground: contract — spec 1: {{extract.<name>}} is the third placeholder,
// written exactly like the other two (no spaces, no other case), the name a
// letter or underscore and letters, digits and underscores; a text can hold it
// among other parts.
func TestParseTemplate_ExtractPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		text  string
		parts []config.TemplatePart
	}{
		{"{{extract.wallet}}", []config.TemplatePart{{Extract: "wallet"}}},
		{"{{extract._x9}}", []config.TemplatePart{{Extract: "_x9"}}},
		{"{{extract.Wallet_ID}}", []config.TemplatePart{{Extract: "Wallet_ID"}}},
		{
			"w-{{extract.w_1}}-{{seq}}-{{uuid}}",
			[]config.TemplatePart{
				{Text: "w-"}, {Extract: "w_1"}, {Text: "-"}, {Placeholder: "seq"}, {Text: "-"}, {Placeholder: "uuid"},
			},
		},
		{"{{extract.a}}{{extract.b}}", []config.TemplatePart{{Extract: "a"}, {Extract: "b"}}},
	} {
		t.Run(tc.text, func(t *testing.T) {
			parts, bad := config.ParseTemplate(tc.text)
			if bad != "" {
				t.Fatalf("ParseTemplate(%q) bad = %q, want none", tc.text, bad)
			}
			if !reflect.DeepEqual(parts, tc.parts) {
				t.Errorf("ParseTemplate(%q) = %+v, want %+v", tc.text, parts, tc.parts)
			}
		})
	}
}

// Ground: contract — spec 1: a placeholder that is not written exactly is
// refused whole, from its {{ to the next }} (or the end of the text).
func TestParseTemplate_ExtractPlaceholderWrittenWrongIsBad(t *testing.T) {
	for _, tc := range []struct{ text, bad string }{
		{"a {{extract.}} b", "{{extract.}}"},
		{"{{extract.1a}}", "{{extract.1a}}"},
		{"{{ extract.w }}", "{{ extract.w }}"},
		{"{{extract.w }}", "{{extract.w }}"},
		{"{{extract. w}}", "{{extract. w}}"},
		{"{{Extract.w}}", "{{Extract.w}}"},
		{"{{extract.a-b}}", "{{extract.a-b}}"},
		{"{{extract.a.b}}", "{{extract.a.b}}"},
		{"{{extract}}", "{{extract}}"},
		{"x {{extract.w", "{{extract.w"},
	} {
		t.Run(tc.text, func(t *testing.T) {
			parts, bad := config.ParseTemplate(tc.text)
			if bad != tc.bad {
				t.Errorf("ParseTemplate(%q) bad = %q, want %q", tc.text, bad, tc.bad)
			}
			if parts != nil {
				t.Errorf("ParseTemplate(%q) parts = %+v, want none with a bad fragment", tc.text, parts)
			}
		})
	}
}

// Ground: contract — spec 1: the refusal of a placeholder that is none names
// the three forms, in a step and in the call.
func TestExtract_TheUnknownPlaceholderErrorNamesThreeForms(t *testing.T) {
	for name, raw := range map[string]string{
		"a step": tplScenario(templatedCall, stepData("true", "            note: \"{{nope}}\"\n")),
		"a call": tplScenario("      templates: true\n      data:\n        id: \"{{nope}}\"\n", plainStep),
	} {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, raw, config.ErrTemplatesUnknown, "{{uuid}}", "{{seq}}", "{{extract.<name>}}")
		})
	}
}

// A scenario that does not extract keeps its extract fields nil.
// Ground: contract — the key left out is nil, as templates.
func TestExtract_LeftOutIsNil(t *testing.T) {
	cfg, err := config.Parse([]byte(twoStepScenario(twoSteps)))
	if err != nil {
		t.Fatalf("Parse() = %v, want a valid scenario config", err)
	}

	call := cfg.Load.Calls[0]
	if call.Extract != nil {
		t.Errorf("call.Extract = %v, want nil", call.Extract)
	}
	for k, step := range call.Steps {
		if step.Extract != nil {
			t.Errorf("Steps[%d].Extract = %v, want nil", k, step.Extract)
		}
	}
}
