// Copyright (C) 2025 Aleutian AI (jinterlante@aleutian.ai)
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
// See the LICENSE.txt file for the full license text.
//
// NOTE: This work is subject to additional terms under AGPL v3 Section 7.
// See the NOTICE.txt file for details regarding AI system attribution.

package correctness

import (
	"fmt"
	"reflect"
	"sync/atomic"

	"github.com/AleutianAI/AleutianFOSS/services/code_buddy/eval"
)

// -----------------------------------------------------------------------------
// Common Property Factories
// -----------------------------------------------------------------------------

// NoSoftSignalProperty creates a property that verifies no soft signals
// are used for state mutations.
//
// Description:
//
//	This is the most critical property for the hard/soft signal boundary.
//	It checks that any delta or state mutation only comes from hard signals
//	(compiler, test, type checker, linter, syntax).
//
// Inputs:
//   - getDelta: Function to extract the delta from the output.
//   - getSource: Function to get the signal source from the delta.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
//
// Example:
//
//	prop := correctness.NoSoftSignalProperty(
//	    func(output any) any { return output.(*CDCLResult).LearnedClauses },
//	    func(delta any) eval.SignalSource { return delta.(*Clause).Source },
//	)
func NoSoftSignalProperty(
	getDelta func(output any) any,
	getSource func(delta any) eval.SignalSource,
) eval.Property {
	return eval.Property{
		Name:        "no_soft_signal_mutations",
		Description: "State mutations only come from hard signals (compiler/test/linter)",
		Tags:        []string{"critical", "boundary"},
		Check: func(input, output any) error {
			delta := getDelta(output)
			if delta == nil {
				return nil // No delta, no problem
			}

			// Handle slice of deltas
			v := reflect.ValueOf(delta)
			if v.Kind() == reflect.Slice {
				for i := 0; i < v.Len(); i++ {
					item := v.Index(i).Interface()
					source := getSource(item)
					if source.IsSoft() {
						return fmt.Errorf("%w: delta %v uses soft signal %s",
							eval.ErrSoftSignalViolation, item, source)
					}
				}
				return nil
			}

			// Single delta
			source := getSource(delta)
			if source.IsSoft() {
				return fmt.Errorf("%w: delta uses soft signal %s",
					eval.ErrSoftSignalViolation, source)
			}
			return nil
		},
	}
}

// IdempotenceProperty creates a property that verifies applying the same
// operation twice has the same effect as applying it once.
//
// Description:
//
//	Idempotence is important for retry safety and crash recovery.
//	This property verifies that f(f(x)) == f(x).
//
// Inputs:
//   - apply: Function to apply the operation.
//   - equals: Function to check equality of results.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func IdempotenceProperty(
	apply func(state, delta any) any,
	equals func(a, b any) bool,
) eval.Property {
	return eval.Property{
		Name:        "idempotence",
		Description: "Applying the same operation twice equals applying once",
		Tags:        []string{"consistency"},
		Check: func(input, output any) error {
			// input is (state, delta) pair
			pair := input.([]any)
			state, delta := pair[0], pair[1]

			// Apply once
			result1 := apply(state, delta)

			// Apply twice
			result2 := apply(result1, delta)

			if !equals(result1, result2) {
				return fmt.Errorf("not idempotent: f(x) != f(f(x))")
			}
			return nil
		},
	}
}

// MonotonicProperty creates a property that verifies a value only increases.
//
// Description:
//
//	Useful for generation counters, sequence numbers, etc. Uses atomic
//	operations for thread safety when used in parallel verification.
//
// Inputs:
//   - getValue: Function to extract the monotonic value.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
//
// Thread Safety: Safe for concurrent use via atomic operations.
//
// Limitations:
//   - In parallel verification, order of checks is non-deterministic.
//     The property verifies that each observed value is >= the previous
//     observed value in that goroutine's execution order.
func MonotonicProperty(getValue func(output any) int64) eval.Property {
	var lastValue atomic.Int64

	return eval.Property{
		Name:        "monotonic_increase",
		Description: "Value only increases, never decreases",
		Tags:        []string{"consistency"},
		Check: func(input, output any) error {
			value := getValue(output)
			for {
				prev := lastValue.Load()
				if value < prev {
					return fmt.Errorf("monotonicity violated: %d < %d", value, prev)
				}
				// Only update if value is greater (monotonic increase)
				if value <= prev || lastValue.CompareAndSwap(prev, value) {
					return nil
				}
				// CAS failed, retry with new prev value
			}
		},
	}
}

