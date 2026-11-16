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

package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

var callDatasetPath = regexp.MustCompile(`^\$\.load\.calls\[\d+\]\.(dataset|dataset_order|dataset_seed|dataset_reuse|templates)$`)

// passPath matches a pass section of a call or of the search and, when the
// match has a key, a limit inside it.
var passPath = regexp.MustCompile(`^\$\.(load\.calls\[\d+\]|load\.breakpoint)\.pass(\.[A-Za-z0-9_]+)?$`)

// scenarioKeyPath matches scenario and steps of a call, stepKeyPath a key of
// one step.
var (
	scenarioKeyPath = regexp.MustCompile(`^\$\.load\.calls\[(\d+)\]\.(scenario|steps)$`)
	stepKeyPath     = regexp.MustCompile(`^\$\.load\.calls\[(\d+)\]\.steps\[(\d+)\]\.([^.\[\]]+)$`)
)

// stepKeysLater are the keys of a call that a step will take, and does not yet:
// refused by name, so the user does not read them as typos.
var stepKeysLater = map[string]bool{
	"dataset_reuse": true, "rps": true, "duration": true, "pass": true,
}

// stepDatasetKeys are the dataset keys a step takes, as a call does.
var stepDatasetKeys = map[string]bool{"dataset": true, "dataset_order": true, "dataset_seed": true}

// errorsLimitPath is the search's errors_limit: left with no value it would
// be read as left out, and the search would run at the default without a word.
const errorsLimitPath = "$.load.breakpoint.errors_limit"

// emptyKeyErrors is the error of each key that must not be written with no value.
var emptyKeyErrors = map[string]error{
	"dataset":       ErrEmptyDatasetPath,
	"dataset_order": ErrDatasetOrder,
	"dataset_seed":  ErrDatasetSeed,
	"dataset_reuse": ErrDatasetReuse,
	"templates":     ErrTemplatesValue,
}

// checkTree runs the checks the decoder cannot make. An anchor name defined
// twice is refused first (see collectAnchors), so every alias has one anchor.
// Then it rejects `dataset:`, `dataset: ~` and `dataset: null` under a call
// (and the same for dataset_order and dataset_seed, whose lost value would
// change the run without a word),
// also behind an anchor (`&a ~`) or a tag (`!!null`, `&a !!null`), and an alias
// (`*a`, also behind a tag) of an anchor that marks a null. The decoder reads
// each as the key left out, so the path is lost without a word and the call
// runs with an empty message. An alias with no anchor is the decoder's error.
// The same for `pass:` of a call or of the search and for each limit inside
// it: a threshold written with no value would never be judged.
func checkTree(raw []byte) error {
	file, err := parser.ParseBytes(raw, 0)
	if err != nil {
		return err
	}

	anchors, err := collectAnchors(file)
	if err != nil {
		return err
	}

	v := nullDatasetVisitor{anchors: anchors}
	walkDocs(file, &v)

	return errors.Join(v.errs...)
}

type nullDatasetVisitor struct {
	anchors map[string]ast.Node
	errs    []error
}

func (v *nullDatasetVisitor) Visit(node ast.Node) ast.Visitor {
	pair, ok := node.(*ast.MappingValueNode)
	if !ok {
		return v
	}
	path := pair.GetPath()
	if scenarioKeyPath.MatchString(path) || stepKeyPath.MatchString(path) {
		v.visitScenarioKey(pair, path)

		return v
	}
	if passPath.MatchString(path) {
		if value, resolved := v.unwrap(pair.Value); resolved {
			if _, null := value.(*ast.NullNode); null {
				v.errs = append(v.errs, fmt.Errorf("line %d: %w", pair.Key.GetToken().Position.Line,
					passErr("%s has no value: write the limit or leave it out", strings.TrimPrefix(path, "$."))))
			}
		}

		return v
	}
	match := callDatasetPath.FindStringSubmatch(path)
	errorsLimit := path == errorsLimitPath
	if path == "$.app.mode" {
		if value, resolved := v.unwrap(pair.Value); resolved {
			if _, null := value.(*ast.NullNode); null {
				v.errs = append(v.errs, fmt.Errorf("line %d: %w: no value", pair.Key.GetToken().Position.Line, ErrInvalidMode))
			}
		}

		return v
	}
	if path == "$.load.stop_on_errors" {
		if value, resolved := v.unwrap(pair.Value); resolved {
			if _, null := value.(*ast.NullNode); null {
				v.errs = append(v.errs, fmt.Errorf("line %d: %w: has no value: write a percent such as 5%%, or leave the key out",
					pair.Key.GetToken().Position.Line, ErrInvalidStopOnErrors))
			}
		}

		return v
	}
	if match == nil && !errorsLimit {
		return v
	}

	value, resolved := v.unwrap(pair.Value)
	if !resolved {
		return v
	}

	_, null := value.(*ast.NullNode)
	if errorsLimit {
		if null {
			v.errs = append(v.errs, fmt.Errorf("line %d: %w: breakpoint.errors_limit has no value: write a percent such as 1%%, or leave the key out for the default",
				pair.Key.GetToken().Position.Line, ErrBreakpoint))
		}

		return v
	}
	// The decoder takes 1.5 and "42" for a whole number; a seed is neither.
	_, whole := value.(*ast.IntegerNode)
	if null || (match[1] == "dataset_seed" && !whole) {
		v.errs = append(v.errs, fmt.Errorf("line %d: %w", pair.Key.GetToken().Position.Line, emptyKeyErrors[match[1]]))
	}

	return v
}

