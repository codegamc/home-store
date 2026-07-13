package storage

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestConditionsMatch(t *testing.T) {
	existing := &ObjectMeta{ETag: `"current"`}
	tests := []struct {
		name       string
		existing   *ObjectMeta
		conditions Conditions
		want       bool
	}{
		{name: "none", existing: existing, want: true},
		{name: "create absent", conditions: Conditions{IfNoneMatch: "*"}, want: true},
		{name: "create existing", existing: existing, conditions: Conditions{IfNoneMatch: "*"}},
		{name: "matching CAS", existing: existing, conditions: Conditions{IfMatch: `"current"`}, want: true},
		{name: "matching CAS list", existing: existing, conditions: Conditions{IfMatch: `"old", "current"`}, want: true},
		{name: "stale CAS", existing: existing, conditions: Conditions{IfMatch: `"old"`}},
		{name: "CAS missing", conditions: Conditions{IfMatch: `"current"`}},
		{name: "wildcard match existing", existing: existing, conditions: Conditions{IfMatch: "*"}, want: true},
		{name: "if-none-match different", existing: existing, conditions: Conditions{IfNoneMatch: `"other"`}, want: true},
		{name: "if-none-match list contains current", existing: existing, conditions: Conditions{IfNoneMatch: `"other", "current"`}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ConditionsMatch(test.existing, test.conditions); got != test.want {
				t.Fatalf("expected %v, got %v", test.want, got)
			}
		})
	}
}

func TestObjectKeyValidation(t *testing.T) {
	invalidUTF8 := string([]byte{0xff, 0xfe})
	tests := []struct {
		key  string
		want bool
	}{
		{key: ""},
		{key: "normal", want: true},
		{key: "../still-an-object", want: true},
		{key: "雪/☃", want: true},
		{key: strings.Repeat("a", 1024), want: true},
		{key: strings.Repeat("a", 1025)},
		{key: strings.Repeat("雪", 342)},
		{key: strings.Repeat("雪", 341), want: true},
		{key: invalidUTF8},
	}
	for _, test := range tests {
		if got := IsValidObjectKey(test.key); got != test.want {
			t.Fatalf("key with %d bytes: expected %v, got %v", len(test.key), test.want, got)
		}
	}
}

func FuzzObjectKeyValidation(f *testing.F) {
	for _, seed := range []string{"", "key", "../x", "雪", strings.Repeat("a", 1024), string([]byte{0xff})} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		got := IsValidObjectKey(key)
		want := key != "" && len(key) <= 1024 && utf8.ValidString(key)
		if got != want {
			t.Fatalf("validation mismatch for %q", key)
		}
	})
}