// BoundedProperty creates a property that verifies a value stays within bounds.
//
// Inputs:
//   - getValue: Function to extract the value.
//   - min: Minimum allowed value.
//   - max: Maximum allowed value.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func BoundedProperty(getValue func(output any) float64, min, max float64) eval.Property {
	return eval.Property{
		Name:        "bounded_value",
		Description: fmt.Sprintf("Value stays within [%v, %v]", min, max),
		Tags:        []string{"boundary"},
		Check: func(input, output any) error {
			value := getValue(output)
			if value < min || value > max {
				return fmt.Errorf("value %v outside bounds [%v, %v]", value, min, max)
			}
			return nil
		},
	}
}

// NonNilProperty creates a property that verifies a field is never nil.
//
// Inputs:
//   - getField: Function to extract the field.
//   - fieldName: Name of the field for error messages.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func NonNilProperty(getField func(output any) any, fieldName string) eval.Property {
	return eval.Property{
		Name:        "non_nil_" + fieldName,
		Description: fmt.Sprintf("%s is never nil", fieldName),
		Tags:        []string{"safety"},
		Check: func(input, output any) error {
			field := getField(output)
			if field == nil {
				return fmt.Errorf("%s is nil", fieldName)
			}
			v := reflect.ValueOf(field)
			if v.Kind() == reflect.Ptr && v.IsNil() {
				return fmt.Errorf("%s is nil pointer", fieldName)
			}
			return nil
		},
	}
}

// ConsistencyProperty creates a property that verifies two values are consistent.
//
// Inputs:
//   - getA: Function to extract first value.
//   - getB: Function to extract second value.
//   - isConsistent: Function to check consistency.
//   - description: Description of what consistency means.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func ConsistencyProperty(
	getA, getB func(output any) any,
	isConsistent func(a, b any) bool,
	description string,
) eval.Property {
	return eval.Property{
		Name:        "consistency",
		Description: description,
		Tags:        []string{"consistency"},
		Check: func(input, output any) error {
			a := getA(output)
			b := getB(output)
			if !isConsistent(a, b) {
				return fmt.Errorf("inconsistent: %v and %v", a, b)
			}
			return nil
		},
	}
}

// -----------------------------------------------------------------------------
// Signal Source Properties
// -----------------------------------------------------------------------------

// OnlyHardSignalsProperty creates a property that verifies all signal sources
// in a collection are hard signals.
//
// Inputs:
//   - getSources: Function to extract signal sources from output.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func OnlyHardSignalsProperty(getSources func(output any) []eval.SignalSource) eval.Property {
	return eval.Property{
		Name:        "only_hard_signals",
		Description: "All signal sources are hard (compiler, test, linter, etc.)",
		Tags:        []string{"critical", "boundary"},
		Check: func(input, output any) error {
			sources := getSources(output)
			for i, source := range sources {
				if !source.IsHard() {
					return fmt.Errorf("signal source %d is not hard: %s", i, source)
				}
			}
			return nil
		},
	}
}

// NoUnknownSourceProperty creates a property that verifies no signal source
// is unknown.
//
// Inputs:
//   - getSources: Function to extract signal sources from output.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func NoUnknownSourceProperty(getSources func(output any) []eval.SignalSource) eval.Property {
	return eval.Property{
		Name:        "no_unknown_source",
		Description: "All signal sources are explicitly set",
		Tags:        []string{"safety"},
		Check: func(input, output any) error {
			sources := getSources(output)
			for i, source := range sources {
				if source == eval.SourceUnknown {
					return fmt.Errorf("signal source %d is unknown", i)
				}
			}
			return nil
		},
	}
}