// visitScenarioKey refuses a scenario, steps, method or timeout written with no
// value (the decoder reads it as left out), a step key a step does not take
// yet, and one it does not have. Every message names the key by its path.
func (v *nullDatasetVisitor) visitScenarioKey(pair *ast.MappingValueNode, path string) {
	line := pair.Key.GetToken().Position.Line
	name := strings.TrimPrefix(path, "$.")

	m := stepKeyPath.FindStringSubmatch(path)
	if len(m) > 3 && m[3] != "method" && m[3] != "timeout" && m[3] != "data" && m[3] != "templates" && !stepDatasetKeys[m[3]] {
		if stepKeysLater[m[3]] {
			v.errs = append(v.errs, fmt.Errorf("line %d: %w: %s is not supported in a step yet", line, ErrScenario, name))
		} else {
			v.errs = append(v.errs, fmt.Errorf("line %d: %w: %s is not a step key: a step has method, timeout, data, templates, "+
				"dataset, dataset_order and dataset_seed", line, ErrScenario, name))
		}

		return
	}
	// A dataset key with no value is lost by the decoder as left out: the step
	// would run on its own data, as the call's keys (emptyKeyErrors). A seed is a
	// whole number, which the decoder does not insist on.
	if len(m) > 3 && stepDatasetKeys[m[3]] {
		value, resolved := v.unwrap(pair.Value)
		if !resolved {
			return
		}

		_, null := value.(*ast.NullNode)
		_, whole := value.(*ast.IntegerNode)

		switch {
		case null:
			v.errs = append(v.errs, fmt.Errorf("line %d: %w: %s has no value: write it or leave it out", line, emptyKeyErrors[m[3]], name))
		case m[3] == "dataset_seed" && !whole:
			v.errs = append(v.errs, fmt.Errorf("line %d: %w: %s is not a whole number", line, ErrDatasetSeed, name))
		}

		return
	}
	// data with no value is an empty message, as in a call.
	if len(m) > 3 && m[3] == "data" {
		return
	}
	// templates is true or false: the decoder reads no value as the key left
	// out, and takes "true" and 1 for a bool.
	if len(m) > 3 && m[3] == "templates" {
		if value, resolved := v.unwrap(pair.Value); resolved {
			if _, isBool := value.(*ast.BoolNode); !isBool {
				v.errs = append(v.errs, fmt.Errorf("line %d: %w: %s is not true or false: write one of them or leave it out",
					line, ErrTemplatesValue, name))
			}
		}

		return
	}

	if value, resolved := v.unwrap(pair.Value); resolved {
		if _, null := value.(*ast.NullNode); null {
			v.errs = append(v.errs, fmt.Errorf("line %d: %w: %s has no value: write it or leave it out", line, ErrScenario, name))
		}
	}
}

// unwrap strips anchors and tags and follows aliases to the node a value is.
// It is false for an alias with no anchor or one that loops.
func (v *nullDatasetVisitor) unwrap(value ast.Node) (ast.Node, bool) {
	followed := map[string]bool{}
	for {
		switch wrapped := value.(type) {
		case *ast.AnchorNode:
			value = wrapped.Value
		case *ast.TagNode:
			value = wrapped.Value
		case *ast.AliasNode:
			name := wrapped.Value.String()
			target, defined := v.anchors[name]
			if !defined || followed[name] {
				return nil, false
			}
			followed[name] = true
			value = target
		default:
			return value, true
		}
	}
}
