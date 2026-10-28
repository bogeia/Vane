package data

import (
	"slices"
	"testing"
)

func TestTextArrayValueRoundTrips(t *testing.T) {
	for _, test := range []struct {
		name  string
		value []string
	}{
		{name: "empty array", value: []string{}},
		{name: "quoted comma and backslash", value: []string{`a"b`, "comma,value", `back\slash`, "plain"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := textArray(test.value).Value()
			if err != nil {
				t.Fatal(err)
			}
			text, ok := encoded.(string)
			if !ok {
				t.Fatalf("Value() type = %T, want string", encoded)
			}

			for _, source := range []struct {
				name  string
				value any
			}{
				{name: "string", value: text},
				{name: "bytes", value: []byte(text)},
			} {
				t.Run(source.name, func(t *testing.T) {
					var decoded textArray
					if err := decoded.Scan(source.value); err != nil {
						t.Fatalf("scan PostgreSQL text array: %v", err)
					}
					if !slices.Equal(decoded, test.value) {
						t.Fatalf("decoded %q, want %q", decoded, test.value)
					}
				})
			}
		})
	}
}

func TestTextArrayScanNullAndUnsupportedValues(t *testing.T) {
	value := textArray{"existing"}
	if err := value.Scan(nil); err != nil {
		t.Fatalf("scan SQL NULL: %v", err)
	}
	if value != nil {
		t.Fatalf("scan SQL NULL left value %q, want nil", value)
	}
	if err := value.Scan(42); err == nil {
		t.Fatal("scan unsupported value type succeeded")
	}
	if !slices.Equal(value, nil) {
		t.Fatalf("unsupported value changed receiver to %q", value)
	}
}