// -----------------------------------------------------------------------------
// Collection Properties
// -----------------------------------------------------------------------------

// NonEmptyProperty creates a property that verifies a collection is not empty.
//
// Inputs:
//   - getCollection: Function to extract the collection.
//   - collectionName: Name of the collection for error messages.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func NonEmptyProperty(getCollection func(output any) any, collectionName string) eval.Property {
	return eval.Property{
		Name:        "non_empty_" + collectionName,
		Description: fmt.Sprintf("%s is not empty", collectionName),
		Tags:        []string{"validity"},
		Check: func(input, output any) error {
			collection := getCollection(output)
			v := reflect.ValueOf(collection)

			switch v.Kind() {
			case reflect.Slice, reflect.Map, reflect.Array, reflect.String:
				if v.Len() == 0 {
					return fmt.Errorf("%s is empty", collectionName)
				}
			default:
				return fmt.Errorf("cannot check emptiness of %T", collection)
			}
			return nil
		},
	}
}

// NoDuplicatesProperty creates a property that verifies a slice has no duplicates.
//
// Inputs:
//   - getSlice: Function to extract the slice.
//   - getKey: Function to get a comparable key from each element.
//   - sliceName: Name of the slice for error messages.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func NoDuplicatesProperty(
	getSlice func(output any) any,
	getKey func(elem any) any,
	sliceName string,
) eval.Property {
	return eval.Property{
		Name:        "no_duplicates_" + sliceName,
		Description: fmt.Sprintf("%s has no duplicate elements", sliceName),
		Tags:        []string{"validity"},
		Check: func(input, output any) error {
			slice := getSlice(output)
			v := reflect.ValueOf(slice)

			if v.Kind() != reflect.Slice {
				return fmt.Errorf("expected slice, got %T", slice)
			}

			seen := make(map[any]bool)
			for i := 0; i < v.Len(); i++ {
				elem := v.Index(i).Interface()
				key := getKey(elem)
				if seen[key] {
					return fmt.Errorf("duplicate found in %s: %v", sliceName, key)
				}
				seen[key] = true
			}
			return nil
		},
	}
}

// AllSatisfyProperty creates a property that verifies all elements satisfy a predicate.
//
// Inputs:
//   - getSlice: Function to extract the slice.
//   - predicate: Function to check each element.
//   - description: Description of what the predicate checks.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func AllSatisfyProperty(
	getSlice func(output any) any,
	predicate func(elem any) bool,
	description string,
) eval.Property {
	return eval.Property{
		Name:        "all_satisfy",
		Description: description,
		Tags:        []string{"validity"},
		Check: func(input, output any) error {
			slice := getSlice(output)
			v := reflect.ValueOf(slice)

			if v.Kind() != reflect.Slice {
				return fmt.Errorf("expected slice, got %T", slice)
			}

			for i := 0; i < v.Len(); i++ {
				elem := v.Index(i).Interface()
				if !predicate(elem) {
					return fmt.Errorf("element %d does not satisfy predicate: %v", i, elem)
				}
			}
			return nil
		},
	}
}

// -----------------------------------------------------------------------------
// Numeric Properties
// -----------------------------------------------------------------------------

// SumProperty creates a property that verifies parts sum to a total.
//
// Inputs:
//   - getParts: Function to extract the parts.
//   - getTotal: Function to extract the expected total.
//   - tolerance: Allowed difference (for floating point).
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func SumProperty(
	getParts func(output any) []float64,
	getTotal func(output any) float64,
	tolerance float64,
) eval.Property {
	return eval.Property{
		Name:        "sum_consistency",
		Description: "Parts sum to total within tolerance",
		Tags:        []string{"consistency"},
		Check: func(input, output any) error {
			parts := getParts(output)
			total := getTotal(output)

			var sum float64
			for _, part := range parts {
				sum += part
			}

			diff := sum - total
			if diff < 0 {
				diff = -diff
			}

			if diff > tolerance {
				return fmt.Errorf("sum %v != total %v (diff %v > tolerance %v)",
					sum, total, diff, tolerance)
			}
			return nil
		},
	}
}

