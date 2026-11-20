package cmd

import (
	"encoding/csv"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// This file is the reason `--format json` and `--format csv` cannot
// drift apart again.
//
// Each data subcommand declares one flat row struct whose `json` tags
// are the canonical §4 field names. The JSON codec marshals that struct
// directly; the CSV codec derives its header and its cells from the
// same struct by reflection. There is no second list of column names to
// forget to update, which is how #20, #27 and #42 happened: the CSV
// writer and the JSON struct were maintained independently and nothing
// compared them.
//
// Adding a field is therefore a one-line change that lands in both
// codecs at once. Renaming one is a wire-format change and shows up in
// cmd/parity_test.go's declared schema, so it cannot pass review
// unnoticed.

// writeCSV renders rows — a slice of flat row structs — as CSV on
// stdout, with the header taken from the struct's json tags in field
// order. An empty slice still emits the header, per CONTRACT §4's
// empty-result rule.
func writeCSV[T any](rows []T) error {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	var zero T
	header, err := csvHeader(reflect.TypeOf(zero))
	if err != nil {
		return err
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		cells, err := csvCells(reflect.ValueOf(row))
		if err != nil {
			return err
		}
		if err := w.Write(cells); err != nil {
			return err
		}
	}
	return w.Error()
}

// csvHeader returns the json tag names of t's fields, in declaration
// order. t must be a struct of scalar (or pointer-to-scalar) fields:
// §4's row schema is flat, so a nested struct is a programming error
// rather than something to flatten silently.
func csvHeader(t reflect.Type) ([]string, error) {
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("csv: row type %v is not a struct", t)
	}
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// Skip unexported fields, matching encoding/json. Row structs
		// use them to carry sort keys (e.g. the measurement instant as
		// a time.Time) that are not part of the wire surface; emitting
		// them in CSV would break parity rather than preserve it.
		if f.PkgPath != "" {
			continue
		}
		name, err := fieldName(f)
		if err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, nil
}

// fieldName extracts the canonical field name from a struct field's
// json tag. A missing tag is an error rather than a fallback to the Go
// field name: the tag is the wire contract, and silently exporting
// `HRAvg` because someone forgot `json:"hr_avg"` is exactly the class
// of drift this file exists to prevent.
func fieldName(f reflect.StructField) (string, error) {
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return "", fmt.Errorf("csv: field %s has no json tag", f.Name)
	}
	name, opts, _ := strings.Cut(tag, ",")
	if name == "" || name == "-" {
		return "", fmt.Errorf("csv: field %s has json tag %q; every row field must be named", f.Name, tag)
	}
	// `omitempty` would make the key disappear from JSON while the CSV
	// column stayed, so the two codecs would disagree on the key set
	// for exactly the rows where the value is missing. Nullable fields
	// use a pointer and emit `null` instead, per §4.
	if strings.Contains(opts, "omitempty") {
		return "", fmt.Errorf("csv: field %s uses omitempty; use a pointer so the key stays and the value is null (CONTRACT §4)", f.Name)
	}
	return name, nil
}

// csvCells renders one row struct as CSV cells. The formatting rules
// are the CSV side of the parity contract: CSV has no types and no
// null, so a number becomes its shortest round-tripping decimal and an
// absent value becomes an empty cell.
func csvCells(v reflect.Value) ([]string, error) {
	out := make([]string, 0, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		if v.Type().Field(i).PkgPath != "" {
			continue
		}
		cell, err := csvCell(v.Field(i))
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", v.Type().Field(i).Name, err)
		}
		out = append(out, cell)
	}
	return out, nil
}

func csvCell(v reflect.Value) (string, error) {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "", nil
		}
		return csvCell(v.Elem())
	}
	switch v.Kind() {
	case reflect.String:
		return v.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Float32, reflect.Float64:
		// 'f' with precision -1 is the shortest decimal that parses
		// back to the same float64 — the same text encoding/json
		// produces, so the two codecs print byte-identical numbers.
		return strconv.FormatFloat(v.Float(), 'f', -1, 64), nil
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	default:
		return "", fmt.Errorf("unsupported kind %s; §4 row fields are scalars", v.Kind())
	}
}

// round returns v rounded to n decimal places.
//
// It round-trips through decimal text rather than using
// math.Round(v*10ⁿ)/10ⁿ, because the multiply-and-divide form
// reintroduces the very noise it is meant to remove: for
// v = 107.35000000000001 and n = 3 it can land on a float whose
// shortest representation still has a long tail. Formatting to n
// decimals and parsing back lands on the nearest float64 to the
// n-decimal value, whose shortest representation is that value.
//
// This is what #26 and #21 asked for — the same rounding markdown
// already applied via fmtRound/fmtFloat1 — but applied to the number
// itself, so both structured codecs inherit it.
func round(v float64, n int) float64 {
	r, err := strconv.ParseFloat(strconv.FormatFloat(v, 'f', n, 64), 64)
	if err != nil {
		return v
	}
	return r
}

// numPtr returns a pointer to v rounded to n decimals, for a nullable
// numeric row field whose value is known.
func numPtr(v float64, n int) *float64 {
	r := round(v, n)
	return &r
}

// intPtr returns a pointer to v, for a nullable integer row field whose
// value is known.
func intPtr(v int) *int { return &v }

// deref returns *p, or 0 when p is nil. Used by the markdown renderers,
// which suppress zero-valued lines anyway and so need no null handling.
func deref[T int | float64](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
