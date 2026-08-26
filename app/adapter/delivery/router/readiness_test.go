package router

import "testing"

func TestSameSet(t *testing.T) {
	cases := []struct {
		name string
		a    []string
		b    map[string]struct{}
		want bool
	}{
		{"equal order-different", []string{"a", "b"}, map[string]struct{}{"b": {}, "a": {}}, true},
		{"duplicates ignored on a", []string{"a", "a", "b"}, map[string]struct{}{"a": {}, "b": {}}, true},
		{"missing id in b", []string{"a", "b"}, map[string]struct{}{"a": {}}, false},
		{"extra id in b", []string{"a"}, map[string]struct{}{"a": {}, "b": {}}, false},
		{"both empty", nil, map[string]struct{}{}, true},
	}
	for _, tc := range cases {
		if got := sameSet(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: sameSet = %v, want %v", tc.name, got, tc.want)
		}
	}
}