// ProbabilityProperty creates a property that verifies values are valid probabilities.
//
// Inputs:
//   - getProbs: Function to extract probability values.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func ProbabilityProperty(getProbs func(output any) []float64) eval.Property {
	return eval.Property{
		Name:        "valid_probabilities",
		Description: "All probabilities are in [0, 1]",
		Tags:        []string{"validity"},
		Check: func(input, output any) error {
			probs := getProbs(output)
			for i, p := range probs {
				if p < 0 || p > 1 {
					return fmt.Errorf("probability %d is %v, not in [0, 1]", i, p)
				}
			}
			return nil
		},
	}
}

// ProbabilityDistributionProperty creates a property that verifies probabilities sum to 1.
//
// Inputs:
//   - getProbs: Function to extract probability values.
//   - tolerance: Allowed difference from 1.0.
//
// Outputs:
//   - eval.Property: A property that can be used for verification.
func ProbabilityDistributionProperty(getProbs func(output any) []float64, tolerance float64) eval.Property {
	return eval.Property{
		Name:        "probability_distribution",
		Description: "Probabilities sum to 1 within tolerance",
		Tags:        []string{"consistency"},
		Check: func(input, output any) error {
			probs := getProbs(output)

			var sum float64
			for _, p := range probs {
				sum += p
			}

			diff := sum - 1.0
			if diff < 0 {
				diff = -diff
			}

			if diff > tolerance {
				return fmt.Errorf("probabilities sum to %v, not 1 (diff %v > tolerance %v)",
					sum, diff, tolerance)
			}
			return nil
		},
	}
}

// -----------------------------------------------------------------------------
// Property Combinators
// -----------------------------------------------------------------------------

// And combines multiple properties into one that passes only if all pass.
//
// Inputs:
//   - name: Name for the combined property.
//   - properties: Properties to combine.
//
// Outputs:
//   - eval.Property: A property that passes only if all pass.
func And(name string, properties ...eval.Property) eval.Property {
	return eval.Property{
		Name:        name,
		Description: "All sub-properties must pass",
		Tags:        []string{"composite"},
		Check: func(input, output any) error {
			for _, prop := range properties {
				if err := prop.Check(input, output); err != nil {
					return fmt.Errorf("%s: %w", prop.Name, err)
				}
			}
			return nil
		},
	}
}

// Or combines multiple properties into one that passes if any pass.
//
// Inputs:
//   - name: Name for the combined property.
//   - properties: Properties to combine.
//
// Outputs:
//   - eval.Property: A property that passes if any pass.
func Or(name string, properties ...eval.Property) eval.Property {
	return eval.Property{
		Name:        name,
		Description: "At least one sub-property must pass",
		Tags:        []string{"composite"},
		Check: func(input, output any) error {
			var lastErr error
			for _, prop := range properties {
				if err := prop.Check(input, output); err == nil {
					return nil
				} else {
					lastErr = err
				}
			}
			return fmt.Errorf("all sub-properties failed, last error: %w", lastErr)
		},
	}
}

// Implies creates a property that checks "if A then B".
//
// Inputs:
//   - name: Name for the combined property.
//   - condition: The "if" part.
//   - consequence: The "then" part.
//
// Outputs:
//   - eval.Property: A property implementing implication.
func Implies(name string, condition, consequence eval.Property) eval.Property {
	return eval.Property{
		Name:        name,
		Description: fmt.Sprintf("If %s then %s", condition.Name, consequence.Name),
		Tags:        []string{"composite"},
		Check: func(input, output any) error {
			// If condition fails, implication is vacuously true
			if err := condition.Check(input, output); err != nil {
				return nil
			}
			// Condition passed, so consequence must pass
			if err := consequence.Check(input, output); err != nil {
				return fmt.Errorf("condition %s passed but consequence %s failed: %w",
					condition.Name, consequence.Name, err)
			}
			return nil
		},
	}
}
